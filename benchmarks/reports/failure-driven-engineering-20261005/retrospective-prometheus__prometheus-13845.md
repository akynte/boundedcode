# Retrospective: prometheus__prometheus-13845 (false pass)

Run `t20261004-de5031`. Qwen3.6-35B-A3B via OpenHands, controlled interrupt at 5 min, then resumed. BoundedCode verified it (targeted + full gate) and marked it a merge candidate. Hidden acceptance failed (`TestLabels_DropMetricName`, labels_test.go:464 "Should be true").

Evidence roots: `A=~/.cache/boundedcode/bench-validation/archive/frozen-prometheus__prometheus-13845`, `E=$A/state/data/tasks/t20261004-de5031/runtime/f1f8.../events`, `DB=$A/state/data/state.db`.

**Headline.** The agent fixed a path that was never broken and left the only broken path as it was. Its patch is behaviourally a no-op for the reported bug. I re-ran the issue's own reproduction on the candidate (`TestRangeQuery/drop-metric-name`) and it still prints `{__address__="bar", job="1", job="1"}`, the same output as in the issue. The patch also breaks compilation under both non-default build tags. BoundedCode passed it because every stage was already green on the unmodified base: no stage could tell the buggy code from the fixed code. On top of that, the agent's sandbox could not build Go at all, so the agent never ran a single test.

## 1. Task and hidden requirement

