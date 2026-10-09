# Evaluation overview

BoundedCode has been evaluated on small samples of public engineering tasks,
on one machine (Linux, RTX 4060 8 GB laptop, 64 GB RAM) with one local model
(Qwen3.6-35B-A3B). This page summarises what was measured and how to read it.
The linked reports carry the method, the per-task data and the failures.

These are small practical samples, not a statistically comprehensive
evaluation. Nothing here supports claims of superiority over other tools.

## Stages

| Stage | Task set | Result | Report |
|---|---|---|---|
| Initial validation | 8 public tasks, screened for environment validity only; frozen build | **0/8**, then **1/8** after fixing four BoundedCode defects | [small real-world validation](small-real-world-validation-2026-10.md) |
| Engineering (development evidence, not a validation) | the first validation's failures, used as a development corpus | fixes to verification, agent tooling, resource handling, retrieval and execution control | [failure-driven](failure-driven-engineering-2026-10.md) · [targeted pass](targeted-engineering-pass-2026-10.md) |
| Second validation (held out) | 6 tasks not used during development and never shown to the agent, screened for issue-derivable acceptance tests; each run once | **5 of 6** strict `TASK_VERIFIED` and passing the datasets' hidden acceptance tests; **6 of 6** hidden tests pass | [second independent validation](second-independent-validation-2026-10.md) |

## The second (held-out) validation

**Task selection:**
- The 6 tasks were selected before execution from the SWE-bench
  Multilingual and Multi-SWE-bench datasets. They cover Go, JavaScript,
  TypeScript, an infrastructure tool and a repository of 1.8 M estimated
  source tokens.
- Screening rejected 10 of 24 candidates whose hidden tests could not be
  derived from their issue (5 ambiguous, 5 not derivable).
- There was no human code intervention.

**Results:**
- **Frontier:** all 5 successes used only the local model. Escalation was
  enabled but never triggered, and no task made a frontier call.
- **False verification passes:** none among the 5 `TASK_VERIFIED` tasks;
  each of them also passed the hidden acceptance tests.
- **Context use:** roughly 18–35 K tokens per task, across repositories of
  0.12–1.83 M estimated source tokens.
  - The share of the repository that entered context therefore depends on
    its size: at most about 1.6 % on the largest repository and 29.1 % on
    the smallest (gin).
  - These figures include all tool output (files read, search and test
    output), so they are upper bounds.
  - Source tokens are estimated as bytes × 10/32.

**Why 5 of 6, not 6 of 6.** The sixth task (Prometheus) was implemented so
that its hidden acceptance test passed. BoundedCode still classified it
UNVERIFIED: its evidence checker did not associate the modified data-driven
test file (`promql/testdata/functions.test`) with the Go test function that
reads it. The official score stays 5 of 6. A verifier that withholds
`TASK_VERIFIED` when it cannot show fail-before/pass-after evidence is
behaving as intended. After this validation, the checker was changed to
attribute changed test data to the Go package whose tests read it. That
change was released in v0.1.0-alpha.3, is covered by unit tests, and has not
been re-validated on real tasks.

## Why the two validations are not an improvement curve

The two task sets were selected differently, so 0/8 → 5/6 does not measure
system improvement. Each result stands on its own, with its own scope.

- **Screening:** the second set was screened for acceptance tests derivable
  from the issue. The first was screened for environment validity only, and
  three of its tasks failed on identifiers that only the reference solution
  introduces.
- **Difficulty:** the first set had a "difficult" slot (a multi-file
  reference patch); the second did not.
- **Configuration:** the second validation ran with `task.ambiguity:
  proceed`, not the default `ask`. Under the default, one clear task (vue)
  would have stopped on a false ambiguity flag.
- **Candidate build:** on the development tasks, the candidate build's checks
  before the freeze passed 0 of 2 (0 of 3 runs)
  ([targeted engineering pass](targeted-engineering-pass-2026-10.md#development-regression-result)).

## What limits these results

- **Sample size:**
  - 8 + 6 tasks.
  - 5 of 6 has an exact (Clopper-Pearson) 95% interval of about 36–99.6%.
  - Rerun variance was not measured.
- **What the screen removes.** The screen removes the failure classes seen
  in development (tests needing names only the reference fix introduces,
  issues that allow several outcomes). Real requests are not screened.
- **Same repositories.** The held-out tasks are new tasks, but from the same
  six repositories as the development corpus (gin, caddy, go-zero, axios,
  vue, prometheus).
- **Possible training contamination.** The tasks are public issues whose
  fixes may be in the model's training data.
- **Pre-registration cannot be shown.** The candidate list and the
  screening first appear in git together with the frozen build. The order
  "selection rule before screening" is stated, but cannot be shown from
  history.
- **Per-task verification settings.** Each repository got a
  `.boundedcode/verification.yaml` that drops checks already failing on the
  untouched base offline. These were measured before the runs, never from
  the solution.
- **Earlier verification gates had false passes:**
  - In the first validation, 4 of 8 frozen runs and 2 of 3 reruns passed
    BoundedCode's verification but failed the hidden tests. That was an
    earlier gate design, before behavioural evidence.
  - In development runs with the current gate design, there were 3 false
    passes in the final failure-driven run and 2 in the targeted pass.
- **Baseline:** no advantage shown. On the two-task baseline in the first
  validation, BoundedCode did not improve the same local model's result and
  was slower; no baseline was run on the held-out set
  ([baseline comparison](../../benchmarks/reports/small-real-world-validation-20261004/baseline-comparison.md)).
- **Untested configurations.** Only the local model on the reference
  machine was evaluated. Cloud providers, other model profiles, macOS and
  Windows were not.

## Other measurements

- Earlier development benchmarks and their raw JSON and Markdown records
  are in [`benchmarks/reports/`](../../benchmarks/reports/). They cover
  decode speed, an 11-task synthetic suite, kill/resume, and cross-service
  ablations.
- The [model evaluation](../design/model-evaluation.md) explains the choice
  of the default model.
- Memory during the second validation:
  - BoundedCode itself stayed under 70 MiB.
  - The model server peaked at 29,310 MiB (28.6 GiB; the report's table
    says 29.3 GiB, a unit-conversion slip).
- No minimum hardware requirement has been measured.
