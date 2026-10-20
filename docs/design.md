# Mode Switching: Design Specification

Status: ready for implementation once the decisions in §2 are confirmed
Audience: implementing agent (Claude Code) and reviewers
Revision: v2.1, 2026-10-20. Replaces v2 (2026-10-17) and v1.

## Changes in v2.1

- **mode-operator ships a Go client library**, the module
  `github.com/bitasylum/mode-operator/client` in `client/` (§6.5). It
  implements the contract in §6.1 and the persistence design in §6.3, in
  three packages the mode-manager team can take as much of as they want. The
  mode manager may import that module and nothing else from mode-operator;
  the CRDs are still the contract.
- **An orchestration server also writes the mode** (§5.4). It powers physical
  servers on and off through out-of-band management, writes `spec.mode` only,
  with a field manager of its own, and never creates the ClusterMode. The mode
  manager adopts its changes as it does a maintainer's.
- **Clients take the rules from the ClusterMode's status** (§6.1, rule 3):
  `status.modes`, `status.defaultMode` and `status.transitions`, which are
  what the API server enforces. The definition is read only until the
  controller has written them.
- **Rule 9 is specified by resourceVersion**, so a stale event is never taken
  for someone else's change, and changes are reported in the order they
  happened.
- **§6.3 is built**, with these changes: creating the ClusterMode emits
  `ModeReset`; a missing definition starts in the ClusterMode's last copy of
  the default mode; any read error other than NotFound stops startup; and the
  fingerprint keeper rewrites the whole record.
- **The mode manager's RBAC is trimmed** (§6.4): every verb on ClusterModes,
  `create` included, is limited to its own ClusterMode, because server-side
  apply creates by name; Nodes `list` and `watch`; and `create`, `get` and
  `patch` on the fingerprint ConfigMap only.
- **The stub uses the library** (§9.5), including §6.3, and its end-to-end
  tests run it with mode-operator on a two-node kind cluster.

## Changes from v1

- **`ClusterMode` is owned by mode-operator.** The controller installs
  the CRDs, reconciles every ClusterMode, and is the only writer of `status`.
  The mode manager writes only `spec.mode`.
- **Allowed modes live in a ClusterModeDefinition**, an admin-owned object
  kept in Git that each ClusterMode refers to. Neither the mode manager nor
  the controller can change it, and there is no compiled-in mode list.
- **More than one ClusterMode may exist.** A workload names the ClusterMode it
  follows with the label `mode.bitasylum.org/clustermode`, which replaces
  `modeswitch.example.io/enabled`. v1 still runs one mode manager and one
  ClusterMode.
- **Status reports progress.** For each ClusterMode it shows the replica count
  the controller has applied for the current mode and how far the pods have
  got: workload counts by state, replica sums, a list of unsettled workloads,
  and conditions.
- **The boot fingerprint moved** from `ClusterMode.status` to a ConfigMap in
  the mode manager's namespace (recommended design, §6.3).
- **The controller is a Kubebuilder project, installed with a Helm chart.**
  Nothing in the design blocks OLM later (§9.4).
- **The CRDs are the only interface with the mode manager**, which is developed
  in another repo. This project ships none of the mode manager's code. §6 is
  a contract the mode manager must meet, with a recommended design for its persistence.
- **Maintainers may edit the mode by hand.** The controller applies a manual
  edit like any other change, status shows that the mode was set by hand,
  and the mode manager adopts the edit.
- **KubeVirt VirtualMachines and VirtualMachinePools are supported**, and the
  controller runs normally on clusters without KubeVirt (§7.9).
- **Gaps found in the v1 review are now specified:** the Node informer
  transform, empty bootIDs, the order of startup writes, HA, write failures,
  delay parsing, offboarding, and the CRD lifecycle.

## 1. Summary

The cluster runs in one of a small set of **modes** (currently five). A Go
service, the **mode manager**, decides the current mode and already broadcasts
it to applications over gRPC. This project adds:

1. Two cluster-scoped custom resources, owned by a new controller,
   **mode-operator**. Admins write a `ClusterModeDefinition` listing the
   allowed modes. The mode manager writes the current mode into a
   `ClusterMode`'s `spec`, and the controller reports progress in its
   `status`.
2. Scaling of opted-in Deployments, StatefulSets, and KubeVirt
   VirtualMachines and VirtualMachinePools. Each names the ClusterMode it
   follows, and the controller scales it to the replica count it declares for
   that ClusterMode's mode, with an optional delay before scaling to zero.
   KubeVirt is optional: without it, the controller runs as usual (§7.9).
3. A contract for the mode manager (§6): it writes the mode to its
   ClusterMode, keeps it across its own restarts, and resets it to the
   definition's default mode (`standby` in v1) after a full cluster reboot.
4. Coexistence with ArgoCD, so GitOps does not fight the controller over
   `spec.replicas`.

v1 runs one mode manager writing one ClusterMode named `default`. The API,
labels, RBAC and metrics are designed so that more mode managers, each with its
own ClusterMode (for example per namespace or per group of applications), can
be added later without breaking changes (§13).

The `mode-operator` repo holds the controller, the CRDs and a Go client
library for them (§6.5). The mode manager is developed in another repo, and
the ClusterMode CRD is the only interface between the two. mode-operator
imports nothing from the mode manager; the mode manager may import the client
library, which depends only on client-go. They run as separate Deployments
with separate ServiceAccounts. A future orchestration server, which powers
physical servers on and off as the mode changes, uses the same library.

## 2. Decisions to confirm before starting

These are placeholders. Ask the user if they aren't already set in the repo.

| Item | Placeholder used in this doc |
|---|---|
| Mode names | `normal`, `peak`, `offpeak`, `maintenance`, `standby`, as examples; the real list is in the ClusterModeDefinition (§5.3) |
| Default mode | `standby`, set as the definition's `spec.defaultMode` |
| Name of the first ClusterModeDefinition | `standard` |
| Name of the first ClusterMode | `default` |

Confirmed with the user:
- Names. The component is `mode-operator`: its Deployment, Helm chart, image,
  field manager (which ArgoCD's ignore rules key on) and metric prefix
  `mode_operator_`. The API group is `mode.bitasylum.org`, which prefixes the
  label and annotations on every workload. The operator has its own repo,
  `github.com/bitasylum/mode-operator`, which is also its Go module path.
  Change any of these only before the first deploy.
- Repos: `ship-core-mode-controller` is the parent, holding the devcontainer
  and the design docs. `mode-operator` and `mode-manager-stub` (§9.5) are
  submodules of it, each with its own history, CI and releases.
- The mode manager is developed in another repo. The ClusterMode CRD is its
  only interface with this project.
- The mode manager and the orchestration server are both Go. mode-operator
  ships a client library for them, in `client/` as its own module (§6.5).
- The orchestration server writes `spec.mode` too, with its own field
  manager, and the mode manager adopts its changes (§5.4).
- The allowed modes are defined in a ClusterModeDefinition that only admins
  can change. Neither the mode manager nor mode-operator can write it.
- The mode manager runs a single replica with no leader election. It may
  become HA later.
- No applications use the label or annotations yet; they will be updated after
  the controller is built. v1 needs no migration from the prototype.
- The mode manager keeps working without the CRD. It then can't persist the
  mode, so a restart starts it in `standby`. That is acceptable.
- A maintainer may edit a ClusterMode's `spec.mode` by hand. The mode manager
  adopts the edit and broadcasts it over gRPC; that work is in its repo.
- Installation is with standard Helm charts. Nothing in the design may block
  OLM support later.
- KubeVirt support is in v1, and the controller must not fail when the
  KubeVirt CRDs aren't installed.
- Single VirtualMachines are the main KubeVirt use. VirtualMachinePools are
  supported too, because they cost little extra (§7.9).
- A VM that the mode turns on uses `runStrategy: Always`.
- Clusters run the latest KubeVirt release. Pools aren't used yet, so the
  controller uses whichever pool API version the cluster prefers.
- The settle timeout defaults to 10 minutes. It can be changed cluster-wide
  with `--settle-timeout` (a Helm value) and per workload with the
  `settle-timeout` annotation.

## 3. Goals and non-goals

### Goals
- A workload opts in with one label, which also names the ClusterMode it
  follows, and declares its replicas per mode with one annotation.
- Mode changes are reflected in workload replicas within a few seconds.
- Admins define the allowed modes in a ClusterModeDefinition, reviewed
  through Git. Neither the mode manager nor the controller can change them,
  and adding a mode needs no controller release.
- KubeVirt VirtualMachines and VirtualMachinePools follow the mode the same
  way, and the controller runs normally on clusters without KubeVirt.
- `kubectl get clustermode` shows, for each ClusterMode, how much of the
  current mode has been applied and how many pods are ready.
- The mode survives mode-manager restarts, rolling node reboots, and controller
  restarts.
- The mode resets to `standby` when every node has rebooted while the mode
  manager was down.
- ArgoCD-managed workloads stay `Synced` while the controller changes their
  replicas.
- Least privilege: the mode manager cannot scale workloads or write ClusterMode
  status, the controller cannot change the mode, and neither can change the
  allowed modes.
- A maintainer can override the mode by editing the ClusterMode, and status
  shows that the mode was set by hand.
- Adding a second mode manager with its own ClusterMode later needs no
  controller change and no breaking API change.

### Non-goals (v1)
- Load-based autoscaling within a mode (HPA/KEDA integration). Workloads
  targeted by an HPA must not opt in.
- Ordered transitions, such as scaling up new workloads before scaling down old
  ones.
- Delays on non-zero scale-downs, such as 6 to 3. Only scale-to-zero is
  delayed; the rest can be added when an application needs it (§13).
- Kinds other than Deployment, StatefulSet, VirtualMachine and
  VirtualMachinePool (§13).
- Changing the mode manager's gRPC API. The mode-manager changes that the
  contract needs (§6) are made in its own repo.
- Tracking image rollouts in status. Status tracks replica counts and pods
  still to stop (§7.4).
- Running more than one mode manager. The design allows it (§13); v1 doesn't
  build or test it.
- An OLM bundle. v1 ships a Helm chart, and nothing in the design blocks OLM
  later (§9.4).
- The mode manager itself. This project specifies what the mode manager must
  do (§6), and ships a client library that implements that contract (§6.5),
  but none of the mode manager's own logic. The stub in §9.5 is a test tool,
  not a mode manager.
- Maintainer overrides that outlast the mode manager's next mode change
  (§13).

## 4. Architecture

```
                        ┌──────────────────────┐   gRPC (unchanged)   ┌──────────────┐
                        │     mode manager     │ ───────────────────▶ │  other apps  │
                        │   (separate repo)    │                      └──────────────┘
                        └──┬────────────────┬──┘
                      read ▲                │ SSA spec.mode
                           │                ▼
   ┌─────────────────────────────┐   ┌──────────────────────────┐
   │ ClusterModeDefinition       │   │ ClusterMode/default      │
   │ allowed modes, default mode │   │  spec   ← mode manager   │
   │ written by admins, via Git  │   │  status ← mode-operator  │
   └──────────────┬──────────────┘   └────────────┬─────────────┘
             read │                    watch spec │ ▲ patch status
                  ▼                               ▼ │
   ┌──────────────────────────────────────────────────────────────┐
   │ mode-operator (Kubebuilder operator)                         │
   └──────────────────────────────┬───────────────────────────────┘
             SSA spec.replicas or │ ▲ watch, including status
                 spec.runStrategy ▼ │
   ┌──────────────────────────────────────────────────────────────┐
   │ Deployments, StatefulSets, KubeVirt VMs and VM pools,        │
   │ labelled clustermode=default                                 │
   └──────────────────────────────────────────────────────────────┘
```

- Admins write ClusterModeDefinitions, through Git. The mode manager and the
  controller only read them, and the mode manager reads one only until the
  controller has copied its rules into the ClusterMode's status (§6.1,
  rule 3).
- The mode manager writes its ClusterMode's `spec`, and maintainers and the
  orchestration server may change `spec.mode` (§5.4). The mode manager's
  fingerprint ConfigMap is internal to it (§6.3), so it isn't shown, and
  neither is the orchestration server.
- The controller owns both CRDs. It is the only writer of every ClusterMode's
  `status` and of `spec.replicas` on opted-in workloads. It never writes
  ClusterMode `spec`.
- The controller doesn't know mode managers exist; it treats every ClusterMode
  the same way.
- There is no gRPC between the mode manager and the controller.
- For a VirtualMachine, the controller writes `spec.runStrategy` instead of
  `spec.replicas` (§7.9).

The controller runs one reconciler per workload kind, plus the status
reconciler. The KubeVirt reconcilers exist only when KubeVirt is installed
(§7.9).

