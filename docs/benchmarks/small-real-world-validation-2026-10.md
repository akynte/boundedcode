# Small real-world validation (2026-10)

> **This is a small validation sample intended to demonstrate practical
> operation, not a statistically comprehensive evaluation.** Eight tasks
> cannot establish success rates, and nothing here supports claims of
> state-of-the-art performance, superiority over other agents, a general
> local completion rate, or equivalence to a paid frontier subscription.

Raw data, manifest, environment, screening, per-task JSON, logs and notes:
[`benchmarks/reports/small-real-world-validation-20261004/`](../../benchmarks/reports/small-real-world-validation-20261004/).

## Result in one paragraph

On 8 public tasks fixed before any run, the frozen build (0217d96) passed the
datasets' hidden acceptance tests (hidden from the agent) on **0 of 8**. Four failures traced to
BoundedCode defects (JavaScript verification never ran tests; secret masking
hid a Go package; a frontier packet was silently blocked; the cross-service
scanner exhausted 58 GiB of memory). After fixing them, the three affected
tasks were rerun: **1 of 3 passed** (axios, local model only). Final outcome:
**1 of 8 accepted, 1 of 8 local-only: no frontier calls (escalation enabled,
not triggered)**. The remaining failures are model
reasoning/coding errors (4) and tasks whose acceptance tests reference
identifiers only the reference solution introduces (3). Context reduction
and resume worked as designed. On the two-task baseline, BoundedCode did not
improve the same local model's result.

## Method

* **Tasks.** 6 from SWE-bench Multilingual and 2 from Multi-SWE-bench, at
  pinned dataset revisions. Slots: 3 Go, 1 JavaScript, 1 TypeScript, 1 large
  repository, 1 DevOps/infrastructure, 1 difficult (multi-file reference
  patch). Per slot, candidates were ranked by a fixed hash of their instance
  id and the first one valid in this environment was taken. The agent saw only
  the issue text; gold patches were used only for screening.
* **Screening (environment validity only):** acceptance tests must fail on the
  base and pass with the reference patch in the offline sandbox. Substitutions,
  all decided before any agent run and recorded in the manifest: axios-5085
  (test needs internet) → axios-6539; caddy 4943/5995/6345/6115 (Go 1.27
  incompatibilities) → caddy-6288; all 7 Hugo instances (Go 1.27 `vet` panics)
  → grpc-go-2744 (the fallback Go slot recorded in the
  [manifest](../../benchmarks/reports/small-real-world-validation-20261004/manifest.json)). Terraform was
  excluded (BUSL-1.1), Preact (browser tests).
* **Acceptance:** the dataset's test patch is applied to the agent's final
  worktree (its files reset to base first) and the dataset's test command
  runs in the offline sandbox; exit status decides.
* **Stack:** BoundedCode + OpenHands SDK 1.51.0 + Qwen3.6-35B-A3B UD-Q4_K_M on
  llama.cpp (RTX 4060 8 GB laptop, 64 GB RAM; MoE expert layers partly on
  CPU, the model server holding ~20-27 GiB of RAM while serving) + codebase-memory-mcp 0.11.0 +
  Serena v1.7.0 + verification + Docker sandbox (no network) + frontier gate
  (Codex on a ChatGPT subscription, policy-triggered, pre-approved; no API
  keys). Details: `environment.json`.
* **Environment-only setup:** a `.boundedcode/verification.yaml` per
  repository, committed into the task's base: the default preset minus checks
  that already fail on the untouched base offline (network tests, expired or
  masked TLS fixtures, Go 1.27 `vet` findings on old code, browser tests).
  Measured before the runs, never from the solution.
* **Frozen build, then fixes.** All 8 tasks ran on the frozen build. Defects
  found were fixed with regression tests; only tasks a proven defect affected
  were rerun. Both results are kept.

## Original frozen results (build 0217d96)

