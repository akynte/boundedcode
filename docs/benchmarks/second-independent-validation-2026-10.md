# Second independent validation (2026-10-05)

> Six previously unused public tasks (SWE-bench Multilingual / Multi-SWE-bench),
> screened **before** the run for acceptance tests derivable from the issue,
> frozen, and run **once each** on a frozen candidate build. Small sample:
> read the result as "5 of 6", not as a rate. Earlier results stand as
> published: **0/8 on the original frozen build, 1/8 after the defect
> fixes**; the development corpus is development evidence only.

Data: [`benchmarks/reports/second-validation-20261005/`](../../benchmarks/reports/second-validation-20261005/)
(candidate manifest, [screening table](../../benchmarks/reports/second-validation-20261005/screening.md),
curator analysis, frozen task specs with verification configs, environment, per-run JSON and
logs, memory samples, `metrics.json`).

## Result

**5 of 6 succeed (83 %), all local-only. 0 false verification passes.**

| Task | Slot | Class | State | Hidden test | Attempts | Wall | Calls | Output tokens | Largest response | Frontier |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-1805 | Go | LOCAL_ONLY_SUCCESS | task_verified | PASS | 1 | 867 s | 24 | 20.2 K | 4,672 | 0 |
| caddy-6370 | Go + infrastructure | LOCAL_ONLY_SUCCESS | task_verified | PASS | 1 | 1,298 s | 21 | 34.6 K | 6,301 | 0 |
| go-zero-1969 | Go | LOCAL_ONLY_SUCCESS | task_verified | PASS | 1 | 757 s | 46 | 14.7 K | 1,615 | 0 |
| axios-5892 | JavaScript | LOCAL_ONLY_SUCCESS | task_verified | PASS | 1 | 733 s | 26 | 6.5 K | 827 | 0 |
| vuejs-11870 | TypeScript | LOCAL_ONLY_SUCCESS | task_verified | PASS | 2¹ | 632 s | 29 | 9.0 K | 927 | 0 |
| prometheus-10720 | Large repository | **FAILED** (strict) | tests_green, UNVERIFIED | PASS | 2 | 1,768 s | 69 | 36.4 K | 4,304 | 0 |

Success = TASK_VERIFIED (a test the change adds fails on the base and passes
with the change) **and** the hidden acceptance test passes. No task was
BLOCKED_ENVIRONMENT, and no escalation was triggered. No human
intervention: the environment-only verification configs were fixed before
the run.

¹ Vue's first attempt failed the repository's lint stage; the retry fixed it
(1.5 min) and was verified.

**Prometheus counts as FAILED although its fix is correct.** The agent:
- added `day_of_year` to the parser and engine,
- wrote data-driven cases in `promql/testdata/functions.test` (leap and
  non-leap year ends), which fail on the base with "unknown function",
- passed the hidden test.

BoundedCode's evidence check for Go looks for `Test…` functions defined in
the changed test files. A `.test` data file defines none, so the check
reported "the changed tests also pass on the base" and asked once for
another test. The second attempt produced the same kind of test, and the
task ended UNVERIFIED, a candidate needing review. This is a
**verification false negative** (BoundedCode gap, see below), not a model
failure. The strict definition is kept as pre-registered.

## Criteria

| Criterion | Target | Result |
|---|---|---|
| Pass rate | ≥ 75 % | 5/6 = 83 % ✔ |
| Local majority | clear | 5/5 successes local-only ✔ |
| False verification passes | 0 | 0 ✔ (every task_verified result passed its hidden test) |
| Security regression | none | none ✔ (no boundary changed; sandbox network none throughout) |
| Runaway / memory | none | none ✔ (largest response 6.3 K; longest task 29.5 min; BoundedCode ≤ 67 MiB, ≥ 33.1 GiB available) |

**Verdict: VALIDATED_FOR_PUBLIC_ALPHA** on this sample. The sample is six
tasks, each screened so that its hidden test follows from its issue. Tasks
whose tests encode undisclosed choices were excluded by design (10 of 24
candidates), and real requests have no such screen.

## Screening (frozen before the run, commit f63dde0)

- **Candidates:** 24, the four best-ranked per slot by a pre-registered hash.
- **Two gates:**
  - environment: acceptance fails on base, passes with gold, offline
  - derivability: curator analysis with reproduction