| Reconciler | Watches | Writes |
|---|---|---|
| `mode-operator-deployment` | labelled Deployments, ClusterModes | Deployment `spec.replicas` and controller annotations |
| `mode-operator-statefulset` | labelled StatefulSets, ClusterModes | StatefulSet `spec.replicas` and controller annotations |
| `mode-operator-virtualmachine` | labelled VirtualMachines, ClusterModes | VirtualMachine `spec.runStrategy` and controller annotations |
| `mode-operator-virtualmachinepool` | labelled VirtualMachinePools, ClusterModes | VirtualMachinePool `spec.replicas` and controller annotations |
| `mode-operator-clustermode` | ClusterModes, and labelled workloads of every kind (including their status) | ClusterMode `status` |

Every reconciler also watches ClusterModeDefinitions, so a change to the
allowed modes re-checks the ClusterModes and workloads that use them (§7.5).

## 5. API: `ClusterMode` and `ClusterModeDefinition`

### 5.1 Schema

```yaml
apiVersion: mode.bitasylum.org/v1alpha1
kind: ClusterMode
metadata:
  name: default                          # any name of 63 characters or fewer
  labels:
    mode.bitasylum.org/writer: mode-manager.ops   # who writes spec (§5.4)
spec:
  definition: standard                   # ClusterModeDefinition (§5.3); can't be changed
  mode: peak                             # must be one of the definition's modes
status:                                  # written by mode-operator only
  observedGeneration: 7
  modes: [normal, peak, offpeak, maintenance, standby]   # copy of the definition's modes (§5.2)
  defaultMode: standby                   # copy of the definition's defaultMode (§5.2)
  transitions:                           # copy of the definition's transitions, when it has them (§5.2)
    - from: peak
      to: [normal]
  mode: peak                             # the mode this status describes
  modeChangedAt: "2026-10-17T09:12:44Z"  # when the controller first saw this mode
  modeSetBy: mode-manager                # field manager that set spec.mode (§5.4)
  workloads:                             # every referencing workload is in one state (§7.4)
    total: 42
    settled: 36
    scaling: 2
    stalled: 1
    applying: 0
    pending: 1
    invalid: 2
  invalidAnnotations: 3                  # workloads with any annotation error, blocking or not
  replicas:                              # sums over workloads that aren't invalid
    desired: 118                         # what the current mode asks for
    applied: 120                         # spec.replicas as set by the controller
    ready: 110                           # status.readyReplicas reported by the workloads
  unsettled:                             # at most 20, most urgent first (§7.4)
    - kind: Deployment
      namespace: shop
      name: checkout
      state: Stalled
      desired: 6
      applied: 6
      ready: 4
      since: "2026-10-17T09:12:45Z"
      message: "4 of 6 pods ready"
  conditions:
    - type: Applied
      status: "False"
      reason: ScaleToZeroPending
      message: "1 of 40 workloads is waiting out its scale-to-zero delay"
      observedGeneration: 7
      lastTransitionTime: "2026-10-17T09:12:45Z"
    - type: Ready
      status: "False"
      reason: Stalled
      message: "1 workload stalled, 2 scaling"
      observedGeneration: 7
      lastTransitionTime: "2026-10-17T09:12:45Z"
    - type: Degraded
      status: "True"
      reason: InvalidAndStalledWorkloads
      message: "3 workloads have invalid annotations, 1 stalled"
      observedGeneration: 7
      lastTransitionTime: "2026-10-17T09:22:45Z"
```

### 5.2 Requirements
- The resource is cluster-scoped and uses the status subresource.
- `spec.definition` is a required string naming a ClusterModeDefinition
  (§5.3). A CEL rule, `self == oldSelf`, stops it changing once set.
- `spec.mode` is a required string of 1 to 63 characters, with the same name
  pattern as the definition's modes (§5.3).
- **The definition is enforced at write time.** CRD validation rules can only
  see the object being written, so the controller copies the definition's
  modes into `status.modes` (§7.4). A CEL rule at the root then rejects a
  change of `spec.mode` to anything outside that copy:
  `self.spec.mode == oldSelf.spec.mode || !has(self.status) || !has(self.status.modes) || self.spec.mode in self.status.modes`.
  The rule uses `oldSelf`, so it runs only on updates. On create, and before
  the controller has filled the copy, any well-formed mode is accepted, and
  the controller reports a mode outside the definition as `InvalidMode`.
  This rule is the least common part of the design, so prove it in envtest
  first (§11).
- `status.modes` has at most 32 items, matching the definition, which keeps
  the rule within the API server's cost budget.
- **Transitions (§5.3).** The controller also copies the definition's
  `transitions` and `defaultMode` into `status.transitions` and
  `status.defaultMode`. Two more root rules apply while `status.transitions`
  is set, and both let a change of `spec.mode` to `status.defaultMode`
  through:
  - the change must be listed:
    `self.status.transitions.exists(t, t.from == oldSelf.spec.mode && self.spec.mode in t.to)`
  - the mode being left must be Ready: `self.status.mode == oldSelf.spec.mode`
    and the `Ready` condition is True. `status.conditions` has at most 8
    items, for the cost budget.
- **The Ready check needs an admission policy too.** CRD rules can't read
  `metadata.generation`, so they can't tell whether the status was computed
  from the latest spec. Matching `status.mode` to the old `spec.mode` isn't
  enough: a ClusterMode moved from a Ready `standby` to `normal` and straight
  back, which the default-mode exemption allows, still shows `standby` Ready
  from before, and could move on before the controller writes status. A
  ValidatingAdmissionPolicy, `mode-operator-transitions`, shipped in
  `config/admission/` and the chart, repeats the Ready check and also
  requires `oldObject.status.observedGeneration ==
  oldObject.metadata.generation`. Its match conditions limit it to updates
  that change `spec.mode` while `status.transitions` is set. The CRD rule
  stays, so an install with only the CRDs keeps the weaker check.
  ValidatingAdmissionPolicy is GA from Kubernetes 1.30, the chart's minimum.
- A CEL rule at the root enforces `size(self.metadata.name) <= 63`, so every
  name is a valid label value. There is no singleton rule.
- All `status` fields are optional. A new object has an empty status until the
  controller reconciles it.
- `status.conditions` is a `[]metav1.Condition` with `+listType=map` and
  `+listMapKey=type`.
- Printer columns:

  | Name | JSONPath | Shown |
  |---|---|---|
  | Mode | `.spec.mode` | always |
  | Applied | `.status.conditions[?(@.type=="Applied")].reason` | always |
  | Ready | `.status.conditions[?(@.type=="Ready")].reason` | always |
  | Workloads | `.status.workloads.total` | always |
  | Settled | `.status.workloads.settled` | always |
  | Age | `.metadata.creationTimestamp` | always |
  | Desired-Replicas | `.status.replicas.desired` | `-o wide` |
  | Applied-Replicas | `.status.replicas.applied` | `-o wide` |
  | Ready-Replicas | `.status.replicas.ready` | `-o wide` |
  | Set-By | `.status.modeSetBy` | `-o wide` |
  | Definition | `.spec.definition` | `-o wide` |

  ```
  $ kubectl get clustermode
  NAME      MODE      APPLIED              READY                WORKLOADS   SETTLED   AGE
  default   standby   ScaleToZeroPending   ScaleToZeroPending   42          36        12d
  ```

  The Applied and Ready columns show each condition's reason rather than
  True or False, so a pending scale to zero reads `ScaleToZeroPending`
  rather than looking like a failure. The conditions themselves stay True or
  False, as Kubernetes requires, for tools such as `kubectl wait`.
- Types, deepcopy and CRD manifests are generated by controller-gen from
  Kubebuilder markers.

### 5.3 ClusterModeDefinition

A cluster-scoped ClusterModeDefinition lists the modes that the ClusterModes
using it may be in, and the mode to start in when there is no mode to restore. Admins write it, normally
through Git and ArgoCD. The mode manager, mode-operator and maintainers can
only read it (§5.6, §6.4, §7.7). There is no compiled-in mode list: the
controller has **no `--modes` flag** and no mode constants, and the names in
this document are examples.

```yaml
apiVersion: mode.bitasylum.org/v1alpha1
kind: ClusterModeDefinition
metadata:
  name: standard
spec:
  modes: [normal, peak, offpeak, maintenance, standby]
  defaultMode: standby   # where the mode manager starts when there is no mode to restore
```

- `spec.modes` is a required list of 1 to 32 mode names, with
  `+listType=set` so the names are unique. Each name is 1 to 63 characters and
  matches `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`, which also rules out `*`, `=` and
  `,`, the characters the `replicas` annotation uses. The size limit keeps CEL
  rules within the API server's cost budget.
- `spec.defaultMode` is required, and a CEL rule enforces
  `self.defaultMode in self.modes`. The mode manager starts in it whenever
  there is no mode to restore: when it first creates the ClusterMode, after a
  full cluster reboot, and when the stored mode is no longer allowed (§6.1,
  rule 6, and §6.3).
- **`spec.transitions` (optional)** makes the modes a state machine: a list
  of rules, one per `from` mode, each with the modes it may move `to`. With
  it set, a ClusterMode may move from one mode to another only if the rule
  for the mode it is leaving lists the new one, and only once the mode it is
  leaving is Ready (§5.2). A mode with no rule can only be left for
  `defaultMode`. Moving to `defaultMode` is always allowed, whatever the
  current mode and whether or not it is Ready, so the mode manager's reset
  after a full cluster reboot (§6.1, rule 6) always works, and a maintainer
  always has a way out of a stuck mode. Without `transitions`, any mode may
  follow any other, at any time. A CEL rule checks that every mode named is in
  `spec.modes`; the list is a map keyed by `from`, so each mode has one rule.
  Another requires a rule for `defaultMode`, since a ClusterMode that resets
  to it could otherwise never leave it, and the list has at least one rule,
  since an empty list would otherwise mean no restrictions at all. The mode
  manager plans paths of several steps itself.
- The definition has no status in v1, and the controller never writes it.
- Several ClusterModes can use one definition, for example when a second mode
  manager handles another group of applications (§13).
- Deleting a definition that ClusterModes still use is allowed. They then
  report `DefinitionNotFound`, and their workloads are left as they are
  (§7.4).

Changing the list:
- **Adding a mode:** add it to the definition first, then start using it in
  the mode manager and in workload annotations. No CRD or controller release
  is needed.
- **Removing a mode:** stop using it in the mode manager and remove it from
  workload annotations first, then from the definition. If a ClusterMode is
  in that mode when it disappears, the controller leaves its workloads as they
  are and reports `InvalidMode` until the mode changes.
- **Strict on purpose.** A workload whose annotation names a mode that isn't
  in the definition is `Invalid` and keeps its current replicas (§7.2). A typo
  such as `peek=6` therefore leaves the workload as it is, rather than scaling
  it to its `*` or zero count in `peak`.

### 5.4 Writers and field managers

| Writer | Writes | How | Field manager |
|---|---|---|---|
| admin | ClusterModeDefinitions | Git, through ArgoCD, or the Helm chart's values (§9.4) | ArgoCD's or Helm's |
| mode manager | `spec.mode`, `spec.definition` and the `mode.bitasylum.org/writer` label, on its own ClusterMode only | server-side apply with `Force: true` | `mode-manager` |
| maintainer | `spec.mode`, by hand | `kubectl patch`, `edit` or `apply` | whatever the tool sets, for example `kubectl-patch` |
| orchestration server | `spec.mode` only, on a ClusterMode that exists | server-side apply with `Force: true`, through the client library's `NewWriter` (§6.5) | its own, for example `orchestrator` |
| controller | all of `status`, on every ClusterMode | merge patch on the status subresource | `mode-operator` |
| anyone else | nothing (RBAC) | | |

- In v1 the mode manager creates its ClusterMode if it is missing (§6.1).
- **Manual edits.** The controller applies a maintainer's edit like any other
  mode change, and `status.modeSetBy` shows the field manager that set the
  mode. The mode manager adopts the edit and broadcasts it over gRPC (§6.1,
  rule 9), so workloads and gRPC subscribers stay on the same mode. The edit
  lasts until the mode manager's own logic next changes the mode.
  In practice maintainers can change only `spec.mode`: `spec.definition`
  can't be changed once set, and they can't write the definition itself.
- **The orchestration server** moves the cluster into a mode before powering
  servers off, and out of it after powering them on. The mode manager treats
  its changes like a maintainer's. It waits until the mode has settled
  (`Ready` True for the current generation) before powering anything off,
  and compares the nodes' boot IDs before and after to confirm a server
  really restarted. Its role is `get`, `list`, `watch` and `patch` on its
  ClusterMode, limited by `resourceNames`: no `create`, and no access to the
  definition, because it reads the rules from the ClusterMode's status.
- The `writer` label records which mode manager writes the object. It is how a
  human finds the gRPC endpoint that matches a ClusterMode, and it gives a
  future admission policy something to check (§13).
- ClusterMode instances **must not be managed by ArgoCD in v1**; Argo would
  revert mode changes. The CRDs may be in Git, and ClusterModeDefinitions
  belong there.

### 5.5 CRD lifecycle and install order
- Both CRDs ship in the controller's Helm chart and are installed with it.
  They go in the chart's `templates/`, not `crds/`, because Helm never
  upgrades or deletes CRDs in `crds/`, and later API versions need upgrades.
  Annotate them `helm.sh/resource-policy: keep`, so `helm uninstall` leaves
  them, and every object of those kinds, in place.
