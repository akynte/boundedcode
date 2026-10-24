# Everyday development: direct mode and the road to a competitive coding agent

Status: **proposed** (2026-10-24), phase 17 implemented (experimental).
Requested by the maintainer: BoundedCode should become a fast, extensible,
open-source coding agent for everyday software development, and when a
change is sensitive or complex, use isolation, resource management and
test-backed evidence to prepare it for review with more confidence. It
should compete with Pi, Claude Code, OpenCode and Qwen Code on day-to-day
usability, and keep its existing capabilities as the differentiator. It
evolves the existing project; it is not a fork of another agent.

## 1. Where BoundedCode stood (2026-10-24)

Every message in the `bcode` chat became a **task**: a worktree on
`agent/<id>`, an OpenHands session in a network-less container, a task
contract, a context pack, deterministic checks and the behavioural-evidence
gate. That is the right shape for a change that must be trusted. It is the
wrong shape for most of a working day:

| Everyday need | What a task offers |
|---|---|
| "What does this function do?" | a worktree, a container, a contract derivation, minutes of overhead, and a branch nobody wanted |
| "Rename this and fix the callers" in the checkout I am looking at | a change on another branch that must be `/apply`-ed |
| "Run the tests and tell me what failed" | not possible without a task |
| a quick follow-up question | a follow-up task from the previous task's branch |
| working on a folder that is not a git repository, or has uncommitted work | refused (tasks need a committed base) |
| a machine without Docker, or a 5 GB sandbox image | refused |

The competitors are agents first. What they ship, from their public
documentation (2026-10):

- **Pi** (`packages/coding-agent`): a minimal loop with read, write, edit,
  bash, grep, find and ls; interactive, print/JSON and RPC modes; a
  TypeScript SDK; extensions, skills, prompt templates, themes and packages;
  session trees; built-in providers with `/login`. Deliberately no
  sub-agents or plan mode built in.
- **Qwen Code**: a Gemini-CLI-derived agent with approval modes, session
  management, checkpointing, MCP tools, sub-agents and agent teams, hooks,
  skills and slash commands, plan mode, LSP integration, a sandbox and git
  worktrees, headless mode (`qwen -p`), IDE plugins, a desktop app, a
  daemon (`qwen serve`), chat channels and SDKs in three languages.
- **Claude Code** and **OpenCode** sit between them: a native loop with
  permission prompts, project instruction files, sessions, MCP, hooks,
  commands and skills, headless mode and IDE integration.

None of them answers "what shows that this change is correct?" with more
than a green test run, and none isolates the agent from the checkout and
the network by default. That is BoundedCode's ground, and it stays.

## 2. The product shape: two modes, one agent

```text
                     bcode (chat)  ·  bcode -p "…"  ·  bcode chat
                                   |
             +---------------------+----------------------+
             |                                            |
       DIRECT MODE (default)                       BOUNDED MODE (/task)
   native Go loop in the checkout               task in a worktree + sandbox
   read · write · edit · bash · grep · glob · ls  OpenHands agent, no network
   approval: ask | edits | auto                 contract · context pack
   policy: no push, no infra mutation,          deterministic checks
           no secret reads                      behavioural evidence
   sessions (jsonl), compaction                 TASK_VERIFIED / tests_green
   metered through the same gateway             ledger, budgets, resume
             |                                            |
             +-------------- hand-off ---------------------+
                 /task REQUEST, or the agent suggests it
```

**Direct mode** is the everyday agent: fast, in the checkout, with the tools
other agents have, and a system prompt that follows the project's own
instruction file. **Bounded mode** is the existing control plane, unchanged.
The two share the configuration, the model provider, the metering gateway,
the audit log, the command policy and the secret-path rules.

Principles:

1. **Direct mode is a first-class product, not a demo.** It must be at
   least as pleasant as the competitors on the everyday tasks in the table
   above before any claim is made about the rest.
2. **Bounded mode is the differentiator, reached from direct mode in one
   step.** `/task REQUEST` from the chat, the agent's own suggestion when a
   change touches authentication, payments, migrations or shared contracts,
   and (later) an automatic hand-off that carries the direct session's
   summary into the task's context pack.
3. **Safety that does not get in the way.** Direct mode has no container,
   so it relies on deterministic policy (the same `internal/policy` rules
   the sandbox enforces: no pushes, no infrastructure mutation, no recursive
   delete of home) and on approval modes, plus path confinement to the
   project and the secret-path mask. The default approval mode (`edits`)
   applies file edits and runs known build, test and read-only commands
   without asking, and asks for everything else.
