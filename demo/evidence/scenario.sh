#!/usr/bin/env bash
# The visible part of the evidence demo (what gets recorded). demo.sh
# prepares the repository and the configuration, then runs this in the
# fixture repository with `bcode` on PATH. Every line shown after "$ " is
# the command that actually ran; nothing is replayed or edited.
#
# env: DEMO_REQUEST  the task request
#      DEMO_LABEL    one line naming the model mode (shown first)
set -uo pipefail

if [ -t 1 ]; then stty cols 100 rows 34 2>/dev/null || true; fi
note() { printf '\n\033[1;36m# %s\033[0m\n' "$*"; sleep 1; }
run() { printf '\033[1;32m$\033[0m %s\n' "$*"; sleep 0.5; bash -c "$*"; }

printf '\033[2mBoundedCode evidence demo. Controlled fixture: demo/evidence/testdata/shop.\n%s\033[0m\n' "$DEMO_LABEL"

note "1. The bug: 10 items should get the 10% bulk discount (900 cents)"
run go run ./cmd/quote 10

note "2. The existing tests pass anyway"
run go test ./...

note "3. Ask BoundedCode to fix it"
out="$(mktemp)"
run "bcode task create \"$DEMO_REQUEST\" -c 'go test ./... passes' --run" 2>&1 | tee "$out"
id="$(sed -n 's/^created task \([^ ]*\).*/\1/p' "$out" | head -1)"
rm -f "$out"
if [ -z "$id" ]; then
  echo "no task was created" >&2
  exit 1
fi

tests="$(git diff --name-only main "agent/$id" -- '*_test.go' | tr '\n' ' ')"
if [ -n "${tests// /}" ]; then
  note "4. Check the evidence by hand: the change's tests on the ORIGINAL code"
  run "git checkout agent/$id -- $tests"
  run go test ./...

  note "5. The same tests with the change, and the fix itself"
  run "git checkout agent/$id -- ."
  run go test ./...
  run "git diff -U0 HEAD -- . ':!*_test.go' | grep '^[-+][^-+]'"
  run go run ./cmd/quote 10
  git reset -q --hard && git clean -fdq # back to the original checkout
else
  note "4. The change adds no test file, so there is no evidence to replay"
fi

note "6. BoundedCode's own evidence record and the final state"
run "bcode task events $id | grep -E 'verify.evidence|verify.evidence_requested' | cut -c1-200"
run "bcode task status $id | head -3"