- Create the ClusterModeDefinition before the mode manager writes a
  ClusterMode that uses it. Until it exists, the ClusterMode reports
  `DefinitionNotFound` and its workloads are left as they are.
- The mode manager can be installed before or after the controller: without
  the CRD it keeps working but can't persist the mode (§6.1, rule 8).
- **Deleting a CRD deletes every object of that kind.** If the ClusterMode
  CRD is deleted, the mode manager recreates its ClusterMode once the CRD is
  back, but if it restarts before then, it starts in `standby`. The
  production install path must never delete either CRD. The README warns that
  Kubebuilder's `make undeploy` and `make uninstall` do delete them.
- The controller owns API version changes (v1alpha1 → v1beta1), including
  conversion. The mode manager depends only on the CRDs, so keep the fields
  it uses unchanged across versions: `spec.mode`, `spec.definition`,
  `status.mode`, `status.observedGeneration` and the `Ready` condition, and
  the definition's `spec.modes`, `spec.defaultMode` and `spec.transitions`.
  Keep serving every version it uses until it has moved to a newer one.

### 5.6 Maintainer access

Ship a ClusterRole, `mode-operator-clustermode-maintainer`, in `config/rbac/` and
the Helm chart. The chart binds it to the groups listed in its values (none by
default). Trim it from the editor role that Kubebuilder scaffolds:

- `clustermodes`: `get`, `list`, `watch`, `update`, `patch`
- no `create` or `delete`, and no access to `clustermodes/status`
- `clustermodedefinitions`: `get`, `list`, `watch` only

Also keep Kubebuilder's viewer roles (`get`, `list`, `watch`) for anyone who
only needs to read modes, and ship a ClusterRole,
`mode-operator-clustermodedefinition-admin`, with full access to
`clustermodedefinitions`, for the admins or GitOps service account that manage
them. The README shows the override command:
`kubectl patch clustermode default --type=merge -p '{"spec":{"mode":"maintenance"}}'`.

## 6. Mode manager contract

The mode manager is developed in another repo, and the ClusterMode CRD is its
only interface with this project. This project ships none of the mode
manager's code, but it does ship a Go client library that implements this
contract (§6.5); the mode manager may import that module and nothing else from
mode-operator. This section covers what the mode manager must do (§6.1), what
it can rely on (§6.2), a recommended design for its persistence (§6.3), its
RBAC (§6.4) and the library (§6.5). Its gRPC API is unchanged; the changes the
contract needs are made in the mode manager's repo.

### 6.1 What the mode manager must do

1. **Write the mode to its ClusterMode.** Use server-side apply with field
   manager `mode-manager` and `Force: true`, setting `spec.mode`,
   `spec.definition` (the name of its ClusterModeDefinition, from its own
   configuration) and the label `mode.bitasylum.org/writer` (for example
   `mode-manager.ops`). Send all three in every apply: server-side
   apply removes a field that its manager owned and then leaves out. Create
   the ClusterMode if it doesn't exist. In v1 its name is `default`; make the
   name configurable. `Force` matters: after a maintainer's manual edit
   (§5.4), another field manager owns `spec.mode`, and an apply without
   `Force` fails with a conflict.
2. **Write before broadcasting.** On every mode change, write `spec.mode`
   before committing the new mode or broadcasting it over gRPC. If the write
   fails while the CRD is installed, keep the previous mode, don't broadcast,
   and retry. This keeps workloads and gRPC subscribers on the same mode. If
   the CRD isn't installed, rule 8 applies instead.
3. **Use only modes its definition allows.** Check a mode against the rules
   before writing it. Take them from the ClusterModeDefinition's copies in the
   ClusterMode's status, `status.modes`, `status.defaultMode` and
   `status.transitions`: they're what the API server checks writes against,
   and the mode manager shows users the modes they allow next. Read the
   definition itself only while the copies are missing, before the
   controller's first status write; after the definition is edited, the copies
   catch up within seconds, and until then they are still what is enforced.
   The API server rejects a mode outside them (§5.2), so a mismatch shows up
   as a failed write under rule 2, never as a wrong mode. The mode manager
   can't change the definition; §5.3 gives the order for adding and removing
   modes. If there are transitions, move only along them, and only
   once the ClusterMode's `status.observedGeneration` equals its
   `metadata.generation`, `status.mode` is the current mode and its `Ready`
   condition is True. Plan paths of several steps as a series of such moves:
   with the README's example rules, peak → standby → maintenance. A move to
   `spec.defaultMode` is always allowed, including the reset in rule 6.
4. **Never write `status`**, or any other field of the ClusterMode, and don't
   revert a maintainer's manual edit except by making a mode change.
5. **Run one writer at a time.** The mode manager is a single replica today.
   Give its Deployment `strategy: Recreate`: the default rolling update briefly
   runs the old and new pods together, and both would write the ClusterMode.
   If the mode manager becomes HA, only the leader writes, chosen with a Lease.
6. **Keep the mode across restarts, and reset it after a full cluster
   reboot.** On startup, restore `spec.mode` if at least one node stayed up
   while the mode manager was down; otherwise start in the definition's
   `spec.defaultMode` (`standby` in v1). §6.3 is a design for this.
7. **Write the API version it was built against**, `v1alpha1` in v1. §5.5 says
   how long a version stays served.
8. **Keep working without the CRD.** If the ClusterMode CRD isn't installed,
   or is removed, the mode manager decides and broadcasts modes as usual
   without persisting them, so a restart starts it in its own built-in default
   mode (`standby` in v1). This is
   acceptable: without the CRD no controller acts on the mode, so workloads
   can't drift from it. Check through discovery whether `clustermodes` is
   served, at startup and then about once a minute. Once it is, write the
   current mode, which creates the ClusterMode.
9. **Adopt changes made by others**, a maintainer's or the orchestration
   server's. Watch its ClusterMode. When `spec.mode` changes to something
   other than the mode manager's current mode, take it as the current mode
   and broadcast it over gRPC, without writing it back. An event counts only
   if its `resourceVersion` is newer than that of the last change the mode
   manager wrote or adopted (compared as Kubernetes compares resource
   versions); otherwise an event from before its own write, delivered after
   it, would undo the write. Events where `spec.mode` already equals the
   current mode are ignored too. Broadcast changes in the order they
   happened: the library reports each one, written or adopted, through
   `Writer.OnChange` under the writer's lock. Its own logic may change the
   mode again later, as usual.

### 6.2 What the mode manager can rely on
- The controller never writes ClusterMode `spec`. Only the mode manager,
  maintainers and the orchestration server do (§5.4).
- Nobody but admins changes the ClusterModeDefinition. The controller copies
  its rules into the ClusterMode's status within seconds of a change, so
  watching the ClusterMode picks up changes, including its `transitions`.
- Under `transitions`, the API server rejects a move that isn't allowed yet,
  and the mode manager sees a failed write under rule 2. With the admission
  policy installed (§5.2), a Ready status counts only once it was computed
  from the latest spec. One window is left: after a full cluster reboot, the
  status from before the reboot counts until the controller's first status
  write, because nothing has changed the generation (§7.8).
- The fields it uses keep their meaning in every API version, and a version
  it uses stays served until it has moved off it (§5.5).
- Mode names come only from ClusterModeDefinitions, so adding a mode needs a
  change to the definition in Git, not a release of this project.
- `status` shows how far a mode change has got (§7.4). The mode manager may
  read it, for example to log when a change has settled, but doesn't have to.
- If the ClusterMode or the CRD is missing, the controller leaves workloads as
  they are.

### 6.3 Recommended persistence design

The client library's `fingerprint` and `startup` packages build this design
(§6.5). The mode-manager team can use them, or build it from this section.

#### Fingerprint ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mode-manager-fingerprint-default
  namespace: <mode manager namespace>
data:
  clusterMode: default
  bootIDs: |                  # one node=bootID per line, sorted by node name
    node-a=3f0c...
    node-b=91ad...
  updatedAt: "2026-10-17T09:12:44Z"
  lastStartupAt: "2026-10-17T08:01:02Z"
  restoredOnStartup: "true"   # "false" if the last startup reset to the default mode
```

- Written with server-side apply, field manager `mode-manager`, in the mode
  manager's own namespace.
- About 100 bytes per node, so it stays far below the 1 MiB ConfigMap limit
  even for thousands of nodes.
- Nodes with an empty `bootID` are left out, both when writing and when
  comparing. Otherwise two empty values would count as a match.

#### Startup

1. List Nodes and build the set of live, non-empty `status.nodeInfo.bootID`
   values.
2. Get the ClusterMode, its ClusterModeDefinition, and the fingerprint
   ConfigMap. A read that fails for any reason other than NotFound stops
   startup, to be retried: taking it for a missing object would reset the
   mode.
3. Decide the mode. The first rule that matches wins:

   | Situation | Mode | Restored | Event on the ClusterMode |
   |---|---|---|---|
   | CRDs not installed | built-in default mode (`standby`) | no | none. Skip steps 4 and 5 and serve gRPC; once the CRDs appear, do steps 4 and 5 with the current mode (§6.1, rule 8) |
   | ClusterModeDefinition not found | the ClusterMode's `status.defaultMode`, the controller's last copy of the deleted definition's rules, which the API server still enforces; otherwise the built-in default mode | no | Warning `DefinitionNotFound`; the ClusterMode reports it too |
   | ClusterMode not found | the definition's default mode | no | Normal `ModeReset` (created in step 4) |
   | stored `spec.mode` isn't in the definition, or isn't a mode the mode manager knows | the definition's default mode | no | Warning `InvalidStoredMode` |
   | ConfigMap missing, it has no bootIDs, or it is for another ClusterMode | the definition's default mode | no | Normal `ModeReset` |
   | at least one stored bootID is in the live set | stored `spec.mode` | yes | Normal `ModeRestored` |
   | no stored bootID is in the live set | the definition's default mode | no | Normal `ModeReset` (full cluster reboot) |

   An overlap means at least one node stayed up, so this was not a full
   cluster reboot. If the definition's default mode isn't one the mode
   manager knows, "the definition's default mode" means its built-in default
   instead (§14).
4. Server-side apply the ClusterMode: `spec.mode`, `spec.definition` and the
   `writer` label. This creates the object if it is missing.
5. Only then, server-side apply the fingerprint ConfigMap with the live set,
   `updatedAt`, `lastStartupAt` and `restoredOnStartup`.
6. Emit the event from the table, and start serving gRPC with the mode.

**The order of steps 4 and 5 matters.** If the process fails between them and
restarts, it reaches the same decision: the old fingerprint is still in place
and the ClusterMode already holds the decided mode. The reverse order is
unsafe. After a full reboot, writing the new fingerprint first and then failing
would make the next startup see an overlap and restore the old mode.

Keep the decision in step 3 a pure function, so the table can be unit-tested.

#### Keeping the fingerprint current
- Run an informer on Nodes with a transform that keeps only the node name and
  `status.nodeInfo.bootID`, to save memory. A metadata-only informer can't be
  used, because the bootID is in `status`.
- On a Node add, delete, or `bootID` change, wait about 2s from the first
  change, without extending the wait for later ones, then rewrite the whole
  record with the new `bootIDs` and `updatedAt`. A server-side apply that
  left out `lastStartupAt` and `restoredOnStartup` would remove them. Skip
  the write if the set hasn't changed.
- Rolling reboots therefore never trigger a reset: each returning node's new
  bootID is recorded while the manager runs.
- If refreshing fails, log the error and retry. A stale fingerprint only
  matters if every node reboots before it is refreshed.

#### Tests worth having
- Every row of the decision table, including empty bootIDs on either side
  never counting as a match.
- The write order: make the fingerprint write fail after a full-reboot
  decision, then check that a second startup still chooses the default mode.
- The fingerprint is sorted by node name, the debounce coalesces bursts, and
  nothing is written when the set hasn't changed.

### 6.4 RBAC (mode manager ServiceAccount)

mode-operator's Helm chart ships the ClusterMode rules as a ClusterRole,
`mode-operator-clustermode-writer`, limited to the ClusterMode names listed in the
chart's values (`default` in v1), because they belong to the CRD. The mode
manager's own chart binds its ServiceAccount to that role and grants the rest.

From mode-operator's chart:
- `clustermodes`: `create`, `get`, `list`, `watch`, `update`, `patch`, all
  limited by `resourceNames` to its own ClusterMode. A plain create can't be
  limited by name, because the name isn't in its URL, but a server-side
  apply that creates the object is authorized as a create of that name, and
  rule 1 creates the ClusterMode that way; a plain create is refused. To list
  or watch one named object under `resourceNames`, the request must use a
  `metadata.name` field selector.
- no access to `clustermodes/status`
- `clustermodedefinitions`: `get`, `list`, `watch`, and no write access

From the mode manager's chart:
- `events` (core) in the `default` namespace, where Kubernetes records events
  about cluster-scoped objects: `create`, `patch`
- for §6.3: `configmaps` in its own namespace (`create`, `get` and `patch`,
  limited by `resourceNames` to the fingerprint ConfigMap, which the library
  creates with server-side apply), and `nodes` (`list`, `watch`)
- `leases` in its own namespace, only once it becomes HA: `get`, `list`,
  `watch`, `create`, `update`, `patch`, `delete`

### 6.5 Client library (`client/`)

The contract in §6.1 is easy to get subtly wrong: which fields every apply
sends, `Force`, the generation check, telling one's own writes from others',
the CRDs coming and going. mode-operator therefore ships one tested
implementation, the module `github.com/bitasylum/mode-operator/client`, for
the mode manager and the orchestration server, which are both Go.

- **Its own module** in `client/`, depending only on `k8s.io/api`,
  `apimachinery`, `client-go`, `klog` and `k8s.io/utils`. It needs Go 1.26 and
  client-go 0.37 or later. It doesn't import mode-operator's API types or
  controller-runtime, and works on unstructured objects through the dynamic
  client, converted into its own structs that carry only the fields §5.5
  keeps stable. mode-operator's Makefile sets `GOWORK=off`, so a workspace
  never leaks into its builds. Releases are tagged `client/vX.Y.Z`.
- **Three packages,** so the mode-manager team can take as much as they want:

  | Package | Does | Depends on |
  |---|---|---|
  | `clustermode` | the CRD interface: read, watch, write, plan and move | client-go |
  | `fingerprint` | boot IDs: take, compare, store in a ConfigMap, keep current | client-go |
  | `startup` | the decision table and write order of §6.3 | both |

- **`clustermode`:**
  - `Reader`: `Available` (discovery), `Get`, `Rules` (the status copies,
    falling back to the definition, rule 3), `Watch`, `WaitFor` and
    `WaitSettled`. `Watch` is an informer limited to the ClusterMode by a
    `metadata.name` field selector, which a `resourceNames` role allows
    (§6.4). Its first list is a quorum read, so the first event is never
    older than a write made just before it. It reports a missing ClusterMode
    once the cache has synced, and ends with `NotInstalled` when the CRDs go.
  - `NewModeManagerWriter` follows rule 1: field manager `mode-manager`, all
    three fields in every apply, and creating the ClusterMode.
    `NewWriter(fieldManager)` is for other writers, such as the orchestration
    server: `spec.mode` only, and never create.
  - `Check`, `Set` (check, then write) and `Apply` (write, and let the API
    server decide). Errors carry a reason: `NotInstalled`, `NotFound`,
    `DefinitionNotFound`, `RulesUnavailable`, `UnknownMode`,
    `TransitionNotAllowed`, `NotSettled`, `NoPath`, `Interrupted`,
    `Forbidden`, `Rejected` and `DefinitionMismatch`. A rejection is
    classified by re-reading the ClusterMode, never by parsing the API
    server's message.
  - `MoveTo` plans the shortest path along the transitions (ties broken by
    the order of `modes`), and waits for each mode to settle before leaving
    it, except into the default mode, which is always allowed. It calls back
    after each step so the mode manager can broadcast it (rule 2), and stops
    with `Interrupted` if someone else changes the mode.
  - `Observe` and `OnChange` implement rule 9.
- **`fingerprint`:** `Take`, `Overlap` and `Compare` (rebooted, unchanged,
  gone, added), which the orchestration server uses around a power cycle;
  `Store`, the ConfigMap in §6.3 written with server-side apply; and
  `Keeper`, the informer that keeps it current.
- **`startup`:** `Decide` is the pure table in §6.3; `Run` does the steps in
  order; `Persist` writes the mode once the CRDs appear (rule 8); and
  `NewRecorder` records events in `default`.
- **Tests:** unit tests with fake clients and a fake clock, and integration
  tests in `client/integration`, a module of their own so that envtest and
  controller-runtime stay out of the library's `go.mod`. They run the real
  CRDs, the admission policy and the roles from `config/` against envtest's
  API server, as users bound to those roles, so RBAC is enforced. The stub
  (§9.5) runs the library with mode-operator on a kind cluster.

## 7. Controller

### 7.1 Workload contract

```yaml
metadata:
  labels:
    mode.bitasylum.org/clustermode: default        # opt in, and follow ClusterMode "default"
  annotations:
    mode.bitasylum.org/replicas: "normal=3,peak=6,offpeak=1"
    mode.bitasylum.org/scale-to-zero-delay: "5m"   # optional
