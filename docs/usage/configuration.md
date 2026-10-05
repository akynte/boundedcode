# Configuration

> Pre-alpha. Keys may change; `version` guards incompatible changes.

Configuration is layered, later layers winning:

1. built-in defaults (below);
2. the user config, `config.yaml` in the config directory;
3. a workspace override, `workspaces/<workspace>.yaml` in the config directory
   (see [Workspace overrides](#workspace-overrides)).

The config directory is `$XDG_CONFIG_HOME/boundedcode` (usually
`~/.config/boundedcode`), or `$BOUNDEDCODE_HOME/config` when
`BOUNDEDCODE_HOME` is set. `boundedcode init` writes a `config.yaml` with
every key; a missing file means all defaults.

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
| `mode` | `managed` | `managed` (boundedcode starts `llama-server`) or `external` (you run an OpenAI-compatible server). |
| `server_binary` | `llama-server` | Required in managed mode. |
| `bench_binary` | `llama-bench` | Path to `llama-bench`, set by `init --llama-bench`. |
| `external_url` | empty | Required in external mode. |
| `host` | `127.0.0.1` | Managed server listen address. |
| `port` | `8765` | Managed server port, 1–65535. |
| `startup_timeout` | `5m` | Time allowed for the managed server to load the model. |
| `request_timeout` | `10m` | Bound on one completion; also detects stalls. |
| `idle_sleep` | `30m` | The managed server unloads the model after this much inactivity and reloads it on the next request. `0s` disables; negative values are rejected. |

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
| `contract` | `true` | Before the first attempt, derive a compact task contract from the request with the local model: what is required, alternatives the request explicitly allows, constraints, what is out of scope, open questions, and acceptance evidence. It is shown to the agent; the request stays authoritative. |
| `ambiguity` | `ask` | What a *material* ambiguity does (plausible readings that change behaviour, an API, data, security, compatibility, tests or output). `ask`: the task blocks before implementation with the questions (SPEC_AMBIGUOUS); answer with `task run TASK --clarify "..."`. `proceed`: the ambiguity is recorded and the agent states and demonstrates the reading it chose (used by benchmarks). Explicitly allowed alternatives never block. |

### `repointel`

| Key | Default | Notes |
|---|---|---|
| `provider` | `codebase-memory-mcp` | The only supported value. The key is kept so older files still load. |
| `binary` | `codebase-memory-mcp` | The integration is tested against release 0.11.0 (`doctor` compares). |
| `cross_service` | `true` | Built-in cross-service contract analyzers in indexing, context packs and escalation. |

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
| `failed_attempts` | `3` | Z2: consecutive failed verification attempts before escalating; >= 1. |
| `rejected_strategies` | `2` | Z2: rejected, materially different strategies before escalating; >= 1. |
| `architectural_risk` | idempotency, outbox, ledger, payment, auth, … | Z1 keywords matched against task text, paths and symbols. |
| `high_risk_review` | auth, crypto, payment, ledger, terraform, … | Z3 keywords. |

Run `boundedcode init` and read the generated file for the full keyword lists.

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
