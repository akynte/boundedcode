#!/usr/bin/env bash
# First-milestone end-to-end run (docs/product-spec.md §6) on the real local
# model, sandboxed, against the payment-platform fixture:
#   init workspace -> index -> non-trivial cross-service task -> interrupt the
#   process mid-task -> resume -> forced condensation on retries -> verified
#   merge candidate -> hidden acceptance checks -> audit log.
# usage: scripts/milestone-e2e.sh [WORKDIR]
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
work="${1:-$HOME/.cache/boundedcode-e2e}"
B="$root/bin/boundedcode"
rm -rf "$work"; mkdir -p "$work"
export BOUNDEDCODE_HOME="$work/home"

# Same runtime/model settings as the user's config, isolated state.
mkdir -p "$BOUNDEDCODE_HOME/config/models"
cp "$HOME/.config/boundedcode/config.yaml" "$BOUNDEDCODE_HOME/config/config.yaml"
cp "$HOME/.config/boundedcode/models/"*.yaml "$BOUNDEDCODE_HOME/config/models/" 2>/dev/null || true

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
"$B" task run "$id" --condense-each-retry > "$work/run1.log" 2>&1 &
pid=$!
until "$B" task events "$id" 2>/dev/null | grep -q 'agent.turn'; do
  kill -0 "$pid" 2>/dev/null || break
  sleep 5
done
if kill -0 "$pid" 2>/dev/null; then
  echo "interrupting run 1 (pid $pid) after first agent turn"
  kill -INT "$pid"; wait "$pid" || true
fi

# Run 2: resume from the ledger, git and the persisted conversation.
"$B" task resume "$id" --condense-each-retry 2>&1 | tee "$work/run2.log"
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
