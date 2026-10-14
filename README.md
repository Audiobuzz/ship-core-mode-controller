# modeswitch-controller

Scales opted-in Deployments/StatefulSets according to a mode streamed from a
gRPC server.

## Opting a workload in

```yaml
metadata:
  labels:
    modeswitch.example.io/enabled: "true"
  annotations:
    modeswitch.example.io/replicas: "normal=3,peak=6,*=0"
```

- `mode=N` per mode; `*=N` for unlisted modes; unlisted with no `*` => 0.
- Unknown mode names or bad syntax => Warning event on the workload, replicas untouched.
- Label present, annotation missing => Warning event, replicas untouched.
- `modeswitch.example.io/scale-to-zero-delay: "5m"` (Go duration, optional):
  when the mode wants 0, hold current replicas for this long first. The start
  time is persisted in `modeswitch.example.io/zero-pending-since`, so the
  countdown survives restarts/failover. Moving to a mode with replicas > 0
  cancels it; moving between two zero modes does not restart it.
- Removing the label stops management (current replicas are left as-is).

## Behaviour

- No changes until the first mode arrives (startup and leader failover).
- Stream drop => last mode held, reconnect with backoff (1s..30s), keepalives on.
- Drift (someone edits replicas) is reverted on the next watch event.
- Writes via server-side apply as field manager `modeswitch-controller`,
  owning `spec.replicas` and the `modeswitch.example.io/applied-mode` annotation.
- Metrics: `modeswitch_current_mode{mode}`, `modeswitch_mode_changes_total`.

## ArgoCD

1. Merge `deploy/argocd-cm-patch.yaml` into `argocd-cm`.
2. Every Application containing switched workloads needs
   `syncOptions: [RespectIgnoreDifferences=true]`.
3. Set `replicas: 0` in Git for switched workloads (creation-time only).

Verify after first sync: the app should be Synced with the controller-chosen
replica count, and `kubectl get deploy X --show-managed-fields -o yaml` should
show `modeswitch-controller` owning `f:replicas`.

If your Argo version doesn't honour `managedFieldsManagers` during sync, fall
back to per-Application `ignoreDifferences` scoped by `name` with
`jsonPointers: [/spec/replicas]`. Don't make that global: it would freeze
replicas for every non-switched workload too.

## Build

```sh
make tools generate build test
```

The proto in `proto/` is a placeholder. Swap in your application's real proto
and adapt `watch()` in `internal/modesource/grpc.go`; nothing else touches
generated types. The server must send the current mode on subscribe.
