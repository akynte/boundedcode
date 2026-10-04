#!/usr/bin/env bash
# First-milestone end-to-end run (docs/product-spec.md §6) on the real local
# model, sandboxed, against the payment-platform fixture:
#   init workspace -> index -> non-trivial cross-service task -> interrupt the
#   process mid-task -> resume -> forced condensation on retries -> verified
#   merge candidate -> hidden acceptance checks -> audit log.
# usage: scripts/milestone-e2e.sh [WORKDIR]
# env: KILL_SIGNAL=INT (default, graceful interrupt) or KILL (crash: no
#      cleanup; the resume waits for the run lease to go stale)
#      SERENA=on|off (default off): Serena symbol navigation for the run
set -euo pipefail
sig="${KILL_SIGNAL:-INT}"
serena="${SERENA:-off}"
root="$(cd "$(dirname "$0")/.." && pwd)"
work="${1:-$HOME/.cache/boundedcode-e2e}"
B="$root/bin/boundedcode"
rm -rf "$work"; mkdir -p "$work"
export BOUNDEDCODE_HOME="$work/home"

# Same runtime/model settings as the user's config, isolated state.
mkdir -p "$BOUNDEDCODE_HOME/config/models"
cp "$HOME/.config/boundedcode/config.yaml" "$BOUNDEDCODE_HOME/config/config.yaml"
cp "$HOME/.config/boundedcode/models/"*.yaml "$BOUNDEDCODE_HOME/config/models/" 2>/dev/null || true
# Reuse the pinned Serena installed by `serena setup` (the version gate still applies).
serena_bin="${XDG_DATA_HOME:-$HOME/.local/share}/boundedcode/tools/serena-1.7.0/.venv/bin/serena"
if [ "$serena" = on ] && [ -x "$serena_bin" ]; then
  python3 - "$BOUNDEDCODE_HOME/config/config.yaml" "$serena_bin" <<'PY'
import sys
p, exe = sys.argv[1], sys.argv[2]
s = open(p).read()
s = s.replace("repointel:\n", "repointel:\n    serena:\n        command: " + exe + "\n", 1)
open(p, "w").write(s)
PY
fi

"$root/scripts/materialize-fixture.sh" "$root/benchmarks/fixtures/payment-platform" "$work/repos" >/dev/null
"$B" workspace create payment-platform
for r in payment-service ledger-service shared-protos; do "$B" workspace add "$work/repos/$r"; done
"$B" index

request='Make payment processing idempotent across services.
(1) payment-service: POST /v1/payments must honour an Idempotency-Key request header. A repeated request with the same key must return the original payment (same id) with status 200 or 201 and must NOT publish a second PaymentCharged event. Requests without the header behave as today.
(2) ledger-service: HandlePaymentCharged must be idempotent per payment_id: processing the same event twice posts only once. Add tests in both services.'
id=$("$B" task create "$request" -r payment-service -r ledger-service \
      -c "same Idempotency-Key returns the same payment id and publishes exactly one event" \
      -c "duplicate PaymentCharged events (same payment_id) are posted once" \
      -c "go test ./... passes in both services" | awk '/created task/{print $3}')
echo "task: $id"

# Run 1: interrupt after the first agent turn has been persisted.
"$B" task run "$id" --condense-each-retry --serena "$serena" > "$work/run1.log" 2>&1 &
pid=$!
until "$B" task events "$id" 2>/dev/null | grep -q 'agent.turn'; do
  kill -0 "$pid" 2>/dev/null || break
  sleep 5
done
if kill -0 "$pid" 2>/dev/null; then
  echo "interrupting run 1 (pid $pid, SIG$sig) after first agent turn"
  kill "-$sig" "$pid"; wait "$pid" || true
fi
if [ "$sig" = KILL ]; then
  # The killed run still holds its lease; a resume is refused until it is stale.
  if out=$("$B" task resume "$id" 2>&1); then echo "unexpected: resume ran while the lease was fresh"; exit 1; fi
  grep -q 'another process' <<<"$out" && echo "concurrent resume refused while the lease is fresh"
  sleep 130
fi

# Run 2: resume from the ledger, git and the persisted conversation.
"$B" task resume "$id" --condense-each-retry --serena "$serena" 2>&1 | tee "$work/run2.log"
"$B" task status "$id"

# Hidden acceptance checks (same as benchmarks/tasks/cross-service-idempotency.yaml).
python3 - "$root/benchmarks/tasks/cross-service-idempotency.yaml" "$BOUNDEDCODE_HOME/data/tasks/$id/work" <<'PY'
import os, re, sys
spec, work = open(sys.argv[1]).read(), sys.argv[2]
for m in re.finditer(r"- repo: (\S+)\n    path: (\S+)\n    content: \|\n((?:      .*\n|\n)+)", spec):
    repo, path, body = m.group(1), m.group(2), m.group(3)
    p = os.path.join(work, repo, path)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    open(p, "w").write("\n".join(l[6:] for l in body.split("\n")))
    print("hidden:", p)
PY
ok=1
for r in payment-service ledger-service; do
  d="$BOUNDEDCODE_HOME/data/tasks/$id/work/$r"
  docker run --rm --network none -u "$(id -u):$(id -g)" -v "$d:$d" -v "$work/repos/$r/.git:$work/repos/$r/.git:ro" -w "$d" \
    -e GOFLAGS=-buildvcs=false -e HOME=/tmp boundedcode-openhands:local go test ./... || ok=0
done
"$B" task events "$id" | awk '{print $2}' | sort | uniq -c
[ "$ok" = 1 ] && echo "HIDDEN ACCEPTANCE: PASS" || echo "HIDDEN ACCEPTANCE: FAIL"