4. **Honest labels.** Direct mode is *experimental* until it has been used
   on real work and measured. Nothing in this document claims parity.
5. **Evolve, do not fork.** The loop is about 1,500 lines of Go in
   `internal/direct`, built on `inference.Gateway` and `inference.Upstream`
   (so every provider works), `internal/policy` and the ledger database.
   No code from another agent is copied.

## 3. Phases

Each phase ends with tests passing, docs and status updated, and one local
commit; the maintainer decides when to push and release.

| Phase | Goal | Exit criteria |
|---|---|---|
| **17** (done, experimental) | Direct mode foundation | `internal/direct`: loop, 7 tools, approval modes, policy, path confinement, sessions, compaction, project instructions; `bcode chat` headless and `bcode -p`; the TUI chat defaults to direct mode with `/task`, `/mode`, `/approval`, `/compact`, `/sessions`, `/resume`; unit tests for every tool and for the TUI; one CLI test against a fake OpenAI server; metering under `source=direct` and audit events |
| 18 | Streaming and the transcript | token streaming through the gateway for the local server and every cloud upstream (SSE translation for Anthropic and Gemini); the TUI renders assistant text as it arrives; markdown rendering (code blocks, lists) in the transcript; a diff preview for `edit`/`write` before approval in `ask` mode; `/undo` of the last file change (direct mode keeps a per-session shadow copy of each file it edits) |
| 19 | Extensibility | project and user **commands** (`.boundedcode/commands/*.md`, `/name args`); **skills** (`.boundedcode/skills/<name>/SKILL.md`, loaded on demand into the prompt); **MCP tools** (stdio servers from `config.yaml` or `.boundedcode/mcp.json`, reusing the MCP client from `internal/repointel`); **hooks** (pre-tool, post-tool, on-turn-end shell hooks with a JSON payload and an allow/deny result) |
| 20 | Repository intelligence in direct mode | opt-in `search_symbols`, `references` and `impact` tools backed by codebase-memory-mcp and Serena when the workspace is indexed; a measured comparison on 10 everyday tasks: tool calls, tokens and time with and without |
| 21 | The hand-off | `/task` without arguments turns the current direct session into a task: the session summary and the files touched become the task's context pack; the task's result links back to the session; the agent's "this deserves bounded mode" suggestion is based on the diff (protected paths, contract files, change size), not only on words in the request |
| 22 | Integrations | `bcode chat --events` becomes a stable JSON event protocol (documented); an ACP or JSON-RPC server (`bcode serve`) for editors; a VS Code extension or an ACP client configuration; a `bcode chat --json-schema`-style SDK surface is **not** planned before a second user asks |
| 23 | Measurement | a 20-task everyday-development suite (explain, locate, small fix, test, refactor, run-and-diagnose) with scripted acceptance, run with the local model and one cloud model, reported like the existing benchmarks: time, tokens, approvals asked, success; direct mode moves from experimental to implemented only on that evidence |

Explicitly not planned: a desktop app, chat channels, agent teams or a
multi-model "arena". They are far from the mission and would not be
measured.

## 4. Phase 17 in detail

### What was built

- `internal/direct`:
  - `direct.go`: the loop. `Runner.Send` appends the user message, calls
    the gateway with the tool schemas, executes tool calls, and stops when
    the model answers without calls, after `direct.max_turns`, or when the
    context is cancelled (`ErrInterrupted`; everything said so far is kept).
    Responses are parsed from the OpenAI shape (string or multipart
    content, `reasoning_content`, tool calls without ids).
  - `tools.go`: `read` (numbered lines, offset/limit, binary detection),
    `write`, `edit` (exact unique match or `replace_all`), `bash` (`bash
    -c`, PowerShell on Windows, own process group, timeout, output capped
    for the model), `grep` (ripgrep when present, a Go fallback that skips
    dependency and build directories), `glob` (`**` patterns, newest first)
    and `ls`. Every path is resolved against the project root through its
    longest existing prefix, so a planted symlink cannot read or write
    outside it. Secret-looking paths and `.git` are refused. `bash` runs
    `policy.CheckCommand` before anyone is asked.
  - approval: `ask` asks for every mutation and command; `edits` (default)
    applies edits inside the project, asks for protected paths (CI,
    verification and repository configuration) and for commands outside a
    list of read-only, build and test commands; `auto` asks for nothing.
    A command line with redirections, substitutions or an unknown simple
    command is never "safe".
  - `session.go`: sessions under `<data>/sessions/<project>/<id>.jsonl`,
    appended per message; compactions are recorded and reloaded in order;
    `--continue`, `--resume ID` and a listing.
  - `compact.go`: when the estimated prompt passes 75 % of the model's
    context, or when the provider reports a context-length error, the
    older messages are summarised by the model and replaced; the last six
    messages stay verbatim.
  - `prompt.go`: the system prompt (how to work, environment, git state)
    plus the first of `AGENTS.md`, `BOUNDEDCODE.md`, `CLAUDE.md`,
    `.boundedcode/instructions.md` found at the root and the user's
    `<config>/AGENTS.md`.
