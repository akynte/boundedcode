# Orchestration benchmark (2026-10-29)

What the shared tool architecture of [Tool capabilities](../architecture/capabilities.md)
changed, measured:
- **Calls of one reply run together**, on real tools.
- **MCP tools found on demand**, with the agent's model.
- **Both modes' suites before and after:** tool-call count, tool and task
  latency, resources, correctness.
- **Session determinism and security-policy consistency**, by tests.

Machine and model as in the [baseline](baseline-2026-10.md): an RTX 4060
Laptop (8 GB) with 64 GB RAM, Qwen3.6-35B-A3B (UD-Q4_K_M), llama.cpp
b11146, 128K context, offload threshold 256. "Before" is commit `74e6f0c`
(Phase 07 and the CI fix); "after" is this phase.

## Summary

- **Running calls together** shortens a reply's tool time when its tools
  are slow:
  - four 200 ms fetches: 808 ms → 205 ms;
  - four searches over Go's source tree: 1,384 ms → 502 ms;
  - four symbol lookups: 73 ms → 34 ms.

  Reading local files was already fast: six reads took 9.4 ms one after
  another and 7.2 ms together.
- **In the agent loop it changes nothing measurable.** Tools take about
  0.2 s of a task's 30–60 s (0.5%), and every batch in the suite took
  under a millisecond. The suite passed 25 of 26 tasks in both runs after,
  24 in both runs before.
- **MCP tool search** on a server with 60 tools:
  - 12 of 12 requests answered with the right tool, as with every
    definition listed;
  - the server processed 2,564 prompt tokens per request instead of 8,096
    (−68%), and each request carried 3.7k tokens of tool definitions
    instead of 10.5k;
  - it took 4.5 model calls per request instead of 2.0.
- **Bounded mode** changed only in how the governor classifies actions,
  and no difference showed beyond run-to-run variation. 16 of 19 passed,
  against 18 the day before. The failures:
  - one task did not start (Docker ran out of file handles), and passed
    when rerun;
  - the known false pass;
  - a task that passed once in two reruns on each build.
- **Determinism and consistency** are covered by tests:
  - the session record is the same with calls run together or in turn;
  - a batch's events come in the same order every time;
  - direct mode refuses to read exactly the files bounded mode masks;
  - every tool is described, and no concurrent tool changes anything.

## Calls run together

`go test ./internal/direct/ -run '^$' -bench ReplyCalls -benchtime 5x -count 3`
runs one model reply's calls with `direct.parallel_tools` 1 and 4: reads
of `net/http`, searches and symbol lookups over Go's standard library
source (the text navigator; no language server), and fetches from a local
server that answers after 200 ms. Report:
[20261029-reply-calls](../../benchmarks/reports/20261029-reply-calls.txt).
Times are the median of three runs of five replies.

| Reply | One after another | Together (4) | Speed-up |
|---|---|---|---|
| 6 × `read` | 9.4 ms | 7.2 ms | 1.3× |
| 4 × `grep` (files, 8,088 Go files) | 1,384 ms | 502 ms | 2.8× |
| 4 × `find_symbol` (`net`) | 73 ms | 34 ms | 2.1× |
| 4 × `web_fetch` (200 ms each) | 808 ms | 205 ms | 3.9× |

The searches run without ripgrep (the machine has none), with the built-in
search; ripgrep is itself parallel, so it gains less. A language server
(Serena) takes one call at a time per server, so its lookups do not run
together.

## MCP tool search

`bcode bench mcpsearch --runs 2`: six requests, each needing one of 60
tools of a local MCP server (six verbs on ten record types, schemas of
three arguments), with `direct.mcp_tool_search` off (every definition
listed) and on (`mcp_search` and `mcp_call`). A request passes when the
right tool ran with the right id and the answer reports what it returned.
Every request is a new session. Report:
[20261029-mcpsearch](../../benchmarks/reports/20261029-mcpsearch.md).

