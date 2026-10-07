#!/usr/bin/env bash
# Install BoundedCode for the current user:
#
#   curl -fsSL https://raw.githubusercontent.com/akynte/boundedcode/main/scripts/install.sh | bash
#
# Installs `boundedcode` and its short name `bcode` into ~/.local/bin (no
# root needed). A release binary is used when the release publishes one,
# verified against the release's SHA256SUMS; otherwise the source is built
# with Go. Then run `bcode` in a project: the first run sets up the rest
# (inference server, model, tools, sandbox), asking before each download.
#
# env: BC_VERSION   release tag or git ref to install (default: the newest
#                   release with a binary, else main)
#      BC_BIN_DIR   install directory (default: ~/.local/bin)
#      BC_REPO      GitHub repository (default: akynte/boundedcode)
#      BC_GIT_URL   git URL to build from (default: the GitHub repository)
set -euo pipefail

REPO="${BC_REPO:-akynte/boundedcode}"
GIT_URL="${BC_GIT_URL:-https://github.com/$REPO.git}"
BIN_DIR="${BC_BIN_DIR:-$HOME/.local/bin}"
VERSION="${BC_VERSION:-}"
ASSET="boundedcode-linux-amd64"

say() { printf '\033[1;35m◆\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✘\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = Linux ] || die "BoundedCode supports Linux only for now."
[ "$(uname -m)" = x86_64 ] || die "BoundedCode supports x86_64 only for now (found $(uname -m))."
command -v curl >/dev/null || die "curl is required."
command -v git >/dev/null || die "git is required (BoundedCode works on git repositories)."

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# try_release TAG: download and verify the release binary; fails quietly
# when the release has none.
try_release() {
  local base="https://github.com/$REPO/releases/download/$1"
  curl -fsSL -o "$tmp/$ASSET" "$base/$ASSET" 2>/dev/null || return 1
  curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" 2>/dev/null || die "release $1 has no SHA256SUMS; refusing an unverified binary"
  (cd "$tmp" && grep " $ASSET\$" SHA256SUMS | sha256sum -c --quiet -) || die "checksum mismatch for $ASSET ($1)"
  chmod 0755 "$tmp/$ASSET"
  say "downloaded release $1 (checksum verified)"
}

build_source() {
  local ref="$1"
  command -v go >/dev/null || die "no release binary for '$ref', and Go is not installed to build it.
  Install Go (https://go.dev/dl/) and run this again, or set BC_VERSION to a release with binaries."
  command -v make >/dev/null || die "make is required to build from source."
  say "building $REPO@$ref from source…"
  git clone -q --depth 1 --branch "$ref" "$GIT_URL" "$tmp/src" 2>/dev/null \
    || git clone -q "$GIT_URL" "$tmp/src"
  git -C "$tmp/src" checkout -q "$ref"
  # GOTOOLCHAIN=auto fetches the Go version go.mod asks for if yours is older.
  (cd "$tmp/src" && GOTOOLCHAIN=auto make -s build VERSION="$(git describe --tags --always 2>/dev/null || echo "$ref")")
  cp "$tmp/src/bin/boundedcode" "$tmp/$ASSET"
}

if [ -n "$VERSION" ]; then
  try_release "$VERSION" || build_source "$VERSION"
else
  # Newest release (pre-releases included) that ships a binary, else main.
  tags="$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=10" 2>/dev/null \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' || true)"
  installed=""
  for t in $tags; do
    if try_release "$t"; then installed=1; break; fi
  done
  [ -n "$installed" ] || build_source main
fi

mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/$ASSET" "$BIN_DIR/boundedcode"
ln -sf boundedcode "$BIN_DIR/bcode"
say "installed $("$BIN_DIR/boundedcode" version) to $BIN_DIR (boundedcode, bcode)"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf '\n  %s is not on your PATH. Add it, e.g.:\n    echo '"'"'export PATH="%s:$PATH"'"'"' >> ~/.bashrc && source ~/.bashrc\n' "$BIN_DIR" "$BIN_DIR" ;;
esac

cat <<'EOF'

  Next: cd into a git repository and run

    bcode

  The first run checks what is missing (inference server, model, tools,
  Docker sandbox) and sets it up with your permission. `bcode setup --check`
  shows the same from the shell.

EOF
printf '  Uninstall: rm %s/{boundedcode,bcode}\n' "$BIN_DIR"
printf '  All data: ~/.config, ~/.local/share, ~/.local/state and ~/.cache under\n'
printf '  boundedcode/ (or their XDG_* overrides), and the sandbox image\n'
printf '  (docker rmi boundedcode-openhands:local).\n\n'
