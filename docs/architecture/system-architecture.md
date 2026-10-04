# System Architecture

Status markers: **[impl]** implemented, **[exp]** experimental, **[plan]** planned.
Keep these markers accurate when code changes.

## 1. Overview

```text
                              USER
                                |
                                v
                 +------------------------------+
                 |  boundedcode CLI (Go)        |
                 +--------------+---------------+
                                |
                                v
                         CONTROL PLANE (Go)
   +-----------+-----------+-----------+-----------+-----------+
   |           |           |           |           |           |
 task       workspace   repointel   verify      policy     frontier
 ledger     + gitops    (MCP client) engine     + sandbox   gate
 (SQLite)   worktrees       |                       |           |
   |                   +----+-----+                 |           v
   |                   v          v                 |     codex exec (ChatGPT
   |    codebase-memory-mcp    Serena v1.7.0        |     sign-in, read-only)
   |    (graph: breadth)       (LSP: depth, one     |
   |                           instance/worktree,   |
   |                           optional)            |
   v                                                v
 agent.Runtime ── JSON-RPC/stdio ──> OpenHands adapter (Python, in container)
                                         |  terminal / file tools run in the
                                         |  container, cwd = task worktree
                                         |
                LLM calls tunnelled back over the same stdio channel
                                         |
                                         v
                         LLM gateway (Go): metering, budgets, audit
                                         |
                                         v
                       inference.Runtime: llama-server (ext proc)
                                         |
                                 local GGUF model
```

## 2. Process model

| Process | Owner | Lifetime |
|---|---|---|
| `boundedcode` CLI | user | per command; long-running for `task run` |
| `llama-server` | supervised by `inference/llamacpp` or user-supplied | long-lived; reused across tasks |
| OpenHands adapter | spawned per task session by `agent/openhands` | per session; may crash or restart without losing the task |
| `codebase-memory-mcp` | `repointel/cbm` keeps one persistent MCP stdio session per command or task (ADR-0006), with a private cache dir and UI/watchers disabled | per command/task |
| `serena start-mcp-server` + language servers (optional) | `repointel/serena.Manager`: one MCP stdio instance per checkout root (task worktree), at most `max_instances` (default 2, LRU), stopped after `idle_timeout` (10 min), at the end of a task run, on call timeout or cancellation; read-only tool set; private `SERENA_HOME` outside the worktree (ADR-0008) | per worktree while in use |
| `codex exec` | spawned by `frontier/codex` per escalation | per escalation |
| verification commands | `verify` (inside sandbox when configured) | per stage |

A daemon is **[plan]**. Everything else in this document is **[impl]**
unless marked otherwise. Version 1 runs the control loop in the foreground CLI
process. Because state is in SQLite, Git and the OpenHands persistence
directory, a killed CLI can be resumed with `task resume`.

## 3. Boundaries (interfaces)

Interfaces exist only where replacement is plausible:

| Interface | Package | Implementations |
|---|---|---|
| `inference.Runtime` | `internal/inference` | `llamacpp` (managed or external) |
| `agent.Runtime` | `internal/agent` | `openhands` (adapter) and `scripted` (deterministic, for tests) |
| `repointel.Intelligence` | `internal/repointel` | `cbm` (codebase-memory-mcp) |
| `repointel.Navigator` | `internal/repointel` | `serena` (Serena v1.7.0 over language servers; keyed by checkout root) |
| `frontier.Provider` | `internal/frontier` | `codex` (subscription CLI) and a manual/clipboard provider |
| `sandbox.Sandbox` | `internal/sandbox` | `docker` and `none` (development only, refused for autonomous tasks unless explicitly overridden) |

Verification and telemetry are concrete packages. They have a single
implementation, and the stage list is data rather than code.

## 4. The adapter protocol

**Decision ([ADR-0004](adr/0004-adapter-protocol.md))**: newline-delimited
JSON-RPC 2.0 over the adapter process's stdin/stdout. Both sides act as
peers:

* Go → adapter: `session.start`, `session.resume`, `session.send`,
  `session.interrupt`, `session.state`, `shutdown`.
* adapter → Go (requests): `llm.complete`. This carries OpenAI-compatible
  chat-completion requests that the adapter's in-container loopback proxy
  receives from LiteLLM.
* adapter → Go (notifications): `event`, which carries OpenHands event
  summaries (action, observation, condensation, error, stuck, finish).

stdio was chosen over a Unix socket or HTTP for three reasons:

1. It needs no port allocation and no socket files.
2. It crosses the Docker Desktop VM boundary (`docker run -i`), which
   bind-mounted Unix sockets do not.
3. It lets the container run with `--network none` while LLM traffic still
   flows, because the only egress is through the Go gateway.

stderr is free-form adapter logging.

## 5. Data and state

