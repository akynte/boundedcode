# Tool capabilities in direct and bounded mode

Status: 2026-10-29. This page compares what the two execution modes give
the agent, and describes what they share and what they keep apart on
purpose. Measured in the [orchestration benchmark](../benchmarks/orchestration-2026-10.md).

The two modes isolate the agent differently, so they do not share an
executor:
- **Direct mode** (`internal/direct`) runs its own agent loop in the
  user's checkout. Every tool call passes the runner's checks, and
  commands run in the command sandbox (`internal/execbox`).
- **Bounded mode** (`internal/orchestrator`) runs an OpenHands agent
  (`adapters/openhands`) in a container, on a worktree. The container is
  the boundary. The task is then verified on the host.

Forcing both through one implementation would either weaken bounded
mode's isolation, by running tools on the host, or remove direct mode's
per-call approval, by moving it into a container. What they share is the
description of what tools do, the policy functions, the editing engine,
the gateway and the accounting.

## Comparison

| | Direct mode | Bounded mode |
|---|---|---|
| **Tools** | 27 built-in tools (`internal/direct/tools*.go`), skills, custom commands, sub-agents (`task`), and MCP servers (`mcp__server__tool`, or behind `mcp_search` and `mcp_call`) | OpenHands SDK built-ins: `terminal`, `file_editor`, `task_tracker`, `think`, `finish`. BoundedCode adds none, and there is no MCP |
| **Tool registration and discovery** | `Runner.buildTools`, filtered for sub-agents (`tools:`); `bcode chat tools` lists them | The adapter registers the SDK tools by name (`adapters/openhands/.../main.py`) |
| **Tool schemas** | JSON Schema on each `Tool`, sent as OpenAI tools; the gateway translates them per provider | The SDK's own schemas; the gateway passes them through unchanged |
| **Permissions and approval** | Per call, in this order: precheck, the user's deny rules, plan mode, the risky-path hand-off, `pre_tool` hooks, the guard (sensitive categories: credentials, network, installs, instruction files, git hooks, privileges), then the approval mode (`ask`, `edits`, `auto`) | None per call. The container is the boundary: no network by default, capabilities dropped, secret paths masked, the user's UID. Verification refuses protected and secret paths in the diff afterwards. The only human approval is for frontier escalation |
| **Secret and protected paths** | `policy.IsSecretPath`, `policy.ClassifyPath` | `policy.FindSecretPaths` (mounted empty), `policy.IsSecretPath` and `IsProtectedPath` (diff scope) |
| **Command policy** | `policy.ClassifyCommand` before each command, plus the sandbox | `policy.CheckCommand` for verification commands; the agent's own commands are bounded by the container |
| **Context** | The conversation, with a working set, pruned observations and the session state (`internal/prune`, [context](../usage/context.md)) | A context pack per attempt, built from the ledger (`internal/contextplan`); OpenHands' condenser inside an attempt |
| **Sessions and state** | A JSON-lines session file, resumable; checkpoints for `/undo` | The SQLite ledger (tasks, strategies, verification runs, events); the worktree branch; OpenHands' persisted conversation |
| **Execution** | Calls of one model reply in order. Calls that change nothing run together (below). Commands get a timeout and their own process group | One agent turn at a time in the container; the adapter refuses a concurrent turn |
| **Cancellation and timeouts** | `context.Context` through every tool; an interrupt answers every call of the reply; command timeouts kill the process group | `session.interrupt`, then the container is killed after a grace period; the turn is bounded by the wall-clock budget and the no-progress governor |
| **Verification** | A syntax check (and `go build`) after each edit; deterministic checks and verified tasks are bounded mode's | Diff scope, integrity, secret scan, the configured stages, behavioural evidence, the compat gate, mutation and counterexample layers |
| **Errors** | Each refused or failed call has a code (`capability.Code`) on its event; the model gets a message | Tool errors stay in the SDK and reach Go as `ObservationEvent.is_error`; infrastructure errors are classified by the runner |
| **Event streaming** | `direct.Event` to the TUI, `--events` JSON lines and the bench | `agent.Event` to the `events` table (`telemetry.Recorder`); the TUI polls it |
| **Traces** | Per-call stats in the gateway; the session file; the bench's per-tool times and outcomes | `model_calls` rows; the adapter log; events |
| **Resource accounting** | The gateway's counters (processed, generated, cached), per turn and per session | The same gateway counters, saved into the task's budget |

## The shared architecture

