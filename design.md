# Mode Switching: Design Specification

Status: ready for implementation
Audience: implementing agent (Claude Code) and reviewers

## 1. Summary

The cluster runs in one of a small set of **modes** (currently five). A Go
service, the **mode manager**, decides the current mode and already broadcasts
it to applications over gRPC. This project adds:

1. A cluster-scoped custom resource, `ClusterMode`, as the Kubernetes-native
   record of the current mode.
2. Persistence in the mode manager: the mode survives mode-manager restarts but
   resets to `standby` after a full cluster reboot.
3. A separate controller, **modeswitch-controller**, that scales opted-in
   Deployments and StatefulSets to a per-mode replica count declared in
   annotations, with an optional per-workload delay before scaling to zero.
4. Coexistence with ArgoCD, so GitOps does not fight the controller over
   `spec.replicas`.

The mode manager and controller live in **one Go module and repo** but run as
**two binaries, two Deployments, two ServiceAccounts**. They share the API
types and the canonical mode list.

## 2. Decisions to confirm before starting

These are placeholders. Ask the user if they aren't already set in the repo.

| Item | Placeholder used in this doc |
|---|---|
| API group | `modeswitch.example.io` |
| Mode names | `normal`, `peak`, `offpeak`, `maintenance`, `standby` |
| Default/reset mode | `standby` |
| Go module path | `github.com/example/modeswitch` |
| Mode manager HA (replica count) | assume >1 replica possible; see §5.4 |
| Where the existing mode manager code lives | assume same repo; else vendor `pkg/modestore` into it |

## 3. Goals and non-goals

### Goals
- A workload opts in with one label and declares its replicas per mode with one annotation.
- Mode changes are reflected in workload replicas within a few seconds.
- The mode survives mode-manager restarts, rolling node reboots, and controller restarts.
- The mode resets to `standby` when every node has rebooted while the mode manager was down.
- ArgoCD-managed workloads stay `Synced` while the controller changes their replicas.
- Least privilege: the mode manager cannot scale workloads, and the controller cannot change the mode.

### Non-goals (v1)
- Load-based autoscaling within a mode (HPA/KEDA integration).
- Ordered transitions, such as scaling up new workloads before scaling down old ones.
- Delays on non-zero scale-downs, such as 6 to 3. Only scale-to-zero is delayed.
- Kinds other than Deployment and StatefulSet.
- Changing the mode manager's existing gRPC API or its mode-decision logic.

## 4. Architecture

```
            ┌────────────────────┐   gRPC (unchanged)   ┌──────────────┐
            │    mode manager    │ ───────────────────▶ │ other apps   │
            │  (existing, Go)    │                      └──────────────┘
            └─────────┬──────────┘
      writes spec.mode│ + status.bootFingerprint
                      ▼
            ┌────────────────────┐
            │ ClusterMode/current│   (cluster-scoped singleton CR)
            └─────────┬──────────┘
                      │ watch (read-only)
                      ▼
            ┌────────────────────┐   SSA spec.replicas   ┌──────────────────────┐
            │ modeswitch-        │ ────────────────────▶ │ labelled Deployments │
            │ controller         │                       │ / StatefulSets       │
            └────────────────────┘                       └──────────────────────┘
```

- The mode manager is the **sole writer** of `ClusterMode/current`.
- The controller is a **read-only consumer** of `ClusterMode` and the sole writer
  of `spec.replicas` on opted-in workloads.
- There is no gRPC between the mode manager and the controller.

## 5. API: `ClusterMode`

### 5.1 Schema

```yaml
apiVersion: modeswitch.example.io/v1alpha1
kind: ClusterMode
metadata:
  name: current            # singleton; enforced by CEL
spec:
  mode: peak               # enum generated from the canonical mode list
status:
  bootFingerprint:         # every node's bootID at the last refresh
    - node: node-a
      bootID: 3f0c...
    - node: node-b
      bootID: 91ad...
  lastModeChange: "2026-10-15T09:12:44Z"
  restoredOnStartup: true  # false if the last startup reset to standby
  observedGeneration: 4
```

### 5.2 Requirements
- The resource is cluster-scoped and uses the status subresource.
- `spec.mode` is a required string with a kubebuilder `Enum` marker generated
  from or kept in sync with `api/v1alpha1/modes.go`. Add a unit test asserting
  the enum marker matches `modes.All`.
- CEL validation at the root enforces `self.metadata.name == 'current'`.
- Printer columns are `Mode` (from `.spec.mode`) and `Age`.
- Types, deepcopy and CRD manifests are generated with `controller-gen`.

### 5.3 Canonical mode list (`api/v1alpha1/modes.go`)

