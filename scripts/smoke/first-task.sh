#!/usr/bin/env bash
# First-task smoke test: the real CLI, model gateway, OpenHands adapter,
# Docker sandbox and verification, on the README's demo bug, with a scripted
# OpenAI-compatible server (scripts/smoke/fake_model.py) in place of a model.
# No model download and no cloud account are needed; nothing leaves the
# machine. It checks the plumbing and the verification gate, not model
# quality.
#
#   scenario 1: the "agent" fixes the bug and adds a test  -> task_verified
#   scenario 2: the "agent" fixes the bug without a test   -> tests_green
#               (UNVERIFIED), after BoundedCode asks once for a test
#
# usage: scripts/smoke/first-task.sh [BINARY]
#   BINARY  default: built from this checkout
# needs: docker (or podman) with the sandbox image, which `setup --only
#   sandbox` builds (about 5 GB, minutes); python3; git; network only for
#   `setup --only tools` when gitleaks or codebase-memory-mcp is missing.
# env: SMOKE_KEEP=1 keep the work directory; SMOKE_SCENARIOS="1 2"
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
# Under the home directory: Docker Desktop shares it with its VM.
work="$HOME/.cache/bc-smoke-first-task-$(date +%s)-$$"
mkdir -p "$work"
cleanup() {
  [ -n "${srv_pid:-}" ] && kill "$srv_pid" 2>/dev/null || true
  [ -n "${SMOKE_KEEP:-}" ] || rm -rf "$work"
}
trap cleanup EXIT

pass=0
ok() { pass=$((pass + 1)); printf '  ok   %s\n' "$*"; }
fail() {
  printf '  FAIL %s\n' "$*" >&2
  [ -n "${out:-}" ] && printf '%s\n' "$out" | tail -40 | sed 's/^/       | /' >&2
  [ -f "$work/model.log" ] && { echo "       model requests:" >&2; sed 's/^/       | /' "$work/model.log" >&2; }
  exit 1
}

B="${1:-}"
if [ -z "$B" ]; then
  B="$work/boundedcode"
  (cd "$root" && make -s build BIN="$B")
fi
export BOUNDEDCODE_HOME="$work/home" BOUNDEDCODE_SECRETS=file
# A placeholder for the scripted server, not a credential.
export BOUNDEDCODE_OPENAI_COMPATIBLE_API_KEY=smoke-placeholder

echo "1. set-up with a cloud-style provider (the scripted server)"
: > "$work/model.log"
python3 "$root/scripts/smoke/fake_model.py" --command-file "$work/command.sh" --log "$work/model.log" > "$work/port" &
srv_pid=$!
for _ in $(seq 50); do [ -s "$work/port" ] && break; sleep 0.1; done
port="$(cat "$work/port")"
[ -n "$port" ] || fail "the scripted server did not start"
out="$("$B" setup --only config --yes 2>&1)" || fail "setup --only config"
out="$("$B" provider use openai-compatible --base-url "http://127.0.0.1:$port/v1" --model smoke --context-window 32768 2>&1)" || fail "provider use"
out="$("$B" provider test 2>&1)" || fail "provider test"
ok "provider configured and answering"
out="$("$B" setup --only tools --yes 2>&1)" || fail "setup --only tools"
out="$("$B" setup --check 2>&1)" || fail "set-up incomplete (build the sandbox image with \`$B setup --only sandbox\`)"
ok "setup --check: complete (inference and model steps not needed with a provider)"

echo "2. a repository with the demo bug"
repo="$work/shop"
mkdir -p "$repo"
cat > "$repo/go.mod" <<'EOF'
module example.com/shop

go 1.22
EOF
cat > "$repo/cart.go" <<'EOF'
package shop

// Item is one line of a cart.
type Item struct {
	PriceCents int
	Qty        int
}

// Total returns the cart total in cents. Orders of 10 or more items get a
// 10% bulk discount.
func Total(items []Item) int {
	sum, count := 0, 0
	for _, it := range items {
		sum += it.PriceCents * it.Qty
		count += it.Qty
	}
	if count > 10 {
		sum = sum * 90 / 100
	}
	return sum
}
EOF
cat > "$repo/cart_test.go" <<'EOF'
package shop

import "testing"

func TestTotalSmallCart(t *testing.T) {
	if got := Total([]Item{{PriceCents: 100, Qty: 5}}); got != 500 {
		t.Fatalf("Total = %d, want 500", got)
	}
}
EOF
git -C "$repo" init -q -b main
git -C "$repo" add -A
git -C "$repo" -c user.name=smoke -c user.email=smoke@example.invalid commit -qm "shop with the bulk-discount bug"
out="$("$B" workspace create shop 2>&1)" || fail "workspace create"
out="$("$B" workspace add "$repo" 2>&1)" || fail "workspace add"
out="$("$B" index 2>&1)" || fail "index"
ok "workspace created and indexed"

request='Orders of exactly 10 items do not get the 10% bulk discount: Total([]Item{{PriceCents: 100, Qty: 10}}) returns 1000 but should be 900.'
fix='cd "$(dirname "$(find . -name cart.go -not -path "*/.git/*" | head -1)")" && sed -i "s/count > 10 {/count >= 10 {/" cart.go'
# The test is passed base64-encoded: the agent's terminal is an interactive
# shell, where tabs trigger completion and quoting is fragile.
test_go="$(printf '\nfunc TestTotalBulkDiscountExactly10(t *testing.T) {\n\tif got := Total([]Item{{PriceCents: 100, Qty: 10}}); got != 900 {\n\t\tt.Fatalf("Total = %%d, want 900", got)\n\t}\n}\n' | base64 | tr -d '\n')"
test_add="echo $test_go | base64 -d >> cart_test.go"

run_task() { # $1 = command for the scripted agent; prints the task id
  printf '%s\n' "$1" > "$work/command.sh"
  : > "$work/model.log"
  out="$("$B" task create "$request" -c "go test ./... passes" --run 2>&1)" && rc=0 || rc=$?
  id="$(sed -n 's/^created task \([^ ]*\).*/\1/p' <<<"$out" | head -1)"
  [ -n "$id" ] || fail "no task created (exit $rc)"
}

for sc in ${SMOKE_SCENARIOS:-1 2}; do
  case "$sc" in
  1)
    echo "3. scenario 1: fix plus a test that demonstrates it"
    run_task "$fix && $test_add"
    grep -q "verification=task_verified" <<<"$out" || fail "task $id did not end task_verified"
    ok "task $id: verification=task_verified"
    out="$("$B" task diff "$id" 2>&1)" || fail "task diff"
    grep -q '^+.*count >= 10' <<<"$out" && grep -q '^+func TestTotalBulkDiscountExactly10' <<<"$out" || fail "diff lacks the fix or the test"
    ok "task diff shows the fix and the new test on branch agent/$id"
    ;;
  2)
    echo "4. scenario 2: fix without a test"
    run_task "$fix"
    grep -q "asking the agent for a test" <<<"$out" || fail "BoundedCode did not ask for a test"
    grep -q "verification=tests_green" <<<"$out" || fail "task $id did not end tests_green"
    grep -q "verification=task_verified" <<<"$out" && fail "a change without a test was verified"
    ok "task $id: asked once for a test, then verification=tests_green (UNVERIFIED)"
    ;;
  esac
done

git -C "$repo" diff --quiet main && [ "$(git -C "$repo" rev-parse --abbrev-ref HEAD)" = main ] || fail "the user's checkout changed"
ok "the user's checkout is untouched (results are on agent/* branches)"
echo "first-task smoke: $pass checks passed"