spec:
  replicas: 0   # creation-time value only (see §8)
```

These go on the **workload's metadata**, not on a pod or VM template. Each
workload follows exactly one ClusterMode. A KubeVirt VirtualMachine uses the
same keys, with counts of 0 or 1 (§7.9):

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  labels:
    mode.bitasylum.org/clustermode: default
  annotations:
    mode.bitasylum.org/replicas: "normal=1,peak=1,*=0"   # 0 or 1 only
    mode.bitasylum.org/scale-to-zero-delay: "10m"        # optional
    mode.bitasylum.org/settle-timeout: "20m"             # optional; slow boot
spec:
  runStrategy: Halted   # creation-time value only (see §8)
```

| Key | Owner | Meaning |
|---|---|---|
| label `.../clustermode` | user | opt-in, and the name of the ClusterMode to follow; the informer cache only holds workloads that have it |
| `.../replicas` | user | `mode=N` entries, comma-separated; `*=N` sets the default for unlisted modes; an unlisted mode with no `*` means 0 |
| `.../scale-to-zero-delay` | user | Go duration to wait before scaling to zero; absent or `0` means immediately. Applies only when the mode wants 0 replicas, so it never delays or blocks a scale-up or a scale-down to a non-zero count |
| `.../settle-timeout` | user | optional Go duration: how long this workload may take to reach its count before it counts as `Stalled`; overrides `--settle-timeout` |
| `.../applied-mode` | controller | the last mode fully applied |
| `.../zero-pending-since` | controller | RFC 3339 start time of a pending scale-to-zero |
| `.../replicas-changed-at` | controller | RFC 3339 time the controller last changed `spec.replicas`; used to detect stalled scaling |

### 7.2 Parsing rules (`internal/modespec`)
- Whitespace around entries, keys, and values is allowed. Empty entries are
  ignored.
- Replicas must be integers ≥ 0 that fit in int32.
- A mode name must be in the `spec.modes` of the ClusterModeDefinition used by
  the referenced ClusterMode. Duplicates (including a duplicate `*`) are an
  error. When a definition changes, every workload that depends on it is
  checked again.
- An annotation with zero entries is an error.
- `scale-to-zero-delay` is parsed with `time.ParseDuration` after trimming
  whitespace. Empty or `0` means no delay. A negative or unparseable value is
  an error.
- `settle-timeout` is parsed the same way, but must be greater than 0. If it
  is absent, the controller's `--settle-timeout` applies.
- All user annotations are validated on every reconcile, whatever the current
  mode, so a mistake is reported as soon as it is applied. (The prototype only
  parsed the delay when scaling to zero.)
- Any error emits a Warning event `InvalidAnnotation`. Do not requeue; the next
  edit or mode change retriggers. What the error blocks depends on the
  annotation:
  - An invalid `replicas` annotation leaves replicas untouched, because there
    is no target to apply.
  - An invalid `scale-to-zero-delay` blocks only scaling to zero: the workload
    stays at its current replicas until the annotation is fixed. Scale-ups, and
    scale-downs to a non-zero count, are applied as normal.
  - An invalid `settle-timeout` blocks nothing. Status uses `--settle-timeout`
    for that workload until the annotation is fixed.
- A missing `replicas` annotation on a labelled workload emits a Warning event
  `MissingAnnotation` and leaves replicas untouched.
- For a VirtualMachine, every count must be 0 or 1 (§7.9).

### 7.3 Workload reconcile algorithm

Inputs: the workload from the cache, and the ClusterMode named by its
`clustermode` label.

1. If the workload is not found or is being deleted, do nothing. A workload
   whose label was removed is "not found" in the label-filtered cache (§7.8).
   A VirtualMachine owned by a VirtualMachinePool is skipped (§7.9).
2. Get the ClusterMode named by the label. If it doesn't exist, emit a Warning
   event `ClusterModeNotFound` and do nothing. Don't requeue; creating the
   ClusterMode retriggers. If its ClusterModeDefinition doesn't exist, or its
   `spec.mode` isn't in the definition's `spec.modes`, do nothing; the
   ClusterMode's status reports why (§7.4).
3. Parse the user annotations and emit events as in §7.2. If `replicas` is
   missing or invalid, stop. If only `scale-to-zero-delay` is invalid, carry
   on; step 6 decides whether it matters. A VirtualMachine also has the checks
   in §7.9.
4. Compute `desired = spec.ReplicasFor(mode)` and `cur`, the workload's
   current count (§7.9; for Deployments and StatefulSets, `spec.replicas` with
   nil as 1).
5. Start from `target = desired`, `appliedMode = mode`, `pending = ""`,
   `requeue = 0`.
6. If `desired == 0` and `cur > 0`:
   - If the delay is invalid, hold without a countdown: `target = cur`,
     `appliedMode` = the existing `applied-mode` annotation, `pending` = the
     existing `zero-pending-since` annotation, unchanged, and `requeue = 0`.
     Fixing the annotation retriggers, and a countdown that was already
     running resumes from its original start.
   - Otherwise, if the delay is greater than 0:
     - Set `since` to the parsed `zero-pending-since`. Use now instead if it is
       missing, malformed, or in the future.
     - If `remaining = delay - (now - since)` is greater than 0, then hold:
       `target = cur`, `appliedMode` = the existing `applied-mode` annotation,
       `pending = since`, `requeue = remaining`.
7. Set `changedAt` to now if `target != cur`; otherwise keep the existing
   `replicas-changed-at` annotation.
8. If the count field already holds the value for `target` (§7.9), and the
   `applied-mode` and `zero-pending-since` annotations already equal the
   computed values, return `RequeueAfter: requeue`. `replicas-changed-at` is
   not compared.
9. Otherwise server-side apply (manager `mode-operator`,
   `Force: true`) the count field for `target` (`spec.replicas`, or
   `spec.runStrategy` for a VirtualMachine, §7.9) plus the controller
   annotations that are non-empty. Omitting `zero-pending-since` makes SSA
   remove it, which is how a pending scale-down is cancelled.
   - For Deployments and StatefulSets, build the request from client-go apply
     configurations, never from a partly filled typed object, which would
     claim ownership of its empty fields. Use controller-runtime's native apply
     if the pinned version has it; otherwise use the client-go clientset, as
     the prototype does.
   - For KubeVirt kinds, apply a minimal unstructured object holding only
     these fields.
10. Emit Normal events: `ScaleToZeroPending`, `ScaleToZeroCancelled`, or
    `ModeApplied`, as appropriate.
11. Return `RequeueAfter: requeue`.

Steps 5 to 7 are a pure function,
`decide(mode, cur, desired, delay, delayValid, annotations, now) → (target, annotations, requeue, event)`,
and are table-tested (§11).

These properties follow from the algorithm:
- Moving between two zero-replica modes does not restart the delay timer. That
  includes moving a workload to another ClusterMode whose mode also wants 0.
- Moving to a mode with replicas above 0 cancels a pending scale-down.
- The delay only ever holds a scale to zero. Scale-ups, and scale-downs to a
  non-zero count, are never delayed, even when the delay annotation is
  invalid.
- The pending start time is stored on the object, so the countdown survives
  controller restarts and failover.

### 7.4 ClusterMode status

The status reconciler runs once per ClusterMode. It lists the workloads whose
`clustermode` label names it, puts each one in exactly one state, and writes the
counts, sums, unsettled list and conditions to `status`. The computation is a
pure function,
`summarize(clusterMode, definition, workloads, now, defaultSettleTimeout) → (status, requeueAfter)`,
and is table-tested (§11). It uses `internal/modespec` to compute each
workload's desired replicas, so it agrees with the workload reconcilers.

#### Workload states

Check the states in this order; the first match wins.

| State | When |
|---|---|
| `Invalid` | An annotation error is stopping the controller from applying the mode: the `replicas` annotation is missing or invalid, or the mode wants 0, the workload still has replicas, and `scale-to-zero-delay` is invalid (§7.2); or a VirtualMachine uses the unsupported `spec.running` field (§7.9). Replicas are left untouched. |
| `Pending` | The mode wants 0 and `zero-pending-since` is set: the workload is waiting out its scale-to-zero delay. |
| `Applying` | The count field doesn't hold the value the mode wants, or `applied-mode` differs from `spec.mode`. The controller hasn't written the workload yet; this normally lasts seconds. |
| `Stalled` | The workload hasn't reached its count (as for `Scaling`), and more than its settle timeout (the `settle-timeout` annotation, or `--settle-timeout`) has passed since `replicas-changed-at`, or since `status.modeChangedAt` if that annotation is absent. If the controller has seen the workload `Settled` since then, the timeout runs instead from when it first saw it short again (see below). |
| `Scaling` | The count is applied, but the workload hasn't reached it yet. What "reached" means depends on the kind (§7.9); for a Deployment or StatefulSet, the pods are ready, none is still to stop, and its `status` is up to date. |
| `Settled` | None of the above: the count is applied and the workload has reached it. |