```go
const (
    ModeNormal      = "normal"
    ModePeak        = "peak"
    ModeOffpeak     = "offpeak"
    ModeMaintenance = "maintenance"
    ModeStandby     = "standby"
    DefaultMode     = ModeStandby
)
var All = []string{ModeNormal, ModePeak, ModeOffpeak, ModeMaintenance, ModeStandby}
func IsValid(m string) bool
```

Both binaries import this package. The controller has **no `--modes` flag**.

### 5.4 Writers and field managers

| Writer | Fields | SSA field manager |
|---|---|---|
| mode manager | `spec.mode`, all of `status` | `mode-manager` |
| controller | none (read-only) | n/a |

If the mode manager runs more than one replica, only the leader writes
`ClusterMode`. Use a `coordination.k8s.io` Lease. If the existing mode manager
already has leader election, reuse it.

`ClusterMode/current` **must not be managed by ArgoCD**. The CRD may be in Git;
the instance must not be, or Argo would revert mode changes.

## 6. Mode manager changes (`pkg/modestore`)

The existing mode manager's decision logic and gRPC server are unchanged.
Integration is through a small library so the change to the existing code is a
few calls.

### 6.1 Public API

```go
package modestore

type Store struct { /* client, name, field manager, logger, clock */ }

func New(cfg *rest.Config, opts Options) (*Store, error)

// Startup returns the mode to begin in and whether it was restored.
// Call once, before serving gRPC. Writes the CR (spec + fresh fingerprint).
func (s *Store) Startup(ctx context.Context) (mode string, restored bool, err error)

// SetMode persists a mode change. Call it on every mode change, BEFORE
// broadcasting over gRPC; if it fails, do not broadcast (return the error).
func (s *Store) SetMode(ctx context.Context, mode string) error

// Run watches Nodes and refreshes status.bootFingerprint whenever any node's
// bootID, or the node set, changes. Blocks until ctx is done. Leader-only.
func (s *Store) Run(ctx context.Context) error
```

### 6.2 Startup algorithm

1. List Nodes and build the set of live `status.nodeInfo.bootID` values.
2. Get `ClusterMode/current`.
   - If it is not found, the mode is `DefaultMode` and `restored` is false.
   - If any `status.bootFingerprint[].bootID` is in the live set, the mode is
     `spec.mode` (provided it is valid) and `restored` is true. At least one
     node stayed up, so this was not a full cluster reboot.
   - If none match, every node has rebooted, so the mode is `DefaultMode` and
     `restored` is false.
   - If the stored `spec.mode` is invalid (for example, a mode was removed),
     the mode is `DefaultMode`. Log a warning.
3. Server-side apply `spec.mode` (manager `mode-manager`), then update status
   with the fresh fingerprint, `restoredOnStartup`, and `lastModeChange` if the
   mode changed.
4. Return the mode. The mode manager starts serving gRPC with it.

### 6.3 Fingerprint refresh (`Run`)

- Run an informer on Nodes, using a metadata-only or field-trimmed transform
  if available to save memory.
- On a Node add, delete, or `bootID` change, debounce by about 2s, then
  rewrite `status.bootFingerprint` to the current set, sorted by node name.
- Rolling reboots therefore never trigger a reset: each returning node's new
  bootID is recorded while the manager runs.

### 6.4 Integration into the existing mode manager

```go
store, _ := modestore.New(cfg, modestore.Options{FieldManager: "mode-manager"})
mode, restored, err := store.Startup(ctx)   // before gRPC serve
// seed existing mode logic with `mode`
go store.Run(ctx)                           // under leader election if HA
// on each mode change, in existing code path:
if err := store.SetMode(ctx, newMode); err != nil { /* do not broadcast */ }
```

### 6.5 RBAC (mode manager ServiceAccount)

- `clustermodes`, `clustermodes/status`: get, list, watch, create, update, patch
- `nodes`: get, list, watch
- `leases` (own namespace): get, list, watch, create, update, patch, delete
  (only if leader election is new)

## 7. Controller (`cmd/modeswitch-controller`)

### 7.1 Workload contract

```yaml
metadata:
  labels:
    modeswitch.example.io/enabled: "true"
  annotations:
    modeswitch.example.io/replicas: "normal=3,peak=6,offpeak=1"
    modeswitch.example.io/scale-to-zero-delay: "5m"   # optional
spec:
  replicas: 0   # creation-time value only (see §8)
```

These annotations go on the **workload's metadata**, not the pod template.

