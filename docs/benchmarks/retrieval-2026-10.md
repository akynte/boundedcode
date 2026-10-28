# Retrieval benchmark (2026-10-28)

Does adaptive retrieval ([reference](../usage/retrieval.md)) put the code
a task needs in front of the model more often than the retrieval it
replaces, and at what cost in context and time? Retrieval is measured
apart from patch correctness: on real issues, against the files their
fixes changed, with no model involved. Its effect on the agent is
measured separately, with the local model.

The approaches compared:
- **basic:** the direct-mode seed pack before this change, a search of
  the project for the names the request mentions (`direct.retrieval:
  basic`).
- **adaptive:** strategies selected from the request and the evidence,
  ranked by reliability (`direct.retrieval: adaptive`, the default).

## Summary

- **Held out (177 issues, 36 repositories, 10 languages):** recall of
  the files a fix changed went from 23% to 51%. Precision went from 9% to
  19%, and a changed file came first 35% of the time instead of 12%.
  Adaptive was better on 58 issues and worse on 1 (sign test p < 0.001).
  These are the shipped build's figures; the frozen build's were 50%,
  21%, 34%, and 57 better and 1 worse.
- **Every workload gained:** recall rose in every request category, every
  language, and in single-language and multi-language repositories alike.
  No repository lost recall; four tied.
- **Cost:** 188 tokens per block instead of 123. Retrieval took 32 ms at
  the median and 0.17 s at the 95th percentile (basic: 0.27 s and its
  3 s limit).
- **With the agent, direct-mode suite:** adaptive passed 44 of 46 runs of
  the 23 runnable tasks, basic 41 of 46 on the same build and 87 of 92
  over four runs. Adaptive used 18% fewer prompt tokens (36k against 44k
  per task) and 5.4 tool calls instead of 6.6. Pass rates are within the
  model's variance.
