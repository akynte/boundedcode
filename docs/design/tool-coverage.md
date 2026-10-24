# Tool and capability coverage: BoundedCode vs. Pi, Qwen Code and Claude Code

Status: 2026-10-24. The maintainer asked that every tool and capability of
Pi, Qwen Code and Claude Code have an equivalent in BoundedCode, while the
architecture stays BoundedCode's own (ADR-0011, ADR-0012). This page is
the matrix. Competitor columns come from their public documentation and
READMEs at the date above; names are theirs. "Equivalent" means the same
job done BoundedCode's way, not a copy: every tool runs through the
`direct` runner's approval modes, permission rules, hooks, path confinement,
secret-path mask and command policy, and is metered through the gateway.

Everything below is **experimental** until the everyday-development
benchmark (design phase 23) has measured it.

## Model tools

| Capability | Pi | Qwen Code | Claude Code | BoundedCode |
|---|---|---|---|---|
| Read a file (numbered lines, offset/limit) | read | read_file | Read | `read` |
| Read images (vision models) | paste/drag | read_file | Read | `read` on png/jpg/gif/webp attaches the image; `@pic.png` too |
| Read notebooks | – | – | NotebookRead (legacy) | `notebook_read`; `read` on `.ipynb` |
| Read several files / globs | – | read_many_files | – | `read_many` |
| Create or overwrite a file | write | write_file | Write | `write` |
| Replace text in a file | edit | edit (replace) | Edit | `edit` (exact match, indentation-tolerant fallback, `replace_all`) |
| Several edits atomically | – | – | MultiEdit | `multi_edit` |
| Edit a notebook cell | – | – | NotebookEdit | `notebook_edit` (replace, insert, delete) |
| Run a shell command | bash | run_shell_command | Bash | `bash` (timeout, own process group, policy) |
| Background commands | – | (background shell) | Bash `run_in_background`, BashOutput, KillShell | `bash run_in_background`, `bash_output`, `kill_shell` |
| Search file contents | grep | grep_search / search_file_content | Grep | `grep` (ripgrep, Go fallback) |
| Find files by pattern | find | glob | Glob | `glob` |
| List a directory | ls | list_directory | LS (legacy) | `ls` |
| Fetch a URL | – | web_fetch | WebFetch | `web_fetch` (HTML to text; optional `prompt` answered by the model) |
| Web search | – | web_search | WebSearch | `web_search` (SearXNG-style endpoint, DuckDuckGo fallback) |
| Task list | – | todo_write | TodoWrite | `todo_write` (shown live; `/todos`) |
| Ask the user a question | – | – | AskUserQuestion | `ask_user` (options or free text; a dialog in the TUI, stdin in the terminal) |
| Plan mode | – (asks Pi to build it) | plan mode / exit_plan_mode | EnterPlanMode / ExitPlanMode | `enter_plan_mode`, `exit_plan_mode` (read-only until the plan is approved) |
| Sub-agents | – (extension) | task / subagents | Task (Agent) | `task` with `explore` (read-only), `general`, and custom agents from `.boundedcode/agents/*.md` |
| Skills on demand | skills | skill | Skill | `skill` tool; `.boundedcode/skills/<name>/SKILL.md` (also `.agents/skills`, `.claude/skills`, user `<config>/skills`) |
| Memory | – | save_memory | `#` memory / CLAUDE.md | `save_memory` into `<config>/AGENTS.md`; project `AGENTS.md`/`BOUNDEDCODE.md`/`CLAUDE.md` in the prompt |
| Language-server symbols | – | LSP integration | LSP (plugins) | `find_symbol`, `find_references`, `find_implementations` through Serena when enabled (ADR-0008) |
| MCP tools | – (extensions) | MCP servers | MCP servers | stdio MCP servers from `direct.mcp` or `.boundedcode/mcp.json` / `.mcp.json`, exposed as `mcp__server__tool` |

## Interaction

