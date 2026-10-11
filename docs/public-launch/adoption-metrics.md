# Adoption metrics

This page defines what counts as evidence that BoundedCode works for
someone other than its maintainer, where each measurement comes from, and
what it can and cannot show. It also records the baseline before any public
launch.

Two rules apply throughout:
- **Popularity is not correctness.** Stars, views and downloads say that
  people looked. Whether BoundedCode does what it claims (a verified change
  is a correct change) is settled only by reproduced tasks and reports of
  wrong verification results.
- **Nothing here is estimated.** A number is either read from a named
  source on a stated date or it is not reported. Where a source cannot
  separate external users from the maintainer or CI, that is stated next to
  the number.

How the feedback behind these indicators is collected, triaged and acted
on is in [feedback-process.md](feedback-process.md). The 14-day plan is in
[launch-evaluation-checklist.md](launch-evaluation-checklist.md).

## No telemetry

BoundedCode collects no usage data, and this process does not add any.
Checked in the code on 2026-10-11:
- The binary has no analytics or update-check endpoint. Its outbound
  connections are the model provider you configure, downloads you start
  (`setup` and the installers: GitHub releases, Hugging Face), and contained
  Codex escalation if you enable it ([SECURITY.md](../../SECURITY.md)).
- The agent's container runs with `--network none`.

Adding telemetry later would need an ADR, an opt-in default and a demonstrated
question that voluntary reports cannot answer. None of the indicators below
needs it.

## Indicators

Ordered from the weakest evidence to the strongest.

| # | Indicator | What it shows | What it does not show | Source | Access |
|---|---|---|---|---|---|
| 1 | Unique visitors and page views | Someone opened the repository | Interest, use, or that they are not the maintainer | Insights → Traffic; `traffic/views` API | **Owner only.** 14 days of history. |
| 2 | Referrers and popular pages | Which channel and page brought visitors | Who they were | Insights → Traffic; `traffic/popular/*` API | **Owner only.** 14 days, top 10. |
| 3 | Unique cloners | A clone happened | A build or a user. Every CI run clones. | Insights → Traffic; `traffic/clones` API | **Owner only.** 14 days. |
| 4 | Release downloads, per asset | An asset was fetched | An installation. This repository's smoke workflow downloads the newest release on every run. | Releases API (`download_count`) | Public. Cumulative, no dates. |
| 5 | Stars, forks, watchers | A bookmark or a copy | Use, or a contribution | Repository page and API | Public |
| 6 | **Reproduced installations** | Someone else installed it and `setup --check` passed, on a named platform | That a task works | First-run and platform compatibility reports | User-reported |
| 7 | **First successful external tasks** | A task in someone else's repository ended with a result they checked against the diff | That the result was right, unless they read the diff | First-run reports (`result` and `correct` fields) | User-reported |
| 8 | **Independent evaluation reports** | A rerunnable result under [the submission rules](../../benchmarks/submitting-results.md) | Generality beyond its task set | Evaluation result issues and pull requests | User-reported, public |
| 9 | **Actionable bug reports** | A defect someone else hit, with enough detail to reproduce | How common it is | Issue forms | User-reported, public |
| 10 | **Wrong verification results** | A false positive or negative in the central claim | Rate, from one report | "Wrong verification result" form | User-reported, public |
| 11 | Repeat use | The same person reports a second task or comes back with another issue | Use that nobody reports | Issue authors over time | Public, only when visible |
| 12 | External contributions | Someone outside the project changed it | Use | Pull requests by others; DCO sign-offs | Public |

Indicators 6 to 10 are the ones this evaluation is about. Indicators 1 to 5
are recorded to see whether the launch reached anyone at all, and to
explain an absence of reports (no visitors, or visitors who did not try it).

### Counting rules

- **The maintainer is excluded.** Issues, tasks and evaluations by
  `akynte`, or run on the maintainer's machines, are development evidence,
  never adoption.
- **One person, one count** per indicator. A second report from the same
  person counts toward repeat use (indicator 11), not toward 6 or 7.
- **A reproduced installation** needs a version, a platform and either a
  `setup --check` result or a stated failure point. A report without them
  is feedback, but not counted as a reproduction.
- **A successful task** (indicator 7) is one with a final result and the
  reporter's answer to "was the change correct?". Results reported without
  that answer are counted separately as "result, correctness unknown".
- **Downloads** are reported with the CI share subtracted (the script below
  does this) and never as a number of users.
- **Self-reported intent** ("would use again") is reported as what it is,
  next to observed repeat use, never instead of it.

## Collecting the numbers

| Measurement | How | Who can do it |
|---|---|---|
| Traffic (1–3) | `scripts/launch-metrics.sh`, or Insights → Traffic in a browser | Maintainer (push access) |
| Downloads (4), stars and forks (5) | `scripts/launch-metrics.sh`, or `gh api repos/akynte/boundedcode/releases` | Anyone |
| Reports (6–12) | The issue list, filtered by label (`first-run`, `installation`, `platform`, `verification-accuracy`, `evaluation-result`, `bug`) | Anyone |

`scripts/launch-metrics.sh` saves the raw API responses and a summary to a
dated directory outside the repository (default `~/boundedcode-metrics`).
It only reads. **GitHub deletes traffic data after 14 days**, so it must
run at least weekly during the evaluation, or the first week's traffic is
lost.

Measurements that need **manual access to GitHub Insights** (or the
maintainer's token): views, unique visitors, clones, unique cloners,
referrers and popular pages. Nobody else can check these numbers, so a
public report quotes them with their date and says they are owner-reported.

Not measured, on purpose:
- who visitors or downloaders are;
- anything from inside the binary;
- click or link tracking (UTM tags, shortened links). Referrers already
  show the channel.

## Baseline before launch

Read on 2026-10-11 at 00:45 UTC, before any public announcement. The
v0.1.0-alpha.5 release was published on 2026-10-10.

| Indicator | Value | Reading |
|---|---|---|
| Views, 14 days | 148 views, 17 unique visitors | Mostly the maintainer: 44 views from 2 visitors came via github.com, and release-editing pages appear in the popular paths. 1 referral from LinkedIn, 1 from Google. |
| Clones, 14 days | 517 clones, 136 unique cloners | Dominated by GitHub Actions: every CI and smoke run checks out the repository. Not evidence of users. |
| v0.1.0-alpha.5 downloads | linux-amd64 6, darwin-arm64 6, `SHA256SUMS` 9, every other asset 1 | 4 + 4 binaries and 8 `SHA256SUMS` were the smoke workflow's installer jobs. The remaining single download of every asset matches one full-release check, most likely the maintainer's. **No external download can be shown.** |
| Stars, forks, watchers | 0, 0, 0 | |
| Issues by others | 0 | Issues #1–#4 were filed by the maintainer as first-contribution items. |
| Pull requests by others | 0 | |
| Indicators 6–12 | 0 | Nothing has been reported yet. |

The baseline is close to zero external adoption, as expected before a
launch. The evaluation compares against it.
