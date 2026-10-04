#!/usr/bin/env bash
# Verify every non-merge commit carries a Signed-off-by trailer matching its
# author (Developer Certificate of Origin, see /DCO).
#
# usage: scripts/check-dco.sh <base> <head>   commits in base..head
#        scripts/check-dco.sh --root <head>   every commit reachable from head,
#                                             including the root commit
# An all-zero base (a newly pushed branch) is treated as --root.
set -euo pipefail
base="${1:?base or --root}"; head="${2:?head}"
if [ "$base" = "--root" ] || [ -z "${base//0/}" ]; then
  range=("$head")
else
  range=("$base..$head")
fi
fail=0
for c in $(git rev-list --no-merges "${range[@]}"); do
  author="$(git show -s --format='%an <%ae>' "$c")"
  if ! git show -s --format='%(trailers:key=Signed-off-by,valueonly)' "$c" | grep -qxF "$author"; then
    echo "commit $c is missing 'Signed-off-by: $author'"
    fail=1
  fi
done
if [ "$fail" -ne 0 ]; then
  echo "Sign off with: git commit -s (or git rebase --signoff <base>, or --root)"
  exit 1
fi
echo "DCO ok"
