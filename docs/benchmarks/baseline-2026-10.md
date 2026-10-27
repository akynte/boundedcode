# Baseline: architecture audit and measured starting point (2026-10-27)

This is the starting point that later changes to BoundedCode are measured
against. It records:
- what the code does today, read from the code rather than from earlier
  documents;
- which languages BoundedCode actually understands, as opposed to edits
  through its generic file and shell tools;
- three runs of the direct-mode suite and three of the bounded suite,
  with pinned model, build, fixtures and environment;
- where each mode's time, tokens and memory go;
- how much the figures vary from run to run, and what they cannot show.

Every figure below comes from the JSON reports in `benchmarks/reports/`
(`20261027-direct-baseline-r{1,2,3}`, `20261027-tasks-baseline-r{1,2,3}`,
and the A/A run `20261027-direct-aa-r4`).

## Summary

- **Where the time goes.**
  - Model calls take 99.5% of direct mode's agent time and 83–87% of
    bounded mode's task time.
  - Within the calls, generating tokens is 71–78% and reading uncached
    prompt the rest. On this laptop that is 37 tokens/s and 340 tokens/s.
  - The harness's own work in direct mode is 0.025 s per task. Bounded
    mode's verification is 3–4% of a task, and container, indexing and packs
    10–13%.
  - Optimising the harness's own code will not show. Fewer generated tokens,
    fewer model calls and fewer attempts will.
- **What the context is made of.** In direct mode's largest requests, the
  tool schemas are 29% (about 2,000 tokens) and the chat template another
  14%; tool results are 38%.
- **Results.**
  - Direct mode passed 21–23 of 23 runnable tasks (40 s per task).
  - Bounded mode passed 16–17 of 18 (5–6.5 minutes per task), with 1–2
    false verifications per run (on the same two tasks) and 8–10 passes it
    could not verify. Section 7 explains those.
- **Languages.**
  - Measured with the model: Go, Python, TypeScript and a Go + TypeScript
    monorepo in both modes; Rust and Java changes in bounded mode only;
    Rust and Java questions in direct mode.
  - BoundedCode's own understanding (checks, evidence, navigation) varies
    widely by language; see the matrix in section 2.
  - None of this shows general support for a language.
- **Noise.**
  - Paired per-task figures move by up to 12% between runs of the same
    build, and whole-run means by up to 60%.
  - `bench compare` detects a change of 10% or more in direct mode, and
    about 20–25% in bounded mode.
- **Findings** F1–F7 (section 1), among them a security gap in direct
  mode's `grep`, and the other problems these runs surfaced were fixed
  afterwards; section 10 lists each cause, fix and check.

## How to compare a later change with this baseline

1. Build BoundedCode at the candidate commit.
2. Run the same suites on the same machine with nothing else running:
   `bcode bench direct` at least twice, and `bcode bench tasks` at least
   twice.
3. Compare the runs with the baseline:

   ```
   bcode bench compare \
     --base benchmarks/reports/20261027-direct-baseline-r1.json \
     --base benchmarks/reports/20261027-direct-baseline-r2.json \
     --base benchmarks/reports/20261027-direct-baseline-r3.json \
     --candidate NEW-1.json --candidate NEW-2.json
   ```

   For the bounded suite, use `20261027-tasks-baseline-r{1,2,3}.json`.

**How `bench compare` decides.** It reads both kinds of report.
- **Verdicts use paired, per-task figures, not whole-run means.**
  - For each task, the candidate's mean over its runs is divided by the
    baseline's, and the median of those ratios over the tasks is the
    metric's change. The metrics are time, model time, model calls, tool
    calls, attempts, verification time, and prompt, processed, generated
    and reasoning tokens.
  - The noise band is the baseline's own. Each baseline run is compared with
    the other baseline runs the same way, and the range of those ratios is
    widened by its width on both sides.
  - Outside the band a metric is called better or worse. With fewer than 3
    baseline runs there is no band and no verdict.
- **Pass and fail are compared task by task.** A task is called regressed
  (or fixed) only when it passed (or failed) in every baseline run and the
  opposite in every candidate run.
- **Whole-run means are shown but not judged.** Section 9 shows why: in
  this baseline, whole-run means moved by up to 60% between runs of the same
  build, while per-task medians moved by up to 12%.

