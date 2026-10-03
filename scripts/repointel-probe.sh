#!/usr/bin/env bash
# Reproduce the Phase 3 repository-intelligence probes
# (docs/design/repointel-gap-report.md) against an isolated cache.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
work="${1:-$HOME/.cache/boundedcode-bench}"
export BOUNDEDCODE_HOME="$work/home"
export CBM_CACHE_DIR="$BOUNDEDCODE_HOME/cache/codebase-memory"
B="$root/bin/boundedcode"
"$root/scripts/materialize-fixture.sh" "$root/benchmarks/fixtures/payment-platform" "$work/payment-platform" >/dev/null
"$B" workspace create payment-platform 2>/dev/null || "$B" workspace use payment-platform
for r in "$work"/payment-platform/*; do "$B" workspace add "$r" 2>/dev/null || true; done
"$B" index
"$B" intel search CreatePayment -r payment-service
"$B" intel trace PaymentCharged -r payment-service
for p in payment-service gateway ledger-service infrastructure shared-protos; do
  echo "== schema $p"; (cd /tmp && codebase-memory-mcp cli --quiet get_graph_schema "{\"project\":\"payment-platform.$p\"}")
done
(cd /tmp && codebase-memory-mcp cli --quiet index_repository "{\"repo_path\":\"$work/payment-platform/gateway\",\"name\":\"payment-platform.gateway\",\"mode\":\"cross-repo-intelligence\",\"target_projects\":[\"*\"]}")
