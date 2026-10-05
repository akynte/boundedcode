# Failure notes

Every failure, with its primary cause (categories from the validation brief).
"Self-verified" means BoundedCode's own verification gate passed while the
dataset's hidden acceptance tests failed (a false pass).

## Original frozen runs (build 0217d96)

| Task | Primary cause | Secondary | Evidence |
|---|---|---|---|
| axios__axios-6539 | **VERIFICATION** (product defect) | TASK_AMBIGUITY, MODEL_REASONING | Task worktrees had no `node_modules`, so every JavaScript stage was skipped and the change was self-verified with no test run. The agent threw an error for protocol-relative URLs (one reading of the issue) and edited an existing spec to expect `isAbsoluteURL('//x') === false` without changing `isAbsoluteURL`, so its own test would have failed. Fixed in bcc17e1 / 7bcc9c6; the post-fix rerun passed. |
| caddyserver__caddy-6288 | MODEL_CODING | — | Self-verified. The lexer change handled the quoting cases in the agent's own `expression_quotes.caddyfiletest` but not the dataset's (`wrong argument count or unexpected line ending after 'expression'`). |
| gin-gonic__gin-3227 | MODEL_REASONING | — | Self-verified after 3 attempts and one Codex call (Z2). The agent fixed redirect handling in `gin.go`; the issue is in `tree.go`'s route lookup (`getValue` must report the trailing-slash hint for parameter routes). It read `tree.go` (15 events), so not a retrieval miss. The Codex advice refined the agent's own wrong-layer approach. Attempt 1 ran 57 minutes to the iteration limit. |
| grpc__grpc-go-2744 | **SANDBOX** (product defect) | FRONTIER (product defect) | Secret masking hid the `credentials/` Go package (the package the issue is about) and diff-scope rejected changes to it; verification could not build. A Z2 escalation was then refused because the packet still contained the home directory, and the refusal left no escalation record. Ended at the 90-minute timeout. Fixed in e86a7aa (narrowed in b50ed59) and b50ed59. |
| prometheus__prometheus-13845 | MODEL_CODING | — | Self-verified, after a controlled interruption and resume (continuity worked). The agent stopped `DropMetricName` from sharing the slice when the name is the first label, but the middle-of-slice path (`append(ls[:i], ls[i+1:]...)`) still mutates the caller's labels: `TestLabels_DropMetricName` (labels_test.go:464). |
| vuejs__core-11899 | **RUNTIME** (product defect) | — | The task never started. Cross-service scanning of `packages/reactivity/src/collectionHandlers.ts` expanded JavaScript constant bindings exponentially; the BoundedCode process reached ~58.9 GiB RSS and was killed (twice; the second time sampled). Fixed in f2b4001. |
| zeromicro__go-zero-2283 | TASK_AMBIGUITY | MODEL_CODING, FRONTIER | The dataset's test requires an unexported type `corsRouter` that only the reference solution defines; the issue does not name it, so no change could pass. Attempt 2 passed full verification; the Z3 high-risk review (Codex) correctly flagged a real security regression (all user middleware moved before JWT auth, a blanket `recover()`); the local model then broke the build and the task blocked after 6 attempts. |
| zeromicro__go-zero-990 | TASK_AMBIGUITY | MODEL_REASONING | The dataset's test requires a new exported function `ReadLink` that the issue (in Chinese) does not mention. Attempt 1 looped for 67 minutes and was stopped as stuck; 90-minute timeout. A first attempt never started (ENVIRONMENT: Docker Desktop could not see the freshly recreated work directory after the operator deleted and recreated its parent while the VM was running); it is recorded separately and not counted. |

## Post-fix reruns (fixes up to ee6f9ca; acceptance re-evaluation with 65f805a)

| Task | Result | Primary cause | Evidence |
|---|---|---|---|
| axios__axios-6539 | **accepted** | — | 1 attempt, local only. Verification ran eslint, 182 mocha tests and the build; the agent ran mocha itself. Found a new defect: the build stage rewrote 12 tracked `dist/` files (fixed in f5ac462; did not affect the result). |
| grpc__grpc-go-2744 | FAILED | TASK_AMBIGUITY | Self-verified in 1 attempt. The masking fix worked (the agent edited `credentials/credentials.go`). The dataset's test calls `appendH2ToNextProtos`, a private helper of the reference solution that the issue does not name, so it does not compile. The agent's change (keep user `NextProtos`, append `h2` if missing) looks behaviourally equivalent on reading; this is not verified and not counted. |
| vuejs__core-11899 | FAILED | MODEL_CODING | Self-verified in 1 attempt; setup now takes seconds with flat memory. The first acceptance run hit a **benchmark-harness** defect (read-only `node_modules` without a writable cache layer; fixed in 65f805a) and is invalid; re-checking the unchanged final worktree: 25 passed, 1 failed, exactly the dataset's fail-to-pass test "SFC scoped CSS > nesting selector with atrule and comment". |

## Patterns

* **Self-verification false passes:** 4 of 8 frozen runs and 2 of 3 post-fix
  runs passed BoundedCode's gate while failing acceptance. The gate checks
  what the repository's tests check; the agent's own new tests did not cover
  the cases the issue required.
* **Dataset coupling:** 3 tasks (go-zero-2283, go-zero-990, grpc-go-2744) have
  acceptance tests that reference identifiers only the reference solution
  introduces. Screening checked that the reference passes and the base fails,
  not that a solution derivable from the issue could pass.
* **Secret masking vs. test fixtures:** TLS key/cert fixtures stay masked by
  design, so tests that read them (gin, caddy, prometheus, grpc-go, axios)
  cannot run in BoundedCode's verification; repository configs skip them.