| Capability | Pi | Qwen Code | Claude Code | BoundedCode |
|---|---|---|---|---|
| `@file` references | yes | `@file` | `@file` | `@path` attaches text files (and images) from the project |
| `!command` shell passthrough | `!cmd`, `!!cmd` | `!cmd` | `!cmd` | `!cmd` (the agent is told), `!!cmd` (it is not) |
| Custom slash commands / prompt templates | prompt templates | custom commands | `.claude/commands`, skills | `.boundedcode/commands/*.md` (also `.claude/commands`, user `<config>/commands`), `$ARGUMENTS`, `$1…`, `` !`cmd` `` injection |
| Queue messages while working | yes | – | – | yes |
| Interrupt | Escape | Esc | Esc | esc |
| Sessions: resume, list | `/resume`, `--continue` | session management | `--continue`, `--resume` | `/resume`, `/sessions`, `--continue`, `--resume ID` |
| Compaction | `/compact` | `/compress` | `/compact` | automatic near the context limit, `/compact` |
| Undo file changes | – | checkpointing / `/restore` | `/rewind` | checkpoints per turn, `/undo`, `bcode chat undo` |
| Approval / permission modes | none (runs tools) | default, auto-edit, yolo, plan | default, acceptEdits, plan, bypassPermissions | `ask`, `edits`, `auto` + plan mode; allow/deny rules `tool(pattern)` |
| Hooks | extensions | hooks | PreToolUse, PostToolUse, UserPromptSubmit, Stop… | `pre_tool`, `post_tool`, `user_prompt`, `stop` (JSON on stdin; exit 2 blocks) |
| Headless / print mode | print, JSON | `-p` | `-p`, `--output-format` | `bcode -p`, `bcode chat -p`, `--events` (JSON lines), piped stdin as context |
| Generate the project instruction file | – | `/init` (QWEN.md) | `/init` (CLAUDE.md) | `/init` (AGENTS.md) |
| List tools, skills, agents, MCP | – | `/tools`, `/mcp`, `/agents` | `/mcp`, `/agents`, `/skills` | `/tools` (also `/skills`, `/mcp`, `/agents`), `bcode chat tools` |
| Cost / usage | – | `/stats` | `/cost` | `/status` (session tokens), `bcode stats` |
| Modes picked for you | – | – | – (auto mode for permissions) | chat / code / verify routed per message (ADR-0012) |
| Isolated, verified change | – | – | – | bounded mode: worktree, sandbox, deterministic checks, behavioural evidence |

## Not equivalent, by design

| Capability | Where | Why not |
|---|---|---|
| RPC mode, TypeScript SDK (Pi); SDKs in three languages (Qwen Code); Agent SDK (Claude Code) | all three | `--events` is the machine interface today; a stable JSON protocol and an editor server are design phase 22 |
| Desktop app, web UI, IDE plugins, chat channels (Telegram, …) | Qwen Code, Claude Code | out of the mission; phase 22 covers editors only |
| Session tree, fork, clone, share to a gist (Pi) | Pi | sessions are linear files; `/resume` of an older session is the fork; sharing a transcript is not planned |
| Agent teams, arena, computer use | Qwen Code | not planned (design document §3) |
| Hosted provider logins (`/login`) | all three | BoundedCode uses API keys in the OS keychain and the Codex subscription for frontier escalation (ADR-0009, ADR-0010) |
| Streaming output | all three | design phase 18 |
| Themes | Pi | one adaptive theme |

## Where the equivalents live

- Tools: `internal/direct/tools.go` (core), `tools_more.go` (multi_edit,
  read_many, todo, plan, ask_user, save_memory, symbols), `notebook.go`,
  `shells.go`, `web.go`, `mcp.go`, `subagent.go`, `skills.go` (skills,
  agents, commands), `attach.go` (`@file`, images), `checkpoint.go`
  (undo), `hooks.go`, `permissions.go`.
- Configuration: `direct.web`, `direct.permissions`, `direct.hooks`,
  `direct.mcp`, `direct.checkpoints` ([configuration](../usage/configuration.md#direct)).
- Tests: `internal/direct/tools_more_test.go` (one test per capability,
  including a fake MCP server and a fake web server), TUI and CLI tests.