| Task | Repository | Category | Result | Self-verified | Wall s | Attempts | Model calls | Prompt tok | Output tok | Cache hit | Pack src tok | Tool-output tok | Verif. runs | Cond./resumes | Serena | Graph | Frontier | Primary cause |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3227 | gin-gonic/gin | Go | FAILED | yes | 4,953 | 3 | 167 | 4.46 M | 135 k | 0.95 | 10,968 | 44,127 | 4 | 6 / 0 | 27 | 9 | 1 (Z2) | MODEL_REASONING |
| grpc__grpc-go-2744 | grpc/grpc-go | Go | FAILED | no | 5,449 | 3 | 171 | 3.48 M | 166 k | 0.95 | 504 | 36,160 | 1 | 6 / 0 | 15 | 15 | 0 (Z2 blocked) | SANDBOX + FRONTIER (defects) |
| zeromicro__go-zero-2283 | zeromicro/go-zero | Go | FAILED | no | 5,255 | 6 | 201 | 7.65 M | 115 k | 0.94 | 12,819 | 95,324 | 7 | 8 / 0 | 108 | 0 | 2 (Z3, Z2) | TASK_AMBIGUITY |
| axios__axios-6539 | axios/axios | JavaScript | FAILED | yes | 590 | 1 | 27 | 1.24 M | 8 k | 0.95 | 7,197 | 26,688 | 2 | 0 / 0 | 11 | 4 | 0 | VERIFICATION (defect) |
| vuejs__core-11899 | vuejs/core | TypeScript | FAILED | — | never started | 0 | 0 | — | — | — | — | — | — | — | — | — | 0 | RUNTIME (defect) |
| prometheus__prometheus-13845 | prometheus/prometheus | large repo | FAILED | yes | 2,068 | 2 | 119 | 3.58 M | 40 k | 0.95 | 6,214 | 43,157 | 2 | 4 / 1 | 38 | 2 | 0 | MODEL_CODING |
| caddyserver__caddy-6288 | caddyserver/caddy | DevOps/infra | FAILED | yes | 1,797 | 1 | 52 | 1.96 M | 43 k | 0.95 | 1,195 | 42,302 | 2 | 1 / 0 | 1 | 1 | 0 | MODEL_CODING |
| zeromicro__go-zero-990 | zeromicro/go-zero | difficult | FAILED | no | 5,450 | 2 | 81 | 2.44 M | 168 k | 0.95 | 0 | 35,327 | 1 | 3 / 0 | 0 | 0 | 1 (Z2) | TASK_AMBIGUITY |

"Pack src tok" is source code BoundedCode placed in context packs;
"tool-output tok" is everything the agent's tools returned (files read, grep,
test output), an upper bound on repository content in context. Tokens are
estimated as bytes × 10/32, as the context planner does.

## Defect-fixed reruns

| Task | Fixed defect | Result | Self-verified | Wall s | Attempts | Model calls | Frontier | Primary cause |
|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | JS dependencies now reach worktrees (bcc17e1, 7bcc9c6) | **LOCAL_ONLY_SUCCESS** | yes | 933 | 1 | 22 | 0 | — |
| grpc__grpc-go-2744 | source code no longer masked (e86a7aa); packet sanitization (b50ed59) | FAILED | yes | 1,754 | 1 | 105 | 0 | TASK_AMBIGUITY |
| vuejs__core-11899 | bounded cross-service evaluation (f2b4001) | FAILED | yes | 1,454 | 1 | 48 | 0 | MODEL_CODING |

The fixes did what they should: axios's verification ran eslint, 182 unit
tests and the build, and the agent ran the tests itself; the grpc agent could
see and edit `credentials/`; Vue's setup took seconds at flat memory. The
first vuejs acceptance run failed in the benchmark harness (read-only
`node_modules` without a cache layer, fixed in 65f805a); the unchanged final
worktree was re-checked: 25 tests passed, the dataset's fail-to-pass test
failed.

## Final accepted outcomes

| Metric | Value |
|---|---|
| Total tasks | 8 |
| Successes (hidden acceptance tests pass) | **1** (axios-6539, post-fix) |
| Local-only successes | 1 |
| Frontier-assisted successes | 0 |
| Failures | 7 |
| Local-only completions | 1 of 8; frozen build: 0 of 8 |
| Total local model calls | 993 (818 frozen + 175 reruns) |
| Total frontier calls | 4 sent (all frozen runs) + 1 blocked before sending |
| Median wall-clock | 1,933 s per task (final runs); 4,953 s for frozen runs that started |
| Median repository source tokens | 743,941 |
| Median task-specific source context tokens | 46,434 (upper bound incl. all tool output); 4,396 from context packs |
| Largest repository | prometheus/prometheus, 2,572,637 source tokens |
| Context on the largest repository | 49,371 tokens upper bound (1.9 %, ~52× less); 6,214 from context packs (0.24 %) |
| Tasks requiring human intervention | 0 code or hint interventions; ENVIRONMENT_ONLY (repository verification configs) on all 8 |
| Resume / context continuity | **PASS** (Prometheus, below) |

## Product defects found (all fixed, with regression tests)

1. **JavaScript verification never ran tests** (bcc17e1, 7bcc9c6): task
   worktrees had no `node_modules`; every JS stage was skipped and changes
   passed verification without any test running. Now the checkout's dependencies are mounted
   read-only (writable tool caches on top), and a declared script with no
   installed dependencies fails instead of being skipped.
2. **Secret masking hid source code** (e86a7aa, narrowed in b50ed59): grpc-go's
   `credentials/` package was masked and edits to it rejected. Code in
   such packages is now visible; `secrets/` and dot directories stay masked.
3. **Frontier packets were silently blocked** (b50ed59): host paths outside the
   workspace survived packet building; the fail-closed check refused the
   packet but kept no packet and no record. All host paths are now rewritten
   (`$HOME`, toolchain caches, JSON/URL-encoded forms), refused packets are
   kept locally and recorded as `blocked`.