- **The issue.** A range query that drops the metric name returns duplicate series. This happens when the metric has a label lexicographically smaller than `__name__` *and* one larger. The issue says `DropMetricName` "modifies `Labels` slice in-place" (introduced in #13446). The quoted failure output shows the original label set turned into `{__address__, job, job}`.
- **The hidden tests.**
  - `TestLabels_DropMetricName` asserts that the receiver is unchanged after `DropMetricName` on `{__aaa__, __name__, bbb}`, where `__name__` sits in the *middle*.
  - `TestRangeQuery/drop-metric-name` is the issue's own reproduction.
- **Gold fix.** One line in `labels.go`: `append(ls[:i], ls[i+1:]...)` becomes `append(ls[:i:i], ls[i+1:]...)`.
- **Is the missed case derivable from the issue? Yes, completely.**
  - "Label smaller than `__name__`" means `i > 0`, which is the `append` branch.
  - "Modifies in-place" can only describe `append`: `ls[1:]` never writes to the backing array.
  - `job, job` is exactly what you get when the in-place `append` shifts `{__address__, __name__, job}` left by one.
  - The issue title says "non-stringlabels build". The string-backed variants concatenate immutable strings and were never buggy.

## 2. Retrieved context

| Pack | Tokens | Needed code present | Label |
|---|---|---|---|
| Initial (`$E/event-00001`) | 4,115 (code section 3,077); nav 19 calls / 2 errors / 27.5 s | All three `DropMetricName` variants (labels.go, labels_dedupelabels.go, labels_stringlabels.go) with full bodies; callers in engine.go (1495, 1633, 2407, 2529) and functions.go; `TestRangeQuery`; the existing `TestLabels_DropMetricName` lines 458–460 | **COMPLETE + NOISY** |
| Resume (`$E/event-00037`) | 4,548: task, "strategies tried", diff (229), impact (120), code (3,137) | Same code plus the current diff | **COMPLETE + NOISY**; the impact layer **failed silently** |

- **Noise.** About 47% of the code-section characters (4,631 of 9,839) were irrelevant: React `DataTable.test.tsx` / `TargetLabels.test.tsx` / `__testdata__` hits for `__name__` and `__address__`, plus `backfillSample.Labels`, `sdCheckResult.Labels` and `labelAndAnnotation.Labels` with long reference lists. These hits came from identifiers in the request matched across languages. They did not cause the failure.
- **Impact layer failure.** The resume pack's IMPACT section reads `changed_total: 0, seed_symbols: 0, impacted_total: 0`, even though git showed `labels.go` modified in the same pack. `impactSection` (internal/contextplan/plan.go:324) renders `detect_changes` output whenever it is non-empty. The tool's view of the code clearly did not see the working-tree edit, and the zero-content section was injected anyway.
- **Repo vs context.** About 8.7K tokens of packs plus file views (labels.go about 16.7K chars, viewed 3 times; engine.go and engine_test.go ranges), against a repo of about 2.57M tokens (6.6 MB of Go). That is under 1%, and it was sufficient.
- **Failing layer.** Not retrieval. The code that mattered was in the very first message.

## 3. Trajectory

**Attempt 1 (17:16:09 to 17:20:43, 274 s, 17 model calls, 271 s of model time)**

- The first reasoning (event 2), written *before any tool call*, already says "the issue is ... when `i == 0` ... returns `ls[1:]` which shares the underlying array". The agent latched onto the "Make common case fast with no allocations" comment and never reconciled this with the issue's precondition that a smaller label exists.
- It then viewed labels.go, engine.go 1490–1510 / 1625–1660 / 1480–1550, `reuseOrGetFPointSlices`, labels_stringlabels.go, labels_test.go 450–480 and engine_test.go `TestRangeQuery`.
- 17:20:16: it edited labels.go so the `i == 0` branch makes a copy.
- It tried `go test` 3 times. Each failed: `module lookup disabled by GOPROXY=off` / `network is unreachable`.
- The interrupt landed in the middle of the third test attempt.

**Attempt 2 (resume; 17:21:05 to 17:42:00, 1,255 s, 102 calls, 4 condensations)**

- The same OpenHands conversation was resumed (`resumed: true`), and the edit was preserved: it appears in CURRENT CHANGES.
- Roughly 10 minutes went into hunting for a Go module cache: events 40–81, 109–157 and 177–181 were about 45 `find` / `go env` / `GOPROXY=` attempts, including `find /` and `git stash` on a read-only filesystem.
- It re-read labels.go twice more, engine_test.go `TestRangeQuery` twice more, and ran `grep duplicate` 4 times. That is rework, but it was caused by condensation, not by the resume itself.
- Event 158 shows a Go-semantics error that locked in the misdiagnosis: "`append(ls[:i], ls[i+1:]...)` creates a new underlying array ... since `ls[:i]` has capacity `i`". That is false: `ls[:i]` keeps `cap(ls)`.
- **Hallucination** (event 212, 17:39): "Looking at the test output, `TestLabels_DropMetricName` ... passed". No test had executed at that point, and none ever did.
- 17:40: it applied the same copy-on-`i == 0` edit to the stringlabels and dedupelabels variants. In both, `ls.data` is a `string`, so `ls.data = result` (a `[]byte`) does not compile. I confirmed this locally: `go build -tags stringlabels` and `-tags dedupelabels` both fail with "cannot use result (variable of type []byte) as string value".
- Its own `go test` and `go build -tags stringlabels` attempts failed on modules again (events 228–231). It called `finish` anyway, claiming all three variants were fixed.
- **No test was written**, although the agent planned one twice (events 158 and 212).

**Resume quality.** Code state was preserved. Environment knowledge ("Go deps unavailable") was not carried in the pack, so the agent rediscovered it. The pack also listed the interrupted attempt under "STRATEGIES ALREADY TRIED (do not repeat) ... rejected because: interrupted". That labels an unfinished strategy as rejected, which is misleading. It was harmless here.

## 4. Incomplete fix and impact

| Modified symbol | References | Sibling paths | Existing tests | Outcome |
|---|---|---|---|---|
| `labels.go:DropMetricName` (default build) | 22+: engine.go 1495 / 1633 / 2407 / 2529, functions.go ×14, promtool tsdb.go:804 (all listed in the pack) | Return paths `ls[1:]` (changed; was harmless) and `append(ls[:i], ls[i+1:]...)` (**unchanged; the bug**) | `TestLabels_DropMetricName:460` already calls the *middle* case, but only checks the return value, never the receiver | Bug not fixed |
| `labels_stringlabels.go:DropMetricName` (`//go:build stringlabels`) | same callers | string concatenation, immutable | same test, under tag | Unnecessary edit; **compile error under the tag** |
| `labels_dedupelabels.go:DropMetricName` (`//go:build dedupelabels`) | same | same | same | Unnecessary edit; **compile error under the tag** |

Would a generic impact expansion have exposed this?

- **Listing all return paths of the modified function, and which ones the diff touched:** yes, directly. It would show that one of two exit branches was untouched, and that branch is the only one that writes through to shared memory.
- **Listing existing tests that call the function, with the argument shape:** yes. Line 460 is the middle-`__name__` case. A mutation-of-input assertion on that existing call would fail on base and on the candidate.
- **Covering all build-tag variants:** all variants were already in context and the agent edited them. What was missing was compiling them. A tag-aware build would have caught 2 of 3 edits as broken, but not the hidden-test failure.

## 5. Verification

- **Targeted run** (`verification_runs` id 1, 236 s): gofmt, `go build ./...`, vet, and `go test` over 66 impact-selected packages, including `model/labels` and `promql`. This was nearly the whole module, because `labels` is imported everywhere.
- **Full run** (id 2, 201 s): `./...` everywhere. golangci-lint was skipped as not installed.
- Both ran with **default build tags only**, so the two broken files were never compiled.
- The targeted run took *longer* than the full run (cold build cache: go-build 54 s vs 18 s), so impact selection saved no time.
- **Why it passed.** On base, the existing suite already passes, including `TestLabels_DropMetricName`; I confirmed this locally. No existing test asserts either that the receiver is unchanged or the range-query duplicate. The candidate added no test. So "pass" carried zero information about the reported bug.

**False-pass chain.** Issue requires "do not mutate the receiver on the middle-`__name__` path". Verification ran only pre-existing default-tag tests, which are green on base. Hidden test 464 asserts `Equal(original, check)` and fails, and `TestRangeQuery/drop-metric-name` also fails. The missing signal was a fail-on-base / pass-on-candidate regression test, plus build-tag compilation.

## 6. Time (wall 2,012.6 s; `task.completed`)

| Segment | Seconds | Notes |
|---|---|---|
| Setup + pack 1 | 30 | nav 27.5 s (19 LSP calls, 2 errors). Indexing was not recorded in this DB (done earlier). |
| Attempt 1 agent | 274 | Diagnosis and edit done by 17:20:16; the rest was failing `go test`. |
| Interrupt → resume + pack 2 | 22 | nav 10.5 s |
| Attempt 2 agent | 1,255 | ~600 s module-cache hunting; ~290 s in 4 condensations (64–84 s each); 6 empty-response nudges; real edits about 1 min |
| Verification targeted / full | 236 / 201 | 22% of wall time; the targeted run was wasted |
| Model time total | 1,502 | 119 calls: prompt 371 s, decode 1,120 s. 215K uncached + 3.4M cached prompt tokens. |

- **Interruption cost.** About 30 s directly. Indirectly, the attempt-1 discovery that tests could not run was rediscovered, about 10 minutes.

## 7. Classification

- **Primary: VERIFICATION_GAP.**
  - The pipeline accepted a bug fix with no discriminating check. Every stage passes on base, and the agent's patch does not change the outcome of the issue's reproduction.
  - Build-constrained files that the diff touched were never compiled under their tags.
  - Evidence: both `verification_runs` passed, and local re-runs of the hidden tests on the candidate fail.
- **Secondary: MODEL_REASONING.**
  - The misdiagnosis contradicts explicit issue facts (§1).
  - The agent had a false belief about Go `append` aliasing (event 158) and hallucinated a passing test (event 212).
  - Evidence: event 2 reasoning, before any tool use.
- **Contributing factors:**
  - **OTHER: agent and verification environments differ.** Verification mounts the host `GOMODCACHE` with `GOPROXY=off` (internal/verify/verify.go:528). The agent runtime only mounts in-repo `DependencyMounts` (internal/agent/agent.go:43, openhands/runtime.go:112), so the agent could not build or test anything. That removed the feedback loop that would have exposed the wrong fix, and it cost about 10 minutes.
  - **IMPACT_ANALYSIS_MISS.** The resume impact section was empty or zero despite a diff, and no sibling return paths or existing call-site tests were surfaced.
  - **RUNTIME_OVERHEAD.** 4 condensations (about 5 min) and a slower-than-full targeted verification.
- **Not causal: retrieval.** The context was COMPLETE, and the noise was about 47% of the code section. RETRY_POLICY and FRONTIER_* were not involved: there were no escalations.

## 8. Generic product improvements (ranked)

1. **Fail-to-pass gate for bug-fix tasks.** Require at least one new or changed test that fails on the base commit and passes on the candidate; run it against base in the sandbox. Without one, verification should report "unverified fix" (not a merge candidate) and send the agent back with a "write a reproduction from the issue" instruction. This would have blocked this false pass on any repo.
2. **Same environment for agent and verification.** Mount the same toolchain caches and env that verification uses (Go mod cache, cargo/pip/maven/npm caches) read-only into the agent sandbox. At session start, preflight that the verification build/test command works for one package, and put that exact command in the prompt. Detect repeated dependency-resolution failures and stop the loop by injecting the fix. Also flag finish messages that claim test results when no test command succeeded.
3. **Build-constraint-aware verification.** For each changed file, read its build constraints (Go `//go:build`, Rust `cfg` features, platform-suffixed files) and add a compile/vet (optionally test) run for the affected packages under each needed tag set. Optionally, derive tag sets from the repo's CI config.
4. **Function-level impact checklist in resume and finish packs.** For each modified function, list (a) every return or exit path and whether the diff touched it, (b) existing tests that call it, with file:line, (c) its sibling variants. Separately, fix `impactSection`: when git shows changes but impact reports `changed_total: 0`, fall back to a git-diff plus reference lookup, and never render an empty or all-zero section.
5. **Issue-precondition mapping.** A planner step that extracts explicit trigger conditions from the request and requires the agent to name the code path each one selects before `finish`.
6. **Resume ledger hygiene.** Carry learned environment facts across resumes (for example "go test unavailable: <error>"), and do not list an interrupted in-progress attempt as a rejected strategy.
7. **Skip targeted verification when impact covers most packages.** When the impact selection is close to `./...`, go straight to the full gate. Here that would have saved about 4 minutes.
