# Targeted engineering pass (2026-10-05)

> **Development evidence, not a benchmark.** The three changes below were
> driven by the development corpus of the
> [failure-driven pass](failure-driven-engineering-2026-10.md) and checked on
> two of its tasks (caddy-6288, axios-6539) plus a deterministic retrieval
> replay. Those tasks are not independent. Earlier results stand as
> published: **0/8 on the original frozen build, 1/8 after the defect
> fixes**, 1/4 on the development corpus. Independent evidence is the
> [second validation](second-independent-validation-2026-10.md).

Data: [`benchmarks/reports/targeted-engineering-20261005/`](../../benchmarks/reports/targeted-engineering-20261005/).
Commits: 43f7cfd (changes), d5a09d3 (metrics), 4b31742 (governor calibration).

## Scope

Only the three improvements the previous pass's evidence supported. No new
providers, no embeddings, vector store or reranker, no sandbox, policy,
secret-handling or frontier-sanitization changes.

| Change | Evidence it answers | Mechanism | Regression tests |
|---|---|---|---|
| A. Runaway generation control | Caddy: 72-109 K generated tokens per unproductive attempt over 40-64 min; 8 K-token visible-output turns | Separate budgets: reasoning (`reasoning_budget` 4096, llama-server), visible output per response (`agent.max_output_tokens` 8192), and a per-attempt **strategy budget** (`agent.strategy`). A deterministic governor tracks progress (first edit, a new test file, an agent-run test going from failing to passing) and stops an attempt that spends `no_progress_tokens` without progress, or exceeds `max_tokens` / `max_duration`. The stopped strategy is recorded (`strategy.stopped`) and the retry pack tells the agent not to repeat it. No escalation is triggered by long output alone. | 3 (`governor_test.go`) |
| B. Requirement ambiguity | axios-6539: the issue allows two outcomes; runs picked different ones | One local-model call (thinking off) derives a task contract: required, acceptable alternatives, constraints, not required, unknown/ambiguous (material or not), acceptance evidence. A **material** ambiguity (≥ 2 readings that change behaviour, API, data, security, compatibility, tests or output) blocks the task for clarification (`task.ambiguity: ask`, the default; answered with `task run --clarify`) or, for benchmarks, is recorded as SPEC_AMBIGUOUS and the agent must state and demonstrate its reading (`proceed`). Explicit alternatives never block. The contract is shown in the context pack; the behavioural-evidence gate is unchanged. | 6 (`contract_test.go`, `task/contract_test.go`) |
| C. Retrieval seed quality | Packs filled with `NewRecorder`, `foo`/`bar`, URL hashes; the quoted error message never searched | Seeds ranked: diagnostics (invariant text of quoted errors, fixed-string search) → names in prose / file paths / repository file links → config keys and flags → sample-code identifiers. Placeholders, URL parts and @mentions dropped; names matching > 25 files demoted; lexical hits prefer code over docs and build output; minified/map files excluded. | 6 (`seeds_test.go`) |

`make check` (build, vet, golangci-lint, unit tests with race, license
check), the Python adapter tests, the container security suites and the
Serena guards passed at 43f7cfd; `make check` passed again at 4b31742 (a
defaults-only change).

## A. Runaway control: calibration and what the development checks showed

The first defaults (60 K tokens without progress, 100 K per attempt, 45 min)
were calibrated on attempt *totals*. The first development check (build
d5a09d3) showed the governor never fired on caddy, whose attempts each ran
33-37 minutes. Replaying the governor's exact progress rules over every
archived 2026-10 run:

| | Max tokens without progress | Tokens per attempt |
|---|---|---|
| Attempts of tasks that passed | ≤ ~20 K | ≤ ~30 K |
| Unproductive attempts (caddy, vue) | 34-56 K | 57-107 K |

Defaults are now 40 K without progress, 50 K per attempt, 35 min (4b31742).
The rule is generic: nothing names a task or repository.

