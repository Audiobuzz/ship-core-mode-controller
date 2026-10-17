#!/usr/bin/env bash
# Everything the container needs beyond the base image + features.
# Runs once per container creation (postCreateCommand); safe to re-run.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "== Claude Code config -> ~/.claude (bind-mounted, survives rebuilds)"
# ~/.claude is bind-mounted from the host, but Claude Code's config file is
# ~/.claude.json — a *sibling* of that directory, not inside it. So it lands
# on the container overlay and dies with the container, taking the model
# access cache and per-project settings with it: every rebuild starts from a
# blank config. Keep the real file inside the mount and link it back.
# Shared with every other devcontainer using the same ~/.claude, which is the
# point — the caches populate once, not once per project.
CLAUDE_CFG="$HOME/.claude/claude.json"
if [ -f "$HOME/.claude.json" ] && [ ! -L "$HOME/.claude.json" ]; then
  # A container-local config already exists: migrate it rather than lose it.
  if [ ! -f "$CLAUDE_CFG" ]; then mv "$HOME/.claude.json" "$CLAUDE_CFG"; fi
  rm -f "$HOME/.claude.json"
fi
[ -f "$CLAUDE_CFG" ] || echo '{}' > "$CLAUDE_CFG"
ln -sf "$CLAUDE_CFG" "$HOME/.claude.json"

echo "== git submodules (mode-operator, mode-manager-stub)"
# Non-fatal: the container may not be able to reach GitHub, and a clone made
# on the host with --recurse-submodules already has them.
git -C "$REPO" submodule update --init --recursive || \
  echo "   (could not update submodules; run 'git submodule update --init' once git can reach GitHub)"

echo "== kind -> ~/go/bin"
# Local Kubernetes clusters in Docker, for running mode-operator end to end.
# ~/go/bin is on PATH via the Go feature.
go install sigs.k8s.io/kind@latest

echo "== kubebuilder -> /usr/local/bin"
# Scaffolds mode-operator (docs/design.md, section 9.2). setup-envtest comes
# from the Makefile kubebuilder generates, so it is not installed here.
curl -fsSL -o /tmp/kubebuilder \
  "https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)"
sudo install -m 0755 /tmp/kubebuilder /usr/local/bin/kubebuilder
rm -f /tmp/kubebuilder

echo "== k9s -> /usr/local/bin"
# Terminal UI for the kind clusters. The latest release, checked against the
# checksums published with it.
K9S_TAG="$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/derailed/k9s/releases/latest)"
K9S_URL="https://github.com/derailed/k9s/releases/download/${K9S_TAG##*/}"
K9S_ASSET="k9s_Linux_$(go env GOARCH).tar.gz"
K9S_TMP="$(mktemp -d)"
curl -fsSL -o "$K9S_TMP/$K9S_ASSET" "$K9S_URL/$K9S_ASSET"
curl -fsSL -o "$K9S_TMP/checksums.sha256" "$K9S_URL/checksums.sha256"
(cd "$K9S_TMP" && grep " $K9S_ASSET\$" checksums.sha256 | sha256sum -c --quiet -)
tar -xzf "$K9S_TMP/$K9S_ASSET" -C "$K9S_TMP" k9s
sudo install -m 0755 "$K9S_TMP/k9s" /usr/local/bin/k9s
rm -rf "$K9S_TMP"

echo "== shellcheck (apt)"
# For linting this repo's scripts by hand and in the editor. mode-operator's
# make shellcheck downloads its own pinned copy, as its CI does.
sudo apt-get update -qq
sudo apt-get install -y -qq shellcheck

echo "== npm globals"
# codex: the second-opinion reviewer (.claude/tools/codex-review.sh).
# Drop it if you will not use a second model family.
npm install -g @openai/codex

echo "== post-create done"
