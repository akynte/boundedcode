# Feedback process

How feedback from people outside the project is collected, reproduced,
classified, prioritized and resolved, and how it changes what gets built
next. The indicators it produces are defined in
[adoption-metrics.md](adoption-metrics.md). The first 14 days after a launch
follow [launch-evaluation-checklist.md](launch-evaluation-checklist.md).

The project has one maintainer. The process is sized for that: one place
to report, a short label set, and a weekly review instead of dashboards.

## Where feedback comes in

**GitHub issues are the only intake.** Each form asks for what a fix or a
measurement needs:

| Form | Use it for | Label |
|---|---|---|
| [First-run report](https://github.com/akynte/boundedcode/issues/new?template=first_run.yml) | How the first install and task went, **including when it worked** | `first-run` |
| [Installation or setup problem](https://github.com/akynte/boundedcode/issues/new?template=installation.yml) | Installer, `setup`, build from source, sandbox image | `installation` |
| [Bug report](https://github.com/akynte/boundedcode/issues/new?template=bug_report.yml) | Anything else that does not work as documented | `bug` |
| [Wrong verification result](https://github.com/akynte/boundedcode/issues/new?template=verification_accuracy.yml) | A false positive or false negative | `verification-accuracy` |
| [Platform compatibility](https://github.com/akynte/boundedcode/issues/new?template=platform_compatibility.yml) | What worked or failed on an OS, architecture or engine | `platform` |
| [Model compatibility](https://github.com/akynte/boundedcode/issues/new?template=model_compatibility.yml) | A local model on your hardware | `model-compatibility` |
| [Evaluation result](https://github.com/akynte/boundedcode/issues/new?template=evaluation_result.yml) | A rerunnable benchmark result, favourable or not | `evaluation-result` |
| [Feature proposal](https://github.com/akynte/boundedcode/issues/new?template=feature_proposal.yml) | A problem the current behaviour does not solve | `enhancement` |

Security problems go through private vulnerability reporting
([SECURITY.md](../../SECURITY.md)) and are outside this process.

**Feedback that arrives elsewhere** (a comment under a post, a reply to a
message): thank the person and point them to the matching form. If they
would rather not open an issue, ask whether you may record it. Only with
their agreement, open an issue yourself from
[templates/external-feedback.md](templates/external-feedback.md), without
their name unless they want it there. Without agreement, the feedback can
still shape your own thinking, but it is not quoted or counted.

## Privacy

- **No telemetry, no tracking.** BoundedCode sends nothing about its use
  ([adoption-metrics.md](adoption-metrics.md#no-telemetry)). Links in
  posts are not tagged or shortened.
- **Ask only for what a fix needs.** The forms ask for versions, platform
  and output, and remind reporters to remove secrets, private paths and
  proprietary code. `doctor --json` includes local paths, and the forms say
  so.
- **Clean up leaks fast.** If a report contains a credential or private
  code, hide the comment (or edit the issue) at once and tell the reporter
  to rotate the credential. Do not quote it in your reply.
- **Quote only with consent.** The first-run form asks whether a report may
  be quoted, with or without the reporter's handle. Other forms do not, so
  ask in the thread before quoting them outside the issue.
- **No contact lists.** Reporters are not added to mailing lists or
  followed up outside the issue thread.

## Labels

Type labels come from the forms (table above). Two more groups are needed
for triage:

| Group | Labels | Meaning |
|---|---|---|
| Status | `needs-repro` | Not yet reproduced; waiting on the maintainer or on information |
| | `reproduced` | Reproduced by the maintainer; a reproduction record is in the thread |
| | `cannot-reproduce` | Tried and failed to reproduce with the information given |
| Priority | `p0` to `p3` | See [Prioritize](#3-classify-and-prioritize) |

`first-run`, the status labels and the priority labels are created by the
commands in the [checklist](launch-evaluation-checklist.md#before-day-0).
GitHub silently drops a form's label if it does not exist.

## Triage

Every new issue goes through these steps. The aim during the launch window
is a first response within **one working day** for `verification-accuracy`
and within **two working days** for everything else. That is a target for
one maintainer, not a guarantee; say so if it slips.

### 1. Acknowledge and check completeness

- Thank the reporter. Confirm what you understood in one sentence.
- If the form's required facts are missing (version, platform, exact
  command, output), ask for them specifically, and label `needs-repro`.
- A first-run report that says everything worked needs no reproduction.
  Record it in the review log, thank the reporter, and close it as
  completed.

### 2. Reproduce

Reproduce **before** fixing, on the closest environment available:

| Report | Reproduce on |
|---|---|
| Linux install or setup | A clean container of the same distribution (as in [onboarding validation](onboarding-validation.md#environments)) |
| macOS or Windows | The `smoke.yml` jobs, or a fork's workflow run with the reporter's steps added; ask the reporter to try a candidate fix if no runner reproduces it |
| A task result | The same public repository and commit, the same task text and, if possible, the same model or provider |
| A wrong verification result | The same, plus the reporter's diff applied in a task's worktree and `boundedcode verify TASK --full` rerun on it |

Post a reproduction record ([templates/reproduction-record.md](templates/reproduction-record.md))
in the thread, whether or not it reproduced. Then label `reproduced` or
`cannot-reproduce`. A `cannot-reproduce` issue stays open for 14 days
after the last request for information, then is closed with a note that it
reopens on new information.

### 3. Classify and prioritize

Each reproduced issue gets one **class**:

| Class | Examples |
|---|---|
| Verification accuracy | False `TASK_VERIFIED`; checks green on a change that does nothing; a correct change refused |
| Safety | Agent effect outside the sandbox or worktree; a dangerous command not blocked. Usually a SECURITY.md matter. |
| Reliability | Crash, hang, lost task state, a task that cannot be resumed, a flaky check |
| Installation and first run | Installer, `setup`, image build, provider configuration, first task never starting |
| Documentation and onboarding | Steps that are wrong, missing or misleading; unclear messages |
| Platform | Works on Linux, fails on macOS or Windows (both experimental) |
| Model or provider | A model or provider that fails to produce usable tool calls |
| Positioning | The reporter expected something the project does not claim to do |
| Feature request | Works as designed; the design does not cover the need |

And one **priority**:

| Priority | Rule | Response |
|---|---|---|
| `p0` | A reproduced false verification pass; any safety defect; data loss; a release asset that fails its checksum | Stop other work. Fix, add a regression test, publish an erratum next to any affected published result, and release. |
| `p1` | Install or first task fails on a validated platform (Linux x86-64) following the documented steps; a crash or hang in a documented path; a false negative that blocks a correct change | Fix in the current iteration. |
| `p2` | Failures on experimental platforms; documentation that misled; a workaround exists | Fix when it fits the iteration; documentation fixes go first. |
| `p3` | Minor defects, polish, feature requests that do not yet meet the [feature criteria](#adding-features) | Record. Revisit at the iteration review. |

The same failure reported by two independent people moves up one level
(not above `p1` unless it meets the `p0` rule).

### 4. Resolve

- **Fix with a test.** A reproduced defect gets a regression test that
  fails before the fix and passes after it. The commit or pull request
  links the issue.
- **Correct published claims.** If the defect affects a number or a claim
  in the README or a report, correct it in the same change, and keep the
  old text visible as an erratum (the reports are never rewritten).
- **Close with evidence.** The closing comment names the fix, the release
  it ships in, and how the reporter can check it. Ask the reporter to
  confirm, but do not wait on it to close.
- **Not fixed is an answer.** Close `wontfix` with the reason if the
  behaviour is intended, and say whether the documentation changed instead.

## Weekly review

Once a week during the launch window, then monthly:
1. Run `scripts/launch-metrics.sh`.
2. Go through every issue opened or updated since the last review. Every
   one should have a type, a status and, if reproduced, a priority.
3. Add a row per report to the review log (in
   [the checklist](launch-evaluation-checklist.md#review-log)): who (handle
   or "anonymous"), indicator, class, priority, status.
4. Check the decision criteria below against the log.
5. Pick the next week's work from the result.

## Decision criteria

The question at each review is not "what do people want?" but "what did
they hit, and how sure are we?". Counts are independent reporters,
excluding the maintainer.

### Improving onboarding

Change the installer, `setup` or the getting-started docs when:
- **one** reporter shows that a documented step is wrong (reproduced); or
- **two** reporters stop at the same step, even if each found a
  workaround; or
- a first-run report takes more than three hours to reach a first result
  on a validated platform, and the reason is in the setup, not the hardware.

Fix the step itself before adding explanation around it. Re-run the
onboarding smoke tests after the change.

### Correcting reliability defects

- A reproduced false verification pass is `p0`, from a single report. It
  takes precedence over everything else, including the launch schedule.
- A reproduced crash, hang or lost state in a documented path is `p1` from
  a single report.
- A failure that cannot be reproduced after the reporter has answered two
  rounds of questions is recorded, not fixed blind. Two independent reports
  of the same symptom reopen it.

### Revising positioning

The README describes BoundedCode as a control plane for AI coding agents
that does not treat a green test run as proof: it runs an agent in a
network-less sandbox and reports a change as verified only with
behavioural evidence. It is not an autonomous merge bot, an IDE assistant
or a model. Revise the
README's opening and the getting-started introduction when:
- **two** first-run reports describe a different expectation in "What you
  expected BoundedCode to be"; or
- the same misreading appears in **three** comments or questions anywhere.

Change the wording, not the claims. Positioning changes may not make a
claim the evidence does not support.

### Expanding validation

Run a new held-out validation (new report, selection rule fixed in
advance, as in [the second validation](../benchmarks/second-independent-validation-2026-10.md)) when:
- any false verification pass is reproduced (after the fix, to check that
  the class of failure is gone); or
- an external evaluation result disagrees with a published result by more
  than run-to-run variation explains; or
- **two** reporters use BoundedCode on a language, repository size or task
  type that no published validation covers.

Until then, published results stay as they are and external results are
added next to them.

### Adding features

Build a feature only when all of these hold:
- **two** independent users describe the same blocked workflow, with a
  concrete repository or task;
- they tried the existing commands or configuration (the proposal form
  asks for this);
- it does not weaken the sandbox, the command policy or the verification
  gate;
- the open `p0` and `p1` defects are done.

Architectural features also need an ADR first. A request that meets only
some of these is recorded as `p3` with the missing conditions listed.

### When nothing comes in

Few or no reports in 14 days is also a result. Read it with the traffic:
- **No visitors:** the launch did not reach anyone. That is a distribution
  problem; do not change the product because of it.
- **Visitors but no first-run reports:** something stops people before
  they try, or they try and leave silently. Re-run the onboarding path on a
  clean machine yourself, check what the README asks of a visitor (time,
  hardware), and make the first-run form easier to find.
- **Do not fill the gap** with invented feedback, solicited stars or
  friendly reviews presented as independent.

## From feedback to roadmap

- The roadmap is [the implementation status](../development/status.md)
  and the open issues. After each iteration review, update the status page
  and link the issues that drove each change.
- Every planned item names the evidence behind it: issue numbers, or "no
  external evidence yet" for maintainer-driven work. Both are allowed; the
  label must be accurate.
- Items without evidence are not removed, but they wait behind items with
  it, unless they are `p0` or `p1`.
- External reports are never merged into development evidence. Results
  from tasks the maintainer used to drive a fix stay development evidence
  ([CONTRIBUTING](../../CONTRIBUTING.md#benchmarks-and-validation)).

## From feedback to case studies

A case study built on external feedback follows the same rules as the
[engineering case study](engineering-case-study.md):
- **Structure:** the report, the reproduction, the root cause, the fix
  and its regression test, and what changed in the published claims.
  Link the issue, the reproduction record and the commits.
- **Consent:** quote a reporter only as their first-run form or thread
  allows. Otherwise describe the report without quoting or naming them.
- **No inflation:** give the number of reports a finding rests on. "One
  external user" is a fine thing to write.
- **Failures count:** a case study about a defect someone else found is
  more useful than one about a success, and is published the same way.

## First iteration after the launch window

These are commitments made before any feedback arrives, so that the first
iteration is not chosen by whichever report was loudest.

1. **Everything `p0`**, first, whatever else is planned.
2. **Reproduced `p1` failures** in installation, first run and documented
   task paths on Linux x86-64.
3. **The onboarding step where most reporters stopped**, if the onboarding
   criteria are met. Fix the step, then the documentation.
4. **macOS and Windows:** if reports show the full flow working or failing
   there, update the [platform matrix](onboarding-validation.md#platform-compatibility-matrix)
   with the reports cited, and fix failures as `p2`. No platform becomes
   "validated" on one report.
5. **Positioning text**, if its criterion is met.
6. **A validation run**, if its criterion is met.
7. **Features** only as the feature criteria allow. Nothing else gets
   built in this iteration.

The iteration ends with a public evaluation report,
`docs/public-launch/launch-evaluation-<date>.md`, written from the review
log and the metrics snapshots. It gives every indicator, including zeros,
says which numbers are owner-reported, and lists what changed because of
which report. If there was too little feedback to meet any criterion, the
report says that, and the next iteration works on reaching testers rather
than on the product.

Not in the first iteration, whatever the feedback:
- telemetry or analytics;
- claims of adoption, popularity or production readiness;
- removing or softening published negative results.