- **An earlier build lost in that suite** (38 of 46). Three retrieval
  defects behind it were found and fixed; see
  [the regression and its fixes](#the-regression-and-its-fixes).
- **With the agent, held-out locate questions:** adaptive passed 41 of 64
  runs over three builds, basic 20 of 32. No measurable difference.
- **Weak spots:**
  - Rust (24% recall);
  - issues that describe behaviour without naming code (31%);
  - TypeScript fell from 44% to 35% recall with the fixes (one issue in
    twelve);
  - symbol precision: adaptive names more right symbols, but a larger
    share of its symbols are wrong.

## Method

**Data.** Two public datasets of real issues with their fixes, pinned by
revision and sha256 in
[`benchmarks/retrieval/manifest.json`](../../benchmarks/retrieval/manifest.json):
- SWE-bench Multilingual, for C, C++, Go, Java, JavaScript, TypeScript,
  PHP, Ruby and Rust;
- SWE-bench Lite, for Python.

Both are MIT-licensed. [`prepare.py`](../../benchmarks/retrieval/prepare.py)
selects the instances and checks out each base commit.

**Split.** The split was fixed before any retrieval ran on the instances:
- Per language, repositories are ordered by `sha256(seed + repo)`. The
  first third make the development split; the rest are held out.
- Per repository, at most 6 instances are taken, ordered by
  `sha256(seed + instance_id)`.
- The result is 90 development instances (17 repositories) and 178 held
  out (36 repositories).
- One held-out instance was skipped: its fix changes no existing code
  file.

Whole repositories are held out, so no tuning saw their code, layout or
naming.

**Gold.**
- The existing files the dataset's fix changes: code in any language,
  infrastructure and API definitions. The datasets keep test changes in
  a separate patch, so tests are never gold.
- The symbols are the definitions that enclose the changed lines.

**Requests.**
- The issue text, as a user would paste it. Issues are categorised by what
  they contain:

  | Category | Contains | Held out |
  |---|---|---|
  | `trace` | a stack trace | 21 |
  | `located-error` | a compiler or runner location | 15 |
  | `names` | code names, paths or quoted messages | 113 |
  | `prose` | none of these | 28 |

- The `+tests` arms add the dataset's failing tests as evidence, in the
  form `FAILED <test id>`, as after a failed test run.

**Measures.** Per instance, then averaged:
- **File recall:** the share of gold files listed.
- **Precision:** the share of listed files that are gold.
- **Gold first / in top 3:** whether a gold file is listed first, or in
  the first three.
- **Symbols right / listed:** listed symbols that are gold symbols.
- **Tokens:** the size of the block. The budget was 1,200 tokens, as in
  direct mode.
- **Latency:** wall time for one retrieval, on this laptop (8 cores),
  with a warm page cache.

**Development and freeze.**
- Strategies, weights and the dropped variants were chosen on the
  development split alone.
- The held-out split ran once, on a frozen build. Its retrieval sources,
  `internal/retrieve` and the language patterns in `internal/lang`, hash
  to `45cbc09a…8da6` (sha256).
- Two defects found afterwards by code review were then fixed:
  - a qualified name such as `ledger.Post` was taken for a file with the
    extension `.Post`;
  - the benchmark's reading of the basic block lost symbols whose
    definition text has parentheses.
- Both splits were re-run with the final build.
- The direct-mode suite then showed a regression. Its three causes were
  fixed, guided by that suite and the development split, and both splits
  were re-run once more with this **shipped build**. No held-out instance
  was examined between the two runs.
- The figures below are the frozen run's, with the later builds' beside
  them.

Reports: development
([frozen](../../benchmarks/reports/20261028-retrieval-dev.md),
[final](../../benchmarks/reports/20261028-retrieval-dev-final.md),
[shipped](../../benchmarks/reports/20261028-retrieval-dev-fix2.md),
[ablations](../../benchmarks/reports/20261028-retrieval-dev-ablations.md));
held out ([frozen](../../benchmarks/reports/20261028-retrieval-heldout.md),
[final](../../benchmarks/reports/20261028-retrieval-heldout-final.md),
[shipped](../../benchmarks/reports/20261028-retrieval-heldout-fix2.md)).
Each has a JSON file alongside with every instance's list.

## Held-out results

| Arm | File recall | File precision | Gold first | Gold in top 3 | Nothing found | Symbols right / listed | Tokens | p50 ms | p95 ms |
|---|---|---|---|---|---|---|---|---|---|
| basic | 23% | 9% | 12% | 19% | 30% | 12 / 95 | 123 | 273 | 3001 |
| adaptive | **50%** | **21%** | **34%** | **47%** | 0% | 30 / 429 | 186 | 32 | 167 |
| basic+tests | 22% | 8% | 12% | 18% | 18% | 12 / 125 | 133 | 335 | 3001 |
| adaptive+tests | **54%** | **19%** | **38%** | **51%** | 0% | 34 / 488 | 195 | 38 | 169 |
| *final build:* adaptive | 51% | 20% | 34% | 48% | 0% | 30 / 444 | 186 | 32 | 172 |
| *final build:* adaptive+tests | 55% | 19% | 37% | 51% | 0% | 34 / 502 | 196 | 38 | 162 |
| *shipped build:* adaptive | 51% | 19% | 35% | 49% | 0% | 29 / 438 | 188 | 32 | 170 |
| *shipped build:* adaptive+tests | 55% | 18% | 38% | 52% | 0% | 33 / 494 | 199 | 38 | 167 |

Paired per instance (two-sided sign test, ties dropped):

| Comparison | Recall better / worse | Precision better / worse |
|---|---|---|
| adaptive vs basic | 57 / 1 (final: 58 / 1; shipped: 58 / 1) | 79 / 13 (final: 80 / 13; shipped: 76 / 13) |
| adaptive+tests vs basic+tests | 64 / 1 (final: 65 / 1; shipped: 66 / 1) | 82 / 13 (final: 83 / 13; shipped: 82 / 13) |

Every p is below 0.001.

**By workload** (basic → adaptive, shipped build; the final build's
adaptive figure in brackets where it differs):

| Workload | n | Recall | Precision |
|---|---|---|---|
| trace | 21 | 29% → 62% | 8% → 16% (18%) |
| located-error | 15 | 7% → 33% | 1% → 13% (17%) |
| names | 115 | 29% → 55% | 12% → 23% |
| prose | 26 | 0% → 31% | 0% → 8% |
| C | 17 | 29% → 53% (54%) | 12% → 25% |
| C++ | 1 | 0% → 100% | 0% → 20% (22%) |
| Go | 17 | 35% → 50% | 15% → 29% |
| Java | 19 | 32% → 63% | 7% → 17% (22%) |
| JavaScript | 9 | 22% → 43% (46%) | 14% → 23% (31%) |
| PHP | 18 | 39% → 57% (51%) | 16% → 33% (27%) |
| Python | 44 | 18% → 57% | 6% → 18% (21%) |
| Ruby | 16 | 25% → 64% | 7% → 15% (20%) |
| Rust | 24 | 6% → 24% | 6% → 6% |
| TypeScript | 12 | 8% → 35% (44%) | 1% → 9% (10%) |
| single-language repositories | 135 | 26% → 52% | 9% → 20% (22%) |
| multi-language repositories | 42 | 14% → 46% (48%) | 6% → 16% |

The category counts differ from the final build's (names 113, prose 28)
because a member written in prose, such as `Immutable.Range`, now counts
as a code name.

Multi-language means that at least two programming languages each make
up 10% or more of the checkout's code files: for example, C with Python
tests, or TypeScript with Rust bindings.

**Per repository** (shipped build), no held-out repository lost recall.
Four tied:
- `astral-sh/ruff` 0% both;
- `fastlane/fastlane` 50%;
- `gohugoio/hugo` 40%;
- `micropython/micropython` 40%.

With the final build, `laravel/framework` (33%) and `sharkdp/bat` (0% →
3%) also tied. Precision fell in two: `fastlane/fastlane` (17% → 9%) and
`uutils/coreutils` (30% → 21%). With 2 to 6 instances per repository,
these are not separate workloads one could route on.

**Checks** (frozen build):
- **The basic arm's time limit.** Basic hit its 3 s limit on 12
  held-out instances, all in large repositories. On the other 165,
  recall was 22% → 50%, so the limit does not explain the gap.
- **Instances seen before.** Eleven held-out instances appeared in
  earlier BoundedCode evaluations (the comparative evaluation and the
  validations). Retrieval was not tuned on them, but their issues were
  read during earlier work. Without them (166 instances): recall 22% →
  50%, precision 8% → 20%, better on 55 and worse on 1.
- **Development split** (90 instances, frozen build): recall 28% → 51%,
  precision 12% → 19%, gold first 20% → 38%. The final build gives the
  same figures; the shipped build 52%, 18% and 39%.

## Workload decisions

The phase's rule: where adaptive selection does not help a workload, keep
the simpler strategy for it.

| Workload or component | Measured | Decision |
|---|---|---|
| Direct mode, every request category and language | better on recall and precision everywhere except Rust precision (6% → 6%, recall 6% → 24%) | adaptive is the default; `direct.retrieval: basic` keeps the earlier search |
| Direct mode with the agent: the suite's edits, explanations, diagnoses and sessions | shipped build: 44 of 46 against basic's 41 of 46 (87 of 92 over four runs), with 18% fewer prompt tokens and fewer tool calls | adaptive |
| Direct mode with the agent: "which files need to change" on held-out issues | 41 of 64 against 20 of 32; tokens and tool calls about the same | adaptive, as it does not lose here and these are the same requests as the rows above; no gain is claimed |
| Questions and configuration requests: documentation found by words | dev ablation: no recall gained, precision lost | dropped; `docs_config` only follows environment variables named in the prose |
| Path-word discovery on every request | dev: +1 point of recall for about 60 more tokens | runs only as the fallback, when nothing reliable was found |
| Bounded mode, retry packs | the request's names already go through Serena and the code graph (ADR-0008), which this benchmark does not exercise | kept; adaptive retrieval only locates the failure output (frames, diagnostics, failing tests), which was recognised for six file extensions before |
| `graph` (refactor requests) | one development instance; not measurable | runs only for refactor and rename requests; reported as unmeasured |

**Ablations** (development split; each strategy removed in turn; file
recall and gold first, change from adaptive):

| Strategy removed | Recall | Gold first | Note |
|---|---|---|---|
| `symbols` | −9 | −8 | precision +2: definitions add recall at some precision |
| `discovery` | −6 | −3 | 12% of instances then find nothing |
| `test_to_impl` (with tests) | −5 | −8 | |
| `stack_trace` | −3 | −4 | trace instances: 92% → 67% |
| `lexical` | −3 | −1 | |
| `paths` | −1 | −4 | |
| `contracts` | −1 | 0 | |
| `modules` | 0 | −3 | |
| `docs_config`, `graph`, `build_error` | 0 | 0 | the 8 development issues with a located error reached the same gold files through other strategies; held out, located-error recall is 7% → 33% |

## Agent-level results

Retrieval quality matters only if it helps the agent. Two suites were run
with the local model (Qwen3.6-35B-A3B, llama.cpp, this laptop), for each
approach.

**Held-out locate tasks.** These are 16 read-only tasks from held-out
issues whose fix changes one or two code files: 1 or 2 per language,
picked by the seeded order before any agent run, made by
[`agent_tasks.py`](../../benchmarks/retrieval/agent_tasks.py). Each
asks which files must change to fix the issue. It passes when the answer
names a file the fix changed. Basic ran twice. Adaptive ran twice on the
frozen build, once on the final build and once on the shipped build. The
basic arm's code is the same in every build.

| Approach | Build | Passed | Tool calls | Prompt tokens | Redundant reads | Seconds, mean / median |
|---|---|---|---|---|---|---|
| basic | (any) | 20 / 32 | 10.4 | 106k | 0.56 | 270 / 114 |
| adaptive | frozen | 23 / 32 | 11.3 | 116k | 1.06 | 233 / 144 |
| adaptive | final | 9 / 16 | 10.2 | 95k | 0.88 | 227 / 207 |
| adaptive | shipped | 9 / 16 | 9.6 | 109k | 0.19 | 293 / 218 |

Per task (basic, then adaptive over the four runs):
- **Both always pass:** six tasks (astropy, caddy-6370, faker, gson,
  laravel-48636, sphinx).
- **Both always fail:** ripgrep-2209 and redis-12472.
- **Adaptive better:** axios-5085 (0/2 → 3/4), immutable-js-2006 (0/2 →
  2/4), nushell-12950 (1/2 → 4/4).
- **Adaptive worse:** lombok-3371 (1/2 → 0/4), jq-2839 (2/2 → 2/4),
  caddy-5404 (2/2 → 3/4).
- **Mixed:** fastlane-20958 (1/2 → 2/4), laravel-52451 (1/2 → 1/4; one
  adaptive failure was a tool call the model server could not parse).

Adaptive passed 64% of its runs and basic 63%. The better retrieval
measured above did not show as more correct answers on these tasks.

**The direct-mode suite** has 25 tasks on the project fixtures: edits,
explanations, diagnoses and sessions. The Java and Rust tasks were
skipped (no toolchain on this machine), leaving 23. Two runs per approach
were made on the final build, and two on the shipped build.

| Approach | Build | Passed | Tool calls | Model calls | Prompt tokens | Seconds, mean / median |
|---|---|---|---|---|---|---|
| basic | final | 46 / 46 | 6.9 | 5.8 | 47.9k | 44 / 23 |
| basic | shipped | 41 / 46 | 6.2 | 5.7 | 41.1k | 39 / 23 |
| adaptive | final | 38 / 46 | 4.7 | 4.5 | 31.9k | 34 / 21 |
| adaptive | shipped | **44 / 46** | 5.4 | 5.2 | 36.4k | 41 / 26 |

Basic's code did not change between the builds, so its 46 and 41 are the
model's variance. Over its four runs basic passed 87 of 92, with 6.6 tool
calls and 44.5k prompt tokens per task.

The shipped build's failures all came from the model's work, not from
retrieval:
- adaptive: `multi-feature-discount` (the discount was not capped at the
  subtotal, as in both basic runs) and `session-x-tools` (a wrong fix of
  `Len`);
- basic: `fix-balance-query` twice, `multi-feature-discount` twice,
  `large-change-txtar` once.

Reports: [basic r1](../../benchmarks/reports/20261028-direct-retrieval-fix2-basic-r1.md),
[r2](../../benchmarks/reports/20261028-direct-retrieval-fix2-basic-r2.md);
[adaptive r1](../../benchmarks/reports/20261028-direct-retrieval-fix2-adaptive-r1.md),
[r2](../../benchmarks/reports/20261028-direct-retrieval-fix2-adaptive-r2.md);
the final build's `20261028-direct-retrieval-{basic,adaptive}-r{1,2}`; the
locate runs' `20261028-agent-retrieval-heldout-*`.

## The regression and its fixes

On the final build, adaptive passed 38 of 46 suite runs and basic 46 of
46. Of adaptive's eight failures:
- three were the model server failing mid-task with HTTP 502. The cause
  was not found; the logs show no memory exhaustion;
- one was the model's reasoning reaching the 8,192-token completion
  limit;
- the rest were traced to the retrieval block. Reading the blocks of the
  failed tasks found three defects.

| Defect | Example | Fix |
|---|---|---|
| A member written in prose was not looked up | "the `Ledger.Balance` query": the definition was not listed | `Type.member` and `module.member` in the prose (not in URLs or code blocks) become names; definitions in a file named after the qualifier come first |
| A path read as an HTTP route | `internal/ledger` gave a route `/ledger`, which pointed at the wrong handler | a route must start a word |
| The relative cut dropped a whole language | in a Go and TypeScript request, `DiscountCents` in the Go API was cut because the TypeScript file scored far higher | up to three named things with nothing above the cut keep their best item |

Unit tests cover each (`TestMembersAndRoutes`, `TestEveryNamedThingKeepsAPlace`,
the "member in prose" case of the retrieval tests).

Re-run on the static benchmark:
- **development split:** recall 51% → 52%, gold first 38% → 39%, symbols
  right 22 → 26;
- **held out:** recall 51% both, precision 20% → 19%, gold first 34% →
  35%. Per issue against the final build, 1 better and 3 worse:
  - `immutable-js-2006` (TypeScript, 100% → 0%): `Immutable.Range`
    became a name to look up. Its definitions were found, so the
    path-word fallback, which had found the changed file, no longer ran;
  - `axios-5316` (60% → 40%) and `jq-2750` (40% → 20%): before, part of
    a GitHub link (`…/blob/<commit>/lib/adapters/http.js`) and the words
    "try/catch" were read as HTTP routes, and the route search found a
    changed file by chance. A link to a file in the repository is not
    yet read as a path;
  - `laravel-48636` (0% → 100%): the issue names `Model::replicate()`,
    whose definition in `Model.php` is now listed.

In the suite, adaptive then passed 44 of 46. The suite guided the fixes,
so this result is not held out; the held-out locate run did not change
(9 of 16 on both builds).

## Limits

- **Gold is the dataset's fix.** Another correct fix could change other
  files. Recall and precision are therefore measured against one valid
  answer, not every valid answer.
- **Synthetic test evidence.** The `+tests` arms present the dataset's
  failing test ids as `FAILED <id>` lines. Real runner output carries more,
  such as assertion messages and frames, so these figures likely
  understate what test evidence gives.
- **Language servers were not exercised.** The benchmark ran without
  Serena: every definition came from patterns. The routing between
  servers and patterns is covered by unit tests (`TestLanguageServerAnswers`,
  `TestCompositeNoText`), not measured.
- **Refactor graph unmeasured.** Only one development instance was a
  refactor.
- **Rust is weakest.** Rust issues often name types re-exported far from
  their definitions (`tokio::sync::broadcast::Sender`), and their fixes
  span many files.
- **Small agent-level sample.** 16 locate tasks with two to four runs
  each, and 23 suite tasks with two runs per build, with one model on one
  machine. Its variance between runs is as large as the differences
  measured: basic passed 46 and then 41 of 46 with the same code.
- **The model server failed three times** (HTTP 502) during one adaptive
  suite run, for a reason not found. Those tasks count as failures.
- **Machine.** Latencies are from one laptop with a warm page cache.