- A workload with an invalid delay that isn't blocked, because its mode
  doesn't want 0, is counted in its normal state. The error still counts in
  `invalidAnnotations` and sets `Degraded`.
- `Settled` compares replica counts only, and doesn't track image rollouts.
  A workload is settled only while none of its pods is still to stop, so a
  scale-down settles once its pods have gone, not just once they stop being
  counted. A StatefulSet keeps terminating pods in `status.replicas` until
  they have gone. A Deployment leaves them out and reports them in
  `status.terminatingReplicas` (a beta field, on by default in recent
  Kubernetes), which must be 0; on a cluster without that field, a
  Deployment's scale-down settles once its count drops. A pod can take up to
  its `terminationGracePeriodSeconds` to stop, so workloads that stop slowly
  need a longer `settle-timeout`. Status alone can't tell a scale-down from
  any other pod shutting down, so evictions, rollouts and pods stuck on a
  lost node count too (§7.8).
- The message for a workload with pods still to stop shows the ready count
  capped at `desired`, because a StatefulSet counts its ready pods above the
  count too: "3 of 3 replicas ready; 2 pods still to stop".
- A workload that falls short of its count after staying `Settled` for at
  least a minute, because of an image rollout or a crashed pod, gets its whole
  settle timeout from that moment, not from the last mode change. A shorter
  spell at its count, such as a crash-looping pod passing readiness for a few
  seconds, carries on the timeout already running. Otherwise every rollout long after a
  mode change would show as `Stalled` at once. The status reconciler keeps
  this history in memory, so after a restart or failover it only has the
  annotations: a workload that is already stuck stays `Stalled`, and a
  rollout in progress at that moment may briefly show as `Stalled`.
- A StatefulSet with `podManagementPolicy: OrderedReady` scales one pod at a
  time. Give large ones a longer `settle-timeout` annotation.
- A stuck `Applying` means the controller is failing to write. That shows up in
  reconcile errors (§7.6), not as `Stalled`.

#### Counts and sums
- `workloads.<state>` is the number of workloads in each state, and
  `workloads.total` is their sum.
- `invalidAnnotations` is the number of workloads with any annotation error,
  including a missing `replicas` annotation and an invalid delay that isn't
  blocking anything yet. It isn't a
  state and isn't part of `total`.
- `replicas.desired`, `replicas.applied` and `replicas.ready` are sums over
  every workload that isn't `Invalid` of, in turn: the replicas the mode asks
  for, `spec.replicas`, and `status.readyReplicas`. `applied` is above
  `desired` while scale-to-zero delays are running, and differs from it
  briefly while workloads are `Applying`. A VirtualMachine counts as one
  replica when it is on.

#### Unsettled list
`status.unsettled` lists up to 20 workloads that aren't `Settled`, so
`kubectl get clustermode -o yaml` shows what is holding a mode change up.

- Order: `Stalled`, `Invalid`, `Scaling`, `Applying`, `Pending`. Within a
  state, oldest `since` first, then by kind, namespace and name.
- Each entry has `kind`, `namespace`, `name`, `state`, `desired`, `applied`,
  `ready`, `since` and a short `message`. For `Invalid` entries the message is
  the annotation error, and `desired` and `since` are omitted when the
  `replicas` annotation is the problem.
- The counts always cover every workload, even when the list is cut off.

#### Conditions

| Type | `True` when | Reasons |
|---|---|---|
| `Applied` | Every workload that isn't `Invalid` has the replicas the mode asks for: none is `Applying` or `Pending`. | True: `AllApplied`, `NoWorkloads`. False, first that applies: `Applying`, `ScaleToZeroPending`. |
| `Ready` | Every workload that isn't `Invalid` is `Settled`. | True: `AllSettled`, `NoWorkloads`. False, first that applies: `Stalled`, `Applying`, `Scaling`, `ScaleToZeroPending`. |
| `Degraded` | `invalidAnnotations` is above 0, any workload is `Stalled`, or the mode can't be applied at all. | True: `InvalidWorkloads`, `StalledWorkloads`, `InvalidAndStalledWorkloads`, `DefinitionNotFound`, `InvalidMode`. False: `AsExpected`. |

- If the ClusterModeDefinition doesn't exist, `Applied` and `Ready` are False
  and `Degraded` is True, with reason `DefinitionNotFound`. If `spec.mode`
  isn't in the definition's `spec.modes`, the reason is `InvalidMode`. Either
  way the counts are left empty and workloads are left as they are. `Degraded`
  is True because no workload can follow the ClusterMode, which is worth an
  alert.
- Every condition carries `observedGeneration` and a message with counts, for
  example "2 of 42 workloads are scaling".
- A pending scale-to-zero makes `Applied` and `Ready` false by design, because
  pods are still running. Alert on `Degraded`, not on `Ready`.

#### Writing status
- When `status.mode` differs from `spec.mode`, set `status.mode = spec.mode` and
  `status.modeChangedAt` to now, and increment `mode_operator_mode_changes_total`
  unless `status.mode` was empty. Because this is stored on the object, a new
  leader doesn't count the same change twice.
- Set `status.modeSetBy` to the field manager that owns `spec.mode` in
  `managedFields`: `mode-manager` normally, or the maintainer's tool after a
  manual edit.
- Copy the definition's `spec.modes`, `spec.defaultMode` and
  `spec.transitions` into `status.modes`, `status.defaultMode` and
  `status.transitions`, which the CRD rules and admission policy in §5.2
  check changes of `spec.mode` against. Update the copies whenever the
  definition changes; a definition that drops its transitions removes
  `status.transitions`. If the definition is deleted, keep the last copies,
  so writes are still checked against the last known rules.
- Write only when the computed status differs from the stored one. Use
  `meta.SetStatusCondition`, so `lastTransitionTime` changes only when a
  condition's status changes.
- Write at most once every 2 seconds per ClusterMode. If the last write was
  more recent, return `RequeueAfter` for the rest of the interval; don't sleep.
- While any workload is `Scaling`, also return `RequeueAfter` for the earliest
  moment one of them would become `Stalled`. No watch event fires when a
  timeout passes.
- Write with a merge patch on the status subresource, field owner
  `mode-operator`. The controller is the only status writer, so
  replacing whole lists is safe.
- Set the metrics in §7.6 from the same computed status, so metrics and status
  always agree. When a ClusterMode is deleted, delete its metric series.

### 7.5 Watches and triggers
- Cache workloads of every kind only when they carry the `clustermode`
  label (an "exists" label selector, set with `cache.Options.ByObject`).
  Unlabelled workloads are never cached.
- Add a field index on the label's value for every kind, so finding a
  ClusterMode's workloads is a lookup rather than a scan.
- The KubeVirt kinds get the same caches, watches and reconcilers, but only
  when KubeVirt is installed (§7.9).

Workload reconcilers (one per kind):
- Watch the kind with a predicate of
  `GenerationChanged OR AnnotationChanged OR LabelChanged`. Label changes
  matter because they move a workload to another ClusterMode.
- Watch `ClusterMode` with `handler.EnqueueRequestsFromMapFunc`, mapping it to
  its workloads of this kind through the field index. The predicate fires on
  create and when `metadata.generation` changes, which covers every change to
  `spec.mode`. It doesn't fire on delete, so deleting a ClusterMode changes
  nothing.
- Watch `ClusterModeDefinition` too, mapping a definition to the workloads of
  every ClusterMode that uses it. A field index on `spec.definition` finds
  those ClusterModes. A change to the allowed modes then re-checks their
  annotations.
- Set `MaxConcurrentReconciles` to 4.
- Drift (for example, a manual `kubectl scale`) is corrected on the resulting
  watch event.

Status reconciler:
- Watch `ClusterMode` with `GenerationChangedPredicate`. Status writes don't
  change the generation, so the reconciler's own writes don't retrigger it.
  Create and delete still trigger it.
- Watch workloads of every kind with `EnqueueRequestsFromMapFunc`, mapping
  a workload to the ClusterMode its label names, with no predicate, because pod
  readiness arrives as changes to the workload's status. On an update, the map
  function runs for both the old and the new object, so a workload that
  switches ClusterMode updates both. A workload whose label is removed leaves
  the filtered cache as a delete event, which updates its old ClusterMode.
- Watch `ClusterModeDefinition`, mapping it to the ClusterModes that use it,
  so their copies of its modes, default mode and transitions stay current.
- Set `MaxConcurrentReconciles` to 2.

### 7.6 Operational
- Use leader election with a Lease named
  `mode-operator.mode.bitasylum.org` in the controller namespace,
  and run 2 replicas. All reconcilers run only on the leader. The leader
  releases the Lease when it shuts down, so a rollout hands over at once
  rather than after the Lease expires.
- Flags: metrics address, probe address, leader election, `--settle-timeout`
  (default `10m`; a workload can override it, §7.1), and `--kubevirt` (`auto` or `disabled`, default `auto`;
  §7.9). No mode flags.
- Metrics, all set by the status reconciler:

  | Metric | Type | Labels |
  |---|---|---|
  | `mode_operator_current_mode` | gauge, 1 for the current mode | `clustermode`, `mode` |
  | `mode_operator_mode_changes_total` | counter | `clustermode` |
  | `mode_operator_workloads` | gauge | `clustermode`, `state` (`settled`, `scaling`, `stalled`, `applying`, `pending`, `invalid`) |
  | `mode_operator_replicas` | gauge | `clustermode`, `type` (`desired`, `applied`, `ready`) |
  | `mode_operator_invalid_annotations` | gauge, from `status.invalidAnnotations` | `clustermode` |
  | `mode_operator_condition` | gauge, 1 when the condition is True and 0 when False | `clustermode`, `type` (`Applied`, `Ready`, `Degraded`) |

  Reconcile errors come from controller-runtime's built-in
  `controller_runtime_reconcile_errors_total{controller}`. Don't add a custom
  error metric.
- Suggested alerts for the README:
  - `mode_operator_workloads{state="stalled"} > 0` for 5m
  - `mode_operator_invalid_annotations > 0` for 15m, which catches a bad delay
    before it blocks anything
  - `mode_operator_condition{type="Degraded"} == 1` for 15m, which also
    catches a ClusterMode that can't apply its mode at all
  - `rate(controller_runtime_reconcile_errors_total{controller=~"mode-operator-.*"}[5m]) > 0` for 10m
- Logs use structured fields: `clustermode`, `mode`, `kind`, `namespace`,
  `name`, `from`, `to`.
- The container runs distroless and nonroot, with a read-only root
  filesystem, all capabilities dropped, and the RuntimeDefault seccomp profile.
  The binary is static (`CGO_ENABLED=0`), built with `-trimpath`; PIE isn't
  used, because a static PIE needs cgo and distroless static has no loader.
- The metrics endpoint accepts TLS 1.3 only. Its clients are metrics
  scrapers, and Go's post-quantum key exchange is TLS 1.3 only.
- CI builds, tests and scans with the Go version of the Dockerfile's builder
  image (`hack/build-go-version.sh`), not go.mod's minimum, and runs
  `govulncheck` on every push and weekly. Dependabot updates Go modules, the
  base images (Go patch releases only) and Actions.

### 7.7 RBAC (controller ServiceAccount)

Generated from `+kubebuilder:rbac` markers.

- `clustermodes`: `get`, `list`, `watch`
- `clustermodes/status`: `get`, `update`, `patch`
- `clustermodedefinitions`: `get`, `list`, `watch`
- `deployments`, `statefulsets`: `get`, `list`, `watch`, `patch`
- `virtualmachines` (`kubevirt.io`) and `virtualmachinepools`
  (`pool.kubevirt.io`): `get`, `list`, `watch`, `patch`. These rules are
  installed even without KubeVirt (§7.9).
- `events` (`events.k8s.io`): `create`, `patch`. controller-runtime's event
  recorder writes `events.k8s.io/v1` Events.
- `leases` (own namespace): `get`, `list`, `watch`, `create`, `update`,
  `patch`, `delete`, and core `events` there for leadership changes. The
  ConfigMap rule Kubebuilder scaffolds for leader election isn't needed.

The controller has no write access to ClusterModeDefinitions, ClusterMode
`spec`, ConfigMaps or Nodes.

### 7.8 Known behaviour and limits
- **Missing or deleted ClusterMode:** workloads that reference it keep their
  current replicas (fail-static) and get a `ClusterModeNotFound` Warning on
  their next reconcile.
- **Offboarding:** removing the `clustermode` label stops management. The
  controller can no longer see the workload, so its annotations and its
  ownership of `spec.replicas` remain, and Argo keeps ignoring replicas. The
  README documents and tests this procedure:
  1. In Git, set `spec.replicas` to the count you want and remove the label.
     Sync.
  2. Clear the workload's field ownership:
     `kubectl patch <kind> <name> --type=merge -p '{"metadata":{"managedFields":[{}]}}'`.
  3. Sync again. Argo now applies the replica count from Git.

  Doing step 2 while Git still says `replicas: 0` scales the workload to zero.
