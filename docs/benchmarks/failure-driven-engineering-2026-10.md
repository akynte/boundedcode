# Failure-driven engineering pass (2026-10-05)

> **This is development-corpus retesting, not a validation.** The same four
> failed tasks from the small real-world validation, plus axios as a
> positive control, were analysed, used to drive general fixes, and rerun
> with the same acceptance tests. Results on tasks used for development are
> optimistic by construction and must not be read as held-out evidence.
> The original validation result stays as published: **0/8 on the frozen
> build, 1/8 after the defect fixes** ([report](small-real-world-validation-2026-10.md)).

Data: [`benchmarks/reports/failure-driven-engineering-20261005/`](../../benchmarks/reports/failure-driven-engineering-20261005/)
(start state, four retrospectives, time profiles before/after, per-run JSON,
memory samples, history-sanitization plan).

## Results at a glance

| | Original frozen validation (0217d96) | Post-defect validation | Development pass 1 (add3aab) | **Final development run (3c75f2e)** |
|---|---|---|---|---|
| gin-3227 | FAIL | — | FAIL¹ | FAIL¹ |
| caddy-6288 | FAIL | — | FAIL | FAIL |
| prometheus-13845 | FAIL | — | **PASS** | **PASS** |
| vuejs-11899 | FAIL (never started) | FAIL | FAIL² | FAIL² |
| axios-6539 (control) | FAIL | PASS | PASS | FAIL³ |

Model: Qwen3.6-35B-A3B (UD-Q4_K_M) throughout. No frontier escalation was
triggered in either development pass. No human code intervention.

¹ The hidden test checks a router-tree invariant the issue does not ask for.
Independently reproduced: on the issue's own scenario (`GET /ping/` with
`RedirectFixedPath`), base and the reference patch return 301; BoundedCode's
patch returns the requested 404. Treated as a task-spec mismatch, like the
three tasks already excluded.
² The patch fixes the reported at-rule bug (checked for the pass-1 run); the
hidden test is an exact-output match that also needs a comment-placement
change the issue does not mention.
³ The issue states two acceptable behaviours ("the expected result would be an
error", or the URL joined to the base). This run threw an error and proved it
with its own failing-then-passing test; the hidden test requires joining.
Earlier runs chose joining and passed: the control's outcome is unstable
(2 of 3 post-fix runs passed), not a product regression.

## A. What changed and whether reliability improved

Seven general changes, each from an observed failure, each with a regression
test (`make check` and the container security suites pass):

| Change | Observed failure | Commit |
|---|---|---|
| Behavioural evidence: `task_verified` only if a test the change added/modified fails on the base and passes on the change; otherwise `tests_green` (unverified) after one request for a reproduction test | 4 frozen false passes had no such test; two patches changed nothing | add3aab |
| Agent sandbox gets verification's read-only Go module cache and offline settings (separate build cache) | On gin, caddy, prometheus the agent could not build or run one test (82/104 commands in one attempt hunting dependencies) | add3aab |
| Thinking budget per response (`reasoning_budget: 4096`) | 8K-token runaway thinking with tool calls trapped inside: 31-51% of model time on several tasks | add3aab |
| Custom build-tag variants of changed Go files are compiled | Prometheus edits to tag-selected implementations did not compile; nothing noticed | add3aab |
| Impact section follows the worktree's git changes after an interruption | Resume pack showed `changed_total: 0` next to a real diff | add3aab |
| Failure digest leads retry packs (failing tests/assertions from the whole output; long commands abbreviated) | Caddy retry pack showed a 55-name skip list and log noise, not the failing assertion | 036ac7d |
| Neutral example paths in the packet sanitizer | Its code named the maintainer's home | f794870 |

Reliability improved where the cause was BoundedCode's: **Prometheus went
from FAIL (a no-op patch, self-verified) to PASS in both development passes**
(1 attempt, ~23 min, local-only: no frontier calls (no escalation was
triggered), shown by its own fail-before/pass-after regression test). It did
not improve enough overall: **1 of 4 development tasks pass**, below the
≥ 3/4 development target; of the other three, two are task-spec problems
(gin, vue) and one is a genuine failure (caddy).

## B. Root causes (retrospectives)

| Task | Primary | Secondary | Evidence |
|---|---|---|---|
| gin-3227 | OTHER: task-spec mismatch | RUNTIME_OVERHEAD (no module cache for the agent), VERIFICATION_GAP | Reference patch does not change the issue's reproduction; ours fixes it. |
| caddy-6288 | VERIFICATION_GAP + environment (agent could not build) | MODEL_REASONING, IMPACT_ANALYSIS_MISS | Frozen patch was dead code: identical behaviour to base on all 5 inputs; real cause (import-chain tokens in snippets) was readable but not found. |
| prometheus-13845 | VERIFICATION_GAP | MODEL_REASONING | Frozen patch changed a path that was never broken; the broken `append` path was untouched; tagged variants did not compile; existing suite was green on base. |
| vuejs-11899 | OTHER: exact-output test needs an underivable detail | IMPACT_ANALYSIS_MISS (duplicated a decl-only filter) | Patch fixes the reported bug; hidden output differs only in comment placement. |

Erratum to the validation's failure notes: the caddy agent did not write
`expression_quotes.caddyfiletest` (it only viewed it); archived `agent.patch`
files were captured after the acceptance test patch was applied and include
its hunks.

## C. Verification: why false passes happened, what prevents them now

Every frozen false pass followed the same chain: *requested behaviour →
BoundedCode ran the repository's existing checks, which already passed on the
untouched base → hidden test exercised the requested behaviour → missing
signal: any test that fails without the change.* Two patches were no-ops.

Now: a task is `task_verified` only with behavioural evidence (Go: a test
defined in the changed test files fails on an export of the base commit;
other languages: the same test stage passes on the untouched base and fails
with the changed tests). Without it, the agent is asked once for a
reproduction test, and the task ends `tests_green` (reported UNVERIFIED),
never as a verified merge candidate. Observed in the final run: caddy's first
tests passed on base too and were rejected; axios' first fix had no test and
was sent back.

**False-pass target not met.** In the final run 3 tasks were `task_verified`
but failed the hidden tests (gin, vue, axios). In each, the evidence test
fails on base and passes with the change, so the gate worked as designed; the
gap is between the issue text and the hidden test (gin, vue) or between two
behaviours the issue allows (axios). No patch reached `task_verified` that leaves the
issue's own stated problem in place, as far as checked (gin: reproduced;
vue: checked for pass 1 only; axios: matches one stated expectation). The
gate cannot detect a test that encodes the wrong one of several readings.