| State | Store | Authority |
|---|---|---|
| workspaces, repos, tasks, steps, attempts, strategies, decisions, verification results, escalations, model calls, audit events, benchmark results | SQLite at `$XDG_DATA_HOME/boundedcode/state.db` (WAL) | **canonical** |
| code changes | Git worktree `agent/<task-id>` | canonical |
| conversation, events, condensation | OpenHands persistence dir `…/tasks/<id>/openhands/` | owned by OpenHands; referenced by `agent_session_id` |
| code graph | codebase-memory-mcp cache | derived; rebuildable |
| Serena instance config and symbol caches | `$XDG_CACHE_HOME/boundedcode/serena/instances/<hash>-<repo>/`, keyed by worktree path (never inside the worktree) | derived; rebuildable; not product state |
| frontier packets and responses | `…/tasks/<id>/frontier/NNN-{packet,response}.md` and a DB row | canonical |

Writes follow *persist-then-act*: the ledger records the intended transition
before a destructive or long-running action starts, and records the outcome
after it finishes.

## 6. Control loop (task run)

```text
create task ──> create worktree ──> plan context pack (ledger + graph + git)
      ^                                    |
      |                                    v
      |                       agent.Send(instruction + pack)
      |                                    |
      |                                    v
      |                       verification (impact-selected, then full)
      |                          | pass                 | fail
      |                          v                      v
      |                    merge candidate      record attempt/strategy
      |                                                 |
      |                       escalation policy (Z1..Z4) ─ yes ─> frontier packet
      |                                                 |               |
      +─────────────── retry with verification feedback <─── advice ────+
```

Budgets bound every loop (attempts, wall clock, local tokens, frontier
escalations). Exhausting a budget parks the task in `blocked`; it never
deletes work.

## 7. Context planning

Context packs are built from authoritative sources in a fixed priority order
with a token budget:

1. task goal, acceptance criteria, phase, remaining steps
2. latest verification failures (truncated, deduplicated)
3. current diff (stat first, then hunks for files under change)
4. impact summary from the graph (callers/callees of changed symbols)
5. relevant source: lines named by failures, then symbols named in the task
   (see 7.1)
6. relevant ADRs and decisions
7. rejected strategies (so the model doesn't repeat them)

Packs are deterministic for a given state, which makes them testable.

### 7.1 Repository intelligence: breadth and depth [impl]

Two backends, one owner per question (measured in
`benchmarks/reports/*-intel-overlap.md`, ADR-0008):

| Question | Default | Fallback | Why |
|---|---|---|---|
| Where is symbol X (definition, body)? | Serena, in the task worktree | graph snippet | type-checked, sees the task's edits; the graph indexes the primary checkout |
| Who references X? | Serena | graph callers (`trace_path`) | the graph matched a same-named function from another package in grpc-go |
| Which types implement interface X? | Serena | graph `IMPLEMENTS` | the graph's `IMPLEMENTS` is method-name based (false positives) |
| Which repositories mention X? | graph (`search_graph`) | all task repositories | cheap (~12 ms), avoids starting language servers |
| What does this diff affect? | graph (`detect_changes`) | none | blast radius is a graph question |
| Cross-service and cross-repository contracts | `internal/xservice` + graph | none | Serena is per repository |
| Architecture, ADRs | graph and repository ADRs | none | |
| Edit a symbol | OpenHands' editor (agent) | none | Serena editing stays disabled (Stage 2 found a silent partial TypeScript rename) |

In a multi-repository task the graph decides where to look and Serena
answers inside each repository ("global graph for breadth, LSP for depth").
Neither backend is queried twice for the same question; Serena failures fall
back per symbol and never block the task.

## 8. Security model (summary)

See [SECURITY.md](../../SECURITY.md) and [sandbox design](../design/sandbox.md).

* The agent process and its tools run in a container. The task work dir is
  mounted read-write. Each repository's git common dir is mounted
  **read-only**, and only the worktree's own admin dir is writable. There is
  no `$HOME`, no SSH agent and no credentials, and the network is `none`.
* Host-side git runs with hooks and fsmonitor disabled and no external
  diff or textconv. It verifies each worktree's `.git` pointer first, because
  the agent can write the worktree.
* Path policy denies `.env*`, `secrets/`, key material and kubeconfigs inside
  the container, using masked mounts.
* Command policy is deterministic and evaluated in Go before verification
  commands run. Inside the agent, the container is the boundary. The agent's
  own commands are not filtered, because they cannot reach anything outside
  the sandbox.
* Frontier credentials stay on the host. `codex exec` runs with
  `--sandbox read-only` on a packet file, never in the agent container.
* Serena runs on the host, never as an agent tool, with a read-only tool
  set, stdio only (no listener), a private home outside agent-writable paths
  (repository `.serena/` configs are ignored and no project is trusted, so no
  `activation_command` runs), the sandbox's secret masks as ignored paths, no
  usage reporting and no network for toolchains (ADR-0008).

## 9. Package map

`cli` → `orchestrator` → {`task`, `contextplan`, `verify`, `policy`,
`frontier`, `agent/*`, `inference/*`, `repointel/*`, `xservice`, `sandbox`,
`gitops`, `workspace`, `telemetry`, `store`}. `xservice` extracts and links
cross-service contracts (HTTP, topics, env, Terraform) to complement the
code graph. `repointel/serena` manages Serena processes behind
`repointel.Navigator`. Lower layers never import `cli` or
`orchestrator`.
