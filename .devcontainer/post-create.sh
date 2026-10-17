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

echo "== apt packages"
sudo apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
  software-properties-common
# KiCad 10 from the project PPA — Ubuntu 24.04's archive stops at 7.0.11,
# and hardware/lib/pcb is validated against 10 (see its WORKFLOW.md).
# Core and libraries must come from the SAME source or footprint formats
# skew: the 7 -> 10 jump moved three conventions this library parses, and
# every one is now pinned by kicadfacts' selftest.
sudo add-apt-repository -y ppa:kicad/kicad-10.0-releases
sudo apt-get update -qq
# kicad without --no-install-recommends: the recommends carry runtime bits.
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
  python3-pip python3-venv \
  poppler-utils \
  kicad kicad-symbols kicad-footprints kicad-templates

echo "== ~/.platformio volume ownership"
# The named-volume mount is created root-owned on first use.
sudo mkdir -p "$HOME/.platformio"
sudo chown "$(id -un):$(id -gn)" "$HOME/.platformio"

echo "== workspace venv (.venv): platformio + the host package"
python3 -m venv "$REPO/.venv"
"$REPO/.venv/bin/pip" install -q -U pip
"$REPO/.venv/bin/pip" install -q \
  platformio==6.1.19 pyserial pytest pymupdf pyyaml \
  -e "$REPO/host"

echo "== CAD venv (hardware/.venv-cad)"
# Separate from the workspace venv on purpose: the OCP kernel is ~1 GB and
# has no business in the environment that runs the firmware toolchain.
python3 -m venv "$REPO/hardware/.venv-cad"
"$REPO/hardware/.venv-cad/bin/pip" install -q -U pip
"$REPO/hardware/.venv-cad/bin/pip" install -q -r "$REPO/hardware/requirements-cad.txt"

echo "== Zener pcb CLI (diodeinc/pcb) -> ~/.local/bin"
# Vendor installer: downloads a checksummed static binary and manages its
# own versioned toolchain under ~/.local/share/pcb. Needs KiCad 10 (above)
# only for `pcb layout`; build/validate is self-contained.
# Drop this block if you will capture schematics some other way.
curl -fsSL https://raw.githubusercontent.com/diodeinc/pcb/main/install.sh | bash

echo "== FreeRouting 2.4.1 (autorouting; hardware/lib/pcb/WORKFLOW.md 'Routing')"
# The pin lives in ONE place, pcb/freeroute.py: version, URL and sha256, with the
# linux-x64 bundle's own Java runtime (upstream wants Java 25; apt has 21). This
# installs it where that module looks, and keeps an install that is already there.
#
# It cannot RUN here yet: freeroute refuses to start FreeRouting without a
# network-less namespace, and this container's seccomp profile blocks unshare.
# See hardware/buckboost-test/change-requests.md CR-004 for what would fix that.
(cd "$(dirname "$0")/../hardware/lib" && python3 -m pcb.freeroute --fetch) || \
  echo "   (FreeRouting not installed — run 'cd hardware/lib && python3 -m pcb.freeroute --fetch' later)"

echo "== npm globals"
# codex: the second-opinion reviewer (.claude/tools/codex-review.sh).
# Drop it if you will not use a second model family.
npm install -g @openai/codex

# ---------------------------------------------------------------------
# OPTIONAL blocks — uncomment what this project actually needs. Each one
# costs minutes on every container rebuild, so an unused block is a tax.
#
# LaTeX, for a paper:
# sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
#   texlive-latex-base texlive-latex-recommended texlive-latex-extra \
#   texlive-pictures texlive-fonts-recommended biber latexmk lmodern
#
# Headless browser, for screenshotting a UI:
# npm install -g playwright && playwright install --with-deps chromium
#
# Android SDK + JDK, for a companion app: keep the version pins in a
# script next to the app that needs them, and call it from here.

echo "== post-create done"
