#!/usr/bin/env bash
# Runs a command on a frozen task's untouched base (prepared by basecheck)
# inside the sandbox image, offline, with the evaluation's caches, and
# prints its output. Diagnosis for the environment-only verification
# configs (deviations.md D1); it never sees agent output or reference
# patches.
#
# usage: basediag.sh TASK_ID COMMAND...
set -euo pipefail
ev="${EVAL:-$HOME/.cache/bc-comparative}"
dir="$ev/basecheck/$1"
shift
[ -d "$dir" ] || { echo "no base checkout $dir (run basecheck first)" >&2; exit 1; }
exec docker run --rm --network none --user "$(id -u):$(id -g)" --ulimit nofile=65536:65536 \
  --tmpfs /tmp/home:rw,exec,size=4g,mode=1777 -e HOME=/tmp/home \
  -v "$dir:$dir" -v "$ev/gomod:$ev/gomod:ro" -v "$ev/cargo:$ev/cargo" -v "$ev/m2:$ev/m2:ro" \
  -e GOMODCACHE="$ev/gomod" -e GOFLAGS=-mod=mod -e GOPROXY=off -e CARGO_HOME="$ev/cargo" -e CARGO_NET_OFFLINE=true \
  -w "$dir" boundedcode-openhands:local sh -c "$*"
