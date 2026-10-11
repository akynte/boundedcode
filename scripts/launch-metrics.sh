#!/usr/bin/env bash
# Saves a snapshot of the repository's GitHub metrics, for the launch
# evaluation (docs/public-launch/adoption-metrics.md). Read-only: it calls
# the GitHub API and writes files, nothing else.
#
# GitHub keeps traffic data (views, clones, referrers) for 14 days only, so
# run this at least weekly or the data is lost. Traffic needs push access to
# the repository: run it as the maintainer, with `gh auth login` done.
#
# usage: scripts/launch-metrics.sh [OUT_DIR]   (default ~/boundedcode-metrics)
#
# Writes OUT_DIR/<UTC timestamp>/*.json (the raw API responses) and
# summary.md, which subtracts the release downloads made by this
# repository's own smoke workflow. The traffic data is aggregate, but it is
# owner-only, so keep the raw files out of the repository and publish only
# what the evaluation report needs.
set -euo pipefail

repo="${BC_METRICS_REPO:-akynte/boundedcode}"
out="${1:-$HOME/boundedcode-metrics}/$(date -u +%Y-%m-%dT%H%M%SZ)"
mkdir -p "$out"

api() { gh api -H "Accept: application/vnd.github+json" "$@"; }

api "repos/$repo" >"$out/repo.json"
api "repos/$repo/releases?per_page=100" >"$out/releases.json"
for t in views clones; do
  api "repos/$repo/traffic/$t?per=day" >"$out/traffic-$t.json"
done
api "repos/$repo/traffic/popular/referrers" >"$out/traffic-referrers.json"
api "repos/$repo/traffic/popular/paths" >"$out/traffic-paths.json"
api --paginate "repos/$repo/issues?state=all&per_page=100" >"$out/issues.json"

# The smoke workflow's installer-unix jobs install the newest published
# release on ubuntu-latest (linux-amd64) and macos-latest (darwin-arm64):
# one binary and one SHA256SUMS each, whenever their "newest published
# release" step runs.
latest="$(jq -r '[.[] | select(.draft | not)][0].tag_name' "$out/releases.json")"
since="$(jq -r '[.[] | select(.draft | not)][0].published_at' "$out/releases.json")"
ci_linux=0 ci_mac=0
for id in $(api "repos/$repo/actions/workflows/smoke.yml/runs?per_page=100&created=>=$since" --jq '.workflow_runs[].id'); do
  while read -r name; do
    case "$name" in
      *ubuntu*) ci_linux=$((ci_linux + 1)) ;;
      *macos*) ci_mac=$((ci_mac + 1)) ;;
    esac
  done < <(api "repos/$repo/actions/runs/$id/jobs" --jq '.jobs[]
    | select(.name | startswith("installer-unix"))
    | select(any(.steps[]; (.name | contains("newest published release")) and .conclusion != "skipped"))
    | .name')
done

asset() { jq -r --arg t "$latest" --arg n "$1" '.[] | select(.tag_name == $t) | .assets[] | select(.name == $n) | .download_count' "$out/releases.json"; }
linux="$(asset boundedcode-linux-amd64)"
mac="$(asset boundedcode-darwin-arm64)"
sums="$(asset SHA256SUMS)"

{
  echo "# Metrics snapshot $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo
  echo "Repository: $repo. Raw API responses are in this directory."
  echo
  jq -r '"- Stars \(.stargazers_count), forks \(.forks_count), watchers \(.subscribers_count), open issues and PRs \(.open_issues_count)"' "$out/repo.json"
  jq -r '"- Views, last 14 days: \(.count) (\(.uniques) unique visitors)"' "$out/traffic-views.json"
  jq -r '"- Clones, last 14 days: \(.count) (\(.uniques) unique cloners; includes CI checkouts)"' "$out/traffic-clones.json"
  echo "- Referrers: $(jq -r 'map("\(.referrer) \(.count)/\(.uniques)") | join(", ")' "$out/traffic-referrers.json")"
  echo
  echo "## Downloads of $latest (published $since)"
  echo
  echo "| Asset | Downloads | By this repository's smoke workflow | Remainder |"
  echo "|---|---|---|---|"
  echo "| boundedcode-linux-amd64 | $linux | $ci_linux | $((linux - ci_linux)) |"
  echo "| boundedcode-darwin-arm64 | $mac | $ci_mac | $((mac - ci_mac)) |"
  echo "| SHA256SUMS | $sums | $((ci_linux + ci_mac)) | $((sums - ci_linux - ci_mac)) |"
  echo
  echo "Other assets:"
  jq -r --arg t "$latest" '.[] | select(.tag_name == $t) | .assets[]
    | select(.name | IN("boundedcode-linux-amd64", "boundedcode-darwin-arm64", "SHA256SUMS") | not)
    | "- \(.name): \(.download_count)"' "$out/releases.json"
  echo
  echo "The remainder still includes the maintainer's own downloads and any"
  echo "other automation. A download is not an installation, and not a user."
} >"$out/summary.md"

cat "$out/summary.md"