| mcp_tool_search | Passed | Model calls per request | Tool definitions | Prompt tokens per request | Processed by the server per request | Median time |
|---|---|---|---|---|---|---|
| off | 12 / 12 | 2.0 | 10.5k | 20.8k | 8,096 | 14.4 s |
| on | 12 / 12 | 4.5 | 3.7k | 21.7k | **2,564** | 13.1 s |

- With search, a request takes about one more round trip: a search, then
  the call.
- Twice the model first tried tools of the same record type that do not
  answer (`search_refund` and `list_refund`; `search_invoice`) before the
  right one (9 and 7 model calls).
- The server processes less because a new session's first prompt carries
  far fewer definitions. Within a long session the definitions are cached
  after the first call. What stays is the share of the working set they
  take: 6.8k of a 32K working set here, a fifth of it.

**The first version did worse.** Its system-prompt note gave only the
server's name and tool count, and the first four requests with search on
went:
- 3 of 4 passed, against 4 of 4 with the tools listed;
- in one, the model searched the project with `ls`, `grep` and `bash` and
  never looked for the tool;
- every one started with commands in the project.

The note now lists the deferred tools' names (a few tokens each, against
about 110 for a definition) and says to use them for what the servers
serve. That change was made after those four requests and measured on the
same bench, so the table above is not a held-out result.

## Agent loop: the direct suite

`bcode bench direct`, two runs per arm, interleaved. 28 tasks; two (Java,
Rust) are skipped for the missing toolchain and count as failures; the
per-task figures are over the other 26, as in the reports.
Reports: `benchmarks/reports/20261029-direct-p8-{before,after}-r{1,2}`.

| Arm | Run | Passed | Model calls per task | Tool calls per task | Prompt tokens per task | Processed per task | Time per task | Tool time per task |
|---|---|---|---|---|---|---|---|---|
| before | 1 | 24 / 26 | 12.0 | 11.8 | 232k | 13.8k | 69 s | 0.21 s |
| before | 2 | 24 / 26 | 6.2 | 6.0 | 56.6k | 4.5k | 49 s | 0.24 s |
| after | 1 | **25 / 26** | 6.3 | 6.1 | 48.0k | 3.3k | 39 s | 0.23 s |
| after | 2 | **25 / 26** | 5.8 | 5.5 | 43.7k | 3.2k | 36 s | 0.21 s |

- **Failures.**
  - `multi-feature-discount` failed in all four runs: the discount exceeds
    the subtotal, as in every earlier phase.
  - Before also failed `session-x-tools` in run 1: the model searched for
    a type in 172 model calls (952 s) and gave up. That run's averages are
    mostly this task.
  - Before failed `fix-balance-query` in run 2. That task passes about
    half the time in earlier phases.
- **Without `session-x-tools`** the arms are the same: 4.9–5.2 model calls,
  30.6–34.0k prompt tokens, 2.3–2.7k processed and 28.9–30.9 s per task in
  all four runs (over all 28 tasks).
- **The model sent calls that run together** in 24 and 25 replies per run
  (56 and 59 calls), all reads and searches. Each batch took under a
  millisecond, so the batch time recorded was 0. The event now records
  microseconds.
- **Outcome codes:** after, 15 `bash` and 4 `read` calls failed (commands
  with a non-zero status, a missing file). There were no refusals.
- **Resources:** the harness's peak memory was 38 MiB after and 68 MiB
  before (the 172-call session); the model server took 28.3 and 28.7 GiB,
  the GPU 7.0 GiB in both.

## Agent loop: bounded mode

`bcode bench tasks`, all 19 tasks, once, against the last bounded run
([20261028-tasks-reasoning-auto](../../benchmarks/reports/20261028-tasks-reasoning-auto.md),
18 of 19). Report: [20261029-tasks-p8](../../benchmarks/reports/20261029-tasks-p8.md).
Bounded mode's agent runs OpenHands' own tools in its container; nothing
of the direct-mode pipeline is on its path. What changed for it is the
governor, which now classifies actions with `capability.BoundedAction`
(the same counts as before, by `TestBoundedActions`).

