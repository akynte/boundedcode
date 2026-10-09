#!/usr/bin/env bash
# Installer and first-run smoke test for Linux and macOS. It runs
# scripts/install.sh into a throw-away HOME and checks what a first user
# depends on:
#
#   install from a release (checksum verified), bcode link, version, an
#   idempotent re-run, refusal of a tampered binary, refusal to replace an
#   unrelated `bcode`, `setup --check` failing until set-up is complete,
#   `setup --only config`, `doctor`, and the cloud-provider path skipping
#   the local-model steps.
#
# usage: scripts/smoke/install.sh [RELEASE_DIR]
#   RELEASE_DIR  a directory laid out like a release: TAG/boundedcode-OS-ARCH
#                and TAG/SHA256SUMS. Default: built here from this checkout
#                with `make build` (needs Go). Nothing is downloaded unless
#                SMOKE_PUBLISHED=1, which installs the newest published
#                release from GitHub instead.
# env: SMOKE_TOOLS=1  also run `setup --only tools` (downloads the pinned
#                     gitleaks and codebase-memory-mcp, a few MB)
#      SMOKE_KEEP=1   keep the work directory
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
installer="$root/scripts/install.sh"
work="$(mktemp -d "${TMPDIR:-/tmp}/bc-smoke-install.XXXXXX")"
[ -n "${SMOKE_KEEP:-}" ] || trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT

pass=0
ok() { pass=$((pass + 1)); printf '  ok   %s\n' "$*"; }
fail() { printf '  FAIL %s\n' "$*" >&2; [ -n "${out:-}" ] && printf '%s\n' "$out" | sed 's/^/       | /' >&2; exit 1; }

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) fail "unsupported OS $(uname -s)" ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) fail "unsupported CPU $(uname -m)" ;; esac
asset="boundedcode-$os-$arch"
sha() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }

bin="$work/bin"
if [ -n "${SMOKE_PUBLISHED:-}" ]; then
  echo "installer smoke: newest published release (network)"
  install_env=(BC_BIN_DIR="$bin")
else
  rel="${1:-}"
  tag=v0.0.0-smoke
  if [ -z "$rel" ]; then
    rel="$work/release"
    mkdir -p "$rel/$tag"
    (cd "$root" && make -s build BIN="$rel/$tag/$asset")
    (cd "$rel/$tag" && sha "$asset" > SHA256SUMS)
  fi
  tag="$(cd "$rel" && ls | head -1)"
  echo "installer smoke: $rel/$tag/$asset"
  install_env=(BC_BIN_DIR="$bin" BC_VERSION="$tag" BC_DOWNLOAD_BASE="file://$rel")
fi

# An isolated user: no config, data or PATH entries from the real one. The
# Docker CLI keeps its own settings (Docker Desktop's context lives there).
export DOCKER_CONFIG="${DOCKER_CONFIG:-$HOME/.docker}"
export HOME="$work/home"
unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_STATE_HOME XDG_CACHE_HOME BOUNDEDCODE_HOME
export BOUNDEDCODE_SECRETS=file TMPDIR="$work/tmp"
mkdir -p "$HOME" "$TMPDIR"

echo "1. fresh install"
out="$(env "${install_env[@]}" bash "$installer" 2>&1)" || fail "install.sh exited non-zero"
grep -q "checksum verified" <<<"$out" || fail "no checksum verification reported"
[ -x "$bin/boundedcode" ] || fail "boundedcode not installed"
[ "$(readlink "$bin/bcode")" = boundedcode ] || fail "bcode is not a link to boundedcode"
grep -q "is not on your PATH" <<<"$out" || fail "no PATH hint for a directory outside PATH"
ok "installed, checksum verified, bcode linked, PATH hint shown"
out="$("$bin/bcode" version)" || fail "bcode version"
grep -q '^boundedcode ' <<<"$out" || fail "unexpected version output"
ok "bcode version: $out"

echo "2. re-run (upgrade in place)"
out="$(env "${install_env[@]}" bash "$installer" 2>&1)" || fail "second install.sh run failed"
[ ! -e "$bin/.boundedcode.new" ] || fail "staging file left behind"
[ -z "$(ls -A "$TMPDIR")" ] || fail "temporary files left in $TMPDIR: $(ls -A "$TMPDIR")"
ok "idempotent; no staging or temporary files left"

