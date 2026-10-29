# Configuration

> Public alpha. Keys may change; `version` guards incompatible changes.

Configuration is layered, later layers winning:

1. built-in defaults (below);
2. the user config, `config.yaml` in the config directory;
3. a workspace override, `workspaces/<workspace>.yaml` in the config directory
   (see [Workspace overrides](#workspace-overrides)).

The config directory is `$XDG_CONFIG_HOME/boundedcode` (usually
`~/.config/boundedcode`), or `$BOUNDEDCODE_HOME/config` when
`BOUNDEDCODE_HOME` is set. `boundedcode init` (or the first step of
`boundedcode setup`) writes a `config.yaml` with every key; a missing file
means all defaults. `setup` also edits `inference.server_binary` and
`bench_binary` after it builds llama.cpp. Tools that `setup` installs go to
`<data dir>/bin` (usually `~/.local/share/boundedcode/bin`), which BoundedCode
puts first on its own `PATH`.

Every file is decoded strictly: an unknown key is an error, so a typo fails
loudly instead of being ignored. After decoding, the whole configuration is
validated; all problems are reported at once. Durations are Go duration
strings (`90s`, `10m`, `4h`).

## Keys

### Top level

| Key | Default | Notes |
|---|---|---|
| `version` | `1` | Config schema version. Any other value is rejected. |
| `models_dir` | empty | Directory holding GGUF model files. |
| `default_model` | `qwen3.6-35b-a3b` | Model profile used when `--model` is not given. |

### `inference`

| Key | Default | Notes |
|---|---|---|
| `provider` | `local` | `local` (llama.cpp, configured by the keys below) or a cloud API: `openai`, `anthropic`, `gemini`, `openai-compatible`. See [Model provider](#model-provider). |
| `providers.<name>` | empty | Each cloud provider's settings (below); kept when you switch providers. |
| `mode` | `managed` | Local provider only: `managed` (boundedcode starts `llama-server`) or `external` (you run an OpenAI-compatible server). |
| `server_binary` | `llama-server` | Required in managed mode. |
| `bench_binary` | `llama-bench` | Path to `llama-bench`, set by `init --llama-bench`. |
| `external_url` | empty | Required in external mode. |
| `host` | `127.0.0.1` | Managed server listen address. |
| `port` | `8765` | Managed server port, 1–65535. |
| `startup_timeout` | `5m` | Time allowed for the managed server to load the model. |
| `request_timeout` | `10m` | Bound on one completion; also detects stalls. |
| `idle_sleep` | `30m` | The managed server unloads the model after this much inactivity and reloads it on the next request. `0s` disables; negative values are rejected. |
| `reasoning_history` | `auto` | How the model's reasoning travels with the conversation. `auto`: it is sent back where the local model's chat template renders it, so the model reads its earlier turns as it wrote them and the server's prompt cache stays valid; never to cloud APIs that do not take it. `keep`: always (for an OpenAI-compatible service that asks for `reasoning_content` in tool-call turns). `drop`: never. Anthropic and Gemini replay their own thinking regardless. See [Inference protocol](inference.md#reasoning-in-the-history). |

### Local model choice

`default_model` names the local model profile (built in, or a YAML file in
`<config>/models/` that overrides one by name). `bcode model recommend` rates
the profiles against this machine and proposes one; `bcode model use NAME`
sets `default_model` (and selects the local provider), and `bcode model fetch
NAME` downloads it into `models_dir` (default `~/.local/share/boundedcode/models`).
Profiles record `status` (`validated`, `experimental`, or `review` for a
license under review, which is never offered), the pinned `source.revision`,
`source.size_bytes` and `source.sha256`, and an optional `source.license_notice`
shown before download. A user profile that overrides a built-in one for the
same file keeps the built-in revision, checksum and status. It replaces the
built-in `server` section as a whole, so server settings added to the
built-in profile later (such as `reasoning_budget` or
`op_offload_min_batch`) do not apply until the override has them too. The
prompt-cache and offload keys are in
[Inference protocol](inference.md#llamacpp-server-settings).

A profile also declares its `capabilities`: `tool_calling` (`native`,
`none`), `structured_output` (`json_schema`, `none`), `thinking` (`toggle`:
it can be turned off per request; `always`; `none`), `context_max` (the
trained context), `tokenizer` (the GGUF tokenizer family, which a draft
model must share), `speculative` (the methods the weights allow: `ngram`,
`draft`, `mtp`) and `limitations`. A user override without them keeps the
built-in ones. They are declarations: `bcode bench models` checks them
against the weights and measures what the model does with BoundedCode's
requests, and `bcode model show NAME` prints them with the memory fit.

`server.speculative` turns on speculative decoding (off by default):

```yaml
server:
  speculative:
    type: ngram-mod        # or ngram-simple, ngram-map-k, draft-simple, draft-mtp
    draft_file: Qwen3.5-0.8B-Q8_0.gguf   # draft-simple only; resolved like file
    draft_gpu_layers: 99   # optional
    n_max: 8               # optional: tokens drafted per step
```

A type the profile's `capabilities.speculative` does not allow is refused.
Whether it is faster depends on the model and the machine:
`bcode bench spec` measures it ([results](../benchmarks/adaptive-2026-10.md#speculative-decoding)).

`bcode model route` shows which measured local model the experimental model
routing would choose for chat and code requests on this machine, and why;
it never switches the server itself.

### Model provider

`bcode provider use NAME [--model ID] [...]` sets these keys, and
`bcode provider key set NAME` stores the API key (from the terminal without
echo, or stdin). The key is kept in the OS credential store (Secret Service
on Linux, the macOS Keychain, Windows Credential Manager), or in an
owner-only `credentials.json` next to this file when no credential store is
available, never in `config.yaml`. `BOUNDEDCODE_<PROVIDER>_API_KEY` (for
example `BOUNDEDCODE_ANTHROPIC_API_KEY`) overrides the stored key, for
servers and CI; vendor variables such as `ANTHROPIC_API_KEY` are not read.
`BOUNDEDCODE_SECRETS=file` skips the credential store.

```yaml
inference:
  provider: anthropic
  providers:
    anthropic:
      model: claude-opus-5-5
      effort: high
    openai-compatible:
      base_url: https://api.groq.com/openai/v1
      model: MODEL-ID
      context_window: 131072
```

| Key (`inference.providers.<name>.`) | Notes |
|---|---|
| `model` | The provider's model id. Required for the selected provider. `bcode provider models NAME` lists them. |
| `base_url` | Endpoint override; required for `openai-compatible`. OpenAI-style URLs end in the API version (`https://api.openai.com/v1`). |
| `context_window` | The model's input limit. `0` asks the provider: Anthropic and Gemini report it; OpenAI and compatible APIs do not, so set it for them. |
| `context_limit` | Cap on the agent's working context before it condenses its history; `0` = 200000. Cloud tokens are billed on every turn. |
| `effort` | `minimal`, `low`, `medium`, `high`, `xhigh` or `max`; mapped to Anthropic `output_config.effort`, Gemini `thinkingLevel` (up to `high`), OpenAI `reasoning_effort`. Empty = the model's default. |
| `input_price`, `cached_input_price`, `output_price` | USD per million tokens, for the cost estimate in `bcode stats`. No prices are built in. |

The token budget (`budgets.max_local_tokens`) applies to whichever provider
runs the task. `--model` on `task create`/`task run` overrides the provider's
model for one task.

### `agent`

| Key | Default | Notes |
|---|---|---|
| `runtime` | `openhands` | The only runtime the CLI can run. (A scripted runtime exists for Go tests and cannot be selected here.) |
| `adapter_dir` | empty | The OpenHands adapter project (`adapters/openhands/python`). Needed when `sandbox.kind` is `none`. |
| `image` | `boundedcode-openhands:local` | Sandbox image, built by `sandbox build`. |
| `max_iterations` | `150` | Agent steps per attempt; must be >= 1. |
| `condenser_max_events` | `80` | History length that triggers OpenHands' summarizing condenser. |
| `max_output_tokens` | `8192` | Cap on one model response (thinking plus visible output). Thinking alone is capped per model profile by `server.reasoning_budget`. |
| `strategy.no_progress_tokens` | `40000` | An attempt is stopped after generating this many tokens without progress (its first edit, a new test file, or an agent-run test going from failing to passing). `0` disables. |
| `strategy.max_tokens` | `50000` | Hard cap on tokens generated in one attempt. |
| `strategy.max_duration` | `35m` | Hard cap on one attempt's agent turn. |

A stopped attempt is recorded (`strategy.stopped` event, rejected strategy
with the reason), its work is checkpointed and verified as usual, the
session is compacted, and the next attempt is told what was tried and to
change approach. Stopping does not by itself escalate to the frontier.

### `task`

| Key | Default | Meaning |
|---|---|---|
| `contract` | `true` | Before the first attempt, derive a compact task contract from the request with the task's model (the local model, or the cloud provider when one is selected): what is required, alternatives the request explicitly allows, constraints, what is out of scope, open questions, and acceptance evidence. It is shown to the agent; the request stays authoritative. HTML comments (issue-template instructions) are removed from the request first, and a contract that names nothing required is asked for once more. |
| `structured_output` | `true` | The calls whose JSON the control plane parses (the contract, its ambiguity check, verification's counterexample tests) send the reply's JSON schema, as each provider takes it: `response_format` (llama.cpp, OpenAI), `output_config.format` (Anthropic), `responseJsonSchema` (Gemini). A provider that rejects it is asked again without. `false`: the prompt alone asks for the JSON. See [Inference protocol](inference.md#structured-output). |
| `ambiguity` | `ask` | What a *material* ambiguity does (plausible readings that change behaviour, an API, data, security, compatibility, tests or output; implementation choices do not count). Before either policy applies, each material ambiguity is checked against the request text with one more local-model call: it is dropped, and the reason recorded as a task decision, when the request's own words settle it (for example an expected output) or when fewer than two of its readings are supported by a quote from the request. If that check fails, the ambiguity stays material. `ask`: the task blocks before implementation with the questions (SPEC_AMBIGUOUS); answer with `task run TASK --clarify "..."`. `proceed`: the ambiguity is recorded and the agent states and demonstrates the reading it chose (used by benchmarks). Explicitly allowed alternatives never block. |

### `task.verification`

The optional verification layers of a bounded task ([Verification](verification.md#verification-layers)).

| Key | Default | Meaning |
|---|---|---|
| `mutation` | `on` | Mutation assessment of the changed lines: `off`, `on` (reported, the label is not lowered), `required` (`TASK_VERIFIED` needs it established), `strict` (and every boundary mutant killed). |
| `mutation_mutants` | `8` | Mutants per repository. |
| `mutation_budget` | `5m` | Time per repository. |
| `counterexamples` | `off` | Independent tests from the task's model in a separate context, run on the change and the base: `off`, `on`, `required`. |

### `direct`

Direct mode: the native agent loop that works in your checkout (the chat's
default; `bcode chat -p`). See [the terminal interface](tui.md#direct-mode)
and [ADR-0011](../architecture/adr/0011-direct-mode.md).

| Key | Default | Meaning |
|---|---|---|
| `approval` | `edits` | What needs your consent. `ask`: every file change and every shell command. `edits`: file edits inside the project and known read-only, build and test commands (`git status`, `go test`, `npm test`, `pytest`, `cargo build`, …) run without asking; edits to protected paths (CI, verification and repository configuration) and other commands ask. `auto`: ordinary work does not ask. In every mode the command policy refuses pushes, protected-branch operations, infrastructure mutation and publishing, paths are confined to the project, and secret-looking paths are not readable. Sensitive operations (reading credentials, network transfers, installing named packages, changing agent instructions or memory, git hooks and execution configuration, privilege, CI configuration) need a permission rule, your words or your approval in every mode, `auto` included: see the [security policy](security-policy.md). |
| `mode` | `auto` | How a message is handled. `auto` routes each message to `chat` (answer without changing files), `code` (change the checkout directly) or `verify` (a bounded task: worktree, sandbox, verification, evidence) from its intent, its risk (the `escalation` keyword lists, risky paths such as authentication, payments, migrations, contracts, infrastructure, protected configuration) and whether a bounded task can run here. `chat`, `code` or `verify` forces one mode. See [ADR-0012](../architecture/adr/0012-automatic-mode-routing.md). |
| `verify_fallback` | `ask` | What happens to a change that deserves a verified task when one cannot run here. `ask`: you decide before the first change of the turn, and per risky path; with no one to ask it is refused. `refuse`: such changes are refused. `allow`: they are made directly, and audited. See the [security policy](security-policy.md#changes-that-deserve-verification). |
| `max_files` | `8` | A change that names more files than this is routed to `verify`. |
| `max_turns` | `80` | Model calls one message may cause (each tool round trip is one). |
| `command_timeout` | `3m` | A shell command is killed after this (the model may ask for a shorter one). |
| `max_tokens` | `0` | Processed tokens per session (`0` = unlimited); the session stops with an error when spent. |
| `instructions` | `AGENTS.md`, `BOUNDEDCODE.md`, `CLAUDE.md`, `.boundedcode/instructions.md` | Project instruction files tried at the project root, in order; the first that exists is part of the system prompt (up to 24 KB). Your own `<config>/AGENTS.md` is added after it. |

The model is the configured provider (`inference`); `--model` or `/model`
override it per session. `agent.max_output_tokens` caps one response here
too. Sessions are stored under `<data>/sessions/<project>/`.

Further keys under `direct` (see the [tool coverage](../design/tool-coverage.md)):

| Key | Default | Meaning |
|---|---|---|
| `web.allow_hosts` | none | Hosts `web_fetch` may read without asking (`docs.example.com`, or `.example.com` for subdomains). Other hosts ask in `ask` and `edits` modes. |
| `web.search_url` | none | A SearXNG-style JSON endpoint with `{query}` for `web_search`; without it DuckDuckGo's HTML endpoint is used. |
| `permissions.allow`, `permissions.deny` | none | Rules `tool(pattern)`: `bash(go test *)`, `edit(src/**)`, `web_fetch(docs.example.com)`, `read(.env*)`. Deny refuses the call; allow skips the approval prompt. The command policy still applies. Sensitive operations take category rules: `network(api.github.com)`, `install(lodash)`, `credentials(.env.test)`, `instructions(AGENTS.md)`, `exec-config(core.hooksPath)`, `privilege(sudo)`, `protected(.github/workflows/*)`; a tool rule such as `bash(curl *)` does not authorize them ([security policy](security-policy.md#authorization)). |
| `hooks` | none | List of `{event, matcher, command, timeout}`. Events: `pre_tool` (exit 2 or `{"decision":"deny"}` blocks the tool), `post_tool` (stdout is shown to the model), `user_prompt` (exit 2 blocks the message, stdout is appended), `stop` (exit 2 asks the model to continue with stderr as the reason). `matcher` is a regular expression on the tool name. The event's data arrives as JSON on stdin. |
| `mcp` | none | Map of MCP servers started over stdio: `{command, args, env, cwd, timeout}`. Their tools are offered as `mcp__<server>__<tool>` and ask before running (except in `auto`). A project's `.boundedcode/mcp.json` or `.mcp.json` (`{"mcpServers": {...}}`) adds servers, which start only once you approve their definition; tool definitions are pinned, and a changed or new tool is withheld until you approve it (`<data>/mcp-trust.json`, [security policy](security-policy.md#mcp-servers)). |
| `sandbox.mode` | `auto` | `auto`: commands run in the strongest sandbox available (bubblewrap, then Landlock, on Linux); `off`: on the host. See [the command sandbox](direct-sandbox.md). |
| `sandbox.fallback` | `ask` | When containment is weaker than strong, commands beyond read-only utilities need authorization: `ask` once per session (refused when no one can be asked), `refuse`, or `allow` (audited). |
| `sandbox.network` | `proxy` | `proxy`: no network except allowed hosts through BoundedCode's proxy; `none`: no hosts; `host`: unconfined. |
| `sandbox.allow_hosts` | none | Hosts commands may always reach (`docs.example.com`, `.example.com` for subdomains). |
| `sandbox.registries` | `true` | Commands may reach the package registries of the project's ecosystems and of the package managers they run. |
| `sandbox.backend` | auto | Force `bwrap`, `landlock` or `none` (a weaker choice is your authorization). |
| `sandbox.read_paths`, `sandbox.write_paths` | none | Further host paths commands may read, or read and write (a toolchain or cache in an unusual place). |
| `sandbox.env` | none | Environment variables passed to commands although they look secret (`GH_TOKEN`). |
| `checkpoints` | `true` | Keep a copy of every file before direct mode changes it, per turn, for `/undo` and `bcode chat undo`. |
| `working_set_tokens` | `0` (32K local, 96K cloud) | The history budget the model is asked to attend to. Before each model call the oldest tool results the model has already seen are set aside (a one-line placeholder; the `recall` tool brings one back), and only if that is not enough is the history summarised. Never above three quarters of the context window. |
| `max_tool_output` | `0` (16 KiB local, 48 KiB cloud) | Cap on one tool result in bytes; longer results keep their head and tail, and the whole result stays available to `recall`. |
| `prune` | `true` | Prune command output that has errors, failing tests, stack traces, progress, downloads or passing tests to sort: the failures, their locations and assertions, the project's stack frames and the outcome are kept, what was dropped is named, and the whole output stays available to `recall` by line range or pattern ([Context and session state](context.md)). |
| `observation_bytes` | `0` (8 KiB local, 24 KiB cloud) | The size such output is pruned to. |
| `compaction` | `structured` | How the history is summarised when setting results aside is not enough. `structured`: the session state, the record built from the session (the user's requests and constraints verbatim, files read and changed with their blobs, symbols, test and build runs, the checkout) plus the model's notes (status, findings, decisions, pending, questions, next step), asked for by continuing the conversation so the prompt cache serves it. `legacy`: a free-text summary of a transcript, as before. |
| `parallel_tools` | `0` (4) | How many calls of one model reply that change nothing (reads, searches, symbol lookups, fetches) run at once. `1` runs every call in turn. Edits, commands and every other call always run alone, in order ([capabilities](../architecture/capabilities.md#direct-modes-tool-pipeline)). |
| `routing` | `off` | `adaptive` (experimental): the local model's thinking is chosen per model call from the call's role. No model call for the router's classification (the rules decide); thinking off for answering a question and for choosing what to read; the model's own for planning, editing and debugging. Only for a model whose profile says `thinking: toggle`. Approval, the guard and verification do not depend on it ([measured](../benchmarks/adaptive-2026-10.md#routing)). |
| `routing_thinking` | empty | Overrides `routing`'s choice per role: `classify`, `plan`, `explore`, `edit`, `debug`, `answer`, each `on` or `off` (`classify` also `skip`). |
| `mcp_tool_search` | `auto` | `auto`: when the MCP servers' tool definitions would take more than a tenth of the working set, they are not listed; the model finds them with `mcp_search` and runs them with `mcp_call`, under the same trust pins, rules and approval. `on` always, `off` never. `bcode bench mcpsearch` measures both with your model ([benchmark](../benchmarks/orchestration-2026-10.md#mcp-tool-search)). |
| `seed_pack` | `true` | Before the first model call of a message, look up the names it mentions (identifiers, paths, quoted errors) in the project and send the places found with the message, definitions first, so the model starts at the right file instead of searching. A session's first message also carries a project map: the projects in the top four directory levels in any language, with their build and test commands, package manager and any missing tool; the CI configuration; top-level directories with source-file counts; top-level files. |
| `retrieval` | `adaptive` | How that lookup works. `adaptive`: strategies chosen from the message and the evidence collected so far (stack traces, compiler and test-runner output, failing tests, paths, module names, symbols, routes, environment variables, named commits, and path words as a fallback), ranked by reliability, with provenance, in any language ([Retrieval](retrieval.md)). `basic`: the earlier lookup, a grep of the names the message mentions. |
| `history_notes` | `true` | After a file change in a git repository, name up to three files its history usually changes together with that file (in at least 5 of the last 1,000 commits and 70% of the commits that changed it), so the model can check whether the change needs them too. |
| `stream` | `true` | Show the answer as it is generated (llama.cpp, OpenAI, OpenAI-compatible services, Anthropic and Gemini). |
| `checks` | `true` | After every file change, run the cheapest check for its language (a Go parse and `go build` of the package, `python3`/`node`/`bash`/`ruby`/`php` syntax checks when installed, JSON and YAML parsing) and tell the model the result. |

Project files direct mode reads: `.boundedcode/skills/<name>/SKILL.md`
(skills; also `.agents/skills`, `.claude/skills`), `.boundedcode/agents/*.md`
(sub-agents: frontmatter `name`, `description`, `read_only`, `tools` as a
comma-separated list of tool names to allow; the body is the agent's prompt), `.boundedcode/commands/*.md` (command templates with
`$ARGUMENTS`, `$1`…, `` !`cmd` ``; also `.claude/commands`). The user's
own live under `<config>/skills`, `<config>/agents`, `<config>/commands`;
`<config>/AGENTS.md` holds saved memory.

### `repointel`

| Key | Default | Notes |
|---|---|---|
| `provider` | `codebase-memory-mcp` | The only supported value. The key is kept so older files still load. |
| `binary` | `codebase-memory-mcp` | The integration is tested against release 0.11.0 (`doctor` compares). |
| `cross_service` | `true` | Built-in cross-service contract analyzers in indexing, context packs and escalation. |
| `compat_gate` | `true` | Cross-repository compatibility gate (experimental). After the full gate passes, each gRPC, protobuf or OpenAPI link that the change affects is checked with the dependent repository's own checks, built against the other task repositories' candidate commits. A `broken` link fails verification; an `untested` one withholds `TASK_VERIFIED`. Needs `cross_service`. Not overridable per workspace. See [cross-repo-compatibility.md](../design/cross-repo-compatibility.md). |

#### `repointel.serena`

Optional LSP navigation; see [serena.md](serena.md).

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | |
| `command` | empty | Serena executable. Empty means the copy `serena setup` installed. |
| `version` | `1.7.0` | Must be `1.7.0`. Other versions fail validation. |
| `auto_upgrade` | `false` | Must be `false`. |
| `transport` | `stdio` | Must be `stdio`. |
| `max_instances` | `2` | Must be >= 1. |
| `idle_timeout` | `10m` | Must be > 0 when enabled. |
| `startup_timeout` | `90s` | Must be > 0 when enabled. |
| `call_timeout` | `30s` | Must be > 0 when enabled. |

### `frontier`

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | Off means local-only: nothing leaves the machine. |
| `provider` | `codex` | `codex` or `manual` (packets are written to disk). |
| `binary` | `codex` | |
| `model` | empty | Empty uses the provider default. |
| `require_approval` | `true` | Ask before sending each packet. |
| `max_packet_tokens` | `24000` | Escalation packet cap; must be >= 1. |
| `timeout` | `15m` | |
| `contain` | `true` | Run the frontier CLI in a container that sees only the packet. |
| `data` | `full` | What a packet may carry. `full`: the context pack, with code, the diff and contracts (host paths rewritten, secrets redacted). `abstract`: the task, the verification failures and the strategies tried, with every line that repeats the repositories' source withheld, and the change as files and line counts; a packet that still repeats a source line is refused. Identifiers, paths and error messages remain, so `abstract` is not proof that the source cannot be inferred ([measured](../benchmarks/adaptive-2026-10.md#privacy)). |

A repository can refuse external models altogether with
`.boundedcode/data-policy.yaml`:

```yaml
external_models: deny   # allow (the default) or deny
```

It is read from your checkout, never from an agent's worktree, and a task's
change can never edit it (`.boundedcode/` is a protected path). With `deny`,
no escalation packet is built for a task that includes the repository, and
direct mode refuses a cloud provider in it. A file that does not parse
denies.

### `sandbox`

| Key | Default | Notes |
|---|---|---|
| `kind` | `docker` | `docker` or `none`. `none` runs agent tools on the host and is for development only. |
| `engine` | `docker` | `docker` or `podman`. |
| `network` | `none` | `none` or `bridge`. |
| `memory` | `8g` | Container memory limit. |
| `cpus` | `8` | Container CPU limit. |

### `budgets`

When a budget runs out, the task is parked as blocked. Work is never thrown away.

| Key | Default | Notes |
|---|---|---|
| `max_attempts` | `6` | Must be >= 1. |
| `max_wall_clock` | `4h` | Must be >= 0. |
| `max_local_tokens` | `4000000` | Must be >= 0. |
| `max_escalations` | `2` | Must be >= 0. |
| `context_pack_tokens` | `24000` | Must be >= 2000. |

### `escalation`

| Key | Default | Notes |
|---|---|---|
| `policy` | `rules` | `rules`, or `adaptive` (experimental): no escalation once no attempt is left to apply the advice; the same failure is escalated once; an attempt that changed nothing escalates at once; architectural risk (Z1) waits for a failed attempt unless the contract has unresolved ambiguities. Z3, Z4, the budget and approval are unchanged ([replayed](../benchmarks/adaptive-2026-10.md#escalation)). |
| `failed_attempts` | `3` | Z2: consecutive failed verification attempts before escalating; >= 1. |
| `rejected_strategies` | `2` | Z2: rejected, materially different strategies before escalating; >= 1. |
| `architectural_risk` | idempotency, outbox, ledger, payment, auth, … | Z1 keywords matched against task text, paths and symbols. |
| `high_risk_review` | auth, crypto, payment, ledger, terraform, … | Z3 keywords. |

Run `boundedcode init` and read the generated file for the full keyword lists.

### `languages` and `build_systems`

Languages and build systems beyond the built-in ones: see
[Languages](languages.md#adding-a-language). Only this file can set them.

| Key | Meaning |
|---|---|
| `languages[].id`, `name` | Identifier and display name. |
| `languages[].extensions`, `filenames`, `shebangs` | How its files are recognised (at least one). |
| `languages[].definitions`, `tests` | Regular expressions: definition lines (a group named `name` is the symbol) and test file paths. |
| `languages[].check` | `{what, tools, args, root}`: the check after each edit; `{file}`, `{dir}`, `{pkg}`, `{root}` are substituted. |
| `languages[].format`, `lint`, `lsp` | Executables. |
| `build_systems[].id`, `kind`, `markers`, `languages`, `tool`, `build`, `test` | Projects are directories with a marker (a file name or `*.ext`). |

## Workspace overrides

A workspace can change task policy without touching the user config. Put the
file at `<config dir>/workspaces/<workspace>.yaml`, for example
`~/.config/boundedcode/workspaces/payment-platform.yaml`:

```yaml
budgets:
  max_attempts: 8
  max_escalations: 0        # never escalate for this workspace
escalation:
  high_risk_review: [ledger, money, settlement]
repointel:
  serena:
    enabled: true
frontier:
  enabled: false
```

Only these keys are allowed:

- `budgets.*`
- `escalation.*`
- `repointel.cross_service`
- `repointel.serena.enabled`
- `frontier.enabled`

Machine settings (inference, sandbox, binaries, Serena pins) stay global.
Verification commands are set per repository, in
`.boundedcode/verification.yaml`, not here.

A key you leave out keeps its value from the user config. A list you set
replaces the user's list; it is not merged with it. The file is decoded
strictly, and the merged result goes through the same validation as
`config.yaml`. Without the file, the workspace uses the user config as is.

## Repository verification (`.boundedcode/verification.yaml`)

Each repository can define its own verification stages. The file is read
from the task's **base commit**, never from the worktree, so the agent cannot
change how its own work is judged; a change to it takes effect for tasks that
start after it is committed. Without the file, the [built-in
presets](#built-in-presets) are used for the languages found at the base
commit. The file is decoded strictly: unknown keys are an error.

```yaml
version: 1
max_changed_files: 200      # diff-scope bound; 0 means 200
deny_paths: ["migrations/*"]    # extra patterns the change must not touch
stages:
  - name: go-test
    run: [go, test, -count=1, "{packages}"]
    scope: always           # always (default) | targeted | full
    timeout: 20m            # default 10m
    requires: [go.mod]      # the stage applies only if these files exist
    tests: true             # runs tests (see behavioural evidence)
    optional: false         # skip instead of fail when the tool is missing
    dir: ""                 # run in this subdirectory (a project in a monorepo)
```

| Key | Meaning |
|---|---|
| `name`, `run` | Required. `run` is an argument vector, not a shell line; use `[sh, -c, "..."]` for a script. Every command goes through the same command policy as the agent's commands. `{packages}` expands to the impact-selected Go packages (or `./...` in the full gate); `{changed}` to the change's files (deleted ones included), for per-file checks such as formatting. |
| `scope` | `targeted` stages run only while iterating, `full` stages only in the full gate before a task becomes a merge candidate, `always` in both. |
| `requires` | Files that must exist for the stage to apply. A required file that exists at the base and is deleted by the change fails the stage, so a change cannot switch a stage off. A stage whose first required file is `go.mod`, or whose command is `go test`, is treated as a Go stage by behavioural evidence (per test function). |
| `tests` | Marks a stage that runs tests. Behavioural evidence reruns such stages on the base commit with and without the change's tests. Without the key, a stage whose name or command mentions "test" counts. |
| `deny_paths` | Repository-relative patterns in Go `filepath.Match` syntax: `*` does not cross `/`, and there is no `**`. A changed file that matches fails the diff scope. |
| `optional` | The stage is skipped, not failed, when its tool is not installed (exit code 127). |
| `dir` | The directory the stage runs in, relative to the repository root (for a project in a monorepo). `requires` are relative to it, and `{packages}` lists the impact-selected packages under it. It must stay inside the repository; a path or symbolic link that leaves it fails the stage. |

The `diff-scope` and `secret-scan` (gitleaks) stages always run in addition.
Stages run in the sandbox without network access; dependencies come
read-only from the repository checkout (for example its `node_modules`).

### Built-in presets

Without a `verification.yaml`, stages are chosen from the files at the base
commit, for every language found; a repository with several gets the stages
of each.

A repository whose root is no project and has no `Makefile` (a monorepo
with, say, `api/go.mod` and `web/package.json`) gets the stages of each
outermost project up to three levels down, named after its directory
(`api/go-test`, `web/npm-test`) and run in it. Projects under `vendor`,
`node_modules`, `third_party`, `testdata`, `fixtures` and hidden
directories are left out, and at most 16 projects get stages. A repository
with a project or a `Makefile` at its root keeps the root's stages only. The toolchains are in the sandbox image (`boundedcode sandbox
build`; `boundedcode setup` rebuilds an image built from an older
definition).

| Language | Detected by (repository root) | Stages | Dependencies (offline) |
|---|---|---|---|
| Go | `go.mod` | `gofmt` (the Go files the change touched), `go-build`, `go-vet`, `go-test`, `golangci-lint` (full gate, optional) | the host's module cache, or the module's own `vendor/` when it has `vendor/modules.txt` |
| JavaScript/TypeScript | `package.json`, `tsconfig.json` | `tsc`, `npm-lint`, `npm-test`, `npm-build` (full gate), each when the project declares it | the checkout's `node_modules` (needed only when `package.json` declares dependencies) |
| Python | Python test files (`test_*.py`, `*_test.py`, `conftest.py`) | `python-test`: pytest, or `unittest` when the project does not use pytest | the checkout's `.venv` or `venv` |
| Rust | `Cargo.toml` | `cargo-build`, `cargo-test` | the host's Cargo registry (`~/.cargo/registry`, `~/.cargo/git`) |
| Java (Maven) | `pom.xml` | `maven-compile`, `maven-test` | the host's `~/.m2/repository` |
| Java/Kotlin (Gradle) | `build.gradle(.kts)`, `settings.gradle(.kts)` | `gradle-compile`, `gradle-test` (with `./gradlew` when present) | the host's `~/.gradle` caches and wrapper distributions |
| C/C++ | `CMakeLists.txt`, `meson.build`, `configure.ac`, or a `Makefile` with C/C++ sources | `cmake-build` + `ctest`, `meson-build` + `meson-test`, `autotools-build` + `make-check`, or `make-build` | none (system libraries must be in the image) |
| Ruby | `Gemfile`, `Rakefile`, `*.gemspec` | `ruby-test`: RSpec, `rake test`, or the `test/` files with minitest | the checkout's `vendor/bundle` (`bundle config set --local path vendor/bundle`) |
| PHP | `composer.json` | `php-test`: PHPUnit or Pest | the checkout's `vendor/` (`composer install`) |
| Terraform, Helm | `*.tf`, `Chart.yaml` | `terraform-fmt`, `helm-lint` (optional) | — |
| Any other | a `Makefile` with a `test` or `check` target | `make-test` | — |

Notes:

- A test stage whose dependencies are not installed fails and says how to
  install them; skipping it would pass a change with no tests run. The same
  goes for a tool missing from an outdated sandbox image.
- Java builds use JDK 11, 17 or 21: the one the Gradle wrapper version runs
  on, or the Java level the `pom.xml` declares (8 and earlier build with 11).
- A Rust toolchain pinned in `rust-toolchain.toml` that is not in the image
  is replaced by the image's (installing it would need the network).
- A Python venv created from a self-contained interpreter (uv, pyenv, conda)
  is used with that interpreter, mounted read-only. One created from the
  system Python works when its version matches the image's (3.13);
  otherwise recreate it with `uv venv --python 3.X`. The tree under test
  always comes first on the import path, ahead of any installed copy of the
  project.
- Only Go has formatter and linter stages: a newer formatter or lint rule
  would fail the untouched base of other projects.
- When nothing applies, verification reports a skipped `tests` stage saying
  that no test runner was found, rather than passing silently.
- Behavioural evidence (a changed test that fails on the base and passes on
  the change) compares failures per test for Go, pytest, unittest, Cargo,
  Maven Surefire, Gradle, minitest, RSpec, PHPUnit, CTest and Meson, and
  per stage otherwise. Rust unit tests inside a source file (`#[cfg(test)]`)
  cannot be evidence: the file holds the code under test too.

## Workspace repositories

Workspace membership is stored in the state database, not in config files:

```bash
boundedcode workspace add PATH [--name NAME]
boundedcode workspace disable REPO   # excluded from new tasks, indexing and queries
boundedcode workspace enable REPO
boundedcode workspace remove REPO    # only if no task ever used it
boundedcode workspace show           # lists disabled repositories, marked
```

`disable` is the reversible option. The repository keeps its id, index and
task history, and tasks that already have a worktree in it keep running.
`remove` is refused once any task has a worktree in the repository, even a
finished task, because task history refers to the repository record. Use
`disable` for those.