## D. Model comparison

Not run. The maintainer chose not to run the comparison with Laguna XS 2.1
and Qwen3-Coder-Next; Qwen3.6-35B-A3B remains the default **by decision, not
by comparison**. Whether another local model would do better on these tasks
is therefore untested.

## E. Final task results (3c75f2e, Qwen3.6, no code changes between runs)

| Task | Previous result | Final result | Root cause addressed | Frontier | Wall time | State |
|---|---|---|---|---|---|---|
| gin | FAIL | FAIL (spec mismatch; issue fixed) | agent toolchain, evidence | no | 451 s (was 4,953) | task_verified |
| caddy | FAIL | FAIL (timeout) | agent toolchain, digest | no | 5,467 s | not completed |
| prometheus | FAIL | **PASS** | evidence, agent toolchain, build tags | no | 1,418 s (was 2,068) | task_verified |
| vue | FAIL | FAIL (exact-output detail) | — | no | 1,998 s | task_verified |
| axios (control) | PASS | FAIL (other valid reading) | regression control | no | 2,069 s | task_verified |

| Final metric | Value |
|---|---|
| Successes (hidden tests pass) | 1/5 (1/4 development tasks) |
| Local-only successes / frontier-assisted | 1 / 0 |
| False verification passes | 3 (gin, vue, axios; see C) |
| Median wall-clock | 1,998 s |
| Median attempts | 1 |
| Context on Prometheus | 3,077 pack tokens + 23,656 tool-output tokens = ~1.0 % of 2.57 M |
| Frontier calls | 0 |
| Peak memory (any run) | BoundedCode 79 MiB, llama-server 29.8 GiB, ≥ 32.8 GiB always available |
| Human interventions | 0 (environment-only repository verification configs, unchanged) |

Raw OpenHands baseline (validation, same model): gin 285 s FAIL, caddy 835 s
FAIL. BoundedCode now takes 1.6× on gin (was 17×) for the same hidden-test
outcome but a demonstrated fix of the issue; caddy remains slower (6.5×) and
unsolved.

## F. Efficiency

