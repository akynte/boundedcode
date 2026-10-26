# Trail study (2026-10-27)

The plan's "traces" item was to record which files successful runs touch
and, later, let that steer the agent. Every git repository already holds
such a trail: which files its commits change together. Before recording
anything, the study measured whether that history predicts the files a
change needs (`bcode bench trail`, `internal/benchmark/trail.go`). No model
is involved.

**Method.** For each recent commit that changes Go code (at most 30
non-test files), files are suggested from the 1,000 commits before it
(larger commits, such as mass renames, are not counted) and compared with
the files the commit changed outside tests that existed before it.

- *At request time*: the files the commit message names (the lease
  deriver, message without its `pkg:` prefix); the same plus the five
  files most often changed with them; the ten files changed most often.
- *At edit time*: for each changed file, up to three files changed with it
  in at least *support* earlier commits and in at least *confidence* of the
  commits that changed it, an association rule as in Zimmermann et al.,
  "Mining Version Histories to Guide Software Changes" (2004). This is what
  a note after an edit would have said. A note on a single-file commit is
  wrong by construction, and is reported separately as the nag rate.

**Pass bars, fixed before the first run.**

- *Request-time hint:* in 2 of 3 repositories, adding the co-changed files
  raises file recall by at least 10 points, and at least 20% of the added
  files were changed.
- *Edit-time note:* at support 3 and confidence 0.5, in 2 of 3
  repositories, at least 50% of suggestions are right; at least 15% of
  multi-file commits get a right note; a note fires on at most 10% of
  single-file commits.

## First run: the newest commits

Repositories: golang.org/x/tools at `86210da64`, spf13/cobra at `adbc881`
and BoundedCode itself. Report:
`benchmarks/reports/20261026T221030Z-trail-study.md`.

| Repository | Commits | Named: recall | Named + 5 co-changed: recall | Added files changed | 10 most changed: recall |
|---|---|---|---|---|---|
| x/tools | 100 | 22% | 32% | 11% | 12% |
| cobra | 100 | 11% | 15% | 5% | 52% |
| boundedcode | 65 | 3% | 7% | 27% | 25% |

| Repository | Support / confidence | Suggestions | Right | Multi-file commits with a right note | Single-file commits with a note |
|---|---|---|---|---|---|
| x/tools | 3 / 0.5 | 134 | 65% | 35% | 18% |
| x/tools | 5 / 0.7 | 44 | 84% | 24% | 4% |
| cobra | 3 / 0.5 | 89 | 57% | 42% | 9% |
| cobra | 5 / 0.7 | 9 | 78% | 9% | 2% |
| boundedcode | 3 / 0.5 | 473 | 56% | 54% | 17% |
| boundedcode | 5 / 0.7 | 118 | 81% | 37% | 0% |

Both pass bars failed.

- *Request-time hint:* only x/tools gains 10 points, and there just 11%
  of the added files were changed.
- *Edit-time note at 3 / 0.5:* the note fires on 17–18% of single-file
  commits in two repositories.
- *The stricter 5 / 0.7:* it looked better, but it was chosen after seeing
  these numbers.

So it was tested on commits that had not been looked at.

## Second run: held-out commits, support 5 / confidence 0.7

The held-out sets, fixed with the bar before the run:
- x/tools: the 200 qualifying commits older than the first run's;
- cobra: the same;
- GitHub's `cli/cli` (`gh-cli` in the report, at `63837fb77`): its newest
  200 commits. It was not studied before.

The bar is the same as the edit-time bar above, at 5 / 0.7. Reports:
`benchmarks/reports/20261026T221045Z-trail-heldout-*.md`.

| Repository | Suggestions | Right | Multi-file commits with a right note | Single-file commits with a note |
|---|---|---|---|---|
| x/tools | 83 | 70% | 17% (of 98) | 5% |
| cobra | 20 | 70% | 12% (of 66) | 2% |
| cli/cli | 124 | 81% | 24% (of 88) | 7% |

x/tools and cli/cli pass; cobra misses on coverage only. The bar is met.

**What the right notes say.**
- x/tools:
  - `go/analysis/passes/modernize/doc.go` names `gopls/doc/analyzers.md` and `gopls/internal/doc/api.json`, which are generated from it;
  - `gopls/internal/protocol/command/interface.go` names `command_gen.go` and `server/command.go`.
- cli/cli:
  - `pkg/cmd/issue/comment/comment.go` names `pkg/cmd/pr/comment/comment.go`;
  - the telemetry interface names its test fake.
- In all three repositories: `go.mod` names `go.sum`.

These are the companion changes a small model finishes without: a
regenerated file, the mirror command, the fake that implements an
interface.

## Decision

- **Request-time hint:** not built.
- **Edit-time note:** built as `direct.history_notes` (default on).
  - After a file change in a git repository, the tool result names up to
    three files that commits changing that file also changed, at support
    5 and confidence 0.7, over the last 1,000 commits.
  - It skips files changed or already named in the session.
  - Reading the history takes about 40 ms on x/tools; it is done once per
    session.
- **Recorded traces of the agent's own runs:** not built. The repository's
  own history already gives the trail, with no cold start.

**Not measured yet: the effect on the agent.** The direct suite's fixtures
have one commit, so the note never fires there. Replay tasks were drafted
(not yet in the repository, since they are not validated):
four cli/cli commits rerun on their parents, with the history before
them, using the new `githistory:` task source, each with the note on and
off.
- Three tasks have a companion file the note names.
- In one the note names files the change does not need.

Checking them means running cli/cli's own build and tests on this machine,
which needs the user's approval. Until then the note rests on the study's
precision, not on agent runs.
