# Second independent validation (2026-10-05)

> Six previously unused public tasks, screened **before** the run for
> acceptance tests derivable from the issue, frozen, and run **once each** on
> a frozen candidate build. This is a small practical validation sample, not
> a statistically comprehensive benchmark: read the result as "5 of 6", not
> as a rate. Earlier results stand as published: **0/8 on the original
> frozen build, 1/8 after the defect fixes**; the development corpus is
> development evidence only.

Data: [`benchmarks/reports/second-validation-20261005/`](../../benchmarks/reports/second-validation-20261005/)
(candidate manifest, [screening table](../../benchmarks/reports/second-validation-20261005/screening.md),
curator analysis, frozen task specs with verification configs, environment, per-run JSON and
logs, memory samples, `metrics.json`).

## Verdict

**VALIDATED_FOR_PUBLIC_ALPHA.**

- **Result:** 5/6 tasks completed successfully (TASK_VERIFIED plus a
  passing hidden acceptance test), all five using only the local model.
- **Verification:** 0 false verification passes.
- **Runaway and memory:** no runaway generation and no memory pressure.
- **The sixth task (prometheus)** was fixed correctly; its hidden test
  passes. It ended UNVERIFIED because of a gap in BoundedCode's evidence
  check, and counts as FAILED under the pre-registered definition.

| Criterion | Target | Result |
|---|---|---|
| Pass rate | ≥ 75 % | 5/6 = 83 % ✔ (6/6 hidden tests pass) |
| Local majority | clear | 5/5 successes local-only ✔ |
| False verification passes | 0 | 0 ✔ |
| Security regression | none | none ✔ |
| Runaway / memory / state loss | none | none ✔ |

## Methodology

1. **Freeze the candidate build** before choosing tasks (build `5b3c54a`;
   product code identical to `4b31742`). No product change was made
   between the freeze and this report.
2. **Pre-register the selection rule.** Six slots: Go (gin),
   Go + infrastructure (caddy), Go RPC (go-zero/grpc-go), JavaScript (axios),
   TypeScript (vuejs/core), large repository (prometheus). Candidates are
   ranked by `sha256("boundedcode-second-validation-2026-10-05:" + id)`, and
   the four best per slot are screened. Every instance used before is
   excluded (gin-3227, caddy-6288, prometheus-13845, vuejs-11899,
   axios-6539, grpc-go-2744, go-zero-2283, go-zero-990), as are gold patches
   that change dependency manifests.
3. **Screen (two gates).**
   - **Environment:** acceptance fails on base and passes with the
     reference patch in the offline sandbox.
   - **Derivability:** a curator who saw the issue, base code, hidden test
     and reference patch judged whether every behaviour the test requires
     follows from the issue (YES/NO/AMBIGUOUS), reproducing the issue on
     base and gold where practical. Only YES enters.
4. **Select and freeze.** Per slot, the best-ranked candidate passing both
   gates is selected. Each base gets an environment-only verification
   config, confirmed to pass on the untouched base. Acceptance is
   re-screened with the configs applied, then everything is committed
   (`f63dde0`, 17:25:54) before the first run (17:26:04).
5. **Run once each**, sequentially, on an otherwise idle machine. The agent
   never sees hidden tests, reference patches or screening notes.
6. **Score.**
   - Success = TASK_VERIFIED **and** the hidden acceptance test passes.
   - Classes: LOCAL_ONLY_SUCCESS, FRONTIER_ASSISTED_SUCCESS, FAILED,
     BLOCKED_ENVIRONMENT.

## Candidate screening

| Verdict over 24 candidates | Count |
|---|---|
| YES (derivable) | 12 |
| AMBIGUOUS | 5 |
| NO | 5 |
| Derivable but environment-invalid (offline / never exits) | 2 |

10 of 24 candidates were rejected because their acceptance tests were not
issue-derivable:
- **Names only the reference patch introduces (4):** `BindHeader`,
  `DontTracingSpanName`, `WithIgnoreTimeout`, `Equal` methods.
- **Incidental choices pinned (3):** whitespace semantics and an exact
  error message; one specific race fix; one snapshot-cleanup policy.
- **Internal function called directly (1).**
- **Fixtures only the gold patch adds (1).**
- **An acceptance command broken under Go 1.27 (1).**

No acceptance command was modified. Full table with reasons:
[screening.md](../../benchmarks/reports/second-validation-20261005/screening.md).

## Frozen task set