A change in the suite, the fixtures, the model or its server settings
makes runs incomparable. Each report's `environment` block records all of
them: task-definition and fixture hashes, model file and checksum, server
version and settings, build, and toolchains.

## 1. Architecture and capability inventory

Read from the code at the measured commit, not from earlier documents.
"Implemented" means code and tests exist; "measured" means a benchmark
here has exercised it with a real model.

### Direct mode (`internal/direct`): experimental

| Capability | State | Evidence |
|---|---|---|
| Agent loop over `inference.Gateway` (every provider), streaming | implemented, measured (local model) | direct suite |
| Tools: read, write, edit, multi_edit, read_many, bash (+ background shells), grep, glob, ls, notebooks, todo, plan mode, web_fetch, web_search, recall | implemented; used in this baseline: bash, read, write, edit, multi_edit, read_many, grep, glob, ls, todo_write (`recall` in the earlier series) | `buildTools` (`tools.go:69`); direct suite tool counts |
| Conditional tools: ask_user, save_memory, skill, task (sub-agents), Serena symbol tools, MCP tools | implemented, not measured | unit tests only |
| Routing chat / code / verify, risky-path hand-off | implemented, not measured on real requests | router tests; the suite routes every request |
| Stable prompt prefix, working set + recall, compaction | implemented, measured | suite: cache hits, set-aside, recall |
| Project map, seed pack | implemented, measured (Go fixtures); project map's detection checked on 12 repositories | suite; detection record |
| Checks after edits | Go build/vet; Python, JS, shell, Ruby, PHP syntax; JSON, YAML parse. **None for TypeScript, Rust, Java, C#, C/C++, Kotlin, Swift** | `check.go` |
| History notes | implemented; rule measured offline on Go history; effect on the agent not measured | trail study |
| Guards: path confinement, secret mask, command policy, approvals, hooks | implemented, unit-tested | see finding F1 |

### Bounded mode: implemented (validated on 14 public tasks earlier)

| Capability | State | Evidence |
|---|---|---|
| OpenHands agent in a network-less container; tools: terminal, file editor, task tracker | implemented, validated | `adapters/openhands/python/src/bc_openhands/main.py:154` |
| Context packs (`internal/contextplan`), Serena / codebase-memory-mcp on the Go side only | implemented | the agent has no MCP or symbol tools |
| Verification presets and behavioural evidence (`internal/verify`) | implemented; per-language support in the matrix below | `TestPolyglotVerification` (sandbox) |
| Task ledger, budgets, resume, strategy governor | implemented, validated | earlier validations |
| Frontier escalation | implemented, not proven | never triggered in validation |
| Cross-repository compatibility gate | experimental, fixtures only | |

### Shared infrastructure

| Component | State |
|---|---|
| Model gateway: metering, budgets, streaming, cloud translation | implemented; cloud streams tested against recorded events only |
| llama.cpp supervision | implemented, measured |
| Ledger (`model_calls`, `verification_runs`, `events`, `escalations`) | implemented |
| Benchmarks: `bench infra`, `bench tasks` (bounded), `bench direct`, `bench lease`, `bench trail`, `bench intel`, `bench compare` | implemented |

### Duplicated responsibilities between the modes

| Concern | Direct | Bounded | Same behaviour? |
|---|---|---|---|
| Finding what to read first | `seedpack.go` + `projectmap.go` | `contextplan` packs (graph, Serena, rg) | No: both start from `contextplan.Seeds`, then differ |
| Shrinking the history | set-aside + `recall`, then its own summary | OpenHands' summarising condenser | No |
| Command policy | `policy.CheckCommand` before every `bash` | none for the agent's shell (the container is the boundary); verification stages use the policy | By design |
| Checks on a change | per-file check after each edit (60 s) | verification stages in the sandbox (minutes) | Different purpose |
| Project and language detection | `internal/ecosystem` (direct) | `verify.presetFor` (root manifests); `workspace.DetectLanguages` (Serena) | Three implementations |
| Test-file detection | `ecosystem.IsTest` (test code) | `verify.IsTestFile` (test code and data) + `pythonTestFile` | Different definitions (`testdata/x.json`, `internal/testutil/`, `.bats`) |
| Token estimate | `contextplan.EstimateTokens` | same | Same; `compact.go` repeats the ratio inline |

### Findings

