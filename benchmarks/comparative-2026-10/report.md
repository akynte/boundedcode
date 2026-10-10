# Comparative evaluation (2026-10): BoundedCode vs. a plain OpenHands agent

**Results at a glance.**
- **Primary comparison** (corrected baseline arm): on 8 public tasks, with the
  same local model, agent SDK, sandbox and 60-minute limit, BoundedCode's
  changes passed the datasets' hidden acceptance tests on **7** tasks and the
  plain agent's on **6**.
- **The difference is one task.** With one run per system per task, it is
  within what sampling variation produces: the two baseline runs of that task
  disagree with each other. No difference in success rate is shown.
- **What the numbers do show:**
  - BoundedCode used about **2.3× the wall-clock time** and **2.1× the
    processed tokens**.
  - Replayed on the plain agent's 16 patches (both baseline arms),
    BoundedCode's verification gate **rejected all 4 that fail the hidden
    tests**, including one with a compile error and one that breaks an
    existing test. It **accepted only patches that pass**.
  - But **half its rejections (4 of 8) were patches that pass the hidden
    tests**: two for formatting and two for breaking an existing test that
    the dataset's own reference fix also breaks. It found **no defect that
    the hidden tests missed**.
  - On BoundedCode's own runs, the gate produced **one false verification**
    and **three missed verifications** of correct changes.

This report follows the frozen [protocol](protocol.md). Every change after the
freeze is listed in [deviations.md](deviations.md) (D1–D3). All numbers come
from `results/` (`summary.json`, `results.json`), produced by `analyze.py`
from the recorded runs.

All planned measurements are complete. One gate replay (corrected arm,
immutable-js-2006) was interrupted by a machine restart and run again from
the start. The interrupted record is kept as `*.interrupted.*` next to the
completed one.

## 1. What was compared

| | BoundedCode (B) | Plain agent (O) |
|---|---|---|
| Agent loop | OpenHands SDK 1.51.0 | the same |
| Model | Qwen3.6-35B-A3B UD-Q4_K_M, local llama.cpp, one shared server | the same |
| Sandbox | Docker, no network, the same image and environment (after D3) | the same |
| Added by BoundedCode | Task contract, context packs, verification gate (build, lint, tests, behavioural evidence), retries (up to 6 attempts), ledger | none: one session, the issue text verbatim |
| Limit | 60 min per task | 60 min per task |

**Tasks.** Eight SWE-bench Multilingual instances, none used in earlier
BoundedCode work. They were selected by a fixed hash ranking and an
environment gate, with no screening for difficulty or clarity:

| Stratum | Tasks |
|---|---|
| Development (repositories BoundedCode was developed on) | gin-gonic/gin ×2 |
| Unseen | immutable-js (JavaScript); bat and axum (Rust); phpspreadsheet and carbon (PHP); fluentd (Ruby) |

