# ship-core-mode-controller

Design and development environment for mode switching: scaling Kubernetes
workloads and KubeVirt VMs to match the cluster's current mode.

| Path | What it is |
|---|---|
| [docs/design.md](docs/design.md) | The design specification |
| [mode-operator/](mode-operator/) | Submodule: the Kubernetes operator, and the Go client library for its ClusterModes ([bitasylum/mode-operator](https://github.com/bitasylum/mode-operator)) |
| [mode-manager-stub/](mode-manager-stub/) | Submodule: a test stand-in for the mode manager, built on the client library, with a web page for setting the mode ([bitasylum/mode-manager-stub](https://github.com/bitasylum/mode-manager-stub)) |
| [.devcontainer/](.devcontainer/) | Development container with Go, kubebuilder, kubectl, Helm, kind, k9s and Docker |
| [reference/modeswitch.zip](reference/modeswitch.zip) | The earlier gRPC prototype, kept until mode-operator v1 lands |

## Getting the code

```sh
git clone --recurse-submodules git@github.com:Audiobuzz/ship-core-mode-controller.git
```

In a clone made without that flag:

```sh
git submodule update --init
```

The submodule URLs in `.gitmodules` are relative, so they follow however this
repo was cloned, over SSH or HTTPS.

## Working with submodules

Each submodule is its own repo, with its own history, CI and releases, and
must build without anything from this repo. A change takes two commits:

1. Commit in the submodule, and push it.
2. Here, commit the submodule's new position: `git add mode-operator` (or
   `mode-manager-stub`), then `git commit`.

Push the submodule before this repo, so the commit this repo points to exists
on GitHub.