| Task | Repository @ base | Benchmark source | Category |
|---|---|---|---|
| gin-gonic__gin-1805 | gin-gonic/gin @ 70a0aba3 | SWE-bench Multilingual | Go |
| caddyserver__caddy-6370 | caddyserver/caddy @ 198f4385 | SWE-bench Multilingual | Go + infrastructure |
| zeromicro__go-zero-1969 | zeromicro/go-zero @ af05219b | Multi-SWE-bench | Go |
| axios__axios-5892 | axios/axios @ ae003913 | SWE-bench Multilingual | JavaScript |
| vuejs__core-11870 | vuejs/core @ 67d6596d | SWE-bench Multilingual | TypeScript |
| prometheus__prometheus-10720 | prometheus/prometheus @ 89de30a0 | SWE-bench Multilingual | Large repository (1.8 M source tokens) |

The verification configs are the default presets minus checks that fail on
the untouched base offline:
- tests needing the network, a browser or masked TLS keys;
- files Go 1.27's gofmt reformats at base;
- Prometheus commands that do not link under Go 1.27;
- `--retries 2` for one axios body-upload test that resets under Node 24.

Each exclusion is listed in its task spec.

## Environment

| Component | Detail |
|---|---|
| Host | i7-13620H (16 threads), 62.5 GiB RAM, RTX 4060 Laptop 8 GB, Debian 13 (Linux 7.1) |
| Model | Qwen3.6-35B-A3B UD-Q4_K_M; llama.cpp v0.5.0 (7fe450e) |
| Model settings | 131 K context, 35 MoE layers on CPU, q8_0 KV cache; temperature 0.6, top-p 0.95, top-k 20 |
| Budgets | `reasoning_budget` 4096 per response; visible output 8192 per response; strategy budget 40 K tokens without progress, 50 K and 35 min per attempt |
| Agent | OpenHands SDK 1.51.0 in Docker 29.8.0, network none, 8 GiB / 8 CPUs; Go 1.27.1, Node 24.21 in the sandbox |
| Repository intelligence | codebase-memory-mcp 0.11.0; Serena v1.7.0 enabled |
| Frontier | Codex CLI 0.156.1 (ChatGPT sign-in), normal Z1-Z4 policy, approvals pre-granted; no paid API keys |
| Task policy | `task.contract: true`, `task.ambiguity: proceed` (benchmark; the default is `ask`) |

Record: [`environment.json`](../../benchmarks/reports/second-validation-20261005/environment.json).
The binary's embedded version string predates the history sanitization; it
maps to `5b3c54a` (see `history-sanitization.md`).
Erratum: `environment.json` says "frozen at 17:30"; the freeze commit is
17:25:54.

## Per-task results

| Task | Class | State | Hidden test | Attempts | Wall | Calls | Output tokens | Largest response | Strategy stops |
|---|---|---|---|---|---|---|---|---|---|
| gin-1805 | LOCAL_ONLY_SUCCESS | task_verified | PASS | 1 | 867 s | 24 | 20.2 K | 4,672 | 0 |
| caddy-6370 | LOCAL_ONLY_SUCCESS | task_verified | PASS | 1 | 1,298 s | 21 | 34.6 K | 6,301 | 0 |
| go-zero-1969 | LOCAL_ONLY_SUCCESS | task_verified | PASS | 1 | 757 s | 46 | 14.7 K | 1,615 | 0 |
| axios-5892 | LOCAL_ONLY_SUCCESS | task_verified | PASS | 1 | 733 s | 26 | 6.5 K | 827 | 0 |
| vuejs-11870 | LOCAL_ONLY_SUCCESS | task_verified | PASS | 2¹ | 632 s | 29 | 9.0 K | 927 | 0 |
| prometheus-10720 | **FAILED** (strict) | tests_green, UNVERIFIED | PASS | 2 | 1,768 s | 69 | 36.4 K | 4,304 | 0 |

¹ The first attempt failed the repository's lint stage; the retry fixed it
in 1.5 min.

- **gin-1805:** router-group static file system called middleware twice on
  404. Fix in `routergroup.go`; `TestLoggerStaticFS404SingleLogLine` fails
  on base.
- **caddy-6370:** `Caddyfile.<ext>` without an adapter. Fix in
  `cmd/main.go`; 4 `Test_isCaddyfile` cases fail on base.
