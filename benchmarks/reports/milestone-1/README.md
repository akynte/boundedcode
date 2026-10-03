# Milestone 1: first end-to-end run on the local model

Date: 2026-10-03. Script: [`scripts/milestone-e2e.sh`](../../../scripts/milestone-e2e.sh).
Task `t20261003-96a305`. Raw files in this directory: `run.log`,
`audit-events.txt` (the task's audit log) and `diff.patch` (the agent's
changes).

## Setup

* Model: Qwen3.6-35B-A3B UD-Q4_K_M on llama.cpp v0.5.0. Profile: 128K ctx,
  ubatch 2048, `n_cpu_moe` 35 (measured in Phase 1).
* Agent: OpenHands SDK 1.51.0 through the adapter, in the Docker sandbox
  (`--network none`, read-only git dirs).
* Workspace: the `payment-platform` fixture (payment-service,
  ledger-service, shared-protos), indexed by codebase-memory-mcp.
* Frontier: **disabled**. Z1 and Z3 fired and were recorded as declined.

## Checklist (product-spec §6)

| # | Requirement | Result |
|---|---|---|
| 1 | Initialize a multi-repo workspace | ✅ 3 repos |
| 2 | Index it | ✅ |
| 3 | Non-trivial task | ✅ cross-service idempotency (HTTP `Idempotency-Key` + idempotent event consumer) |
| 4 | OpenHands runs against local Qwen | ✅ 33 model calls, all local |
| 5 | Repository intelligence provides targeted context | ✅ the initial pack included graph snippets (551 tokens) |
| 6 | Changes in an isolated worktree | ✅ `agent/t20261003-96a305` in both repos. `main` is untouched. |
| 7 | Verification finds problems and feeds them back | ✅ attempt 2 failed `gofmt` in ledger-service; the retry pack carried the failure and attempt 3 fixed it |
| 8 | Survives an agent/runtime restart | ✅ the process was killed (`SIGINT`) after the first agent turn. `task resume` reopened the **same** OpenHands conversation (`d0602af7…`) with a resume pack rebuilt from the ledger, git and verification. |
| 9 | Survives a condensation/resume cycle | ✅ 2 condensations (1 forced via `--condense-each-retry`) |
| 10 | Final changes pass deterministic verification | ✅ targeted + full gate in both services |
| 11 | Completed without frontier usage | ✅ |
| 12 | Separate hard task with controlled frontier escalation | ⏳ separate run (see the Phase 7 report) |
| 13 | Everything visible in the audit log | ✅ `audit-events.txt`: sessions, packs, turns, verification, condensation, triggers, completion |
| 14 | No secrets exposed | ✅ sandboxed; gitleaks clean on the diff and on this report |
| 15 | License compliance | ✅ `make licenses` |

## Measurements

| Metric | Value |
|---|---|
| Wall-clock (both runs, including interrupt) | 642 s |
| Attempts | 3 (1 interrupted, 1 gofmt failure, 1 pass) |
| Model calls | 33 (1 cut off by the deliberate interrupt) |
| Prompt tokens | 653,592, of which **561,625 (86%) served from llama.cpp's prompt cache** |
| Generated tokens | 14,599 at 36.3 t/s average decode |
| Largest single prompt | 34,760 tokens |
| Frontier escalations | 0 (Z1 and Z3 suggested, declined because frontier is disabled) |

## Quality finding (honest result)

The task passed the hidden acceptance tests **as originally written**
(sequential duplicate requests). Code review of `diff.patch` then found a
**check-then-act race** in payment-service. The idempotency lookup and the
insert use separate lock acquisitions, so concurrent retries with the same
key can create two payments and two events. A stricter hidden test with 32
concurrent same-key requests, run under `-race`, reproduces it
(`ids=2 events=2`). The benchmark task now includes that test.

Under the stricter criteria this run counts as a **failure**. It is exactly
the class of defect that the Z3 pre-merge review is meant to catch. Phase 7
runs the same task with frontier enabled to measure whether escalation
changes the outcome.

## Bugs found in boundedcode by this run (fixed)

* The task ledger recorded `local tokens 0` because the runtime used a
  different gateway instance from the runner's metering one. Calls were
  still logged in `model_calls`. Fixed: the runner passes its gateway in
  `OpenRequest`, and a regression test covers it.
* Interrupted attempts were left as `active` strategies, and successful
  attempts kept a placeholder summary. Fixed and tested.