- **Outcome:**

  | Verdict | Candidates |
  |---|---|
  | YES (derivable) | 12 |
  | AMBIGUOUS | 5 |
  | NO | 5 |
  | Derivable but environment-invalid | 2 |
- **Most common rejection:** the hidden test calls names that only the
  reference patch introduces (4 candidates). Next most common: it pins
  incidental choices.
- **No acceptance command was modified.**
- **Full table:**
  [screening.md](../../benchmarks/reports/second-validation-20261005/screening.md).
- **Freeze-record erratum:** `environment.json` says "frozen at 17:30".
  The freeze commit is 17:25:54, and the run started at 17:26:04.

## Per-task notes

- **gin-1805:** a router-group static file system called middleware twice
  on 404. The agent fixed `routergroup.go` and added
  `TestLoggerStaticFS404SingleLogLine`, which fails on base.
- **caddy-6370:** `Caddyfile.<ext>` without an adapter. The agent fixed
  `cmd/main.go` and added `Test_isCaddyfile` cases (4 fail on base). The
  quoted error message was the first retrieval seed.
- **go-zero-1969:** `options` not enforced for `json.Number`. The fix in
  `core/mapping/unmarshaler.go` passed the hidden `UnmarshalKey` test. This
  was the curator's noted risk, and it did not materialise.
- **axios-5892:** upper-case `Content-Encoding`. The fix in
  `lib/adapters/http.js` plus a mocha test failing on base.
- **vuejs-11870:** `renderList` over `shallowReactive` arrays.
  - The task contract flagged a **material ambiguity that is not one**:
    "nested properties or direct items". The issue is clear.
  - Under the benchmark policy (`proceed`), it was recorded as
    SPEC_AMBIGUOUS and the work continued.
  - Under the default `ask` policy, this task would have stopped for a
    needless question.
- **prometheus-10720:** see above.

## Mechanisms from the targeted pass, observed here

| | Observation |
|---|---|
| Strategy governor | 0 stops. No attempt approached 40 K tokens without progress. |
| Visible-output cap | Not reached (largest response 6,301). |
| Task contract | Derived for 4 of 6. On caddy and prometheus the model returned no required items ("nothing required"), and the task ran without a contract. |
| SPEC_AMBIGUOUS | 1 of 6 (vue). A false positive, as above. |
| Retrieval seeds | caddy: diagnostic first, plus `filepath.Base` and `cmd/main.go`. vue: `shallowReactive`, `renderList`, with sample names demoted to last. The other four issues had no code-like seeds; the agent found the files itself. |
| Context | Pack plus tool output: 1.6-3.7 % of the repository on the four large repositories; 7.2 % (axios) and 29 % (gin, a small repository). |

## Resources

| Metric | Value |
|---|---|
| Wall-clock | median 812 s, total 6,055 s (1 h 41 min) |
| Prompt-cache hit rate | 0.93-0.95 |
| Memory: BoundedCode | ≤ 67 MiB |
| Memory: llama-server | ≤ 29.3 GiB |
| Memory: Docker VM | ≤ 10.2 GiB |
| Memory: containers | ≤ 2.4 GiB |
| Memory: minimum available | 33.1 GiB |

## What this does and does not show

- **Shows:** on issue-derivable tasks across Go, JavaScript, TypeScript, an
  infrastructure tool and a 1.8 M-token repository, the frozen build with a
  local 35B-A3B model solves and **proves** most tasks in 10-30 minutes,
  with no frontier calls. Its verification did not certify a wrong patch.
- **Does not show:**
  - performance on requests whose expected behaviour is not derivable from
    the text (half of the screened candidates);
  - stability across reruns (each task ran once);
  - behaviour under the default `ask` ambiguity policy.

## Known gaps found (not fixed during validation)

1. **Data-driven tests are not attributed** by the Go evidence check
   (`testdata/*.test` read by a `Test…` function elsewhere), which produces
   false UNVERIFIED results. Fix candidate: when changed test files define
   no tests, run the package's tests on the base export and on the change,
   and compare the failing sets.
2. **Contract quality:**
   - 2 of 6 derivations returned nothing required;
   - 1 false material ambiguity in this run;
   - the development axios run named the wrong alternatives.

   Under `ask`, a false positive costs a needless question.
3. The development caddy run showed that a test which does not compile on
   the base counts as behavioural evidence. This is weaker than a
   behavioural failure; no false pass resulted in this validation.