The Java slot stayed empty (its candidates target Java 7, which the
sandbox's JDK cannot build), and three slots had fewer valid candidates than
planned. The full ranking and screening are in `candidates.json` and
`screening.json`.

## 2. Results

### Primary comparison: BoundedCode vs. the corrected baseline arm

| Task | Stratum | B: hidden | B: verification | B: attempts | B: time | B: tokens | O: hidden | O: time | O: tokens | Gate replay of O's patch |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-3820 | dev | fail | task_verified | 3 | 40m54s | 327k | fail | 8m10s | 56k | rejected: gofmt |
| gin-4003 | dev | **pass** | task_verified | 2 | 7m34s | 61k | **pass** | 5m09s | 40k | rejected: gofmt |
| immutable-js-2006 | unseen | **pass** | task_verified | 3 | 16m19s | 127k | fail | 5m40s | 47k | rejected: tests run out of memory |
| bat-2393 | unseen | **pass** | task_verified | 1 | 17m53s | 63k | **pass** | 6m28s | 54k | `task_verified` |
| axum-691 | unseen | **pass** | task_verified | 1 | 11m12s | 63k | **pass** | 6m13s | 61k | `task_verified` |
| phpspreadsheet-3463 | unseen | **pass** | tests_green | 2 | 6m10s | 46k | **pass** | 8m25s | 52k | `tests_green` (no test stage) |
| carbon-3103 | unseen | **pass** | tests_green | 2 | 17m56s | 118k | **pass** | 4m36s | 31k | rejected: breaks `testSetTestNow` |
| fluentd-3616 | unseen | **pass** | tests_green | 2 | 8m30s | 58k | **pass** | 11m05s | 71k | `tests_green` (no test stage) |

**Paired outcomes.**

| | Count |
|---|---|
| Both pass | 6 |
| BoundedCode only | 1 (immutable-js-2006) |
| Baseline only | 0 |
| Neither | 1 (gin-3820) |

The exact McNemar p-value on the 1 vs 0 discordant pairs is 1.0. It is
reported for completeness only: eight tasks cannot establish a difference.

**By stratum:**
- **Development:** BoundedCode 1/2, baseline 1/2.
- **Unseen:** BoundedCode 6/6, baseline 5/6.

### Secondary: BoundedCode vs. the original baseline arm (environment defect, D3)

The original arm's agent lacked the offline build environment (deviations
D3).
- **Outcomes:** BoundedCode 7/8, original baseline 6/8.
- **Paired:** both 6, BoundedCode only 1 (**axum-691**), baseline only 0,
  neither 1 (gin-3820).

**The discordant task differs between the arms:**
- **axum-691:** the original baseline failed it with a compile error it
  could not see (its `cargo` could not run). The corrected baseline passed
  it.
- **immutable-js-2006:** the original baseline passed it, and the corrected
  baseline failed it.

So the identity of the one discordant task changed between two runs of the
same baseline. That is direct evidence that single-run differences here are
dominated by sampling variation (temperature 0.6), not by the systems.

### Effort and resources

| | BoundedCode | Corrected baseline | Ratio |
|---|---|---|---|
| Wall clock, all 8 tasks | 126 min | 56 min | 2.3× |
| Processed tokens (uncached prompt plus generated) | 862k | 413k | 2.1× |
| Generated tokens | 145k | 71k | 2.0× |
| Model calls | 338 | 212 | 1.6× |
| Attempts | 16 (8 tasks) | 8 (one session each) | |

- **Per task:** BoundedCode took longer on 6 of 8 tasks. It was faster on
  phpspreadsheet-3463 and fluentd-3616.
- **Model server:** the llama.cpp server's peak resident memory was
  25.8–28.4 GiB per run (26,412–29,102 MiB) for both systems.
- **BoundedCode's own process** stayed at or below 34 MiB.
- **Containers** peaked at 3.0 GiB (BoundedCode on immutable-js, running
  its full test suite).
- **The Docker VM** (Docker Desktop) held 13–15 GiB.
- **Free memory** never fell below 31 GiB of 64 GiB.
- **Cost.** There is no API cost: the model is local. Energy was not
  measured. The token counts above let a reader price the runs for any
  provider.

## 3. Verification: does it add measurable value?

### BoundedCode's own runs (8)

| Verification state | Runs | Hidden pass | Notes |
|---|---|---|---|
| `task_verified` | 5 | 4 | One false pass: gin-3820. |
| `tests_green` (checks pass, no evidence) | 3 | 3 | All three were missed verifications (see the list below). |

The missed verifications had two causes:
- **phpspreadsheet-3463 and fluentd-3616: no test stage.** The test stage was
  removed by the environment-only configs, because these suites time out or
  are unstable offline (D1).
- **carbon-3103: the agent's test was not evidence.** The agent's requested
  test also passes on the base, so the gate correctly refused to count it.
  The fix itself was right.

**The false pass is a specification gap, not a broken fix.**
- BoundedCode's gin-3820 patch fixes the issue's own reproduction. An
  analysis test of the issue's example
  (`analysis/gin3820_issue_repro_test.go.txt`) fails on the base with
  "unexpected end of JSON input" and passes with the patch.