Before the fixes, model inference was 81-91 % of wall-clock on every run;
after, 72-96 % except axios (44 %, where verification is 14 % and the
evidence check and evidence-retry overhead 39 %). Repository intelligence,
context planning, setup and session start are each ≤ 3 % throughout. The overhead versus raw OpenHands was generation
volume, not subsystem cost: frozen gin generated 135 K tokens (23× the
baseline), 69 % of them in 17 runaway calls. With the agent able to run tests
and the thinking budget, gin used 10.5 K tokens and 23 calls. Remaining waste:
caddy still produced 11 generations of 4 K+ tokens (one 8 K, visible output,
which the thinking budget does not cap) over 201 calls; the evidence check
adds base-export and control runs (≈ 2-5 min on JavaScript repositories).
Profiles: `profile-before.json`, `profile-after.json`.

## G. Repository intelligence

Not the bottleneck. Retrieval was COMPLETE-but-NOISY (prometheus) or
PARTIAL-and-NOISY (gin, caddy, vue: seeds taken from identifiers in issue code
blocks, such as `NewRecorder` or CSS class names); in every case the agent
reached the right files itself within minutes. **Embeddings: still
unnecessary.** A deterministic "where is this behaviour owned" query would
not have helped gin (the issue pointed at the right file; the hidden test
did not match the issue). Seed noise is a real but minor improvement target.

## H. Frontier

No escalation triggered in the development passes, so the patch-mode
experiment did not apply. Historical evidence: gin's Z2 advice was correct
and the local model applied it exactly; go-zero's Z3 review found a real
security regression and the local model failed to implement the fix. The
handoff is not the limiting factor; local implementation of advice
sometimes is. No change made.

## I. Memory

Resolved. Under the same Vue workload BoundedCode peaked at 29 MiB (was
~58.9 GiB); across all ten development runs at most 79 MiB. Aggregate use
peaked around 31 GiB (llama-server ~30 GiB, Docker VM ≤ 9.6 GiB, containers ≤ 2.9 GiB), always
leaving ≥ 32.8 GiB available. **No memory admission guard is needed now.**

## J. Security

No boundary was weakened. The agent's module cache is read-only and its
build cache is separate from verification's (Go caches test results). The
evidence check exports the base with `git archive` into the task cache,
writes only regular files inside it, applies secret masks and dependency
mounts as verification does, and never modifies git metadata. TLS fixtures
stay masked (the documented limitation stands: no fixture-only exception was
added, because it could not be done without weakening the boundary).
Frontier sanitization unchanged and fail-closed.

## K. Dominant original bottleneck

**A combination.** Verification and orchestration/environment dominated
the original failures: the agent could not run tests and the gate
could not fail on no-op patches; fixing those turned Prometheus into a pass
with the same model. Local model capability remains the limit on caddy
(runaway output, no reproduction test in 90 minutes). Two of the four
"valid" tasks have hidden tests not derivable from their issues.

## L. Product answer

**VIABLE WITH IMPORTANT LIMITATIONS.** The architecture now behaves as
intended on the failures that were its own: the agent can test, the gate
refuses no-op patches, runaways and memory blowups are bounded. Real-task
reliability is still low (1/4 on the development corpus, unstable on an
ambiguous control), and verification cannot catch a patch that implements a
different valid reading of the request.

## M. Next step

**ANOTHER_TARGETED_ENGINEERING_PASS** (release status
NEEDS_ANOTHER_TARGETED_ENGINEERING_PASS), limited to what the evidence
supports: cap or detect runaway visible output; surface requirement
ambiguity before implementation (the axios issue states two behaviours);
reduce retrieval seed noise. A second validation should wait until these are
done and should screen tasks for issue-derivable acceptance tests.

## History

See `benchmarks/reports/failure-driven-engineering-20261005/history-sanitization.md`:
unpushed commits contain absolute home paths in report files; a rewrite of
the unpushed range is planned, not executed. Nothing has been pushed.

## Revision note (2026-10-05)

Wording only; no result changed.
- "benchmark" replaced by "validation" where it described BoundedCode's own validations; "independent evidence" is now "held-out evidence".
- §A: "local only" qualified as no frontier calls (no escalation was triggered); "proven by its own regression test" is now "shown by its own fail-before/pass-after regression test".
- §C and §E: "verified" outcomes written as `task_verified` / "Successes (hidden tests pass)".
- §D: "dropped at the user's request" is now "The maintainer chose not to run the comparison"; a reference to an internal checklist ("section 15 verdict A") was removed.
- §G and §K: letter codes from an internal checklist ("Embeddings: A", "F: a combination", "(A)", "(B)", "(D)") removed; the findings are unchanged.
- Erratum to "History": the history sanitization described there was executed on 2026-10-05 before the first push; see `benchmarks/reports/failure-driven-engineering-20261005/history-sanitization.md`.
