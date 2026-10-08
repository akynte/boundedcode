# System Architecture

Status markers: **[impl]** implemented, **[exp]** experimental, **[plan]** planned.
Keep these markers accurate when code changes.

## 1. Overview

```text
                              USER
                                |
                                v
                 +------------------------------+
                 |  bcode / boundedcode (Go)    |
                 |  CLI · TUI with chat [exp]   |
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
                       inference.Upstream (chosen by inference.provider)
                         |                                  |
                         v                                  v
       local: llama-server (ext proc)        cloud [exp]: OpenAI, Anthropic,
                         |                   Gemini or OpenAI-compatible API
                 local GGUF model            (HTTPS from the host; the key is
                                             added by the gateway only)
```

The gateway speaks the OpenAI chat format to the agent and translates to
each upstream (ADR-0010). Anthropic thinking blocks and Gemini thought
signatures are kept in a replay store and sent back verbatim on the next
turn. The agent sandbox never sees an API key and keeps `--network none`.

### Model choice **[exp]**

`internal/hw` probes the machine (RAM, GPUs and their memory, Apple Silicon
unified memory, free disk) per OS. `model.FitFor` rates each catalog profile
against that snapshot: whole model on the GPU, MoE experts offloaded to
system RAM, dense layers split between GPU and RAM, CPU only, or too large
(a rule of thumb). `model.Recommend` keeps the validated default when it
runs on the accelerator (GPU or offload) and otherwise suggests the best
fitting profile. `model fetch` downloads
weights at a pinned revision, sha256-verified, with resume.

### Platforms **[exp]**

Linux is the validated platform. On macOS and Windows the agent and
verification still run in a Linux container (Docker Desktop or a Podman
machine). On Windows, host paths are mounted at `/host/<drive>/...` and
translated in both directions (`sandbox.ContainerPath`), task worktrees use
relative `.git` pointers, and process trees end with `taskkill /T`. Pinned
tools come from per-platform release assets (`internal/install`).

### Interactive interface **[exp]**

`bcode` with no arguments opens `internal/tui`, a client of the control plane
with no logic of its own:

