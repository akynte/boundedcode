# Phase 7: frontier escalation, live run

Date: 2026-10-03. Task: `cross-service-idempotency` (benchmark suite),
including the stricter hidden check: 32 concurrent same-key requests under
`go test -race -count=3`. Provider: Codex CLI 0.156.1 with ChatGPT Plus
sign-in. No API key was used, and the provider refuses API-key auth.

## Same task, with and without frontier

| | Local only ([milestone 1](../milestone-1/)) | Local + frontier (this run) |
|---|---|---|
| Original hidden checks | pass | pass |
| **Concurrent idempotency check** | **fail** (2 payments / 2 events) | **pass** |
| Attempts | 3 | 3 |
| Wall-clock | 642 s | 1,649 s |
| Frontier messages | 0 | 2 |
| Packets | n/a | Z1 1,139 tokens; Z3 5,463 tokens |

Flow:

1. **Z1** (architectural risk: idempotency/ledger/payments across 2 repos)
   sent the design question before implementation. Codex's advice
   (`001-Z1-response.md`) named the concurrency race explicitly and called
   for an atomic claim plus concurrent tests.
2. The local agent implemented a first version that **still had the race**.
   It passed our verification on attempt 2.
3. **Z3** (pre-merge, high-risk paths) review (`002-Z3-response.md`) said
   "Do not merge yet". It listed the race, a lost key on publish failure, a
   fingerprint collision, and a ledger zero-value panic plus mixed-batch
   drop.
4. The local agent applied the review (attempt 3). Verification passed, and
   the stricter hidden checks passed.

Reading: on this task the decisive contribution was the **pre-merge review
(Z3)**, not the up-front design advice (Z1). The local model did not reliably
turn design advice into a correct implementation, but it did fix concrete
review findings. This is a single task, so it shows the mechanism works.
It is not a statistically meaningful success rate.

## Security finding during this run (fixed)

The Z3 answer cited absolute host paths with line numbers. Two things
caused this:

1. **Packets contained host paths.** codebase-memory-mcp snippets include
   `file_path: /home/<user>/…`.
2. **Codex read files from disk.** `codex exec --sandbox read-only`
   restricts *writes*. Commands run by Codex's own agent can still *read*
   the host filesystem.

Both escalations in this run used the pre-fix code. Only synthetic fixture
code existed in the referenced paths. Fixes (commit after this report):

* Packets rewrite host paths to workspace-relative names, and a packet that
  still contains the home directory is **not sent** (`frontier.CheckPacket`).
* `frontier.contain: true` (default) runs `codex exec` in a container. Only
  an empty workdir and the Codex credential directory are mounted, the
  container drops all capabilities, and network access is kept so it can
  reach OpenAI. Verified: the containerized Codex reports "Logged in using
  ChatGPT", and neither host repositories nor `$HOME` are visible.

The packets and answers in this directory have host paths replaced with
relative ones.

## Token accounting note

The suite report's "local tokens" (2,045,070) summed **all** prompt tokens,
including prefix-cache hits (more than 80% on agent turns). The ledger now
records processed tokens (uncached prompt plus generated), generated
tokens, and cached prompt tokens separately. The budget limits processed
tokens.