- **F1 (security, direct mode): `grep` through ripgrep does not apply the
  secret-path mask.** When `rg` is installed, the tool searches the tree
  with ripgrep, which skips hidden and git-ignored files but not, for
  example, `credentials.json` or `*.pem`; the other file tools refuse
  secret-looking paths (`tools.go:852-882`). Not fixed in this phase.
- **F2 (measurement): bounded mode's agent calls are not streamed**, so it
  has no time to first token, and its condenser's history rewrites are not
  marked in the ledger.
- **F3 (duplication): three ecosystem detectors and two test-file
  classifiers.** The classifiers differ on purpose: verification's counts
  test data and helper directories (`testdata/`, `testutil/`) as tests,
  while the one behind history notes counts test code only. They should be
  reconciled into one with both answers, not left to drift.
- **F4 (language support): direct mode's checks after edits have no
  TypeScript, Rust, Java, C# or C/C++ check**, and the post-edit pitfall
  notes are Go-only.
- **F5 (direct mode): an empty answer is accepted.** When the model's last
  call produces only reasoning, direct mode ends the turn with no answer
  (seen once in this baseline).
- **F7 (bounded mode, any language): verification finds projects only at
  the repository root.** The Go + TypeScript monorepo has `api/go.mod` and
  `web/package.json` and nothing at the root, so verification ran no test
  stage. The task passed its hidden checks but could never be verified.
- **F6 (documentation): the README's reference hardware says Debian 13**;
  the machine now runs Ubuntu 26.04.1.

## 2. Language support

Reading, searching, editing and running commands work in any language in
both modes; that is the shell and file tools, not language support. The
matrix lists what BoundedCode itself understands about each language, and
what has been measured.

Key:
- **M**: measured with a real model on tasks in that language, in this
  baseline or earlier validations.
- **T**: covered by automated tests with fixtures but no model. For the
  verification presets, that is `TestPolyglotVerification` in the sandbox
  image, which is opt-in (`BC_TEST_DOCKER_IMAGE`). It was run for this baseline against the new image: Python, Rust, Maven, Gradle, PHP, CMake and Make passed. Ruby was first skipped: installing its test dependencies failed on a Docker Desktop file-sharing race (section 10). After that fix, all eight cases passed, Ruby with per-test evidence.
- **–**: absent.

