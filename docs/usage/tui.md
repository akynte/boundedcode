# Terminal interface

Run `bcode` (or `boundedcode`) with no arguments in a project. It opens a
full-screen interface that starts in a **chat** for that folder, like Claude
Code, Codex or OpenCode. The rest of the CLI is in the other views. Use it
instead of the CLI or alongside it: both read and write the same state
database, so a task started from one shows up in the other.

Every message is routed automatically to one of three modes
([ADR-0012](../architecture/adr/0012-automatic-mode-routing.md)):

| Mode | What happens | Chosen when |
|---|---|---|
| **chat** | the agent reads and searches the code and answers; no file changes | a question, an explanation, a review, "run the tests" |
| **code** | the change is made directly in your checkout (direct mode) | a request for a change that is small and low-risk |
| **verify** | the request runs as a task in an isolated worktree and sandbox, with verification and behavioural evidence (bounded mode) | the change concerns authentication, payments, migrations, shared contracts or infrastructure; asks for tests or proof; spans several services; or names many files |

The decision is shown with each message (`◇ code · a request for a
change`). Two switches happen on their own during a turn: a chat answer
that needs an edit becomes code at the first edit, and a code turn that is
about to change a risky path stops before the edit and hands the request
to a verified task (earlier edits of that turn are kept and listed). When
a verified task cannot run here (no git commit, set-up incomplete, no
sandbox image) the reason is shown and the request is answered in code
mode. Before its first change you are asked whether to make it directly,
without verification (`direct.verify_fallback`, [security
policy](security-policy.md#changes-that-deserve-verification)).
`/mode chat|code|verify` forces a mode and `/mode auto` restores routing;
`/task`, `/code` and `/chat` force one message.

```bash
cd ~/src/my-service
bcode                                 # same as: boundedcode tui
bcode tui -C ~/src/other-service      # another directory
bcode tui --task <id>                 # open a task directly
```

It needs an interactive terminal of at least 60×16 cells. Scripts should keep
using the CLI.

## Direct mode (chat and code)

Direct mode (experimental, [ADR-0011](../architecture/adr/0011-direct-mode.md))
is a native agent loop in the `bcode` binary. Ask about the code or describe
a change; the agent reads, searches, edits and runs commands in the folder
you started in (the git root when there is one), and answers in the
transcript:

```
❯ why does TestRetry flake?
  ⚙ grep TestRetry in internal/client
  ⚙ read internal/client/retry_test.go
  ⚙ $ go test ./internal/client -run TestRetry -count=5
◆ The test shares a package-level timer between cases; the second case
  sees the first case's fired timer. Resetting it in each case fixes it.
  · 14s · 9.2k tokens
❯ fix it and run the test again
```

What happens to your files:
- `read`, `grep`, `glob` and `ls` are confined to the project; paths that
  look like secret stores (`.env*`, keys, cloud credentials, …) and `.git`
  are not readable.
- `edit` and `write` are applied to your checkout. In the default approval
  mode (`edits`) they are not asked about, except for protected paths (CI,
  verification and repository configuration). `/approval ask` asks for every
  change with a preview; `/approval auto` asks for nothing.
- `bash` runs commands in the project as you, with the network. Known
  read-only, build and test commands (`git status`, `go test`, `npm test`,
  `pytest`, `cargo build`, …) run without asking in `edits` mode; other
  commands ask first, with the command shown. In every mode the policy
  refuses `git push`, operations on protected branches, infrastructure
  mutation (`terraform apply`, `kubectl apply`, …) and publishing.
- Nothing is committed unless you ask for it.

Each conversation is a **session**, stored per project. `/sessions` lists
them, `/resume [ID]` continues one, `/new` starts another. When the
history outgrows the working set (`direct.working_set_tokens`, 32K tokens
for a local model), the oldest tool results the model has already seen are
set aside behind a one-line placeholder, and the `recall` tool brings one
back; only when that is not enough is the conversation summarised
(`/compact` does it on request). `esc` interrupts a run; what was done so
far is kept, and messages typed during a run are queued.

The agent reads the project's instruction file (`AGENTS.md`,
`BOUNDEDCODE.md`, `CLAUDE.md` or `.boundedcode/instructions.md`) and your
own `<config>/AGENTS.md`.

Its tools are the ones other coding agents have, and a few more: read,
write, edit, multi_edit, read_many, notebook_read/edit, bash (with
background shells), grep, glob, ls, web_fetch, web_search, todo_write,
ask_user (a dialog opens), plan mode, sub-agents (`task`), skills,
save_memory, language-server symbols when Serena is enabled, and the tools
of configured MCP servers. The full matrix against Pi, Qwen Code and
Claude Code is in [tool coverage](../design/tool-coverage.md).

To keep a small local model fast and on track, direct mode also:
- keeps the system prompt identical from turn to turn, so the model
  server's prompt cache holds across turns;
- looks up the names a message mentions before the first model call, and
  sends a project map with a session's first message: the projects it
  finds in any language (a Go service, a pnpm workspace, a Cargo
  workspace, a Maven build, …), how each is built and tested, which tools
  are not installed, and the CI configuration;
- answers as it is generated (`direct.stream`);
- checks every file change right away (a Go build of the package, syntax
  checks for other languages) and puts the verdict in the tool result;
- in a git repository, names the files that its history usually changes
  together with the edited file (`direct.history_notes`).

See [configuration](configuration.md) for each setting.

Direct mode needs only the configuration and the model: no Docker, no
repository tools, and the folder need not be a git repository. When a
change is sensitive (authentication, payments, migrations, shared
contracts) or large, the agent suggests bounded mode.

From a script, `bcode chat -p "…"` (or `bcode -p "…"`) sends one message
and prints the answer; piped input is appended as context; `--continue`
and `--resume ID` continue a session; `--events` prints JSON events.

## Bounded mode (verify)

Requests routed to verify, `/task REQUEST`, and every message under
`/mode verify`, run in bounded mode. On first use in a repository, the chat registers it
as a workspace named after the directory, then indexes it in the
background. Then you describe what you want:

```
❯ return 404 instead of 500 when the user does not exist, with a test
◆ Task t20261006-1a2b3c · branch agent/t20261006-1a2b3c
  ━━ Attempt 1 fresh
  ⚙ terminal $ go test ./internal/users/...
  ⚙ file_editor str_replace internal/users/handler.go
  ✔ verification users (targeted) passed
  ✔ Done · verified with behavioural evidence · 1 attempt(s) · 38k tokens · 4m12s
  2 file(s) changed +41 -3
  /diff review  ·  /apply bring into your checkout  ·  type to follow up
```

In bounded mode, what a message does depends on the conversation's current
task:

| Current task | Your message |
|---|---|
| none, or failed or cancelled | starts a new task from your `HEAD` |
| running | is queued and sent when the run ends (`esc` interrupts the run) |
| stopped to ask (ambiguous request) or paused | is the answer or extra guidance, and the task resumes |
| completed | starts a follow-up task **from the previous task's branch**, so it builds on that work |

Your checkout is never touched while tasks run. Each task works in its own
worktree on `agent/<task>`, inside the sandbox. When you are happy with the
result, `/apply` brings the changes into your checkout: staged by default,
committed with `/apply --commit`. It is refused if your checkout has
uncommitted changes or the changes do not apply cleanly. The same is
available from the shell as `bcode task apply <id>`.

Commands (type `/` for suggestions; `tab` completes):

| Command | |
|---|---|
| `/task REQUEST` | run the request as a verified task now |
| `/undo` | restore the files changed by the last turn (repeat to go further back) |
| `/export [FILE]` | write the session's transcript as Markdown (`session-ID.md` in the project by default) |
| `/tools` (also `/skills`, `/mcp`, `/agents`) | what the agent can use here |
| `/todos` | the agent's task list |
| `/init` | write an `AGENTS.md` for the project |
| `/name args` | a project or user command template (`.boundedcode/commands/name.md`) or skill |
| `!cmd`, `!!cmd` | run a shell command here; `!` shows the agent the output, `!!` does not |
| `@path` in a message | attach a file (or image) from the project |
| `/code MESSAGE`, `/chat MESSAGE` | force one message into code or chat |
| `/mode [auto\|chat\|code\|verify]` | automatic routing (default), or a forced mode |
| `/approval [ask\|edits\|auto]` | what needs your consent in direct mode |
| `/compact` | summarise the conversation to free context |
| `/sessions`, `/resume [ID]` | list and continue direct-mode sessions |
| `/diff` | the current task's changes, coloured |
| `/apply [--commit]` | bring them into your checkout (asks first) |
| `/verify [--full]` | re-run verification |
| `/review` | frontier review of the current task (when frontier is enabled) |
| `/cancel` | cancel the current task |
| `/new` | a new session; the next task starts from `HEAD` |
| `/open` | open the task's full view (activity, diff, verification, escalations) |
| `/status` | the session's tokens, or the current task's status, attempts, tokens and time |
| `/model NAME` | use another model profile for the next runs |
| `/index` | re-index the repository |
| `/setup` | install and configure what is missing |
| `/git-init` | make the folder a git repository (when it is not one) |
| `/clear`, `/help`, `/quit` | |

Keys: `enter` sends; `alt+enter` or `ctrl+j` adds a line; `↑/↓` recall earlier
messages; `pgup/pgdn` scroll; `esc` interrupts a run, or leaves the input so
that `1`–`9` and `←` navigate.

## First run and setup

The chat shows a checklist of prerequisites until they are all met. `/setup`
opens the set-up wizard (also `m` in the System view and `p` in the Runtime
view, or "Model and provider" in the command palette):

1. **Where the model runs.** The wizard shows this machine's hardware and
   asks for a local model or a cloud model API.
2. **Local:** a list of model profiles with how each fits this machine (on
   the GPU, GPU and RAM, partly or only on the CPU), its size and status
   (validated or experimental), and the suggested one preselected. Profiles
   whose license is under review are not offered. The confirmation names the
   license and the download size.
3. **Cloud:** OpenAI, Anthropic, Gemini or an OpenAI-compatible service (with
   its URL). The API key is typed into a masked field and stored in the
   system's credential store (or an owner-only file); it is never shown,
   passed on a command line or written to a log. The wizard then lists the
   provider's models with their context windows, asks for a context window
   where the provider does not report one, and sends one short test request.
4. **Install what is missing**, after one confirmation that lists it:

`bcode setup` from the shell works through the same steps in order and skips
what is already done:

1. **Configuration:** written automatically with default paths.
2. **Repository tools:** pinned, checksum-verified codebase-memory-mcp and
   gitleaks for this OS and CPU, installed into
   `~/.local/share/boundedcode/bin`.
3. **Inference server:** the pinned llama.cpp. On Linux with git, cmake and a
   C++ compiler it is built from source (with CUDA when an NVIDIA GPU and
   the CUDA toolkit are present), which is the validated build; otherwise,
   and on macOS and Windows, the official prebuilt release of the same
   commit is downloaded and checked against its pinned sha256 (CUDA 12.8 on
   Linux with an NVIDIA GPU but no CUDA toolkit, Metal on Apple Silicon,
   CUDA 12.4 on Windows with an NVIDIA GPU, else CPU). To use a server you already
   run, set `inference.mode: external` instead. Not needed with a cloud
   provider.
4. **Model weights:** the default profile's GGUF at a pinned revision,
   checksum-verified. This is a large download; review the model license
   first.
5. **Sandbox image:** built locally with Docker from a build context embedded
   in the binary. It is never pushed.
6. **Frontier container:** only on macOS and Windows with contained frontier
   escalation enabled: a pinned Linux build of the Codex CLI, mounted into
   the frontier container in place of this machine's own codex.

Every step that downloads or builds asks first, and declining leaves it for
later. `bcode setup --check` reports the state and exits non-zero while
anything is missing; `bcode setup --yes` runs
everything without asking. `--only` names steps (`config`, `tools`,
`inference`, `model`, `sandbox`, `frontier`), and `--only STEP --force` runs a step that
already looks complete, for example `bcode setup --only inference --force`
to rebuild llama.cpp with CUDA after installing the CUDA toolkit. `--force`
never rewrites the configuration. Error messages for a missing dependency
name the step that installs it.

## How it works

The interface does not have its own implementation of any operation.

- **Actions** (run, verify, cancel, index, runtime start and so on) run the
  real CLI commands in-process. The policy, sandboxing, verification and audit
  records are therefore identical to the CLI. Each action shows the exact
  command line it ran.
- **Views** read the task ledger, the audit log and status directly and
  refresh every second. A task run by another process, such as
  `boundedcode task run` in another terminal, is shown live too.
- **Questions** that the CLI asks on the terminal appear as dialogs. These
  include frontier escalation approval (packet size, destination, packet
  path) and the confirmation before `serena setup` installs anything. Nothing
  leaves the machine without a "yes".
- **Logs** go to `<state dir>/tui.log`, never to the screen.

## Views

Switch views with `1`–`9` (with the chat input left via `esc`), the arrow keys (`←` into the sidebar, then `↑/↓`), the mouse or the command palette.

| # | View | What you can do |
|---|---|---|
| 1 | **Chat** | Describe changes and steer tasks in the current repository (see above). |
| 2 | **Tasks** | List tasks with status, phase, verification state, attempts and tokens. Filter by status (`f`) and search (`/`). Create (`n`), run or resume (`r`), request a frontier review (`e`), verify (`v` targeted, `V` full gate), cancel (`c`), clean up (`x`), store a manual frontier answer (`a`). |
| | **Task detail** (`enter`) | Six tabs. **Overview**: request, criteria, budget meters, attempts, decisions, worktrees. **Activity**: a live timeline of agent actions, verification, escalations and blocks. **Diff**: a coloured diff per repository. **Verification**: the cross-repository compatibility report, when the gate ran (each affected link with its result, sides and commits, and reason; stale results marked), then stages and failure output. **Escalations**. **Output**: the command output of runs started here. `i` interrupts a run; it can be resumed later. |
| 3 | **Workspaces** | Create (`n`) and select (`enter`) workspaces. Add repositories (`a`), enable or disable them (`d`), remove them (`D`). Index one repository (`i`) or all of them (`I`) in any index mode. |
| 4 | **Intel** | Code-graph search, trace, snippet, impact and architecture; cross-service links and endpoints; Serena symbol, refs and impls, optionally against a task worktree (`Root`). |
| 5 | **Runtime** | Inference server state (serving, sleeping, stopped), endpoint, memory and context. Model profiles with their status, size and fit for this machine. Start with the selected model (`s`), stop (`S`); use (`u`), download (`d`, after a confirmation) or delete (`x`) the selected model; model and provider settings (`p`). Inspect a model profile and its llama-server arguments (`m`). The server log tail is shown at the bottom. |
| 6 | **Frontier** | Provider settings and the Codex login state; escalation summary and history. Open the escalation's task (`enter`) or store a manual answer (`a`). |
| 7 | **Stats** | Metrics (local-only rate, usage and estimated cost per provider, escalation rate, verified tasks per hour, tokens, frontier token share) for all time, 24 hours, 7 days or 30 days (`tab`/`→` cycles). |
| 8 | **System** | Configuration paths and `doctor` checks with hints. Model and provider settings (`m`); remove the cloud provider's API key (`K`). Probe (`s`) or set up (`S`) Serena. Build the sandbox image (`b`). Initialize the configuration (`i`). |
| 9 | **Console** | Run any command, including `bench infra`, `bench tasks`, `bench direct` and `bench intel`, with tab completion and history. This view also lists every operation started from any view, with its output. Interrupt the selected operation with `x`. |

## Keys

| Key | Action |
|---|---|
| `1`–`9` | switch view (outside text inputs) |
| `←`, then `↑/↓` | move to the sidebar and switch views with the arrows; `→`/`enter` returns to the view (`←` stays inside views that use it: task tabs, the repositories pane, Intel's query kinds) |
| `ctrl+k` or `:` | command palette (fuzzy search over every action) |
| `?` | shortcuts of the current view |
| `J` | operations and their output (Console) |
| `↑/↓` `j/k`, `pgup/pgdn`, `g/G` | move and scroll; the mouse wheel also scrolls |
| `tab` / `shift+tab` | next / previous tab or form field |
| `ctrl+s` | submit a form (`enter` also submits from the last single-line field) |
| `esc` | close a dialog, leave a field or go back |
| `q` | back from a task, otherwise quit |
| `ctrl+c` | quit; asks first when operations are running and interrupts them (tasks keep their work) |

## Safety

- Run options mirror the CLI flags. The "without a container sandbox" option
  is offered only when `sandbox.kind` is `none`.
- Verifying on the host (when `sandbox.kind` is `none`) asks for confirmation
  first.
- Cancel, remove, stop and clean up ask before acting. Clean-up keeps the
  `agent/<task>` branch unless you choose to delete it.
- Quitting while a task runs interrupts it the same way `Ctrl-C` does in the
  CLI: the run lease is released and the task resumes later from its ledger.
