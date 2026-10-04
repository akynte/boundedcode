# Milestone 1 re-run after the 2026-10-04 audit

Date: 2026-10-04. Script: [`scripts/milestone-e2e.sh`](../../../scripts/milestone-e2e.sh)
with `KILL_SIGNAL=KILL SERENA=on`. Task `t20261004-ed6daf`. Raw files:
`run.log` (script output), `run1.log` (the killed run), `audit-events.txt`
(the task's audit log), `diff.patch` (the agent's changes).

This re-run differs from [milestone 1](../milestone-1/README.md) in three
ways. The interruption is a **SIGKILL** (no signal handler, no cleanup)
instead of SIGINT. Serena v1.7.0 is enabled. And it runs on the post-audit
code: run leases, base-commit verification config, per-worktree impact
indexing and adapter protocol v1.

## Setup

* Model: Qwen3.6-35B-A3B UD-Q4_K_M on llama.cpp v0.5.0 (managed, same
  profile as milestone 1).
* Agent: OpenHands SDK 1.51.0 through the adapter in the Docker sandbox
  (`--network none`, read-only git dirs).
* Workspace: the `payment-platform` fixture (payment-service,
  ledger-service, shared-protos), indexed by codebase-memory-mcp 0.11.0.
* Serena 1.7.0 (pinned install, version gate applied).
* Frontier: **disabled**. Z1, Z2 and Z3 fired and were recorded as declined.

## Checklist (product-spec §6)

| # | Requirement | Result |
|---|---|---|
| 1 | Initialize a multi-repo workspace | ✅ 3 repos |
| 2 | Index it | ✅ graph and contracts (4 cross-service links) |
| 3 | Non-trivial task | ✅ cross-service idempotency |
| 4 | OpenHands runs against local Qwen | ✅ 37 model calls, all local, 0 failed |
| 5 | Repository intelligence supplies targeted context | ✅ Serena answered 3 symbols per pack (7 calls, 0 errors); graph breadth (4 calls); impact from per-worktree indexes (~1.5 s per repo) |
| 6 | Changes in an isolated worktree | ✅ `agent/t20261004-ed6daf` in both repos |
| 7 | Verification finds problems and feeds them back | ✅ attempt 2 failed `gofmt` in payment-service; attempt 3 fixed it |
| 8 | Survives a runtime restart | ✅ the CLI was **SIGKILLed** during verification of attempt 1. The resume took over the stale lease, recorded `task.recovered` (1 orphaned attempt rejected), and reopened the **same** OpenHands conversation (`416f7e55…`) |
| 9 | Survives a condensation/resume cycle | ✅ 2 condensations (1 forced), 1 session resume |
| 10 | Final changes pass deterministic verification | ✅ targeted + full gate in both services |
| 11 | Completed without frontier usage | ✅ `boundedcode stats`: 1/1 local-only |
| 13 | Everything visible in the audit log | ✅ `audit-events.txt` |
| 14 | No secrets exposed | ✅ sandboxed; gitleaks clean on this report |
| — | Hidden acceptance tests | ✅ PASS |
| — | Leftover processes after the run | none: no `bc-*` containers, Serena, language server, codebase-memory or adapter processes |

Item 12 (frontier escalation) is not part of this run; see
[phase7-frontier](../phase7-frontier/README.md) and the live contained-Codex
check in the 2026-10-04 audit.

## Measurements

| Metric | Value |
|---|---|
| Wall-clock recorded by the ledger (excluding the 130 s lease wait) | 740 s |
| Attempts | 3 (1 killed, 1 gofmt failure, 1 pass) |
| Model calls | 37 |
| Prompt tokens | 859,912, of which 766,711 (89 %) served from llama.cpp's prompt cache |
| Generated tokens | 16,348 at 36.2 t/s average decode |
| Largest single prompt | 39,530 tokens |
| Context packs | 1,550 (initial), 5,488 (resume), 5,682 (retry) tokens |
| Frontier escalations | 0 sent, 3 declined (frontier disabled) |

## Notes

* The machine was otherwise idle during the run. The numbers are a single
  run, not a benchmark.
* The script's check that a resume is refused while the killed run's lease
  is still fresh did not print, because of a `pipefail` bug in the script
  (fixed afterwards). The refusal itself is covered by `TestSIGKILLResume`.

## Live failure injection (same day, same workspace)

Two small ledger-service tasks on the real model, each with one component
killed during the agent's first turn:

| Injected failure | What happened | Outcome |
|---|---|---|
| `kill -9` of the managed `llama-server` (task `t20261004-e050ff`) | 8 s later the turn ended with an error. The gateway had recorded 2 `transport_error` calls; the runner restarted the server (`EnsureModel`) and retried. The failed turn was **not** counted as an attempt or toward Z2 (`infra.failure` event). | completed, 1 attempt, 2 min 6 s, full gate passed |
| `docker kill` of the adapter container `bc-<task>` (task `t20261004-7a233d`) | `adapter process exited (exit status 137)`; the runner reopened the session from OpenHands persistence with a resume pack. The crashed turn counts as an attempt (an agent can crash its own container). | completed, 2 attempts, 2 min 0 s, full gate passed |

No `bc-*` containers or adapter processes remained afterwards.