- **Single-node clusters:** any reboot of the node is a full cluster reboot, so
  the mode resets to `standby`.
- **Recreated workloads:** if Argo deletes and recreates a workload, it starts
  at the Git value of 0 until the controller applies the mode, normally within
  seconds.
- **HPA:** a workload targeted by an HPA must not opt in, because both would
  write `spec.replicas`. v1 does not detect this.
- **KubeVirt:** see §7.9 for `virtctl start` and `stop`, paused VMs, and VMs
  that belong to a pool.
- **Pods stuck terminating:** a pod on a node that has gone down stays
  terminating until the node returns, its Node object is deleted, or someone
  force-deletes the pod. Its workload stays unsettled, goes `Stalled`, and
  makes the ClusterMode `Degraded`. With `transitions`, `Ready` stays False,
  so only the default mode can be reached until the pod has gone. A
  StatefulSet already behaved this way, because it doesn't replace such a
  pod.
- **Ready while the controller isn't writing status:** the admission policy
  (§5.2) proves only that the status was computed from the latest spec, not
  that it still reflects the workloads. While the controller is down, or
  after a full cluster reboot until its first status write (normally seconds
  after it starts), the last status it wrote counts, even if pods have failed
  since. After a reboot, if the mode manager's reset to the default mode
  changes nothing, because the ClusterMode was already in it, the generation
  is unchanged, so a move on the old `Ready` is accepted in that window.
- **Getting out of a stuck mode** (README): change to the default mode; if
  that can't become Ready either, remove `transitions` from the definition;
  with the controller down, a cluster admin can remove `status.transitions`
  through the status subresource, which the controller copies back from the
  definition when it next runs.
- **StatefulSets and pods inside the count:** a StatefulSet counts a
  terminating pod in `replicas`, and in `readyReplicas` while it is still
  ready, so only pods above its count, or pods no longer ready, keep it
  unsettled. A Deployment counts every terminating pod.
- **A definition with no `defaultMode` rule, or an empty `transitions`
  list,** is rejected (§5.3).
- **Definition deleted, or `InvalidMode`:** `Ready` is False, so under
  `transitions` only the default mode can be reached.

### 7.9 Workload kinds and KubeVirt

The reconcile and status logic in §7.3 and §7.4 is the same for every kind.
What differs sits behind a small internal interface in `internal/workload`,
with one implementation per kind. Each implementation:
- reads the current count and the field it comes from
- builds the server-side apply request for a target count
- says whether the workload has reached its count
- gives the largest count the kind allows

| Kind | API | Count field | Largest count | Reached when |
|---|---|---|---|---|
| Deployment | `apps/v1` | `spec.replicas`, nil as 1 | int32 max | `status.observedGeneration` ≥ `metadata.generation`, `status.replicas` and `status.readyReplicas` both equal `spec.replicas`, and `status.terminatingReplicas` is 0 where the cluster reports it |
| StatefulSet | `apps/v1` | `spec.replicas`, nil as 1 | int32 max | `status.observedGeneration` ≥ `metadata.generation`, and `status.replicas` (which counts pods until they have gone) and `status.readyReplicas` both equal `spec.replicas` |
| VirtualMachinePool | `pool.kubevirt.io`, the version the cluster prefers (`v1alpha1` today) | `spec.replicas`, nil as 1 | int32 max | `status.replicas` and `status.readyReplicas` both equal `spec.replicas` |
| VirtualMachine | `kubevirt.io/v1` | `spec.runStrategy`: `Halted` is 0, any other value is 1 | 1 | On: `status.ready` is true. Off: `status.printableStatus` is `Stopped` |

#### KubeVirt is optional
- `--kubevirt` is `auto` (the default) or `disabled`. With `auto`, the
  controller checks through discovery at startup which of the KubeVirt kinds
  above are served, and sets up caches, reconcilers and status watches only
  for those. VirtualMachine must be served as `kubevirt.io/v1`. For
  VirtualMachinePool the controller uses whichever version the API server
  prefers. On a cluster without KubeVirt it runs exactly
  as it would without this feature, and logs once that KubeVirt support is
  off.
- About once a minute, on every replica, it checks discovery again. If the set
  of served KubeVirt kinds, or the preferred pool version, has changed,
  because KubeVirt was installed, removed or upgraded, the controller logs
  the change and exits cleanly, and Kubernetes restarts it with the new set.
  controller-runtime can't remove a watch once it has started, and a watch on
  a removed CRD fails continuously, so a restart is the simplest correct
  behaviour.
- If `kubevirt.io/v1` isn't served, VirtualMachines count as absent and the
  controller logs a warning.
- The RBAC rules for the KubeVirt kinds are always installed. Kubernetes
  accepts rules for resources that don't exist, so neither the Helm chart nor
  a future OLM bundle depends on KubeVirt.
- KubeVirt objects are handled as unstructured objects. The controller doesn't
  import `kubevirt.io/api`, so it isn't tied to a KubeVirt release.

#### VirtualMachines
- **Counts are 0 or 1.** The `replicas` annotation has the usual format, but
  every value, including `*`, must be 0 or 1. Anything larger is an
  `InvalidAnnotation`.
- **Turning on.** A count of 1 sets `spec.runStrategy` to `Always`. KubeVirt
  then keeps the VM running and starts it again if the guest shuts itself down
  or crashes, which matches how pods behave. A per-VM choice of
  `RerunOnFailure`, for VMs that should stay off after shutting themselves
  down, can be added later (§13).
- **Other strategies are corrected.** The no-op check in §7.3, step 8,
  compares the field's value, not just the count. A VM left on another
  strategy, such as `Manual`, is set to `Always` when the mode wants it on.
- **`spec.running` isn't supported.** KubeVirt rejects `spec.running` and
  `spec.runStrategy` together, so a VM that still uses the deprecated
  `spec.running` field is counted as `Invalid`, with a Warning event asking
  its owner to switch to `runStrategy`.
- **Turning a VM off shuts the guest down** and loses its memory. The
  scale-to-zero delay applies as for any other kind.
- **`virtctl start` and `virtctl stop` change `spec.runStrategy`**, so on an
  opted-in VM they count as drift and are reverted. Change the ClusterMode
  instead. `virtctl restart` doesn't change `runStrategy` and is unaffected.
- **A paused VM isn't ready**, so in a mode that wants it on, it shows as
  `Scaling` and then `Stalled`.

#### VirtualMachinePools
- Label the pool, not its VMs. A VirtualMachine whose controller owner is a
  VirtualMachinePool is never managed on its own, even if it inherits the
  label from the pool's VM template. The controller skips it, leaves it out
  of status, and emits a Warning event `OwnedByPool`.
- Pools need nothing beyond their per-kind implementation and the rule above,
  which is why v1 supports them alongside single VMs.
- KubeVirt documents pools as `pool.kubevirt.io/v1alpha1`. The controller
  uses the version the cluster prefers, so a newer one is picked up without a
  code change. It touches only `spec.replicas`, `status.replicas` and
  `status.readyReplicas`. If a future version moved those fields, its pools
  would show as `Stalled`, so the problem is visible rather than silent.
- Pools aren't in use yet. The manual check in §11 covers them once one is.

## 8. ArgoCD coexistence

1. Merge this into `argocd-cm`:
   ```yaml
   resource.customizations.ignoreDifferences.apps_Deployment: |
     managedFieldsManagers: [mode-operator]
   resource.customizations.ignoreDifferences.apps_StatefulSet: |
     managedFieldsManagers: [mode-operator]
   resource.customizations.ignoreDifferences.kubevirt.io_VirtualMachine: |
     managedFieldsManagers: [mode-operator]
   resource.customizations.ignoreDifferences.pool.kubevirt.io_VirtualMachinePool: |
     managedFieldsManagers: [mode-operator]
   ```
2. Every Application containing switched workloads sets
   `syncOptions: [RespectIgnoreDifferences=true, ServerSideApply=true]`.
3. Switched workloads set `replicas: 0` in Git, or `runStrategy: Halted` for
   a VirtualMachine. Argo uses this value only when it creates the workload,
   and the live value after that.
4. The `clustermode` label and the user annotations live in Git. The
   controller's annotations don't.
5. Never put a global `jsonPointers: [/spec/replicas]` ignore in place. It
   would freeze replicas for non-switched workloads too.
6. ClusterMode instances are not in Git in v1 (§5.4).
7. Fallback, to be documented and not built: if the installed Argo version
   doesn't honour `managedFieldsManagers` during sync, use per-Application
   `ignoreDifferences` scoped by `name` with `jsonPointers`.

## 9. Repositories and project layout

There are three repos. `ship-core-mode-controller` is the parent: it holds the
devcontainer, this document and the prototype for reference, and has the other
two as git submodules. `mode-operator` holds the controller (§9.1 to §9.4), and
`mode-manager-stub` holds a test stand-in for the mode manager (§9.5). Each
submodule builds, tests and releases on its own, with nothing from the parent.
The parent has no `go.work`: Go finds one in any parent directory, so it would
change which library version mode-operator and the stub build against. The
stub has its own, ignored by git, for building against a local library
checkout (§9.5).

Each submodule holds only what its users need: code, a README, godoc, and
build and CI files. Design rationale stays in this document.

### 9.1 mode-operator layout (Kubebuilder go/v4)

```
PROJECT                           (Kubebuilder project file)
go.mod                            (module github.com/bitasylum/mode-operator)
api/
  v1alpha1/
    groupversion_info.go
    clustermode_types.go
    clustermodedefinition_types.go
    zz_generated.deepcopy.go      (generated)
cmd/
  main.go                         (controller binary)
internal/
  controller/
    workload_controller.go        (one reconciler per workload kind)
    kubevirt_discovery.go         (optional kinds; restart when they change, §7.9)
    decide.go                     (pure, §7.3)
    annotations.go                (parsed user annotations, shared with summarize)
    setup.go                      (cache filter and field indexes, §7.5)
    clustermode_controller.go     (status reconciler)
    summarize.go                  (pure, §7.4)
    metrics.go
    constants.go
    suite_test.go                 (envtest)
  modespec/                       (annotation parser + tests)
  workload/                       (the per-kind interface and implementations, §7.9)
    deployment.go
    statefulset.go
    virtualmachine.go             (unstructured)
    virtualmachinepool.go         (unstructured)
test/
  crds/kubevirt/                  (minimal stand-in KubeVirt CRDs for envtest)
client/                           (module github.com/bitasylum/mode-operator/client, §6.5)
  clustermode/  fingerprint/  startup/
  integration/                    (module of its own: envtest suites, and testenv helpers)
  README.md
config/
  crd/                            (generated)
  rbac/                           (controller, generated from markers; maintainer and viewer roles)
  admission/                      (the transitions ValidatingAdmissionPolicy and its binding, §5.2)
  manager/
  default/
  samples/                        (example ClusterModeDefinition and ClusterMode)
  mode-manager/rbac/              (writer ClusterRole for the mode manager, §6.4)
  argocd/argocd-cm-patch.yaml
  examples/app-deployment.yaml
  examples/argocd-application.yaml
dist/chart/                       (Helm chart; CRDs, role rules and admission policy synced from config/; §9.4)
hack/sync-chart.sh, test-chart.sh
Makefile                          (Kubebuilder targets: manifests, generate, fmt, vet, test, build, docker-build)
Dockerfile
README.md
```

- `config/mode-manager/rbac/` and `client/` are the only mode-manager
  artifacts in mode-operator: the ClusterMode permissions its install needs
  (§6.4), and the library (§6.5).
- `make test-client` runs the library's unit and integration tests, and
  `lint`, `vulncheck` and Dependabot cover all three modules. The image
  leaves out `client/`.

### 9.2 Scaffolding

```sh
kubebuilder init --domain bitasylum.org --repo github.com/bitasylum/mode-operator
kubebuilder create api --group mode --version v1alpha1 --kind ClusterMode \
  --namespaced=false --resource --controller
kubebuilder create api --group mode --version v1alpha1 --kind ClusterModeDefinition \
  --namespaced=false --resource --controller=false
```

Then add the workload reconcilers by hand in `internal/controller`, and trim
the scaffolded editor role into the maintainer role (§5.6).

### 9.3 What happens to the prototype

The gRPC prototype is kept in the parent repo as `reference/modeswitch.zip`.
This document is authoritative where the two differ.

- **Keep and adapt:**
  - `internal/modespec`: its parser already takes the set of known modes. Pass
    the modes of the referenced ClusterMode's definition instead of the
    `--modes` flag,
    and add the delay and settle-timeout rules.
  - The reconcile logic and SSA apply in `internal/controller/reconciler.go`:
    move the decision logic into `decide()`.
  - `constants.go`: replace `LabelEnabled` with the `clustermode` label, and
    add `replicas-changed-at`.
  - The security settings in `deploy/controller.yaml`, the ArgoCD patch, and
    the examples.