- `internal/cli/chat.go`: `bcode chat -p "…"` (stdin is appended as
  context), `--continue`, `--resume`, `--approval`, `--model`, `--events`
  (JSON lines) and `chat sessions`; `bcode -p "…"` is the same. Model calls
  are metered in `model_calls` with `source=direct` and the session id;
  tool starts, denials and compactions are audit events (`direct.*`),
  without file contents or outputs.
- `internal/tui`: the chat defaults to direct mode. A turn shows tool
  activity, denials, the answer and the token count; approvals are the
  usual dialog; `esc` interrupts; messages typed during a run are queued.
  `/task REQUEST` runs a bounded task from direct mode; `/mode bounded`
  makes every message a task (the previous behaviour); `/approval`,
  `/compact`, `/sessions`, `/resume`, `/status` (session tokens).
- `direct` configuration section: `approval`, `max_turns`,
  `command_timeout`, `max_tokens`, `instructions`.

### What was deliberately left out

- Streaming (phase 18). A turn shows tool activity live but the answer
  arrives whole; the gateway strips `stream` today.
- Sub-agents, plan mode, commands, skills, MCP, hooks (phase 19).
- Any claim about quality. Direct mode has only been smoke-tested on a toy
  fixture (see Evidence).

### Evidence

- `go test ./internal/direct`: loop, tools, confinement (including symlink
  escapes), policy before approval, safe-command table, consent per mode,
  grep/glob/ls, session persistence and compaction order, compaction by
  size and by provider error, interruption, max turns, response shapes.
- `go test ./internal/tui -run Chat`: direct mode default, queueing and
  interruption, `/task` from direct mode, approvals through the prompter,
  sessions/resume/approval/compact, readiness without a sandbox, use
  outside git.
- `go test ./internal/cli -run TestChat`: headless chat against a fake
  OpenAI-compatible server: tool call, project instructions in the system
  prompt, session listing and continuation, root `-p`, metering and audit
  rows.
- **Smoke runs with a real model (2026-10-24).** The development machine
  had no llama.cpp installed, so a CPU build of llama.cpp (b11853) served
  Qwen3-4B-Instruct (Q4_K_M, 16 K context) as an external server, with
  `bcode chat -p` on the evidence demo's `shop` fixture (a 10 % bulk
  discount applied at `> 10` instead of `>= 10` items):
  - "What does this package do?": `glob`, `read`, a correct two-sentence
    answer; about 10 s.
  - "Fix the bug in cart.go, then run go test": in three runs after the
    fixes below, two made the correct one-line edit (tabs preserved) and
    ran `go test ./...` without being asked (safe command); one run read
    the file, ran the tests (green on the buggy code), made no edit and
    reported the fix as done. That is the failure bounded mode exists
    for, and it shaped the last change below.
  - Three defects found and fixed by these runs: the model announced a plan
    without calling a tool (one nudge per message now asks it to act); the
    model sent spaces where the file has tabs, so `edit` failed eight times
    in a row (`edit` now matches lines ignoring indentation and re-indents
    the replacement like the file; an identical failing call is flagged
    after three repeats); `bash` and relative paths used the git root
    instead of the directory `bcode` was started in (the working directory
    is now the start directory, confinement stays at the root).
  - Every turn now ends with the list of files whose git status changed, in
    the CLI footer and the transcript, so "fixed it" with no changed file is
    visible at a glance.
  These are smoke runs on a toy fixture with a 4 B model, not a
  measurement; the everyday-development suite is phase 23.