| Check | Build | Hidden test | State | Wall | Attempts | Calls | Output tokens | Largest response | Strategy stops |
|---|---|---|---|---|---|---|---|---|---|
| caddy-6288 (before: final dev run) | 3c75f2e | FAIL | not completed (timeout) | 5,467 s | – | 201 | – | 8 K | – |
| caddy-6288 | d5a09d3 | FAIL | task_verified | 4,358 s | 3 | 140 | 119 K | 8,192 (output cap) | 0 |
| caddy-6288 | 5b3c54a (recalibrated) | FAIL | tests_green (UNVERIFIED) | 2,517 s | 2 | 82 | 63 K | 4,547 | 0 |
| axios-6539 | d5a09d3 | FAIL | task_verified | 1,853 s | 2 | 47 | 17 K | 2,251 | 0 |

- Caddy now finishes (2,517 s, was 5,467 s and timed out) with half the
  generation. In the recalibrated run neither attempt crossed the budget;
  attempt 2 ended through the existing stuck detector. Replaying the
  d5a09d3 run under the new defaults, its first attempt (56 K without
  progress) would have been stopped. So the governor bounds the failure
  mode, but **these checks did not exercise a live stop**.
- The 8,192-token response in the d5a09d3 run is visible output hitting the
  new per-response cap. Before, such responses were unbounded.

## B. Ambiguity: detection worked, classification did not

- axios-6539: the contract flagged one **material** ambiguity, so the run
  was recorded SPEC_AMBIGUOUS. The ambiguity it named was wrong: "throw a
  TypeError or return HTTP 400", while the issue's actual alternatives are
  "an error" or "the URL joined to the base", and it listed **0
  acceptable alternatives**. The agent threw an error and proved it with a
  failing-then-passing test. The hidden test requires joining. Under `ask`,
  the user would have been asked a wrong question, but would still have
  been stopped before the work.
- caddy-6288: contract with 1 requirement and no material ambiguity in both
  runs (correct).
- Contract derivation took ~7 s per task.

## C. Retrieval: replay (deterministic, no model)

| Task | Before | After |
|---|---|---|
| caddy-6288 | seed `expression` (common word) | diagnostic "wrong argument count or unexpected line ending after" → `caddyconfig/caddyfile/dispenser.go`, where the error is raised |
| prometheus-13845 | `__name__`, `jDomantas` (a username), … | names from prose; common names demoted; context = `model/labels/labels*.go` (all three tagged implementations) and tests |
| gin-3227 | `NewRecorder`, `NewRequest` first | diagnostic "page not found", the linked `gin.go`, then names |
| vuejs-11899 | `lKcLp` (URL hash), `foo`, `bar` | no seeds (nothing code-like left) |
| axios-6539 | sample identifiers | same sample identifiers; `lib/adapters/http.js` and `lib/core/buildFullPath.js` included, `dist/` copies still present (ranked lower) |

In the live caddy runs the pack held one file (975 tokens), the error's
origin. Whether that helped could not be separated from model variance.
The run still failed.

## Verification honesty in the development checks

Two development runs ended `task_verified` with hidden tests failing:

- **caddy (d5a09d3):** the evidence was "changed tests do not build against
  the base". The gate counts a test that does not compile on the base as
  failing there, which is legitimate for tests of new API but weaker than a
  behavioural failure.
- **axios:** the other valid reading of the issue.

The recalibrated caddy run correctly ended UNVERIFIED: its tests passed on
the base too.

## Development regression result

**0 of 2 development tasks pass their hidden tests (0/3 runs).** Runaway
time is bounded: caddy completes in 42 min, was 91 min with a timeout.
Ambiguity is detected but mis-described. Retrieval reaches the right files
on four of the five issues.

## Security

No boundary changed. The contract call goes through the metering gateway
to the local model only; nothing reaches the frontier. `contract.json` is
written atomically, mode 0600, in the task directory. Seeds run `rg` with
`--fixed-strings`, `--` before the pattern, and an argument vector (no
shell), confined to the repository. The governor only cancels the agent's
turn.