- Its evidence test, which fails on the base and passes with the change,
  demonstrates that.
- The hidden test checks the same behaviour one layer lower (gin's generic
  form-mapping table), which the issue never names.
- Both baseline arms made the same choice of layer and failed the same way.

### The gate on the baseline's patches (gate replay)

The baseline's patch was applied unchanged in a fresh BoundedCode task, and
BoundedCode's gate decided. Every completed replay reproduced the baseline's
own hidden outcome.

| Original baseline arm | Gate verdict | Hidden | What the gate saw |
|---|---|---|---|
| gin-3820 | rejected | fail | Breaks the existing test `TestBindingFormFilesMultipartFail`, which passes on the base, with the reference fix and with BoundedCode's patch |
| gin-4003 | `task_verified` | pass | |
| immutable-js-2006 | rejected | pass | The project's formatting check (prettier) fails |
| bat-2393 | `tests_green` | pass | No test added |
| axum-691 | rejected | fail | **Compile error** (`Arc<str>` vs `String`) |
| phpspreadsheet-3463 | `tests_green` | pass | No test stage (D1) |
| carbon-3103 | rejected | pass | Breaks the existing test `TestingAidsTest::testSetTestNow`, which passes on the base. The dataset's reference fix breaks it too; BoundedCode's own patch does not. |
| fluentd-3616 | `tests_green` | pass | No test stage (D1) |

| Corrected baseline arm | Gate verdict | Hidden | What the gate saw |
|---|---|---|---|
| gin-3820 | rejected | fail | gofmt |
| gin-4003 | rejected | pass | gofmt |
| immutable-js-2006 | rejected | fail | The test suite runs out of JavaScript heap |
| bat-2393 | `task_verified` | pass | The agent's own test fails on the base and passes with the patch |
| axum-691 | `task_verified` | pass | The same |
| phpspreadsheet-3463 | `tests_green` | pass | No test stage (D1) |
| carbon-3103 | rejected | pass | Breaks `testSetTestNow`, as in the original arm |
| fluentd-3616 | `tests_green` | pass | No test stage (D1) |

Each replay's hidden result equalled the baseline run's own, in all 16.
The gate stops at its first failing check, so a patch rejected for
formatting was not tested further.

**Gate verdict vs. hidden result, all 16 baseline patches:**

| Gate verdict | Hidden pass | Hidden fail |
|---|---|---|
| Accepted (`task_verified` or `tests_green`) | 8 | 0 |
| Rejected | 4 | 4 |

**Reading.**
- **It caught every failing baseline patch.** All 4 patches that fail the
  hidden tests were rejected, each for a concrete reason: a compile error
  (axum, original arm), a broken existing test (gin-3820, original), the
  test suite running out of memory (immutable-js, corrected) and formatting
  (gin-3820, corrected). For three of them, the reason the gate gave is a
  real defect of the patch. A user of BoundedCode would have seen the
  defect before merging. The hidden tests also caught all 4.
- **It found no defect that the hidden tests missed.** The earlier draft of
  this report claimed one (carbon-3103). It was wrong: the dataset's
  reference fix breaks the same existing test.
- **Half its rejections were correct patches** (by the hidden tests):
  - two formatting failures (prettier on immutable-js in the original arm,
    gofmt on gin-4003 in the corrected arm);
  - two carbon patches that break `testSetTestNow`, as the reference fix
    does. BoundedCode's own patch passes both that test and the hidden one,
    so the stricter bar was reachable. Whether it should be required is a
    judgement the dataset does not make.