- **Remove:** `internal/modesource`, `proto/`, the `generate` target for
  protobuf, the gRPC and protobuf dependencies, `Broadcaster`, `ModeState`, and
  the `--modes`, `--mode-server*` and `--resync-interval` flags.
  `reference/modeswitch.zip` can go once mode-operator v1 lands.

### 9.4 Packaging: Helm now, OLM later

v1 is installed with a Helm chart in `dist/chart/`. The CRDs, the rules of
every role and the admission policy in it are copied from `config/` by
`hack/sync-chart.sh`, which
`make manifests` runs, so they always match controller-gen's output; `make
chart-check` fails in CI if they drift. The rest of the chart is maintained by
hand. Kubebuilder's Helm plugin was the plan, but it only works with
Kubebuilder's default object names (`controller-manager` and so on): with the
names below it merged objects into one file and dropped its values. Its first
output was used as a starting point.

Objects are named `<release>-<component>`, which is `mode-operator-controller`
for the Deployment, ServiceAccount and ClusterRole with the default release
name. Kubebuilder's `manager` names were dropped because they read as
belonging to the mode manager.

The chart contains:
- both CRDs, in `templates/` with `helm.sh/resource-policy: keep` (§5.5),
  behind `crds.install` and `crds.keep` values that default to true
- the controller Deployment (2 replicas, spread over nodes, with a
  PodDisruptionBudget), ServiceAccount, RBAC and leader-election Role, with
  the flags exposed as values, for example `controller.settleTimeout: 10m`
- the maintainer and viewer ClusterRoles (§5.6), with optional bindings for
  the groups in `rbac.maintainers.groups` and `rbac.viewers.groups`
- the mode-manager writer ClusterRole for the names in
  `modeManager.clusterModes` (§6.4)
- the transitions ValidatingAdmissionPolicy and its binding (§5.2), which
  aren't kept on uninstall; the CRD's own rules still apply without them
- the definition admin ClusterRole (§5.6), with an optional binding for
  `rbac.definitionAdmins.groups`, and optionally ClusterModeDefinitions from
  the `definitions` value, so they are kept in Git with the rest of the
  install. They carry `helm.sh/resource-policy: keep`, so they also stay when
  removed from the values. `helm install` can't
  create them in the same release as the CRDs, because it checks every
  object's kind first, so a plain Helm install adds them with a later `helm
  upgrade`. Argo CD applies CRDs first, and the definitions carry
  `SkipDryRunOnMissingResource` so a single sync works.
- optionally, a ServiceMonitor and a NetworkPolicy for the metrics, and a TLS
  Secret (`metrics.certSecret`) to serve metrics with instead of a
  self-signed certificate
- Argo CD renders charts with `helm template` and ignores Helm's
  `resource-policy: keep`. Under `crds.keep`, the CRDs, and the definitions
  always, also carry Argo CD's `Prune=false,Delete=false` sync options, so
  deleting the Argo CD Application can't delete them
- the chart refuses to render more than one replica without leader election
- `hack/test-chart.sh` (`make test-chart`) installs the chart on a new kind
  cluster and checks install, upgrade, a mode change and uninstall

To keep OLM possible later without a redesign:
- `config/` stays the source of truth. Operator SDK builds an OLM bundle from
  the same `config/` and `PROJECT` file that the chart is generated from.
- Nothing at runtime depends on Helm: no Helm hooks, no `lookup`, and no
  install-time jobs. All configuration is flags on the Deployment.
- The controller watches all namespaces, which matches OLM's `AllNamespaces`
  install mode.
- v1 has no webhooks. If a later API version needs a conversion webhook, build
  it so that OLM can manage its certificates, as Operator SDK documents.
- Release the image, chart and CRD together under one semantic version, which
  OLM's upgrade graph needs.
- The Image workflow publishes `ghcr.io/bitasylum/mode-operator` for amd64
  and arm64: `main` and `sha-<commit>` from every push to main (development
  builds), and `<version>` and `<major>.<minor>` from `v<semver>` tags, which
  must be on main. No `latest`. Each image has an SBOM, BuildKit provenance
  and a signed GitHub attestation. Publishing the chart (for example as an
  OCI artifact) is still to do.

### 9.5 Stub mode manager (`mode-manager-stub`)

A test tool in its own repo, module `github.com/bitasylum/mode-manager-stub`.
It stands in for the real mode manager so that mode-operator can be exercised
end to end, and it is the reference consumer of the client library (§6.5): it
does everything through the library, as the real mode manager would.

- **Behaviour:**
  1. On start, `startup.Run` decides the mode (§6.3), retrying with backoff.
  2. Without the CRDs, it holds the mode in memory, checks discovery at
     `--discovery-interval`, and calls `startup.Persist` once they appear
     (rule 8).
  3. While running, a `fingerprint.Keeper` keeps the fingerprint current, and
     every watch event goes to `Writer.Observe`. Changes by others are
     adopted through `OnChange` and never written back (rule 9).
  4. If the ClusterMode is deleted, it writes the current mode again,
     retrying with backoff until it's back. If the CRDs are removed, it goes
     back to step 2.

  A mode set while the CRDs are missing must be a valid mode name, and in
  `--known-modes` if that is set: nothing else checks it before it is
  written. Mode changes, the start of a move and the switches between phases
  are serialized, so the mode written when the CRDs appear is the one held
  last. Changes are refused while starting and while a move runs. A move
  waits at most `--move-step-timeout` (10 minutes) for each mode to settle,
  and `DELETE api/move` stops it. If the mode is written once the CRDs appear
  but the fingerprint isn't, the stub doesn't write the mode again, which
  would undo changes made meanwhile: the keeper saves the fingerprint, with
  its own retries.
- **Web page and JSON API,** served with `net/http` and embedded static
  files, with no JavaScript framework and relative URLs, so it also works
  through the API server's service proxy. The page offers the modes the rules
  allow next (`Writer.Set`), a move in as many steps as the transitions need
  (`Writer.MoveTo`, in the background), and a free-text write that leaves
  the checking to the API server (`Writer.Apply`). It shows the ClusterMode's
  status, the startup decision, the last adopted change and the fingerprint.
  The API is `GET api/state`, `POST api/mode {mode, check}` and
  `POST api/move {target}`; a refused change answers with the library's
  reason, and a change while busy or starting with 409.
- **Flags:** `--cluster-mode`, `--definition`, `--writer-label`,
  `--builtin-default`, `--known-modes`, `--namespace`,
  `--fingerprint-configmap`, `--discovery-interval`, `--debounce`,
  `--move-step-timeout`, `--listen` and `--kubeconfig`, checked at startup so
  a value the API server would refuse doesn't leave the stub retrying. The
  chart sets them from values, except four: the
  namespace is the release's, the fingerprint ConfigMap's name follows from
  the ClusterMode's (so its Role can name it), the listen address is the
  container port, and the in-cluster config replaces a kubeconfig. A values
  schema rejects malformed durations and names, and a role to bind that
  isn't a `clustermode-writer`.
- **Left out on purpose:** gRPC and leader election. It runs one replica with
  `strategy: Recreate` (rule 5).
- **Test clusters only.** The page has no login. Its Service is ClusterIP,
  reached with `kubectl port-forward` or the API server's service proxy;
  never expose it through an Ingress.
  - A NetworkPolicy, on by default, denies all ingress except from the peers
    in `networkPolicy.from`. Port-forward isn't affected, and a NetworkPolicy
    can't block the pod's own node, which kubelet's probes need. On a
    single-node cluster that includes the API server; on more nodes, the
    service proxy needs the API server's addresses listed, which depend on
    the network plugin. It needs a network plugin that enforces
    NetworkPolicy, as kind's does.
  - POSTs must be `application/json`, and Go's `CrossOriginProtection`
    refuses cross-origin ones, so a page on another site can't change the
    mode through a tester's port-forward.
  - Responses carry a strict Content-Security-Policy, and request bodies are
    limited to 4 KiB.
- **RBAC:** the ServiceAccount is bound to mode-operator's writer ClusterRole
  (§6.4), and the stub's chart grants the rest of §6.4.
- **Packaging:** a distroless, non-root image built for amd64 and arm64, and
  a small Helm chart. The Image workflow publishes
  `ghcr.io/bitasylum/mode-manager-stub` the same way as mode-operator's
  (§9.4).
- **Building against the library:**
  - `go.mod` pins the library to a mode-operator commit, or later a
    `client/vX.Y.Z` tag, and CI builds and tests against that. `make
    check-no-replace` keeps `replace` directives out of `go.mod`.
  - For local work, `make workspace` writes a `go.work`, ignored by git, that
    uses `../mode-operator/client`. `make docker-build` then passes the
    library as a named build context: the Dockerfile has an empty `library`
    stage that the context overrides, and it runs `go work init` only when
    the library is there.
  - `make verify-published` builds and tests with `GOWORK=off`, as CI does.
    `make docker-build` labels the image with the library it has, and `make
    docker-push` refuses an image built against a local checkout.
- **Unit tests** run the manager on client-go's fake clients: the CRDs
  removed and reinstalled with a mode set in between, a deleted ClusterMode
  recreated after a failed attempt, a failed fingerprint save once the CRDs
  appear, a move stopped and one timed out, the checks on changes, and a
  snapshot with no nulls for the page. The flags are checked too.
- **End-to-end tests** (`make test-e2e`) create a two-node kind cluster with
  a kubeconfig of their own, and build mode-operator's image and chart from
  `../mode-operator` or, in CI, a clone of the commit `go.mod` pins. The
  script refuses to run if a cluster of that name exists, and marks the
  cluster it creates with a random run ID; the tests refuse to run unless the
  kubeconfig's context is that cluster's and it carries the ID. Helm's and
  kubectl's own settings that could point elsewhere are ignored. Ginkgo
  specs, in order:
  1. Before the CRDs exist, the stub holds the mode in memory.
  2. A pod in another namespace can't connect to the stub, while the API
     server's service proxy can.
  3. Once mode-operator and the definition are installed, it writes the mode
     and the fingerprint.
  4. A missing ClusterMode is created in the default mode, with `ModeReset`.
  5. A mode change scales the test workload.
  6. A transition the rules don't allow is refused, with and without the
     check, and a move goes through the intermediate mode.
  7. A maintainer's `kubectl patch` is adopted and not written back.
  8. A change by another writer is adopted: the test impersonates a
     ServiceAccount with the orchestration server's role (§5.4) and uses
     `NewWriter`, as the orchestration server will.
  9. A deleted ClusterMode is written again in the current mode.
  10. With the ClusterMode CRD deleted, the stub holds a mode set meanwhile,
      and writes it once a `helm upgrade` has put the CRD back.
  11. A restart keeps the mode, with `ModeRestored`.
  12. A full reboot resets to the default mode. kind nodes share the host's
      boot ID, so the test rewrites the stored boot IDs instead.

  Each kind node uses inotify instances from one per-user limit, so with
  other kind clusters running, `fs.inotify.max_user_instances` needs to be
  512 or more; the script warns when it is lower.

## 10. Tech stack

- Go: the version the pinned k8s.io libraries require, 1.23 at minimum
- Kubebuilder (go/v4 layout), with controller-runtime and the matching k8s.io
  libraries pinned to the latest mutually compatible stable versions
- controller-gen for CRDs, deepcopy and RBAC markers
- envtest (via setup-envtest) for integration tests
- `prometheus/client_golang` for metrics
- Helm for packaging (§9.4), tested with Helm 4.3; Helm 3.8 or later should
  work
- KubeVirt kinds are handled as unstructured objects. Don't import
  `kubevirt.io/api`.
- The client library (§6.5) depends only on the k8s.io libraries; its
  integration tests use envtest and controller-runtime in a module of their
  own. The stub's end-to-end tests use Ginkgo and Gomega, and kind.
- No other new dependencies. The gRPC and protobuf dependencies are removed.

## 11. Testing

### Unit tests
- `modespec`: table tests covering every rule in §7.2, including the delay
  and settle-timeout rules.
- `decide()`: immediate scaling, pending, still pending, expired, cancelled,
  zero-to-zero mode moves, a future `since`, a malformed `since`,
  `replicas-changed-at` changing only when replicas change, and an invalid
  delay. An invalid delay holds a scale to zero without starting a countdown
  and keeps an existing `zero-pending-since`, but never blocks a scale-up or a
  scale-down to a non-zero count.
- Each workload kind (§7.9): reading the count, building the apply request,
  and "reached". Include a VM left on another strategy being set to `Always`,
  `spec.running`, pool-owned VMs, and the 0-or-1 limit.
- The KubeVirt discovery check reports a change when a kind appears or
  disappears, or the preferred pool version changes, and not otherwise.
- `summarize()`:
  - every workload state, including the `Stalled` timeout boundary and the
    fallback of `since` to `modeChangedAt`
  - an invalid delay counting as `Invalid` only while it blocks a scale to
    zero, and always counting in `invalidAnnotations`
  - a workload's own `settle-timeout`, and an invalid one falling back to
    `--settle-timeout`
  - `Invalid` workloads left out of the replica sums, and counts that add up
    to `total`
  - the order of the unsettled list and its cap of 20
  - every condition and reason, including `NoWorkloads`, `InvalidMode` and
    `DefinitionNotFound`
  - `status.modes` copied from the definition, and kept when the definition is
    deleted
  - `modeChangedAt` and the mode-change counter changing only when the mode
    changes
  - the `RequeueAfter` for the earliest stall deadline
  - `modeSetBy` taken from the field manager that owns `spec.mode`

### Integration tests (envtest)

envtest runs only the API server. There are no Deployment or StatefulSet
controllers and no kubelet, so tests write workload `.status` themselves to
simulate pods becoming ready.

Controller:
- Install the CRDs, create a ClusterModeDefinition, start the controller, and
  create labelled and unlabelled Deployments and StatefulSets.
- Create ClusterMode `default` and assert replicas on labelled workloads only.
- Change the mode and assert the new replicas.
- Add a mode to the ClusterModeDefinition and switch to it. Then remove a mode
  that a workload still names, and assert the workload becomes `Invalid` with
  its replicas unchanged.
- A ClusterMode whose definition doesn't exist reports `DefinitionNotFound`
  and changes nothing. Creating the definition then applies the mode.
- Maintainer edit: patch `spec.mode` with a different field manager, and assert
  the new replicas and `status.modeSetBy`. A later apply by `mode-manager`
  with `Force: true` takes the mode back.
- Create two ClusterModes in different modes and assert that each workload
  follows the one its label names. Change a workload's label, then assert it
  moves and both ClusterModes' counts update.
- A workload referencing a missing ClusterMode gets a `ClusterModeNotFound`
  Warning and keeps its replicas. Creating the ClusterMode then applies them.
- Assert that `managedFields` shows `mode-operator` owning
  `f:replicas`.
- Delayed zero: use a delay of about 2s, assert replicas are held, then 0.
  Change the mode back mid-delay and assert the scale-down is cancelled and
  the pending annotation removed.
- Invalid `replicas` annotation: assert a Warning event, unchanged replicas,
  the workload counted as `invalid`, and `Degraded=True`.
- Invalid `scale-to-zero-delay`: assert a Warning event and `Degraded=True`
  straight away. Moving between non-zero modes still scales the workload up
  and down. Moving to a zero mode holds it at its current replicas, counted as
  `invalid`. Fixing the annotation lets the delay run.
- Status: after a mode change, assert the workloads show as `Applying` or
  `Scaling`, then `Settled` once the test sets `readyReplicas`. With
  `--settle-timeout` of a few seconds and no ready pods, assert `Stalled`,
  `Degraded=True`, and an entry in `status.unsettled`.
- Delete the ClusterMode: assert no workload changes (fail-static).
- Manual `kubectl scale` drift is reverted.
- Remove a workload's label: assert its replicas are left alone and it drops
  out of the status counts.
- Without KubeVirt CRDs: the controller starts and runs with no errors, and
  only the Deployment and StatefulSet reconcilers exist.
- With the stand-in KubeVirt CRDs from `test/crds/kubevirt/` (minimal schemas
  that keep unknown fields):
  - a VirtualMachine is halted and started with the mode (`Halted`, then
    `Always`), and a count above 1 is rejected
  - a VirtualMachinePool scales
  - a VM owned by a pool is skipped
  - both kinds show up in status once the test writes their status fields

CRD contract (the test plays the mode manager, a maintainer and an admin).
Write these first, because they prove the write-time check in §5.2:
- Server-side apply of `spec.mode` and `spec.definition` with field manager
  `mode-manager` creates the ClusterMode, and a second apply changes the mode.
- Once the controller has filled `status.modes`, the API server rejects a
  change to a mode outside it, whether from the mode manager or a maintainer.
  A status update by the controller is still accepted when the current mode
  has just been removed from the definition.
- The CRDs reject a change to `spec.definition`, an empty or duplicate
  `spec.modes`, a badly formed mode name, a `defaultMode` that isn't in
  `modes`, and a ClusterMode name longer than 63 characters.
- After the controller has reconciled, `managedFields` shows `mode-manager` as
  the only owner of `spec.mode` and `spec.definition`, and `mode-operator` as
  the only owner of `status`.
- Transitions: both suites install `config/admission/` and wait until the API
  server enforces it. The CRDs reject a transition to or from a mode outside
  `modes`, a rule with no `to`, a definition without a rule for
  `defaultMode`, and an empty `transitions` list. A ClusterMode moves only
  along the rules and only once Ready; the default mode is always reachable.
  The admission policy rejects a move on a Ready status from before the last
  change of mode (to the default mode and back), and accepts it once
  `status.observedGeneration` has caught up. With the controller running, a
  definition that drops its transitions clears `status.transitions` and
  lifts the restrictions.

Client library (`client/integration`, as users bound to the roles from
`config/`, with the test writing status in the controller's place):
- The mode manager's writer creates the ClusterMode with all three fields,
  owned by `mode-manager`. Another writer can't create it, owns `spec.mode`
  after its write, and `mode-manager` takes it back.
- A viewer and another writer get the rules from the ClusterMode alone.
  `Rules` falls back to the definition before the first status write, and
  reports `RulesUnavailable` when it may not read the definition. After a
  definition edit, `Check` follows the status copy until it catches up.
- The watch works under `resourceNames`, and ends when the CRDs go.
- Each pre-check reason, including `NotSettled` from the admission policy; a
  raw rejection classified by re-reading; `MoveTo` in two steps, and
  interrupted by a maintainer's patch; `WaitSettled`.
- The fingerprint store and keeper on Nodes with boot IDs.
- The startup write order: a user who may not patch the ConfigMap makes `Run`
  fail after resetting the ClusterMode, and a second `Run` still decides a
  full reboot.

### Manual checklist (README)
- Until the real mode manager is ready, use the stub (§9.5) to set modes for
  these checks.
- After an Argo sync, the Application is `Synced` with controller-chosen
  replicas.
- `kubectl get clustermode` shows the mode, conditions and counts, and
  `-o wide` shows the replica sums.
- With the real mode manager, checking its side of §6.1:
  - Restarting the mode-manager pod restores the mode.
  - Rebooting all nodes results in `standby`. If it uses the design in §6.3,
    simulate this by editing the fingerprint ConfigMap to unknown bootIDs and
    restarting it.
  - A mode-manager rollout never runs two instances at once.
  - With the CRD absent, it still serves gRPC, and it creates its ClusterMode
    once the CRD is installed.
- A maintainer's `kubectl patch` changes the mode, and the mode manager adopts
  it and broadcasts it over gRPC.
- `helm upgrade` upgrades both CRDs, and `helm uninstall` leaves the CRDs,
  every ClusterModeDefinition and every ClusterMode in place.
- Neither the mode manager's nor mode-operator's ServiceAccount can create,
  change or delete a ClusterModeDefinition.
- `kubectl auth can-i` with `--as=system:serviceaccount:<ns>:<sa>` for each
  ServiceAccount, and with `--as`/`--as-group` for a maintainer, confirms the
  permissions in §12.
- The offboarding procedure in §7.8 works on a real Argo-managed workload.
- On a cluster with KubeVirt: VMs halt and start with the mode, a pool scales,
  and `virtctl stop` on an opted-in VM is reverted. Installing or removing
  KubeVirt restarts the controller once, after which it carries on.

## 12. Acceptance criteria

- [ ] `make manifests generate build test` passes cleanly, and `go vet` is clean.
- [ ] Every unit and envtest case in §11 passes.
- [ ] With no KubeVirt CRDs installed, the controller starts and runs without
      errors. Nothing imports `kubevirt.io/api`.
- [ ] Write permissions match §5.6, §6.4 and §7.7: the mode manager can't
      write `clustermodes/status`, Deployments or StatefulSets; maintainers
      can change ClusterMode `spec` but can't create, delete or write status;
      the controller can't write ClusterMode `spec` or ConfigMaps; and only
      admins can write ClusterModeDefinitions.
- [ ] Nothing in the controller references gRPC or a compiled-in mode list;
      mode names come only from ClusterModeDefinitions. Everything
      listed under "Remove" in §9.3 is gone.
- [ ] Nothing in mode-operator imports mode-manager code; the CRDs are the only
      interface §6 relies on.
- [ ] `make test-client` passes, the client module's `go.mod` has nothing
      beyond the k8s.io libraries, and the stub's `make test-e2e` passes
      against the library version its `go.mod` pins.
- [ ] The README documents the workload contract, the mode-manager contract,
      how to add and remove modes, maintainer overrides, ClusterMode status
      and conditions, ArgoCD setup,
      metrics and suggested alerts, reset semantics, offboarding, and the CRD
      lifecycle warning.
- [ ] Example manifests apply cleanly with `kubectl apply --dry-run=server`.
- [ ] `helm lint` passes, the chart's rendered output applies cleanly with
      `kubectl apply --dry-run=server`, and its CRD and RBAC match `config/`.

## 13. Future extensions (designed for, not built in v1)

None of these needs a breaking change.

| Future need | How to add it |
|---|---|
| More mode managers, each with its own ClusterMode | Deploy another mode manager that meets §6.1, with its own ClusterMode name, using a shared or its own ClusterModeDefinition, and its own fingerprint ConfigMap if it uses §6.3. No controller change. |
| Mode manager with more than one replica | Leader election with a Lease; only the leader writes. The contract already requires one writer at a time (§6.1, rule 5), so nothing in mode-operator changes. |
| Mode manager moves into this project | The client library already implements §6.1 and §6.3 (§6.5); the mode manager's own code moves in beside it. |
| Maintainer override that lasts until cleared | An annotation on the ClusterMode, for example `mode.bitasylum.org/hold: "true"`, that tells the mode manager not to write until it is removed. The controller doesn't change. |
| Strictly one mode manager per ClusterMode | The writer role already limits `create` by name (§6.4), so a mode manager can create and write only the ClusterModes its role names. Binding each mode manager to a role that names only its own ClusterModes gives one mode manager per object. To enforce that rather than rely on the bindings, an admission policy checks the `writer` label against the requesting ServiceAccount. |
| A mode manager confined to one namespace | Add a namespaced `Mode` kind and a label `mode.bitasylum.org/mode` that resolves in the workload's namespace. |
| A ClusterMode refusing workloads outside its scope | Add optional `spec.allowedNamespaces`. Workloads outside it get a Warning and are left unchanged. |
| Protect definitions in use | A finalizer that blocks deleting a ClusterModeDefinition while a ClusterMode uses it. |
| Controller-assisted offboarding | A release path that hands `spec.replicas` ownership back, replacing the manual procedure in §7.8. |
| A per-VM run strategy | An optional annotation, for example `mode.bitasylum.org/run-strategy: RerunOnFailure`, for VMs that should stay off after shutting themselves down. VMs without it keep `Always`. |
| Limit what the controller can change | An optional ValidatingAdmissionPolicy (GA in Kubernetes 1.30) that lets the controller's ServiceAccount change only `spec.replicas`, `spec.runStrategy` and the `mode.bitasylum.org/` annotations of workloads. Its RBAC allows any patch, so this would contain a compromised controller. |
| Other kinds, such as VirtualMachineInstanceReplicaSet | Another implementation of the per-kind interface (§7.9). |
| Break-before-make transitions | A per-rule `strategy: BreakBeforeMake` on `transitions`. The status reconciler drives a phase stored in ClusterMode status: first only decreases are applied, then, once every one has finished (its pods gone), the increases. Open choices: whether pending scale-to-zero delays hold up the increases, whether to carry on after the settle timeout, and restarting the phases when the mode changes mid-way. Each workload's `terminationGracePeriodSeconds` (times its replicas for an OrderedReady StatefulSet) bounds how long its part takes. Ordered waves per workload would generalise it. |
| An override for transitions | An annotation on the ClusterMode that skips the transition rules for one write, for emergencies. RBAC can't limit who sets an annotation, so restricting it to a group needs a ValidatingAdmissionPolicy. |
| Delays on non-zero scale-downs, such as 6 to 3 | Add a separate optional annotation, for example `scale-down-delay`, with its own controller-owned pending marker. `scale-to-zero-delay` keeps its meaning, so existing workloads are unaffected. |
| OLM distribution | Generate an Operator SDK bundle from `config/` and the `PROJECT` file. §9.4 lists the choices that keep this open. |

## 14. Open questions (ask; don't guess)

Each has a v1 default to use until it is answered.

1. For the mode-manager team: when the CRD is installed but a write fails, for
   example because the API server is unreachable, should the mode manager
   still broadcast? v1 default: no; it keeps the previous mode and retries, so
   workloads and gRPC subscribers stay on the same mode (§6.1, rule 2).
2. For the mode-manager team: should a long outage with no reboot also reset
   to `standby`? v1 default: no.
3. For the mode-manager team: if the definition's default mode isn't one the
   mode manager knows (the library's `KnownModes`), what should it start in?
   v1 default: its built-in default mode. If the definition doesn't have that
   mode either, the API server rejects the write, and startup retries until
   the definition or the mode manager changes.