* the **chat** maps each message to a task operation for the repository in the
  current directory: `task create` (from `HEAD`, or with `--from` to start from
  the previous task's branch), `task run --clarify` for a task waiting on an
  answer, and `task apply` to bring a result into the checkout;
* the **views** read the ledger, audit log and status, and run the remaining
  commands;
* **`setup`** checks and installs prerequisites with pinned installers and a
  sandbox build context embedded in the binary (`assets.go`), so an installed
  binary needs no source checkout.

## 2. Process model

| Process | Owner | Lifetime |
|---|---|---|
| `boundedcode` CLI | user | per command; long-running for `task run` |
| `bcode` / `boundedcode tui` **[exp]** | user | interactive session. The chat and the views run the same CLI commands in-process (`internal/tui` → `cli.tuiBackend.Exec`), so policy, sandboxing and audit are unchanged; approvals that the CLI asks on stdin appear as dialogs. A task it runs holds the usual lease, so a parallel `task run` is refused |
| `llama-server` | supervised by `inference/llamacpp` or user-supplied | long-lived; reused across tasks. A managed server unloads the model after `inference.idle_sleep` (default 30 min; measured: RSS 19.3 GiB → 0.8 GiB, VRAM 7.1 → 0.2 GiB) and reloads it on the next request (1.7 s with a warm page cache); `runtime stop` ends it |
| OpenHands adapter | spawned per task session by `agent/openhands` | per session; may crash or restart without losing the task |
| `codebase-memory-mcp` | `repointel/cbm` keeps one persistent MCP stdio session per command or task (ADR-0006), with a private cache dir and UI/watchers disabled; own process group with a parent-death signal; calls are time-bounded and a dead or hung session falls back to the CLI | per command/task |
| `serena start-mcp-server` + language servers (optional) | `repointel/serena.Manager`: one MCP stdio instance per checkout root (task worktree), at most `max_instances` (default 2, LRU), stopped after `idle_timeout` (10 min), at the end of a task run, on call timeout or cancellation; read-only tool set; private `SERENA_HOME` outside the worktree (ADR-0008) | per worktree while in use |
| `codex exec` | spawned by `frontier/codex` per escalation, in a named container removed on cancel | per escalation |
| verification commands | `verify`, in named sandbox containers removed on cancel or stage timeout | per stage |

A daemon is **[plan]**. Everything else in this document is **[impl]**
unless marked otherwise. Version 1 runs the control loop in the foreground CLI
process. Because state is in SQLite, Git and the OpenHands persistence
directory, a killed CLI can be resumed with `task resume`. A run holds a
lease on its task (heartbeat every 15 s); a second `task run` is refused,
a lease older than 2 minutes is taken over after a crash, and `task cancel`
stops a running task within one heartbeat (ADR-0002).

## 3. Boundaries (interfaces)

Interfaces exist only where replacement is plausible:

| Interface | Package | Implementations |
|---|---|---|
| `inference.Runtime` | `internal/inference` | `llamacpp` (managed or external) |
| `inference.Upstream` **[exp]** | `internal/inference` | `OpenAIUpstream` (llama-server and OpenAI-compatible APIs), `AnthropicUpstream` (official Go SDK), `GeminiUpstream` (REST) |
| `inference.ReplayStore` **[exp]** | `internal/inference` | file-backed store of provider reasoning blocks for verbatim replay |
| `agent.Runtime` | `internal/agent` | `openhands` (adapter) and `scripted` (deterministic, for tests) |
| `repointel.Intelligence` | `internal/repointel` | `cbm` (codebase-memory-mcp) |
| — (concrete) | `internal/stats` | success metrics from the ledger (`boundedcode stats`) |
| `repointel.Navigator` | `internal/repointel` | `serena` (Serena v1.7.0 over language servers; keyed by checkout root) |
| `frontier.Provider` | `internal/frontier` | `codex` (subscription CLI) and a manual/clipboard provider (separate from the agent's model provider) |
| `sandbox.Sandbox` | `internal/sandbox` | `docker` and `none` (development only, refused for autonomous tasks unless explicitly overridden) |

Verification and telemetry are concrete packages. They have a single
implementation, and the stage list is data rather than code.

## 4. The adapter protocol

**Decision ([ADR-0004](adr/0004-adapter-protocol.md))**: newline-delimited
JSON-RPC 2.0 over the adapter process's stdin/stdout. Both sides act as
peers:

* Go → adapter: `session.open` (start a conversation, or resume
  `conversation_id` from the persistence directory), `session.send`,
  `session.condense`, `session.interrupt`, `session.state`, `shutdown`.
* adapter → Go (requests): `llm.complete`. This carries OpenAI-compatible
  chat-completion requests that the adapter's in-container loopback proxy
  receives from LiteLLM.
* adapter → Go (notifications): `ready`, sent once the SDK has loaded and
  carrying `protocol_version`, and `event`, which carries OpenHands event
  summaries (action, observation, condensation, error, stuck, finish).

Go refuses an adapter whose `protocol_version` differs from its own (or is
missing) and asks for the sandbox image to be rebuilt; versioning rules are
in ADR-0004. A `session.send` whose context ends is interrupted with
`session.interrupt`; if the agent does not stop within a grace period the
adapter is killed (container removed, or host process group killed).

stdio was chosen over a Unix socket or HTTP for three reasons:

1. It needs no port allocation and no socket files.
2. It crosses the Docker Desktop VM boundary (`docker run -i`), which
   bind-mounted Unix sockets do not.
3. It lets the container run with `--network none` while LLM traffic still
   flows, because the only egress is through the Go gateway.

stderr is free-form adapter logging. Go redacts it line by line and writes
it to a per-task `adapter.log` beside the task's runtime directory.

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
create task ──> create worktree ──> task contract (local model)
                                     | material ambiguity, policy ask ──> blocked until `task run --clarify`
                                     v
                         plan context pack (ledger + contract + graph + git)
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
escalations). Each agent turn is bounded by the remaining wall-clock budget.
Within a turn, thinking is capped per response (`reasoning_budget`), visible
output per response (`agent.max_output_tokens`), and a deterministic
strategy governor stops an attempt that generates `agent.strategy.*` tokens
without progress (first edit, new test file, an agent-run test going from
failing to passing) or exceeds its token/time cap; the stopped strategy is
recorded and the retry is told not to repeat it. Long output alone never
triggers frontier escalation.

A verification pass is `task_verified` only with behavioural evidence: a
test the change adds or modifies fails on an export of the base commit and
passes with the change. Otherwise the agent is asked once for such a test,
and the task can end `tests_green` (UNVERIFIED), never as a verified merge
candidate.
Exhausting a budget parks the task in `blocked`; it never deletes work.

Failure handling inside the loop:

* A model-server outage during a turn (transport errors recorded by the
  gateway) is repaired with `Ensure` and retried; it does not count as an
  attempt or toward Z2. After 3 repairs the task blocks.
* An adapter crash reopens the persisted conversation (or a fresh one with
  a resume pack).
* A checkpoint commit that fails, or a worktree whose `.git`, admin dir or
  `HEAD` was tampered with, blocks the task.
* After each checkpoint the ledger records changed files, repositories and
  symbols (from the diff), step progress and verification state.

## 7. Context planning

Context packs are built from authoritative sources in a fixed priority order
with a token budget:

1. task: goal, acceptance criteria, phase, completed/remaining steps,
   decisions, verification state, changed files and symbols, and each
   repository's branch and HEAD
2. frontier guidance to apply, if any
3. latest verification failures (truncated, deduplicated)
4. rejected strategies (so the model doesn't repeat them)
5. current diff (stat first, then hunks, at most 150 lines per file; secret
   paths omitted)
6. impact of the change from the graph (the task worktree is indexed as its
   own project, because `detect_changes` only diffs the indexed checkout)
7. cross-service contracts touching the task's repositories
8. relevant source: lines named by failures, then retrieval seeds from the
   request in rank order: quoted error messages (located by fixed-string
   search), names in prose and linked files, config keys and flags, then
   identifiers from code samples; placeholders and names matching many files
   are demoted (see 7.1)
9. relevant ADRs
10. rules

Packs are deterministic for a given state, which makes them testable. Files
are read only inside the worktree (no symlink escapes, no secret paths) and
the rendered pack is redacted.

### 7.1 Repository intelligence: breadth and depth [impl]

Two backends, one owner per question (measured in
`benchmarks/reports/*-intel-overlap.md`, ADR-0008):

| Question | Default | Fallback | Why |
|---|---|---|---|
| Where is symbol X (definition, body)? | Serena, in the task worktree | graph snippet | type-checked, sees the task's edits; the graph indexes the primary checkout |
| Who references X? | Serena | graph callers (`trace_path`) | the graph matched a same-named function from another package in grpc-go |
| Which types implement interface X? | Serena | graph `IMPLEMENTS` | the graph's `IMPLEMENTS` is method-name based (false positives) |
| Which repositories mention X? | graph (`search_graph`) | all task repositories | cheap (~12 ms), avoids starting language servers |
| X not found by Serena or the graph | ripgrep (whole word, ≤10 hits, ±3 lines, no secret paths or symlinks) | none | exact lexical stage of the retrieval order |
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
cross-service contracts (HTTP, OpenAPI, gRPC, protobuf, SQL, topics, env,
Terraform) to complement the code graph. `repointel/serena` manages Serena processes behind
`repointel.Navigator`. Lower layers never import `cli` or
`orchestrator`.
