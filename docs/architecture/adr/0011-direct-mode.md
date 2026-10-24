# ADR-0011: Direct mode, a native agent loop for everyday development

Status: accepted (2026-10-24). Amends ADR-0001 ("BoundedCode does not
implement a new agent loop") for everyday work; bounded mode keeps the
OpenHands loop and everything ADR-0001, 0003 and 0004 decide.

## Context
Until now every request became a task: worktree, container, contract,
context pack, checks and evidence (ADR-0003, ADR-0004). That is what makes a
change trustworthy, and it is too slow and too heavy for most of a
developer's day (questions, small fixes, running tests, folders that are not
committed git repositories, machines without Docker). The maintainer asked
for an everyday coding agent that competes with Pi, Claude Code, OpenCode
and Qwen Code, with the existing control plane as the advantage for
sensitive changes ([design](../../design/everyday-agent.md)).

OpenHands cannot fill this role: it runs as a Python process in the sandbox
image, needs the container or a `uv` environment, and its tools and
approvals are its own. Reusing it outside the sandbox would give neither the
speed nor the control over approvals that an everyday agent needs.

## Decision
1. **Two modes.** *Direct mode* is a native Go agent loop
   (`internal/direct`) that works in the user's checkout with `read`,
   `write`, `edit`, `bash`, `grep`, `glob` and `ls`. *Bounded mode* is the
   existing task runner. The chat defaults to direct mode; `/task` and
   `/mode bounded` reach bounded mode. `bcode chat -p` and `bcode -p` are
   the headless form.
2. **One gateway, one policy.** Direct mode calls the model through
   `inference.Gateway`, so every provider of ADR-0010 works unchanged, usage
   is metered (`model_calls.source = direct`) and budgets apply. Shell
   commands pass `policy.CheckCommand` before approval is even asked; paths
   are confined to the project root through their resolved real path;
   secret-looking paths (`policy.IsSecretPath`) and `.git` are not readable.
3. **Approval modes instead of a container.** `ask`, `edits` (default: file
   edits inside the project and known read-only, build and test commands
   run without asking; protected paths and other commands ask) and `auto`.
   The deterministic policy applies in every mode. Direct mode does not
   claim containment: the agent runs as the user, with the network.
4. **Sessions are files, not ledger rows.** A session is a newline-
   delimited JSON file under the data directory, appended per message,
   so an interrupted turn loses nothing and any session can be continued.
   Tasks stay the unit of the ledger; a session may later be handed to a
   task (design phase 21).
5. **Project instructions are honoured.** The first of `AGENTS.md`,
   `BOUNDEDCODE.md`, `CLAUDE.md` and `.boundedcode/instructions.md` at the
   root, and the user's own `<config>/AGENTS.md`, go into the system prompt.
6. **Status: experimental** until measured on an everyday-development
   suite (design phase 23).

## Consequences
- The "no new agent loop" rule of ADR-0001 now applies to bounded mode
  only. Direct mode is small (about 1,500 lines) and has no agent framework
  dependency.
- Direct mode works without Docker, without repository tools and in
  folders that are not git repositories; the set-up checklist requires only
  the configuration and the model for it.
- Edits in direct mode land in the checkout immediately. `/task` remains
  the way to get an isolated branch, deterministic checks and evidence.
- Streaming, commands, skills, MCP tools and hooks are separate phases
  ([design](../../design/everyday-agent.md#3-phases)); none is implied by
  this decision.

## Evidence
Unit tests in `internal/direct`, chat tests in `internal/tui` and a
headless test against a fake OpenAI-compatible server in `internal/cli`
(listed in the design document). No real-task validation yet.