| Run | Passed | Self-verified, hidden checks failed | Attempts per task | Time per task | Processed tokens per task |
|---|---|---|---|---|---|
| 2026-10-28 | 18 / 19 | 1 | 1.89 | 269 s | 27.4k |
| after | 16 / 19 | 2 | 1.94 | 276 s | 26.3k |

The three failures:
- **`api-contract-description` did not start:** Docker Desktop had run out
  of file handles (the race-detector runs before it), and the container's
  mount failed. Rerun alone it passed in 364 s
  ([recheck](../../benchmarks/reports/20261029-recheck-tasks-p8-api-contract.md)).
- **`go-debug-ledger-balance-query`:** the agent changes the task's tests,
  which the check refuses. It fails this way in every run.
- **`cross-service-idempotency`:** the agent's change passed its own
  checks, and a hidden test of concurrent requests found duplicate
  events. Rerun twice on each build, it passed once on each:

  | Build | Run 1 | Run 2 |
  |---|---|---|
  | before (`74e6f0c`) | passed, 433 s | failed, 3 attempts, 1,082 s |
  | after | failed (self-verified), 694 s | passed, 927 s |

  Reports: `benchmarks/reports/20261029-recheck-tasks-p8-{before,after}-cross-service-r{1,2}`.

On the other 16 tasks (leaving out `api-contract-description`,
`cross-service-idempotency`, and `multi-feature-discount`, which passed in
both but took 571 s after and 258 s before), the agent made 20.8 tool calls per task after and 20.9 before, processed 24.1k
and 23.4k tokens, and took 259 s and 237 s. The two runs of 2026-10-28
(`-auto` and `-drop`) differ more than that: 269 s and 321 s per task.

## Determinism and consistency

| What | How it is checked |
|---|---|
| The same session record with calls together or in turn | `TestBatchingKeepsSessionDeterministic`: a reply of six reads, a search and a listing, then an edit, at `parallel_tools` 1 and 4: every message is identical |
| A batch's events in a fixed order | the same test, run twice at 4: identical event sequences |
| An interrupt during a batch | `TestInterruptedBatch`: every call of the batch is answered, the rest skipped |
| Changes keep their order | `TestChangeSplitsBatch`: reads before a write run together, a read after it sees the write |
| No data races | `go test -race` on `internal/direct`, `execbox`, `fileedit`, `orchestrator`, `benchmark`, `tui` and `inference` (in a container with a C compiler) |
| The same files readable in both modes | `TestSecretPathsAgreeAcrossModes`: 18 files (`.env`, keys, cloud credentials, a Go package named `credentials`, docs about secrets …); direct mode refuses exactly what bounded mode masks |
| Every tool described, no concurrent tool that changes anything | `TestEveryToolIsDescribed` |
| Bounded actions classified as before | `TestBoundedActions` and the governor's tests |

The race run found a race in the command sandbox, which is fixed. A
timeout's cancel read the process group id while it was still being
written, so it could leave the group running. Running the race detector
also showed that `grep` results came in a different order on machines
with ripgrep, which made session records depend on timing; they are now
sorted.

## Limits

- One model on one machine. The direct suite ran twice per arm; the
  bounded suite once, with two tasks rerun.
- The microbenchmark's fetches are to a local server with a fixed delay,
  not to real sites.
- The MCP bench's server and requests were written for it. Its system
  prompt note was changed after a first partial run (above).
- The race detector did not run on `internal/cli`: Docker Desktop's file
  sharing ran out of file handles linking its tests. CI runs it.
- `replace_symbol` is tested with a fake language server; it was not used
  by the model in these runs (the suite has no language server).
