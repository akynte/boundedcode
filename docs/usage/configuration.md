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
same file keeps the built-in revision, checksum and status.

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
| `contract` | `true` | Before the first attempt, derive a compact task contract from the request with the local model: what is required, alternatives the request explicitly allows, constraints, what is out of scope, open questions, and acceptance evidence. It is shown to the agent; the request stays authoritative. HTML comments (issue-template instructions) are removed from the request first, and a contract that names nothing required is asked for once more. |
| `ambiguity` | `ask` | What a *material* ambiguity does (plausible readings that change behaviour, an API, data, security, compatibility, tests or output; implementation choices do not count). Before either policy applies, each material ambiguity is checked against the request text with one more local-model call: it is dropped, and the reason recorded as a task decision, when the request's own words settle it (for example an expected output) or when fewer than two of its readings are supported by a quote from the request. If that check fails, the ambiguity stays material. `ask`: the task blocks before implementation with the questions (SPEC_AMBIGUOUS); answer with `task run TASK --clarify "..."`. `proceed`: the ambiguity is recorded and the agent states and demonstrates the reading it chose (used by benchmarks). Explicitly allowed alternatives never block. |

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
```

| Key | Meaning |
|---|---|
| `name`, `run` | Required. `run` is an argument vector, not a shell line; use `[sh, -c, "..."]` for a script. Every command goes through the same command policy as the agent's commands. `{packages}` expands to the impact-selected Go packages (or `./...` in the full gate). |
| `scope` | `targeted` stages run only while iterating, `full` stages only in the full gate before a task becomes a merge candidate, `always` in both. |
| `requires` | Files that must exist for the stage to apply. A required file that exists at the base and is deleted by the change fails the stage, so a change cannot switch a stage off. A stage whose first required file is `go.mod` is treated as a Go stage by behavioural evidence. |
| `tests` | Marks a stage that runs tests. Behavioural evidence reruns such stages on the base commit with and without the change's tests. Without the key, a stage whose name or command mentions "test" counts. |
| `deny_paths` | Repository-relative patterns in Go `filepath.Match` syntax: `*` does not cross `/`, and there is no `**`. A changed file that matches fails the diff scope. |
| `optional` | The stage is skipped, not failed, when its tool is not installed (exit code 127). |

The `diff-scope` and `secret-scan` (gitleaks) stages always run in addition.
Stages run in the sandbox without network access; dependencies come
read-only from the repository checkout (for example its `node_modules`).

### Built-in presets

Without a `verification.yaml`, stages are chosen from the files at the base
commit, for every language found; a repository with several gets the stages
of each. The toolchains are in the sandbox image (`boundedcode sandbox
build`; `boundedcode setup` rebuilds an image built from an older
definition).

| Language | Detected by (repository root) | Stages | Dependencies (offline) |
|---|---|---|---|
| Go | `go.mod` | `gofmt`, `go-build`, `go-vet`, `go-test`, `golangci-lint` (full gate, optional) | the host's module cache |
| JavaScript/TypeScript | `package.json`, `tsconfig.json` | `tsc`, `npm-lint`, `npm-test`, `npm-build` (full gate), each when the project declares it | the checkout's `node_modules` |
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
