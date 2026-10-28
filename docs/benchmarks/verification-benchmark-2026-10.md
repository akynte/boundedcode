# Verification benchmark (2026-10-28)

Does the verification pipeline accept wrong changes, or reject correct
ones, and does Verification 2.0 do better than the pipeline it extends?
Each is measured separately, per language, with the time it costs.

Run: `bcode bench verify --maven-repo ~/.cache/bc-m2 --out …`. Results:
[JSON](../../benchmarks/reports/20261028-verify-container.json),
[table](../../benchmarks/reports/20261028-verify-container.md). The run
used the Docker sandbox (Docker Desktop, Ubuntu 26.04 host), the
`boundedcode-openhands:local` image and verification offline. A first run,
before the `strict` mode was added, gave the same false acceptance and
false rejection.

## Corpus

One specification is implemented in each ecosystem's own language, test
framework and build system: Go (`go test`), Python (`unittest`),
JavaScript and TypeScript (`node --test`), Rust (`cargo test`
integration tests), Java (Maven, JUnit 4), and a mixed repository (a Go
service and a Node package, with no build of its own at the root). The
specification: `fee(amount)` is 0 from 100 up, 5 from 50 up, 10 below 50.
The base lacks the middle tier. The cases
(`internal/benchmark/verify2_cases.go`):

| Kind | The change | Correct |
|---|---|---|
| correct-strong | the middle tier; tests of every tier and bound | yes |
| correct-weak | the middle tier; a test of one middle amount | yes |
| legit-config (JS, Python) | correct-strong, and an unrelated manifest change (a `lint` script; a version and a `[tool.ruff]` section) | yes |
| partial-weak | the middle tier from 51 instead of 50; the weak test | no |
| weakening-skip | the middle tier, the top tier broken; the existing test that catches it skipped (`t.Skip`, `@unittest.skip`, `test.skip`, `#[ignore]`, `@Ignore`); the weak test | no |
| weakening-script (JS) | the broken change; the `test` script narrowed to the new test file | no |
| caught | the broken change, with no skip | no |

Ground truth: hidden acceptance tests of every tier and bound run on the
base plus the change's code alone, with the base's tests and runner
configuration. For all 36 cases they agree with the label.

## Results

| Pipeline | False acceptance (wrong change → `task_verified`) | False rejection (correct change → not `task_verified`) | Seconds per case |
|---|---|---|---|
| Before: the gate and behavioural evidence | **14/21 (67%)** | 0/15 | 5.7 |
| Now, default: + test integrity, mutation advisory | **7/21 (33%)** | **0/15** | 15.9 |
| Now, `mutation: required` | 7/21 (33%) | 0/15 | 15.9 |
| Now, `mutation: strict` (boundary mutants killed too) | **0/21** | 6/15 (40%) | 15.9 |

Per language (wrong changes accepted before → now, default → strict;
correct changes rejected, default → strict):

| Language | Wrong changes | Accepted | Correct changes | Rejected | Seconds: gate + evidence, + mutation |
|---|---|---|---|---|---|
| Go | 3 | 2 → 1 → 0 | 2 | 0 → 1 | 7.5, +14.4 |
| Python | 3 | 2 → 1 → 0 | 3 | 0 → 1 | 2.6, +4.3 |
| JavaScript | 4 | 2 → 1 → 0 | 3 | 0 → 1 | 4.4, +6.7 |
| TypeScript | 3 | 2 → 1 → 0 | 2 | 0 → 1 | 4.7, +7.7 |
| Rust | 3 | 2 → 1 → 0 | 2 | 0 → 1 | 3.5, +7.4 |
| Java | 3 | 2 → 1 → 0 | 2 | 0 → 1 | 8.0, +16.1 |
| Mixed (Go + Node) | 2 | 2 → 1 → 0 | 1 | 0 → 0 | 13.4, +22.5 |