4. **Cross-service scanning exhausted memory** (f2b4001): exponential
   expansion of JavaScript constant bindings drove the process to ~58.9 GiB
   on vuejs/core and the kernel killed it. Evaluation is now bounded; the
   scan takes 46 ms there.
5. **Verification changed the candidate** (f5ac462): axios's `npm run build`
   stage rewrote 12 tracked files after the attempt commit. Verification now
   undoes its own side effects and reports them. (Found in a rerun; did not
   change any result.)

Also found: a defect in the benchmark harness itself (acceptance checks
without the cache layer, 65f805a), and a mistake while running the validation (deleting the work
directory under a running Docker Desktop VM) that cost one non-run attempt.

## Model and task failures

* MODEL_REASONING: gin-3227 (fixed the wrong layer, `gin.go` instead of `tree.go`).
* MODEL_CODING: caddy-6288 (partial lexer fix), prometheus-13845 (one of two
  slice-sharing paths fixed), vuejs-11899 (one CSS nesting case missed).
* TASK_AMBIGUITY: go-zero-2283 (`corsRouter`), go-zero-990 (`ReadLink`),
  grpc-go-2744 rerun (`appendH2ToNextProtos`): the acceptance tests use
  identifiers that only the reference solution introduces and the issue does
  not name. Screening did not check for this. The grpc-go change looks
  behaviourally equivalent to the reference on reading; it is not counted.
* **Self-verification false passes:** 4 of 8 frozen runs and 2 of 3 reruns
  passed BoundedCode's verification but failed the hidden tests. The gate is
  only as strong as the repository's tests plus the agent's own new tests.

## Memory-pressure finding

The first run was interrupted when the machine ran out of memory. With a
memory sampler, the second occurrence showed the BoundedCode process itself
at 58.9 GiB (defect 4), not llama.cpp (145 MiB at the time) or the Docker VM
(stopped). Beyond that defect, nothing in the stack budgets memory as a
whole: Docker Desktop's VM may use all host RAM and llama.cpp holds ~20-27 GiB
while serving. See `memory-pressure.md`.

## Answers

* **Q1. Can BoundedCode complete real software-engineering tasks?** Yes, but
  only once in this sample: axios-6539 after the JavaScript fix (one
  attempt, local model). The frozen build completed none.
* **Q2. Most of this sample with only the local model?** No: 1 of 8.
* **Q3. Large repository with a small fraction in context?** Yes, as a
  mechanism: on Prometheus (2.57 M source tokens) at most ~1.9 % of the
  repository entered context (0.24 % via context packs; the 1.9 % includes all
  tool output and is an upper bound), and the agent produced a plausible
  change that passed BoundedCode's verification at the time. The change was
  still wrong.
* **Q4. Does task state survive resume/context reset?** Yes. The Prometheus
  run was interrupted after 5 minutes; a newly built runner resumed the same
  task and agent session (1 resume, 4 condensations), continued and reached
  normal verification.
* **Q5. Did frontier escalation help when needed?** Not measurably: 4 Codex
  calls, no accepted task. The Z3 review on go-zero-2283 caught a real
  security regression (middleware moved ahead of JWT auth), but the local
  model could not apply the advice; the Z2 advice on gin refined a
  wrong-layer approach; one escalation was blocked by defect 3.
* **Q6. Did BoundedCode improve the same local model on the baseline
  subset?** No. gin-3227 and caddy-6288 failed the same tests under both;
  BoundedCode took 17× (gin) and 2× (caddy) longer. See
  `baseline-comparison.md`.

## Verdict

**NOT_VALIDATED.** This sample does not demonstrate that BoundedCode works as
intended on real tasks: 0/8 on the frozen build, 1/8 after fixes. It did
demonstrate the control-plane mechanisms (context reduction, resume,
verification, escalation policy, sandbox and secret protections) and found
five product defects, now fixed. Release recommendation: **NOT_READY** for a
public release or alpha that claims practical effectiveness.

## Limitations

8 tasks, one machine, one local model, one run per task (no repetition), a
Go 1.27-only sandbox image that excluded otherwise-eligible instances, three
tasks whose tests are coupled to reference-only identifiers, and
environment-only verification configs written by the evaluator.

## Revision note (2026-10-05)

Wording only; no result changed.
- "benchmark" replaced by "evaluation" where it described this validation.
- First uses of "hidden acceptance tests" and "local-only" qualified.
- "Local-only completion rate 12.5 % (1/8); frozen build: 0 %" is now "Local-only completions: 1 of 8; frozen build: 0 of 8"; "Verified successes" is now "Successes (hidden acceptance tests pass)".
- grpc-go-2744 substitution: "the brief's list" is now the fallback slot recorded in the manifest (linked).
- Stack line: MoE expert layers partly on CPU and the model server's RAM use (from the memory-pressure finding) added next to the hardware.
- "operator error" is now "a mistake while running the validation"; "verified untested" and "verified change" (Q3) reworded to say what passed; Q3's 1.9 % marked as an upper bound.