if [ -z "${SMOKE_PUBLISHED:-}" ]; then
  echo "3. tampered release"
  bad="$work/bad-release"
  cp -R "$rel" "$bad"
  printf 'tampered' >> "$bad/$tag/$asset"
  before="$(sha "$bin/boundedcode" | cut -d' ' -f1)"
  if out="$(env "${install_env[@]}" BC_DOWNLOAD_BASE="file://$bad" bash "$installer" 2>&1)"; then
    fail "a tampered binary was installed"
  fi
  grep -q "checksum mismatch" <<<"$out" || fail "no checksum mismatch message"
  [ "$(sha "$bin/boundedcode" | cut -d' ' -f1)" = "$before" ] || fail "the installed binary changed"
  ok "checksum mismatch refused; installed binary untouched"
fi

echo "4. unrelated bcode in the install directory"
other="$work/other-bin"
mkdir -p "$other"
printf '#!/bin/sh\necho someone else\n' > "$other/bcode"
chmod +x "$other/bcode"
if out="$(env "${install_env[@]}" BC_BIN_DIR="$other" bash "$installer" 2>&1)"; then
  fail "an unrelated bcode was replaced"
fi
grep -q "is not a link to BoundedCode" <<<"$out" || fail "no refusal message"
[ "$("$other/bcode")" = "someone else" ] || fail "the unrelated bcode was modified"
ok "refused to replace an unrelated bcode"
out="$(env "${install_env[@]}" BC_BIN_DIR="$other" BC_FORCE=1 bash "$installer" 2>&1)" || fail "BC_FORCE=1 install failed"
[ "$(readlink "$other/bcode")" = boundedcode ] || fail "BC_FORCE=1 did not replace it"
ok "BC_FORCE=1 replaces it"

if [ -n "${SMOKE_PUBLISHED:-}" ]; then
  echo "5-6. skipped: the first-run checks test this checkout's binary, not the published one"
  echo "installer smoke: $pass checks passed"
  exit 0
fi

echo "5. first run: setup --check and doctor"
export PATH="$bin:$PATH"
repo="$work/repo"
git init -q "$repo"
cd "$repo"
if out="$(bcode setup --check 2>&1)"; then fail "setup --check succeeded with nothing set up"; fi
grep -q '^\[todo\] Configuration' <<<"$out" || fail "configuration not reported as missing"
grep -q 'setup step(s) to do' <<<"$out" || fail "no summary of what is left"
ok "setup --check exits non-zero and lists what is missing"
out="$(bcode setup --only config --yes 2>&1)" || fail "setup --only config"
cfg="$HOME/.config/boundedcode/config.yaml"
[ -f "$cfg" ] || fail "no configuration written"
perm="$(stat -c %a "$cfg" 2>/dev/null || stat -f %Lp "$cfg")"
[ "$perm" = 600 ] || fail "configuration mode is $perm, want 600"
ok "setup --only config wrote $cfg (mode 600)"
out="$(bcode setup --check 2>&1)" || true
grep -q '^\[ok  \] Configuration' <<<"$out" || fail "configuration not reported as done"
grep -q 'provider use NAME' <<<"$out" || fail "the local-model steps do not name the cloud alternative"
ok "local-model steps name the cloud alternative"
out="$(bcode doctor 2>&1)" && rc=0 || rc=$?
grep -q '^\[ok  \] config' <<<"$out" || fail "doctor does not report the configuration"
grep -qi 'panic' <<<"$out" && fail "doctor panicked"
ok "doctor runs (exit $rc; failures are the parts not installed here)"

echo "6. cloud-provider path"
out="$(bcode provider use openai-compatible --base-url http://127.0.0.1:9/v1 --model smoke --context-window 32768 2>&1)" || fail "provider use"
out="$(bcode setup --check 2>&1)" || true
[ "$(grep -c 'not needed: using' <<<"$out")" = 2 ] || fail "inference and model steps are not skipped for a cloud provider"
ok "with a cloud provider, the llama.cpp and model steps are skipped"

if [ -n "${SMOKE_TOOLS:-}" ]; then
  echo "7. setup --only tools (network)"
  # Hide tools installed elsewhere so the pinned downloads are exercised.
  out="$(PATH="$bin:/usr/bin:/bin" bcode setup --only tools --yes 2>&1)" || fail "setup --only tools"
  data="$HOME/.local/share/boundedcode/bin"
  "$data/gitleaks" version >/dev/null || fail "gitleaks not runnable"
  "$data/codebase-memory-mcp" --version >/dev/null 2>&1 || "$data/codebase-memory-mcp" version >/dev/null 2>&1 || fail "codebase-memory-mcp not runnable"
  ok "pinned gitleaks and codebase-memory-mcp downloaded, verified and runnable"
fi

echo "8. uninstall"
rm "$bin/boundedcode" "$bin/bcode"
[ -z "$(ls -A "$bin")" ] || fail "files other than boundedcode and bcode were installed: $(ls -A "$bin")"
ok "uninstall leaves nothing in the bin directory"

echo "installer smoke: $pass checks passed"