- **Evidence adds little as a filter here.** `task_verified` selected 7
  patches in the corrected arm (BoundedCode's 5 plus 2 replays), 6 of which
  pass the hidden tests. "Checks pass" selected 12, of which 11 pass. The
  one exception in both is the same gin-3820 false pass. On this sample,
  behavioural evidence is not a better predictor than green checks.
- **Where the gate shows value** is the correctness checks (build, existing
  tests, the test suite) on output that the plain agent did not check
  itself. In this sample, that value overlaps with what the hidden tests
  already detect.

## 4. Where each system succeeded or failed

| Task | Outcome | Category (analyst judgement, `failure-analysis.json`) |
|---|---|---|
| gin-3820 | All three runs fail | **SPEC_GAP.** All three patches fix the issue as reported, in the multipart binding path. The hidden test requires the fix in the generic form mapping, where the reference fix is. |
| immutable-js-2006 | Corrected baseline fails | **WRONG_BEHAVIOUR.** An extra early-return condition makes the hidden Range test run out of memory. The original baseline and BoundedCode passed. |
| axum-691 | Original baseline fails | **WRONG_BEHAVIOUR.** The right fix, but a type error stops it compiling. The agent could not build (D3). |
| The other 5 | All pass | |

Both systems succeeded on the same tasks, except for the one discordant
task per arm.

## 5. What can be attributed to architecture

- **Retries after failed verification.** Two BoundedCode runs failed a
  check and retried: gin-3820 and immutable-js-2006. Both failures were
  formatting (gofmt, prettier), so the retry fixed formatting, not
  behaviour. The other extra attempts (6 runs) answered BoundedCode's
  request for a test. No retry is shown to have turned a wrong fix into a
  right one, including on immutable-js-2006, the one task where BoundedCode
  passed and the corrected baseline failed.
- **The axum-691 difference in the original arm** came from the baseline's
  environment defect (D3) plus a one-token type error. It is not something
  BoundedCode's architecture did in that run: BoundedCode compiled on its
  first attempt.
- **The immutable-js-2006 difference in the corrected arm:** the original
  baseline passed the same task, so sampling variation explains it as well
  as the architecture does.
- **Cost is clearly attributable.** BoundedCode's extra time and tokens come
  from:
  - indexing;
  - the contract;
  - the requested tests;
  - verification runs, including the evidence runs on the base and the
    control.

**Conclusion.** No difference in success can be attributed to BoundedCode's
architecture on this sample. The cost difference can. So can the gate's
rejection of every failing baseline patch, shown by replay rather than by
outcome, at the price of rejecting as many correct ones.

## 6. What remains unproven

- **Any difference in success rate.** Eight tasks with one run per system
  cannot show one. Two runs of the same baseline changed which task was
  discordant. Showing a 10–15 point difference would need many more tasks
  and repeated runs.
- **Whether the gate's defect detection turns into better outcomes.** It
  would, if the agent then fixed what the gate reports. BoundedCode's runs
  here never needed that.
- **Generality.** One machine and one local model. Two of eight tasks had no
  usable test suite offline, so BoundedCode could not verify them. The Java
  slot was empty.
- **Comparison to other products.** The baseline is the same agent loop
  without BoundedCode. Other agents (SWE-agent, Aider, OpenHands' full
  application, commercial agents) were not compared.

## 7. Integrity notes

- **Pre-registration.** The protocol, the task set and the tooling were
  committed before any comparative run (`d4ce733`). Each deviation was
  committed before the runs it affects (D1 `591665e`, D2 and D3
  `86c123d`).
- **Pilot.** The declared pilot pair (gin-3741, `results/pilot/`) is
  excluded. It found the D1 fault. Its BoundedCode run was itself a false
  verification: the agent edited unrelated, environment-broken tests to skip.
- **No cherry-picking.** No run was repeated or dropped. The original
  baseline arm is published next to the corrected one. The interrupted
  replay record is kept. A claim in an earlier draft (a carbon regression
  missed by the hidden tests) was withdrawn after checking it against the
  reference fix.
- **Hidden tests and reference patches** were never visible to either agent:
  they live outside every sandbox mount and are applied only after a run.
- **Remaining asymmetries:**
  - BoundedCode gets more attempts and a token budget, and the baseline gets
    one session. That is the architecture being compared.
  - The corrected baseline arm ran after all BoundedCode runs, not
    alternated with them.
  - The model server woke from idle sleep before some runs. That affects
    wall-clock time only.
