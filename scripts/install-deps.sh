#!/usr/bin/env bash
# Install pinned external tools (linux x86_64) into BIN_DIR, verifying each
# archive against a SHA-256 pinned in this file. Pins and licenses are recorded
# in docs/architecture/upstream-components.md.
#
# usage: scripts/install-deps.sh [BIN_DIR] [tool...]   tools: gitleaks codebase-memory-mcp (default: all)
set -euo pipefail

BIN_DIR="${1:-$HOME/.local/bin}"; shift || true
TOOLS=("$@"); [ ${#TOOLS[@]} -eq 0 ] && TOOLS=(gitleaks codebase-memory-mcp)

GITLEAKS_VERSION=8.30.1
GITLEAKS_SHA256=551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb
CBM_VERSION=0.11.0
CBM_SHA256=032b33c1833919a2d1de67ff6367fa6ea46aee8689c86ef223c88fae3b6e4536

[ "$(uname -s)-$(uname -m)" = "Linux-x86_64" ] || { echo "only linux x86_64 is pinned; install manually" >&2; exit 1; }
mkdir -p "$BIN_DIR"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

fetch() { # url sha256 dest
  curl -fsSL --retry 3 -o "$3" "$1"
  echo "$2  $3" | sha256sum -c --quiet - || { echo "checksum mismatch for $1" >&2; exit 1; }
}

for t in "${TOOLS[@]}"; do
  case "$t" in
    gitleaks)
      fetch "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" "$GITLEAKS_SHA256" "$tmp/gl.tgz"
      tar -xzf "$tmp/gl.tgz" -C "$tmp" gitleaks
      install -m 0755 "$tmp/gitleaks" "$BIN_DIR/gitleaks"
      ;;
    codebase-memory-mcp)
      fetch "https://github.com/DeusData/codebase-memory-mcp/releases/download/v${CBM_VERSION}/codebase-memory-mcp-linux-amd64.tar.gz" "$CBM_SHA256" "$tmp/cbm.tgz"
      mkdir -p "$tmp/cbm" && tar -xzf "$tmp/cbm.tgz" -C "$tmp/cbm"
      install -m 0755 "$(find "$tmp/cbm" -type f -name codebase-memory-mcp | head -1)" "$BIN_DIR/codebase-memory-mcp"
      ;;
    *) echo "unknown tool: $t" >&2; exit 1 ;;
  esac
  echo "installed $t -> $BIN_DIR/$t"
done