| Key | Owner | Meaning |
|---|---|---|
| label `.../enabled` | user | opt-in; also the informer cache filter |
| `.../replicas` | user | `mode=N` entries, comma-separated; `*=N` sets the default for unlisted modes; an unlisted mode with no `*` means 0 |
| `.../scale-to-zero-delay` | user | Go duration; absent or `0` means scale to zero immediately |
| `.../applied-mode` | controller | the last mode fully applied |
| `.../zero-pending-since` | controller | RFC 3339 start time of a pending scale-to-zero |

### 7.2 Parsing rules (`internal/modespec`)
- Whitespace around entries, keys, and values is allowed. Empty entries are ignored.
- Replicas must be integers ≥ 0 that fit in int32.
- A mode name must pass `v1alpha1.IsValid`. Duplicates (including a duplicate
  `*`) are an error.
- An annotation with zero entries is an error.
- Any error emits a Warning event `InvalidAnnotation` and leaves replicas
  untouched. Do not requeue; the next edit or mode change retriggers.
- A missing `replicas` annotation on a labelled workload emits a Warning event
  `MissingAnnotation` and leaves replicas untouched.

### 7.3 Reconcile algorithm

Inputs: `mode` (from the `ClusterMode` informer) and the workload from the cache.

1. If `ClusterMode/current` doesn't exist or has an invalid mode, do nothing.
2. If the workload is not found or is being deleted, do nothing.
3. Parse the annotation; on error, act as in §7.2.
4. Compute `desired = spec.ReplicasFor(mode)` and `cur = spec.replicas` (treat nil as 1).
5. Start from `target = desired`, `appliedMode = mode`, `pending = ""`, `requeue = 0`.
6. If `desired == 0`, `cur > 0`, and the delay is greater than 0:
   - Set `since` to the parsed `zero-pending-since`. Use now instead if it is
     missing, malformed, or in the future.
   - If `remaining = delay - (now - since)` is greater than 0, then hold:
     `target = cur`, `appliedMode` = the existing `applied-mode` annotation,
     `pending = since`, `requeue = remaining`.
7. If `cur`, the `applied-mode` annotation and `zero-pending-since` all already
   equal the computed values, return `RequeueAfter: requeue`.
8. Otherwise server-side apply (manager `modeswitch-controller`,
   `Force: true`) `spec.replicas = target` plus the controller annotations that
   are non-empty. Omitting `zero-pending-since` makes SSA remove it, which is
   how a pending scale-down is cancelled.
9. Emit Normal events: `ScaleToZeroPending`, `ScaleToZeroCancelled`, or
   `ModeApplied`, as appropriate.
10. Return `RequeueAfter: requeue`.

These properties follow from the algorithm:
- Moving between two zero-replica modes does not restart the delay timer.
- Moving to a mode with replicas above 0 cancels a pending scale-down.
- The pending start time is stored on the object, so the countdown survives
  controller restarts and failover.

### 7.4 Watches and triggers
- Cache the Deployment and StatefulSet informers with a label selector on
  `.../enabled=true`, using `cache.Options.ByObject`.
- Use one controller per kind. Each watches its kind with a predicate of
  `GenerationChanged OR AnnotationChanged`.
- Watch `ClusterMode` in each controller. On a change to `spec.mode`, map it
  to requests for every cached workload of that kind. Use `Watches` with
  `handler.EnqueueRequestsFromMapFunc` and a predicate that fires only when
  `spec.mode` changes.
- Set `MaxConcurrentReconciles` to 4.
- Drift (for example, a manual `kubectl scale`) is corrected on the resulting
  watch event.

### 7.5 Operational
- Use leader election with a Lease in the controller namespace, and run 2 replicas.
- Flags: metrics address, probe address, leader election. No mode flags.
- Metrics:
  - `modeswitch_current_mode{mode}` gauge
  - `modeswitch_mode_changes_total`
  - `modeswitch_reconcile_errors_total{kind}`
  - `modeswitch_scale_to_zero_pending` gauge (count of workloads)
- Logs use structured fields: `mode`, `kind`, `namespace`, `name`, `from`, `to`.
- The container runs distroless and nonroot, with a read-only root
  filesystem, all capabilities dropped, and the RuntimeDefault seccomp profile.

### 7.6 RBAC (controller ServiceAccount)
- `clustermodes`: get, list, watch
- `deployments`, `statefulsets`: get, list, watch, patch
- `events` (core): create, patch
- `leases` (own namespace): get, list, watch, create, update, patch, delete

## 8. ArgoCD coexistence

1. Merge this into `argocd-cm`:
   ```yaml
   resource.customizations.ignoreDifferences.apps_Deployment: |
     managedFieldsManagers: [modeswitch-controller]
   resource.customizations.ignoreDifferences.apps_StatefulSet: |
     managedFieldsManagers: [modeswitch-controller]
   ```
