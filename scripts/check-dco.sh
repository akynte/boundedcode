#!/usr/bin/env bash
# Verify every commit in BASE..HEAD carries a Signed-off-by trailer matching
# its author (Developer Certificate of Origin, see /DCO).
set -euo pipefail
base="${1:?base}"; head="${2:?head}"
fail=0
for c in $(git rev-list --no-merges "$base..$head"); do
  author="$(git show -s --format='%an <%ae>' "$c")"
  if ! git show -s --format='%(trailers:key=Signed-off-by,valueonly)' "$c" | grep -qxF "$author"; then
    echo "commit $c is missing 'Signed-off-by: $author'"
    fail=1
  fi
done
if [ "$fail" -ne 0 ]; then
  echo "Sign off with: git commit -s (or git rebase --signoff $base)"
  exit 1
fi
echo "DCO ok"