| Concern | Shared | Mode-specific, and why |
|---|---|---|
| What a tool does | `internal/capability`: `Descriptor` (effects: read, write, exec, network, interact, delegate, state; concurrency; the subject rules match; source; where it is enforced). Direct tools are described in `internal/direct/capabilities.go`, bounded tools in `capability.BoundedTools` | Direct tools are Go functions; bounded tools are the SDK's. Each mode keeps its executor |
| Classifying actions | `capability.BoundedAction` classifies OpenHands actions for the governor; direct tools by their descriptor | |
| Registration and discovery | `Runner.Capabilities()`; `bcode chat tools` (and `--json`) lists both modes' tools with their effects; MCP discovery through `mcp_search` | Bounded mode's tools are fixed by the SDK |
| Approval and authorization | `internal/policy`: secret paths, protected paths, command and path classification | Direct mode asks per call (the categories of the [security policy](../usage/security-policy.md)); bounded mode bounds by container and checks the diff with the same secret and protected path rules |
| Results and structured errors | `capability.Code` and `capability.Error`; `capability.Tally` counts calls by outcome | The model-facing messages are unchanged |
| Editing | `internal/fileedit`: replacement (exact, then tolerant of indentation), changes to several files together or not at all, conflict detection, atomic replacement | Bounded mode's agent edits with the SDK's `file_editor` inside the container |
| Cancellation and timeouts | `context.Context` everywhere; process trees (`execbox`, `proctree`) | The container's grace period and kill |
| Event streaming and traces | The gateway's call log; `capability.Tally` | `direct.Event` and `agent.Event` stay separate: a TUI streams one and polls the other |
| Session state | | Kept apart: a conversation file and a task ledger answer different questions (resume a chat, recover a task) |
| Resource accounting | `inference.Gateway` counters in both | |

## Direct mode's tool pipeline

`runTool` runs three stages:
1. **Prepare**, in call order: the call's tool and arguments; for every
   file the call names, the precheck, deny rules, plan mode, the risky-path
   hand-off, `pre_tool` hooks, the guard and approval; then the checkpoint
   and the "tool start" event.
2. **Execute:** the tool itself. It touches no state the other stages
   keep.
3. **Finish**, in call order: the check after an edit, the history note,
   `post_tool` hooks, the "tool end" event and the observation for the
   model.

**Concurrent calls.** Consecutive calls of one model reply run their
execute stage together, at most `direct.parallel_tools` at once (default
4; 1 turns it off), when their tools change nothing:
- `read`, `read_many`, `grep`, `glob`, `ls`, `notebook_read`;
- `find_symbol`, `find_references`, `find_implementations`, `recall`;
- `web_fetch`, `web_search`, `mcp_search`.

Everything else runs alone and keeps its place in the reply: edits,
commands (`bash`, even read-only ones), background shells, the task list,
plan mode, questions to the user, memory, skills, sub-agents and MCP
tools. MCP tools are external and their effects unknown, and a server's
client takes one call at a time.

- Results enter the session in call order, so the session record is the
  same with or without concurrency (`TestBatchingKeepsSessionDeterministic`).
- A batch's events come in a fixed order: the starts in call order, then
  the ends in call order. The TUI pairs them by call id.
- An interrupt during a batch records every call of it and skips the rest
  of the reply.
- `grep` sorts its results by path (ripgrep's parallel search otherwise
  answers in a different order each run), so a record does not depend on
  timing.

**Editing:**
- `write`, `edit`, `multi_edit` and `replace_symbol` replace files
  atomically: a temporary file is renamed over the file.
- `multi_edit` changes several files when its edits name their own
  `path`. If any edit fails, nothing is written, and a deny rule on any of
  its files refuses the call.
- **Conflicts.** The session's file stamps record the blob of each file
  the model saw. A `write` over a file that changed since then is refused
  (code `conflict`): the new content was written from the old one. An
  `edit` whose text still matches goes ahead and says the file changed.
- **`replace_symbol`** replaces a definition by the range a language
  server reports. It is offered only when one is configured (Serena): the
  definition patterns used for other languages know where a symbol starts,
  not exactly where it ends. Every language keeps `edit` and `multi_edit`.

**MCP discovery.** When the definitions of the configured MCP servers'
tools would take more than a tenth of the working set
(`direct.mcp_tool_search: auto`), they are not listed. The model gets:
- `mcp_search`, which returns matching definitions as an ordinary result;
- `mcp_call`, which runs the tool it names through the same pipeline
  under the tool's own name: trust pins, deny rules, approval, provenance
  and the data header.

The tools sent with each request stay the same for the whole session, so
the prompt cache keeps its prefix. Adding definitions as they were found
would change the system prompt the server renders them into.

## Not done, and why

- **Running the agent's commands together.** Two commands can write the
  same files, ports or caches; nothing in a command line says they cannot.
- **Running sub-agents together.** On a local server with one slot,
  sub-agents' model calls queue behind each other, so running them
  together saves nothing. With a cloud provider it could, but it would make
  the order of their events and results depend on timing. Not measured.
- **Agent teams.** No evidence that they improve outcomes here. `task`
  runs one sub-agent to completion inside the turn.
- **Replacing OpenHands in bounded mode.** No measured reason to. Its
  tools, container and condenser are kept.
- **One event type for both modes.** The TUI streams direct events and
  polls bounded ones from the ledger, and each carries what its consumer
  needs. The shared vocabulary (tool, effects, outcome code) is what the
  two have in common.
