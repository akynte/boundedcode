#!/usr/bin/env bash
# Runs the installer smoke test (scripts/smoke/install.sh) as a non-root
# user in fresh Linux distribution containers, against a release built from
# this checkout. Only the distributions' own packages (curl, git,
# ca-certificates) are downloaded; the BoundedCode release is a local
# directory mounted read-only.
#
# usage: scripts/smoke/install-containers.sh [IMAGE...]
#   default images: debian:13 ubuntu:24.04 fedora:42
# env: SMOKE_PLATFORM  container platform (default: this machine's, e.g.
#                      linux/amd64; linux/arm64 runs under emulation where
#                      the engine supports it)
#      ENGINE          docker (default) or podman
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
engine="${ENGINE:-docker}"
images=("$@")
[ ${#images[@]} -gt 0 ] || images=(debian:13 ubuntu:24.04 fedora:42)
platform="${SMOKE_PLATFORM:-linux/$(case "$(uname -m)" in x86_64|amd64) echo amd64 ;; *) echo arm64 ;; esac)}"
arch="${platform#linux/}"

# Under the home directory: Docker Desktop shares it with its VM; a fresh
# name per run (recreated paths can break its bind mounts).
stage="$HOME/.cache/bc-smoke-$(date +%s)-$$"
trap 'rm -rf "$stage"' EXIT
tag=v0.0.0-smoke
mkdir -p "$stage/release/$tag" "$stage/scripts/smoke"
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$stage/release/$tag/boundedcode-linux-$arch" ./cmd/boundedcode)
(cd "$stage/release/$tag" && sha256sum "boundedcode-linux-$arch" > SHA256SUMS)
cp "$root/scripts/install.sh" "$stage/scripts/"
cp "$root/scripts/smoke/install.sh" "$stage/scripts/smoke/"

failed=()
for img in "${images[@]}"; do
  echo "=== $img ($platform)"
  case "$img" in
    fedora*|rocky*|alma*) prep='dnf -y -q install git-core curl ca-certificates which >/dev/null' ;;
    *) prep='apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends git curl ca-certificates >/dev/null' ;;
  esac
  if "$engine" run --rm --platform "$platform" -v "$stage:/stage:ro" "$img" bash -c "
      set -e; $prep
      useradd -m smoke
      su smoke -c 'bash /stage/scripts/smoke/install.sh /stage/release'"; then
    echo "=== $img: PASS"
  else
    echo "=== $img: FAIL"
    failed+=("$img")
  fi
done
[ ${#failed[@]} -eq 0 ] || { echo "failed: ${failed[*]}"; exit 1; }
echo "all images passed"