| | Go | Python | JS / TS | Rust | Java | C / C++ | Ruby | PHP | Other |
|---|---|---|---|---|---|---|---|---|---|
| Project detection, build and test commands (direct mode's project map) | T | T | T | T | T (Maven, Gradle) | T (CMake, Meson, Autotools, Make) | T | T | T: .NET, Elixir, Swift, Dart, Zig, Haskell, OCaml, Clojure, Bazel, Terraform, Helm |
| Check after each edit (direct) | build / vet | syntax | JS syntax; **TS none** | – | – | – | syntax | syntax | shell syntax; JSON, YAML parse |
| Notes on code that compiles but is likely wrong (direct) | 1 rule | – | – | – | – | – | – | – | – |
| Outline of long files (direct) | parsed | pattern | pattern | pattern | pattern | pattern | pattern | pattern | pattern |
| Test files left out of history notes | T | T | T | T | T | T | T | T | T: .NET, Swift, Elixir, Dart, Bats |
| Symbol navigation (optional Serena, both modes' context) | T | T | T | T | – | – | – | – | – |
| Verification preset (bounded) | T, M | T, M | T, M | T, M | T (Maven, Gradle); M through `make test` (plain javac) | T | T | T | Terraform fmt, Helm lint, `make test` fallback |
| Behavioural evidence granularity (bounded) | per test function | per test (pytest, unittest) | per stage | per test (Cargo) | per test (Surefire, Gradle) | per test (CTest, Meson) | per test (minitest, RSpec) | per test (PHPUnit) | per stage |
| Cross-repository compatibility gate | gRPC, protobuf | – | OpenAPI (spec-reading tests) | – | – | – | – | – | – |
| Direct suite tasks (this baseline) | 15 | 2 | 2 | 2 | 2 | – | – | – | 2 mixed Go + TS |
| Bounded suite tasks (this baseline) | 9 | 1 | 2 | 1 | 1 | – | – | – | 1 mixed Go + TS; SQL, Terraform, YAML, Dockerfile: 1 each |

## 3. What is measured, and how

**New telemetry in this phase.**

- **Gateway, per model call.** Kept in memory for benchmarks, and in the
  ledger as `model_calls.reasoning_tokens` and `model_calls.ttft_ms`:
  - prompt, cached, generated and reasoning tokens;
  - time to the first streamed token, which also covers answers that are
    only tool calls;
  - the server's own prompt-processing (prefill) and generation (decode)
    times, and the total time.
- **Direct suite, per task:**
  - the call log;
  - time split into model, tools (including the checks after edits) and the
    harness's own work, which is broken down by phase;
  - time per tool;
  - prompt-cache invalidations and the tokens they recompute;
  - the largest request broken down by section;
  - peak memory of the harness, the model server and the GPU;
  - stalls;
  - a failure class (model, timeout, interrupted, routing, environment,
    harness).

  A task whose toolchain is missing is skipped and counted apart, and does
  not fail the run.
- **Bounded suite, per task:**
  - model calls, read from the task's ledger;
  - verification runs and their time;
  - stalls (governor stops, agent stuck, blocked);
  - peak memory of the sandbox containers, the model server and the GPU;
  - a failure class that separates false verifications (self-verified,
    hidden checks fail) from missed ones (hidden checks pass, not verified).
- **Both reports** pin the environment, as listed in the comparison
  section above.

**Exact or estimated.**
- Token counts the server reports (prompt, cached, generated) are exact.
- Reasoning tokens are counted with the model's own tokenizer (llama-server
  `/tokenize`) after each task, and labelled `tokenizer`. With a provider
  that reports them they are labelled `provider`, and otherwise `estimate`
  (bytes × 10 / 32).
- A request's sections are counted with the tokenizer too, each section
  text on its own. What the server counted beyond them is reported as
  `framing`: the chat template, the message delimiters, and how the template
  renders tool schemas, which differs from the schemas' JSON.
- A prompt-cache invalidation is a call that reused fewer than its shared
  prefix with the previous call, less 32 tokens. Those 32 tokens absorb the
  end of the previous prompt (the generation prompt), which the next request
  does not repeat.
- Memory is sampled every second (processes, from `/proc`) or every five
  seconds (GPU through `nvidia-smi`, containers through `docker stats`), so
  a shorter peak can be missed. GPU memory is the whole device's.

## 4. Environment

| | |
|---|---|
| Build | `8e4caea75a51`, from a clean worktree |
| Machine | Lenovo LOQ 15IRH8: i7-13620H (16 threads), 62,741 MiB RAM, RTX 4060 Laptop 8 GB (driver 595.99.02), Ubuntu 26.04.1 |
| Model | Qwen3.6-35B-A3B UD-Q4_K_M (`a483e9e6`, sha256 `ac0e2c11…31a61`, 22.1 GB; the file was re-hashed after the runs and matches) |
| Server | llama.cpp 0.5.0-dev build 11146 (`7fe450e19`), CUDA; 131,072-token context, flash attention, q8_0 KV cache, 35 MoE expert layers on the CPU, 8 threads, 1 slot |
| Sampling | temperature 0.6, top-p 0.95, top-k 20 (not deterministic) |
| Toolchains on the host | Go 1.27.3, Node 24.22.0, Python 3.14.4, git 2.53.0, Docker 29.9.1. No Rust, Java, make, rg |
| Sandbox image | `boundedcode-openhands:local` built from the pinned Dockerfile (Go, Node, Python, Rust 1.99, JDK 11/17/21, Maven, Gradle, C/C++, Ruby, PHP) |
| Suites | direct: 25 tasks, tasks hash `547dd200a937ade8`; bounded: 19 tasks; fixtures `payment-platform` `c151d9d9d3c4dcd4`, `polyglot` `1379bbda7393ab7e` |
| Settings | direct: approval `auto`, checks, seed pack and history notes on, default working set (32K); bounded: the default configuration (frontier off, ambiguity `ask`, sandbox network none) |

The machine ran nothing else during the runs. The model server stayed
loaded between runs, so every run started warm.

## 5. Direct mode: results

Three runs of 25 tasks. The two Rust and Java change tasks were skipped
every time because their toolchains are not on this host, so 23 tasks
ran per run.

| | Run 1 | Run 2 | Run 3 | Mean | Spread |
|---|---|---|---|---|---|
| Passed | 22/23 | 23/23 | 21/23 | 95.7% | 91–100% |
| Seconds per task | 50.6 | 34.9 | 34.5 | 40.0 | 34.5–50.6 |
| Model calls per task | 6.3 | 5.1 | 5.4 | 5.6 | |
| Tool calls per task | 7.0 | 6.0 | 6.1 | 6.3 | |
| Prompt tokens per task | 52,645 | 33,210 | 40,081 | 41,979 | |
| Prompt-cache hit rate | 91.7% | 89.7% | 90.8% | 90.8% | |
| Generated tokens per task | 1,350 | 923 | 878 | 1,051 | |
| of which reasoning | 524 | 245 | 229 | 333 | |
| Time to first token, median | 1,827 ms | 1,769 ms | 1,737 ms | 1,778 ms | |
| Largest single prompt | 37,305 | 25,037 | 32,719 | | |
| Cache invalidations (tokens recomputed) | 2 (614) | 2 (614) | 2 (614) | | |
| Processed tokens per passed task | 5,657 | 4,328 | 4,573 | 4,853 | |
| Peak memory: harness / model server / GPU | 37 / 28,007 / 7,141 MiB | 32 / 29,372 / 7,141 | 35 / 29,676 / 7,141 | | |

**Per language** (passes over the three runs):

| Language | Tasks | Passed | Notes |
|---|---|---|---|
| Go | 15 | 45 of 45 | includes two tasks on golang.org/x/tools and two multi-turn sessions |
| Python | 2 | 6 of 6 | |
| TypeScript | 2 | 6 of 6 | |
| Go + TypeScript (one repository) | 2 | 4 of 6 | the feature task failed twice: the agent changed all four files across both languages and the schema but did not cap the discount at the subtotal, which the request asked for |
| Rust | 2 | 2 of 3 run | the change task was skipped (no cargo); the question failed once, with an empty answer (see below) |
| Java | 2 | 3 of 3 run | the change task was skipped (no JDK) |

**Failures, by cause.**
- Model: 3 (the two discount caps above, and one empty answer).
- Environment: the 6 skipped runs of the Rust and Java change tasks.
- Timeouts, harness errors, routing hand-offs, stalls: none.

The empty answer: the last model call of `rust-explain-ledger` in run 3
generated 139 tokens, 138 of them reasoning and no text. Direct mode
accepted the empty text as the answer.

## 6. Where direct mode's time and tokens go

Over the three runs: 69 tasks, 389 model calls.

- **Model calls take 99.5% of the agent's time.**
  - Per task: 39.7 s in model calls, 0.16 s in tools (with the checks after
    edits), 0.025 s in the harness's own work.
  - The harness's phases over all three runs: checks after edits 2.9 s,
    history notes 0.09 s, project map 0.05 s, seed pack 0.02 s, working set
    0.01 s.
  - Tools over all three runs: bash 7.4 s (61 calls), edit 2.6 s (73; most
    of it the checks), glob 0.4 s, write 0.2 s; read and grep are negligible
    (205 and 25 calls).
- **Within the model calls, generating takes 71% of the time and reading the
  prompt 28%.**
  - Decode: 1,947 s for 72,488 tokens, 37.2 tokens/s.
  - Prefill: 774 s for 262,937 uncached prompt tokens, 340 tokens/s.
  - Reasoning was about a third of the generated tokens (333 of 1,051 per
    task).
- **The prompt cache holds**: 91% of prompt tokens were served from it, and
  only 2 calls per run lost part of it (614 tokens).
- **What the largest request of a task is made of** (69 tasks, mean 6,810
  tokens, counted with the model's tokenizer):

  | Section | Share | Mean tokens |
  |---|---|---|
  | Tool results | 38.3% | 2,606 |
  | Tool schemas | 29.3% | 1,992 |
  | Chat template and framing | 14.2% | 965 |
  | System prompt | 5.2% | 356 |
  | Tool-call arguments | 4.9% | 337 |
  | Environment and mode framing | 3.9% | 269 |
  | Project map | 1.5% | 105 |
  | Assistant text | 1.4% | 98 |
  | User text | 0.8% | 55 |
  | Seed pack | 0.3% | 22 |

  The tool schemas are the same in every request, so after the first call
  they come from the cache. They still take context and prefill on every
  new session.
- **Time to first token** is about 1.8 s (median of task medians). The
  largest single value was 12.6 s, on a long session's call.

**Where the bottleneck is, on this hardware:** generating tokens, then
reading uncached prompt. The harness's own work is three orders of
magnitude smaller than the model's. Changes that cut generated tokens
(including reasoning) or model calls will show; changes to the harness's
own speed will not.

## 7. Bounded mode: results

Three runs of 19 tasks, each in the sandbox, with the default
configuration.
- One task, `go-large-iface-method`, could not start in any run: its source
  is a Go module (`go.temporal.io/sdk`) that is not in this machine's module
  cache. It is classed as environment, and 18 tasks ran.
- Escalation to a frontier model is off by default, and no run made a
  frontier call.

| | Run 1 | Run 2 | Run 3 |
|---|---|---|---|
| Passed hidden checks | 16/18 | 17/18 | 16/18 |
| Self-verified (`TASK_VERIFIED`) | 10 | 9 | 8 |
| False verifications (self-verified, hidden checks fail) | 2 | 1 | 2 |
| Missed verifications (hidden checks pass, not verified) | 8 | 9 | 10 |
| Seconds per task | 332 | 297 | 384 |
| Model calls per task | 22.8 | 19.7 | 23.7 |
| Attempts per task | 2.00 | 2.11 | 2.06 |
| Prompt tokens per task (cache hit rate) | 361,681 (91.3%) | 298,056 (91.4%) | 392,327 (91.3%) |
| Processed tokens per task | 38,589 | 31,615 | 42,652 |
| Generated tokens per task | 7,507 | 6,609 | 9,052 |
| Cache invalidations (tokens recomputed) | 26 (39,812) | 22 (16,993) | 30 (57,991) |
| Peak memory: sandbox / model server / GPU / harness | 668 / 29,553 / 7,141 / 34 MiB | 652 / 29,611 / 7,161 / 34 | 596 / 29,672 / 7,161 / 34 |
| Wall time of the run | 1.66 h | 1.49 h | 1.92 h |

**Per task:**
- **Passed in all three runs** (16 tasks):
  - Go: 6 tasks;
  - TypeScript: both tasks;
  - one task each in Java, Rust, Python, Go + TypeScript, SQL, Terraform,
    Kubernetes YAML and Dockerfile.
- **False verifications:**
  - `cross-service-idempotency` was self-verified in all three runs and
    failed its hidden test every time.
  - `go-debug-ledger-balance-query` was self-verified and failed in runs 1
    and 3; in run 2 it passed, unverified.
- **Never verified though they pass in every run**, for three reasons:
  - The change has no test stage to show it: Docker, Terraform, and the
    Go + TypeScript monorepo (finding F7).
  - The change preserves behaviour, so no test can fail on the base:
    `go-refactor-validate` and `go-iface-add-method`.
  - Evidence for JavaScript and TypeScript is per stage, not per test: both
    TypeScript tasks were never verified. `ts-bugfix-cart-discount` took 4–5
    attempts in every run.
  - Three more tasks were verified in some runs and not in others
    (`api-contract-description`, `go-bugfix-ledger-balance`,
    `sql-migration-idempotency-key`).
- **Retries:** almost every task used two attempts. The first change usually
  passes the checks without a test that demonstrates it, and bounded mode
  then asks once for one.
- **No governor stops, stuck agents or blocked tasks.**

## 8. Where bounded mode's time goes

Over the three runs (54 task runs):
- **Model calls take 83–87% of a task's time**, and decode is 76–78% of that.
- **Verification takes 3.0–3.7%** (11 s per task, about four verification
  runs).
- **The rest is 10–13%** (35–38 s per task): starting the container,
  indexing the repository with codebase-memory-mcp, building context packs,
  and the adapter.
- A task makes about 22 model calls, against 5.6 in direct mode.
  - Each call resends a history that grows to 300–390K prompt tokens per
    task.
  - 91% of those tokens come from the cache.
  - There were 22–30 invalidations per run. Their causes are not attributed:
    in total they are of the order of the extra attempts plus condensations
    (18–20 and 1–7 per run), but the two match for only 9–12 of 18 tasks.
    The ledger does not mark where the history was rewritten (F2).

**On the same changes**, direct mode (three runs) against bounded mode
(three runs):

| Change | Direct: passed, mean time | Bounded: passed, verified, mean time |
|---|---|---|
| Python: `remove` overdraw | 3/3, 28 s | 3/3, 3 verified, 805 s (189–1,724 s) |
| TypeScript: second discount code | 3/3, 21 s | 3/3, 0 verified, 352 s |
| Go + TypeScript: discount in the summary | 1/3, 86 s | 3/3, 0 verified (no test stage), 528 s |

Bounded mode took 6–30 times as long. It got the mixed-language feature
right every time, where direct mode missed a stated requirement twice. That
is one task with three runs per mode, and it is consistent with the
evidence loop's purpose, not proof of it.


## 9. Run-to-run variation and what limits these figures

- **Variation is large on long tasks.**
  - `session-x-tools` took 410 s, 165 s and 146 s in the three runs. That
    alone makes run 1's mean 11 s per task higher.
  - Per-run means moved by 25–60% between runs: model calls 5.1–6.3,
    prompt tokens 33K–53K, generated tokens 878–1,350, time 34.5–50.6 s. The pass count moved by 2 tasks (21–23 of 23).
  - A candidate run therefore needs at least two runs, and `bench compare`
    to judge it against the baseline's range.
- **An A/A check of the comparison.** A fourth direct run of the same build
  (`20261027-direct-aa-r4`) was compared with the three baseline runs.
  - With the noise band as first written (the leave-one-out range widened by
    its width), two of eight paired metrics came out "worse": tool calls
    1.09 against a band ending at 1.07, and prompt tokens 1.03 against 1.02.
  - The band now covers at least ±10% around no change, and all eight are
    within noise. That floor was set from this check: a change smaller than
    10% in a paired metric cannot be detected against this baseline.
  - The bounded bands are wider: time 0.75–1.21, model calls 0.76–1.19,
    generated tokens 0.74–1.22. A bounded change needs to move a metric by
    about 20–25% before it registers.
  - No bounded A/A run was made (a fourth bounded run takes about two
    hours).
  - In the A/A run, `session-ledger` failed once after passing in all three
    baseline runs. A single candidate run therefore never makes a task
    "regressed".
- **Sampling is not deterministic** (temperature 0.6), and the server's
  prompt cache carries over from the previous task.
- **The suites are small, and the tasks were written for this project.**
  - The direct suite's non-Go tasks are one change and one question per
    language, on small fixtures.
  - A pass on them shows that the loop works for that language. It does not
    show that BoundedCode supports the language in general.
- **Rust and Java changes were not run in direct mode.** Direct mode runs
  checks on the host, and this host has no cargo or JDK. Their hidden checks
  were validated in the sandbox image instead.
- **One machine, one model.** No cloud provider was measured; that costs
  money and needs the maintainer's keys. Time figures depend on this GPU
  and this CPU offload.
- **Answers are judged by patterns.** A correct answer worded differently
  fails (`expect`). No such case was seen in these runs.
- **Memory is sampled**, and GPU memory is the device's, other processes
  included.

## 10. Findings fixed after the baseline (2026-10-28)

Each problem the runs surfaced was traced to its cause and fixed. The
baseline figures above are unchanged; they describe the build that was
measured (`8e4caea`).

| Finding | Cause | Fix | Checked by |
|---|---|---|---|
| F1: direct mode's `grep` through ripgrep showed secret files | ripgrep's ignore rules do not know the secret-path policy | ripgrep reports JSON; every file passes the same check as the other tools | a fake-ripgrep test with `credentials.json`, `*.pem` and `.env.example` |
| False verification of `cross-service-idempotency` | one repository's test verified a change to two | each changed repository whose change goes beyond tests and documentation needs its own evidence | orchestrator tests (the old rule reproduces the false pass). In the rerun both services had evidence; the hidden test still fails on concurrent requests, which no agent test covered. That is the gate's documented limit: a test shows only the reading the agent chose |
| TypeScript tasks never verified, 4–5 attempts, a stray `package-lock.json` | npm stages demanded `node_modules` for a project with no dependencies, so they failed on every clean checkout, and the evidence then said "also pass on the base" | `node_modules` is required only when dependencies are declared; a stage that fails on the base either way is reported as such | evidence tests (the old preset reproduces it). In the rerun `ts-bugfix-cart-discount` was verified in 2 attempts and 111 s, with no stray file |
| F7: the Go + TypeScript monorepo had no test stage | presets looked only at the repository root | the stages of each outermost subproject, run in its directory (stages take a `dir`) | preset, package-mapping, directory and end-to-end tests. In the rerun the `api/*` and `web/*` stages ran and the task was verified in one attempt |
| F5: an empty answer was accepted | the loop ended when the model's last call had no text | the model is asked once for an answer | unit test. It fired once in the rerun of the direct suite (`empty_answer` stall) |
| No `java` or `javac` for the bounded agent | the image set `JAVA_HOME` but did not add a JDK to `PATH` | the default JDK is on `PATH`; verification still puts the version a project asks for first | the image and the Maven and Gradle presets, 3 times each |
| `go-large-iface-method` could not start | its module, and the modules it needs, were not in the cache | the harness downloads the module and its whole build list (`go mod download all`, as that module), with checksums verified | a local-proxy test that also builds offline. The first rerun exposed that downloading only the module was not enough |
| `go-large-iface-method` still could not pass | the Go preset's `gofmt` checked every file, and Temporal's SDK ships one without a final newline | `gofmt` checks only the files the change touched (`{changed}`) | a verification test with an unformatted file at the base |
| `go-large-iface-method` passed unverified | the task's own `go test ./converter/...` stage was not recognised as a Go stage (no `requires: [go.mod]`), and re-running named tests needed `{packages}` | a stage that runs `go test` is a Go stage; with fixed packages the test filter is appended after them | an evidence test with such a stage (the old code gives the run's exact reason) |
| Ruby's preset could not be re-validated | on Docker Desktop's file sharing, `install -m` of a freshly built native extension fails with EPERM in many attempts | the test builds gems in the container and copies them | Ruby passed 3 of 3, with per-test evidence |
| F2: bounded cache misses unattributed | the condenser's calls were recorded as the agent's | condenser calls are recorded as `agent:condenser`; misses at a change of caller or around the condenser count as expected | gateway and adapter tests |
| F3: two test-file classifiers | two definitions in two packages | one place, `ecosystem.IsTestOrData` (verification) extending `ecosystem.IsTest` (history notes) | the existing verification tests, unchanged, and a relationship test |
| F6: README's OS | the machine was reinstalled | corrected | — |
| Server version in reports | the first output lines are log lines | `llamacpp.Version`, as `doctor` uses | a test with a fake server |

Not changed:
- **The three project detectors.** Verification reads the base commit's
  file list on purpose, so an agent cannot change which stages run. Direct
  mode's map walks the working tree. Serena needs languages, not projects.
  They answer different questions from different inputs.
- **`go-debug-ledger-balance-query`'s false verification.** The agent's
  code fix was right. It added its test to `ledger_test.go`, which the
  task's criteria forbade and its hidden check enforces. The gate does not
  read criteria written in natural language.

A rerun of the direct suite with these fixes (`20261028-direct-fixcheck`)
is within the baseline's noise on every paired metric, and passed 21 of
23. Its two failures are model mistakes the baseline also showed.

Bounded reruns of the affected tasks, one run each
(`20261028-tasks-fixcheck`, `20261028-tasks-fixcheck-large`):

| Task | Baseline (3 runs) | After the fixes |
|---|---|---|
| `ts-bugfix-cart-discount` | passed, never verified, 4–5 attempts, 282–447 s | passed, verified, 2 attempts, 111 s |
| `multi-feature-discount` | passed, never verified (no test stage) | passed, verified, 1 attempt, 301 s; the `api/*` and `web/*` stages ran |
| `go-large-iface-method` | could not start | passed in 2 attempts, 380 s. Not verified: the agent's tests call the method the change adds, so they cannot build on the base (new API, a documented limit) |
| `cross-service-idempotency` | failed, falsely verified | failed, with evidence in both services; the hidden test is about concurrent requests |
| `java-bugfix-paid-total`, `py-bugfix-inventory` | passed, verified | passed, verified |

The first rerun of `go-large-iface-method` exposed two more causes, each
fixed and in the table above: the module's dependencies were missing, and
`gofmt` failed on a file the change did not touch. The `fixcheck` report
holds that run (blocked after 6 attempts); `fixcheck-large` holds the run
with all fixes.

## 11. Not measured yet

- Cloud providers (time to first token and caching behave differently).
- Direct mode on Rust, Java, C#, C/C++, Ruby or PHP changes (no host
  toolchains here).
- Direct mode's routing on real requests, and its sub-agents, skills and
  MCP tools.
- Bounded mode's time to first token: its agent's calls are not streamed.
- Context size by section in bounded mode, beyond the context packs.