2. Every Application containing switched workloads sets
   `syncOptions: [RespectIgnoreDifferences=true, ServerSideApply=true]`.
3. Switched workloads set `replicas: 0` in Git. Argo uses this value only when
   it creates the workload and the live value after that.
4. Never put a global `jsonPointers: [/spec/replicas]` ignore in place. It
   would freeze replicas for non-switched workloads too.
5. Fallback, to be documented and not built: if the installed Argo version
   doesn't honour `managedFieldsManagers` during sync, use per-Application
   `ignoreDifferences` scoped by `name` with `jsonPointers`.

## 9. Repository layout

```
api/v1alpha1/
  groupversion_info.go
  clustermode_types.go
  modes.go
  zz_generated.deepcopy.go        (generated)
cmd/
  modeswitch-controller/main.go
  mode-manager/                   (existing; minimal integration edits only)
internal/
  controller/                     (reconciler, constants, metrics)
  modespec/                       (annotation parser + tests)
pkg/
  modestore/                      (mode manager persistence library + tests)
config/
  crd/                            (generated)
  rbac/controller/
  rbac/mode-manager/
  deploy/controller.yaml
  argocd/argocd-cm-patch.yaml
  examples/app-deployment.yaml
  examples/argocd-application.yaml
Makefile                          (generate, manifests, build, test, docker)
Dockerfile.controller
README.md
```

A prior prototype (`modeswitch.zip`, using a gRPC subscription instead of the
CR) may be supplied as a reference for the reconciler, parser and manifests.
This document is authoritative where the two differ. In particular,
`internal/modesource` and the `--modes` and `--mode-server*` flags must not be
carried over.

## 10. Tech stack

- Go 1.23 or newer
- controller-runtime and the matching k8s.io libraries, pinned to the latest
  mutually compatible stable versions
- controller-gen for CRDs, deepcopy and RBAC markers
- envtest for integration tests
- No new dependencies beyond these and `prometheus/client_golang`.

## 11. Testing

### Unit tests
- `modespec`: table tests covering every rule in §7.2.
- `modes`: the CRD enum matches `modes.All`.
- `modestore.Startup` with a fake clientset:
  - no CR → standby, not restored
  - one bootID overlaps → restores the stored mode
  - zero overlap → standby
  - stored mode invalid → standby
  - empty fingerprint → standby
- `modestore.Run`: a bootID change rewrites the sorted fingerprint, and the
  debounce coalesces bursts.
- Reconciler decision logic: extract it as a pure function
  `decide(cur, desired, ann, delay, now) → (target, annotations, requeue)` and
  table-test it, covering immediate scaling, pending, still pending, expired,
  cancelled, zero-to-zero mode moves, a future `since`, and malformed `since`.

### Integration tests (envtest)
- Install the CRD, start the controller, and create labelled and unlabelled
  Deployments and StatefulSets.
- Create `ClusterMode` and assert replicas on labelled workloads only.
- Change the mode and assert the new replicas.
- Assert that `managedFields` shows `modeswitch-controller` owning `f:replicas`.
- Delayed zero: use a delay of about 2s, assert replicas are held, then 0.
  Change the mode back mid-delay and assert the scale-down is cancelled and
  the pending annotation removed.
- Invalid annotation: assert a Warning event and unchanged replicas.
- Delete `ClusterMode`: assert no changes (fail-static).
- Manual `kubectl scale` drift is reverted.

### Manual checklist (README)
- After an Argo sync, the Application is `Synced` with controller-chosen replicas.
- `kubectl get clustermode` shows the mode.
- Restarting the mode-manager pod restores the mode.
- Rebooting all nodes (or simulating it in a test by rewriting the
  fingerprint to unknown bootIDs) results in standby.

## 12. Acceptance criteria

- [ ] `make generate manifests build test` passes cleanly; `go vet` is clean.
- [ ] Every unit and envtest case in §11 passes.
- [ ] The two binaries have disjoint write permissions, as in §6.5 and §7.6.
- [ ] Nothing in the controller references gRPC or a mode list other than `api/v1alpha1`.
- [ ] The mode manager's existing gRPC API and tests are unchanged and still pass.
- [ ] The README documents the workload contract, ArgoCD setup, metrics, and reset semantics.
- [ ] Example manifests apply cleanly with `kubectl apply --dry-run=server`.

## 13. Open questions (ask; don't guess)

1. What are the final mode names and API group?
2. Does the mode manager currently run more than one replica, and does it
   already have leader election?
3. Should the mode manager adopt manual edits to `ClusterMode` (an operator
   override), or revert them? v1 default: it is the sole writer, and RBAC
   prevents others from writing.
4. Should a long mode-manager outage (with no reboot) also reset to standby?
   v1 default: no.
