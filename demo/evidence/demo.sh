#!/usr/bin/env bash
# Evidence demo: BoundedCode on a controlled fixture whose existing tests
# pass despite a bug. See demo/evidence/README.md for what it shows and what
# it does not.
#
# usage: demo/evidence/demo.sh [--model scripted|URL] [--record DIR] [--binary PATH] [--keep]
#   --model scripted  (default) a scripted stand-in for the model
#                     (scripts/smoke/fake_model.py running the commands in
#                     demo/evidence/scripted-agent.txt): deterministic, no
#                     model download, labelled as a simulation of the agent.
#   --model URL       a real model behind an OpenAI-compatible server you
#                     already run, e.g. the local llama.cpp server:
#                     http://127.0.0.1:8765. The model name sent is the
#                     default profile's (qwen3.6-35b-a3b); DEMO_MODEL_LABEL
#                     names the model in the recording.
#   --record DIR      record the session with util-linux `script` into DIR:
#                     session.log and session.timing (raw and unedited),
#                     session.cast (asciicast v2, converted losslessly) and,
#                     when agg is installed (or AGG=path), demo.gif rendered
#                     with waits longer than DEMO_IDLE_LIMIT (default 2)
#                     seconds shortened.
#   --binary PATH     the boundedcode binary (default: built from this checkout)
# needs: git, Go (for the fixture's own tests), Docker with the sandbox
#   image (`bcode setup --only sandbox`), python3.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
mode=scripted record="" binary="" keep=""
while [ $# -gt 0 ]; do
  case "$1" in
    --model) mode="$2"; shift 2 ;;
    --record) record="$2"; shift 2 ;;
    --binary) binary="$2"; shift 2 ;;
    --keep) keep=1; shift ;;
    *) echo "unknown argument $1" >&2; exit 2 ;;
  esac
done

# Under the home directory: Docker Desktop shares it with its VM.
work="$HOME/.cache/bc-demo-evidence-$(date +%s)-$$"
mkdir -p "$work/bin"
srv_pid=""
cleanup() {
  [ -n "$srv_pid" ] && kill "$srv_pid" 2>/dev/null || true
  [ -n "$keep" ] || rm -rf "$work"
}
trap cleanup EXIT
log="$work/setup.log"
step() { printf 'demo: %s\n' "$*"; }
quiet() { "$@" >> "$log" 2>&1 || { echo "demo: failed: $*" >&2; tail -20 "$log" >&2; exit 1; }; }

if [ -z "$binary" ]; then
  binary="$work/boundedcode"
  step "building boundedcode from this checkout"
  (cd "$root" && make -s build BIN="$binary")
fi
ln -s "$binary" "$work/bin/bcode"
export PATH="$work/bin:$PATH" BOUNDEDCODE_HOME="$work/home" BOUNDEDCODE_SECRETS=file

step "configuring an isolated BoundedCode home ($mode)"
if [ "$mode" = scripted ]; then
  # A placeholder for the local scripted server, not a credential.
  export BOUNDEDCODE_OPENAI_COMPATIBLE_API_KEY=demo-placeholder
  python3 "$root/scripts/smoke/fake_model.py" --command-file "$here/scripted-agent.txt" \
    --log "$work/model.log" > "$work/port" &
  srv_pid=$!
  for _ in $(seq 50); do [ -s "$work/port" ] && break; sleep 0.1; done
  quiet bcode setup --only config --yes
  quiet bcode provider use openai-compatible --base-url "http://127.0.0.1:$(cat "$work/port")/v1" --model scripted-agent --context-window 32768
  label="Model: SCRIPTED stand-in (fixed commands in demo/evidence/scripted-agent.txt), not a real model. The rest is the real product."
else
  quiet bcode init --external-url "$mode"
  label="Model: ${DEMO_MODEL_LABEL:-real model at $mode}. Unscripted run."
fi
quiet bcode setup --only tools --yes
bcode setup --check >> "$log" 2>&1 || { echo "demo: set-up incomplete (build the sandbox image: bcode setup --only sandbox)" >&2; tail -8 "$log" >&2; exit 1; }

step "creating the fixture repository"
repo="$work/shop"
cp -R "$here/testdata/shop" "$repo"
export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.invalid GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.invalid
quiet git -C "$repo" init -q -b main
quiet git -C "$repo" add -A
quiet git -C "$repo" commit -qm "shop: bulk discount"
cd "$repo"
quiet bcode workspace create shop
quiet bcode workspace add .
quiet bcode index

export DEMO_REQUEST="Orders of exactly 10 items do not get the 10% bulk discount: go run ./cmd/quote 10 prints a total of 1000 cents, but it should be 900."
export DEMO_LABEL="$label"
if [ -z "$record" ]; then
  bash "$here/scenario.sh" | tee "$work/scenario.out"
  # The scripted stand-in is deterministic: anything but this outcome is a
  # regression. A real model's outcome is reported as it is.
  if [ "$mode" = scripted ]; then
    for want in "asking the agent for a test" "--- FAIL: TestTotalBulkDiscountAtExactlyTen" "total 900 cents" "verification=task_verified"; do
      grep -qF -- "$want" "$work/scenario.out" || { echo "demo: FAIL: the scripted scenario did not show: $want" >&2; exit 1; }
    done
    step "scripted scenario: all steps shown"
  fi
  exit 0
fi

mkdir -p "$record"
record="$(cd "$record" && pwd)"
step "recording into $record"
cols=100 rows=34
# The command line is recorded in the log's header, so it names no local
# path; TZ=UTC keeps the local time zone out of the timestamps.
export DEMO_SCENARIO="$here/scenario.sh" TZ=UTC
script -q -E never --log-timing "$record/session.timing" --log-out "$record/session.log" \
  -c 'stty cols '"$cols"' rows '"$rows"' 2>/dev/null; bash "$DEMO_SCENARIO"' < /dev/null
python3 "$here/to_cast.py" "$record/session.timing" "$record/session.log" "$record/session.cast" \
  --cols "$cols" --rows "$rows" --title "BoundedCode evidence demo"
if [ "$mode" = scripted ] && [ -f "$work/model.log" ]; then cp "$work/model.log" "$record/scripted-model-requests.log"; fi
agg="${AGG:-$(command -v agg || true)}"
if [ -n "$agg" ]; then
  "$agg" --idle-time-limit "${DEMO_IDLE_LIMIT:-2}" --font-size 15 --theme monokai \
    "$record/session.cast" "$record/demo.gif"
  step "wrote $record/demo.gif (waits longer than ${DEMO_IDLE_LIMIT:-2} s shortened)"
else
  step "agg not found: render the GIF with: agg --idle-time-limit 2 $record/session.cast demo.gif"
fi
