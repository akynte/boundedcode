# 14-day launch evaluation checklist

A day-by-day plan for the first 14 days after a public launch. It produces
the numbers defined in [adoption-metrics.md](adoption-metrics.md) and runs
the triage and decision rules in [feedback-process.md](feedback-process.md).

**Status: not started.** No launch has happened and no external feedback
has been collected. Day 0 is the day of the first public post. Fill in the
dates and the review log as the days pass; do not fill them in ahead.

| | Date |
|---|---|
| Day 0 (first public post) | |
| Day 7 (midpoint review) | |
| Day 14 (evaluation) | |

## Before day 0

Do not start the clock until these are done.

- [ ] **The first task works on a clean machine without a GPU.** The
      `smoke.yml` first-task job passed on `ubuntu-latest` on 2026-10-10
      (run 38091633797, commit `c2a3ace`). Re-run it on the commit you
      launch from: `gh workflow run smoke.yml`.
- [ ] **Hardware expectations are written down** in the getting-started
      guide: what runs without a GPU, what a local model needs, and the
      sandbox image's size and build time (1 min 40 s to about 8 min on a
      GitHub runner).
- [ ] **One real task has run with a cloud or other OpenAI-compatible
      provider**, by the maintainer, with the result recorded.
- [ ] **Labels exist** (a form's label is silently dropped if it does not):

      ```sh
      gh label create first-run -c 0052cc -d "First-run report"
      gh label create needs-repro -c d4c5f9 -d "Not yet reproduced"
      gh label create reproduced -c 0e8a16 -d "Reproduced; record in the thread"
      gh label create cannot-reproduce -c cfd3d7 -d "Could not reproduce with the information given"
      gh label create p0 -c b60205 -d "Stop other work: false verification pass, safety, data loss"
      gh label create p1 -c d93f0b -d "Fix this iteration"
      gh label create p2 -c fbca04 -d "Fix when it fits"
      gh label create p3 -c c5def5 -d "Recorded; revisit at review"
      ```

- [ ] **Every issue form renders** at
      [/issues/new/choose](https://github.com/akynte/boundedcode/issues/new/choose),
      including the new first-run form, and the security link opens private
      reporting.
- [ ] **Notifications reach you** for new issues (Watch → Custom → Issues,
      or All Activity).
- [ ] **Time is reserved:** about 30 minutes a day for triage for the 14
      days, and longer slots on days 7 and 14.
- [ ] **Baseline snapshot:** `scripts/launch-metrics.sh`. The pre-launch
      baseline from 2026-10-11 is in
      [adoption-metrics.md](adoption-metrics.md#baseline-before-launch);
      take a fresh one on day 0 before posting.

## Days 1 to 14

### Every day

- [ ] Read new issues, comments and replies to the launch posts.
- [ ] Respond within the targets: one working day for
      `verification-accuracy`, two for everything else. Point feedback that
      arrives elsewhere to the matching form.
- [ ] Label each new issue with its type and `needs-repro`. Close
      first-run reports that say everything worked, after recording them.
- [ ] Move anything that looks like a vulnerability to private reporting.

### Days 3 and 10

- [ ] Run `scripts/launch-metrics.sh`. GitHub keeps traffic for 14 days
      only, so these runs, with days 0, 7 and 14, keep a complete record.

### Day 7: midpoint review

- [ ] Snapshot: `scripts/launch-metrics.sh`.
- [ ] Every open issue has a type and status; reproduced ones have a
      priority and a reproduction record.
- [ ] Review log updated (below).
- [ ] Check the decision criteria. Start any `p0` or `p1` fix now; do not
      wait for day 14.
- [ ] If there are no first-run reports, read the traffic snapshots:
      no visitors, or visitors who did not try it
      ([When nothing comes in](feedback-process.md#when-nothing-comes-in)).
      Do not change the product because of a lack of reach.

### Day 14: evaluation

- [ ] Final snapshot: `scripts/launch-metrics.sh`.
- [ ] Fill in the results table below from the review log and snapshots.
- [ ] Apply the [decision criteria](feedback-process.md#decision-criteria)
      and list which ones were met, by which issues.
- [ ] Choose the first iteration using
      [the pre-committed order](feedback-process.md#first-iteration-after-the-launch-window).
- [ ] Write `docs/public-launch/launch-evaluation-<date>.md`: the results
      table, every indicator including zeros, which numbers are
      owner-reported, the criteria met, the iteration plan, and what
      changed because of which report.
- [ ] Update [the implementation status](../development/status.md) and the
      platform matrix in [onboarding validation](onboarding-validation.md)
      with any reports that justify it, citing the issues.

## Results

Filled in on day 14 from the review log. Counts exclude the maintainer.

| Indicator | Day 0 | Day 14 | Notes |
|---|---|---|---|
| Unique visitors, last 14 days (owner-reported) | | | |
| Top referrers (owner-reported) | | | |
| Release downloads, CI share subtracted | | | Not a count of users |
| Stars, forks | | | |
| First-run reports | | | Of which: everything worked / stopped before a task / task result |
| Reproduced installations, by platform | | | |
| First external tasks with a result, correctness checked | | | Of which TASK_VERIFIED; of which the reporter judged wrong |
| Independent evaluation reports | | | |
| Actionable bug reports (reproduced / not) | | | |
| Wrong verification results reported (reproduced) | | | |
| Repeat reporters | | | |
| External pull requests | | | |
| Median time to first response | | | Against the targets |

## Review log

One row per external report or contact, added as it is triaged. Use a
handle only if the reporter's issue is public under that handle; otherwise
"anonymous".

| Date | Issue | Reporter | Indicator (#) | Class | Priority | Status | Notes |
|---|---|---|---|---|---|---|---|
| | | | | | | | |