- **go-zero-1969:** `options` not enforced for `json.Number`. Fix in
  `core/mapping/unmarshaler.go`; 3 new tests fail on base. The curator's
  noted risk (the hidden test goes through `UnmarshalKey`) did not
  materialise.
- **axios-5892:** upper-case `Content-Encoding` not decompressed. Fix in
  `lib/adapters/http.js`; the mocha test fails on base.
- **vuejs-11870:** `renderList` over `shallowReactive` arrays. Fix in
  `renderList.ts`; the vitest case fails on base.
- **prometheus-10720:** new `day_of_year()`. The agent added it to the
  parser and engine and wrote cases in `promql/testdata/functions.test`
  (Jan 1, leap and non-leap Dec 31); these fail on base with "unknown
  function". See Failures.

## Local vs frontier usage

All five successes are local-only. No escalation was triggered on any task
(0 frontier calls), so FRONTIER_ASSISTED_SUCCESS is 0.

## Verification accuracy

| | Count |
|---|---|
| task_verified and hidden PASS (true pass) | 5 |
| task_verified and hidden FAIL (false pass) | **0** |
| UNVERIFIED and hidden PASS (false negative) | 1 (prometheus) |
| UNVERIFIED and hidden FAIL | 0 |

Every verified result rested on a behavioural failure on the base. None
relied on a test that merely fails to compile there.

## Runaway-control results

- **Governor:** no stops; no attempt came near 40 K tokens without
  progress.
- **Visible-output cap:** never reached (largest response 6,301).
- **Thinking budget:** in effect on every response.
- **Longest task:** 29.5 min.
- **Unproductive generation:** none to stop. Successful tasks were not
  slowed: the median task took 812 s, and total generation per task was
  6.5-36 K tokens.

## Ambiguity

| | Result |
|---|---|
| Contract derived | 4 of 6. On caddy and prometheus the model's contract listed nothing required, and the task ran without one. |
| SPEC_AMBIGUOUS | 1 of 6 (vue) |

The vue flag was a **false positive**: "nested properties or direct items"
for a clear request. Under `proceed` the task continued and passed. Under
the default `ask`, it would have stopped for a needless question. No task
was blocked.

## Context efficiency

| Task | Repository source tokens | Pack + tool output | Share |
|---|---|---|---|
| prometheus | 1,827,362 | 29.2 K | 1.6 % |
| vuejs/core | 1,223,814 | 33.2 K | 2.7 % |
| go-zero | 751,149 | 27.8 K | 3.7 % |
| caddy | 701,528 | 18.3 K | 2.6 % |
| axios | 274,912 | 19.7 K | 7.2 % |
| gin | 120,111 | 35.0 K | 29.1 % |

Retrieval seeds:
- **caddy:** the quoted error message, then `filepath.Base` and
  `cmd/main.go`.
- **vue:** `shallowReactive` and `renderList`, with sample names last.
- **The other four:** no code-like seeds; the agent located the code itself.

Prompt-cache hit rate was 0.93-0.95.

| Resource | Peak |
|---|---|
| BoundedCode | 67 MiB |
| llama-server | 29.3 GiB |
| Docker VM | 10.2 GiB |
| Containers | 2.4 GiB |
| Minimum available | 33.1 GiB |

## Failures

**prometheus-10720 (FAILED, verification false negative).** The Go evidence
check counts `Test…` functions defined in the changed test files. The
agent's test is a data file, `promql/testdata/functions.test`, read by
`TestEvaluations` in another file, so the check found no test to run on the
base. It reported "the changed tests also pass on the base commit" and
asked once for another test. The second attempt added more cases to the
same file with the same outcome, and the task ended UNVERIFIED, a
candidate that needs review. The fix and the test are correct (the hidden
test passes), so the cause is BoundedCode's evidence check, not the model.
Fix candidate: when changed test files define no tests, run the package's
tests on the base export and on the change, and compare the failing sets.

## Limitations

- **Six tasks, one run each.** Variance across reruns is unmeasured.
- **Screened tasks only.** Screening kept only issue-derivable tests:
  10 of 24 candidates were rejected, and real requests are not screened.
- **Ambiguity policy.** The run used `proceed`. The default `ask` would
  have blocked one clear task (vue) on a false ambiguity.
- **Evidence-check gap.** The Go evidence check misses data-driven tests
  (above). It also counts a test that does not compile on the base as
  failing there, a weaker signal, seen in a development run but not here.
- **One configuration.** One machine, one model; the frontier was enabled
  but never triggered.