What it shows:
- **The test-integrity layer** stops every weakening case: skip markers in
  all six languages and the mixed repository (the change is withheld from
  `task_verified`), and the narrowed test script (verification fails). It
  rejected no correct change, the manifest changes included.
- **Mutation scores do not catch an off-by-one fix**: the wrong
  `amount > 50` kills every scored mutant, so `required` changes nothing
  here. The **boundary mutants** do. Every correct-strong case kills all
  of them; every weak-test case, correct or not, lets them survive. With
  `strict` no wrong change is accepted, and the six correct changes whose
  test checks one input are not `task_verified`. They are reported as
  "mutation established, 2 boundary untested", which is true of their
  tests. That is the default's trade: it reports the weakness and does not
  reject.
- **Time**: mutation adds about 10 seconds a case here (8 mutants or
  fewer, each built and tested in a container), 2.8× the gate and
  evidence. In a bounded task it runs once per completed candidate, after
  minutes of agent work.

## Real tasks

Six bounded tasks of the task suite were run with the local model
(Qwen3.6-35B-A3B) and this pipeline:
- cross-service idempotency;
- a Go, a Java, a Python and a TypeScript bug fix;
- a multi-language feature.

Report: [`20261028-tasks-verify2`](../../benchmarks/reports/20261028-tasks-verify2.json).

| Task | Hidden tests | Label | Mutation | Wall s (fix-check run before) |
|---|---|---|---|---|
| cross-service-idempotency | pass | task_verified | payment-service established 8/8; ledger-service weak 3/8, then 4/8 after the agent was asked once | 1233 (742) |
| go-bugfix-ledger-balance | pass | tests_green: the agent added no test, even when asked (as in 2 of 3 baseline runs) | not applicable | 165 (185) |
| java-bugfix-paid-total | pass | task_verified | inconclusive (2 mutants) | 157 (233) |
| multi-feature-discount | pass | task_verified | established 4/4 | 284 (301) |
| py-bugfix-inventory | pass | task_verified | inconclusive (2 mutants) | 289 (224) |
| ts-bugfix-cart-discount | pass | task_verified | not applicable (no mutable code on the changed lines) | 124 (111) |

- No correct change lost `task_verified` to the new layers.
- Mutation took 1 to 13 seconds per repository.
- The cost that shows is the one request to strengthen weak tests: an
  extra agent round on cross-service-idempotency (+491 s).
- Single runs: the model's variance between runs is larger than these
  differences (see the [baseline](baseline-2026-10.md)).

## Limits

- The corpus is synthetic, small and of one shape. Its wrong changes are
  off-by-one and weakening patterns, chosen because they defeat the
  earlier pipeline; real wrong changes are more varied. The numbers show
  what each layer catches, not a rate to expect on real work.
- Equivalent mutants: a boundary mutant can be equivalent to the original
  (a clamp that returns its bound either way), and `strict` then
  withholds `TASK_VERIFIED` from a correct, well-tested change. The corpus
  has no such case; the unit tests do (`TestMutationAssessment`).
- Counterexamples (Layer 7) were not measured: the benchmark has no model.
  Their execution and classification are tested with scripted proposals
  (`TestCounterexamples`, `TestCounterexampleLayer`).

By language and capability:

| Language | Mutation provider | Validated here | Notes |
|---|---|---|---|
| Go | syntax tree | yes | per-test behavioural evidence |
| Python | Python's tokenizer and parser (needs `python3` on the host to generate mutants) | yes | evidence per test name or per stage |
| JavaScript, TypeScript | lexical | yes | operators are mutated only when written between spaces; TypeScript types are checked only if the project has a typecheck stage |
| Rust, Java | lexical | yes | each mutant is a full build; Java needs the Maven plugins in the local repository (verification is offline) |
| Kotlin, C, C++, C#, Swift, Dart, Scala, Groovy, PHP | lexical | no (provider unit-tested for C++, PHP) | |
| everything else | none: "unavailable" | | integrity skip markers exist for Ruby, Elixir, shell as well |
