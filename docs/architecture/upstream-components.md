# Upstream Components

Each component is used as an **external process or a dependency, never as a
fork**. Licenses are covered in
[upstream-license-matrix.md](../licensing/upstream-license-matrix.md). All
entries were last verified on **2026-10-03**; the Codex CLI and llama.cpp
rows were re-checked on **2026-10-04**, and the per-platform release assets
and the cloud SDK rows were added on **2026-10-07**.

| Name | Source | Pinned | Integration | Update policy |
|---|---|---|---|---|
| llama.cpp | github.com/ggml-org/llama.cpp | `v0.5.0` (`7fe450e1`); prebuilt archives from build `b11146`, the build tag on the same commit | external `llama-server`/`llama-bench` binaries: built from source by `scripts/build-llama-cpp.sh` or `setup` on Linux, else the prebuilt archive for the platform (macOS arm64 Metal, macOS x64 CPU, Windows x64 CUDA 12.4 with its runtime archive or CPU, Windows arm64 CPU, Linux CUDA 12.8 or CPU), or supplied by the user | Follow stable `vX.Y.Z` releases, not hourly `bNNNNN` prereleases; prebuilt archives are taken only from the build tag on the pinned commit. The build script refuses the pinned tag at any other commit (`PINNED_COMMIT`). Archives are pinned by sha256 in `internal/install/pins.go`. Re-run the infra benchmark on upgrade. |
| OpenHands Software Agent SDK | github.com/OpenHands/software-agent-sdk, PyPI `openhands-sdk`, `openhands-tools` | `1.51.0` (`a955aa5d`) | Python dependency of our adapter (`adapters/openhands/python`) | Pin exact versions in `pyproject.toml` and `uv.lock`. Re-run the adapter acceptance test on upgrade. |
| codebase-memory-mcp | github.com/DeusData/codebase-memory-mcp | `v0.11.0` (`8972ea69`) | external binary, used through its `cli <tool> --format json` mode | Pin a release asset by checksum per platform (linux, darwin and windows on amd64 and arm64) in `internal/install/pins.go`; `scripts/install-deps.sh` pins the Linux assets with the same digests. Re-run the repointel benchmark on upgrade. |
| gitleaks | github.com/gitleaks/gitleaks | `v8.30.1` (`83d9cd68`) | external binary, used as a verification stage and in CI | Pin a release asset by checksum per platform, as for codebase-memory-mcp. |
| Serena (optional) | github.com/oraios/serena, PyPI `serena-agent==1.7.0` | `v1.7.0` (`949a27ef`) | external `serena start-mcp-server` processes (MCP over stdio), one per task worktree, managed by `internal/repointel/serena` (ADR-0008) | **Manual only.** Exact pin in `configs/serena/pyproject.toml` + `uv.lock` (wheel hash); `scripts/serenaguard` enforces it in CI. No updater may change it. Upgrades need a license review (v2 is GPL-3.0-or-later), a compatibility review, `bench intel` and explicit approval. |
| Codex CLI | github.com/openai/codex | user-installed; tested with `codex-cli 0.156.1` (tag `rust-v0.156.1`, `b412ff32`). On macOS and Windows, `setup` also downloads that release's Linux musl build (amd64 or arm64, sha256-pinned) | external `codex exec` process for frontier escalation, with ChatGPT sign-in; on macOS and Windows the contained route mounts the Linux build into the frontier container, where the host's codex cannot run | User-managed on the host. We depend only on documented `exec` flags. The Linux build's pin moves with the tested version (`install.CodexVersion`). |
| Anthropic Go SDK | github.com/anthropics/anthropic-sdk-go | `v1.79.0` (`go.mod`) | Go dependency of the gateway's Anthropic upstream (ADR-0010), with environment defaults disabled so it reads no vendor variables | Update with `go get`; re-run the provider translation tests. The OpenAI-compatible and Gemini upstreams use plain HTTP and have no SDK. |
| go-keyring | github.com/zalando/go-keyring | `v0.2.8` (`go.mod`) | Go dependency: provider API keys in Secret Service, the macOS Keychain or Windows Credential Manager, with an owner-only file fallback | Update with `go get`; re-run the secrets tests. |

## Observed facts used by the integration

These were verified from the pinned sources and the local binaries, not from
memory.

### llama.cpp (`llama-server`)
* Health: `GET /health` returns 503 while loading and 200 `{"status":"ok"}`
  when ready. `GET /props` returns `default_generation_settings.n_ctx`,
  `model_path` and `build_info`. `GET /metrics` needs `--metrics`.
* Responses include a `timings` object with `prompt_n`, `prompt_ms`,
  `predicted_n`, `predicted_ms` and `cache_n`, which we use for decode and
  prompt-processing metrics.
* MoE offload: `--n-cpu-moe N` keeps the expert weights of the first N layers
  in system RAM, `--cpu-moe` keeps all of them there, and `-ot` gives
  fine-grained control.
* `--fit on` is the default and auto-adjusts unset options to memory. We set
  explicit values from benchmark results so runs are reproducible.
* `--jinja` is on by default and is required for tool calling.
  `--reasoning on|off|auto` and `--chat-template-kwargs` control thinking.
* Releases are now semver `vX.Y.Z`. The `bNNNNN` builds are hourly
  prereleases.

### OpenHands SDK
* It requires Python 3.12 or newer.
* Local models: `LLM(model="openai/<alias>", base_url="http://…/v1", api_key="<placeholder>")`.
* Persistence: `Conversation(agent, workspace, persistence_dir, conversation_id)`
  writes `<persistence_dir>/<id-hex>/base_state.json` and `events/`.
  Constructing it again with the same id resumes the conversation.
* Condensation: `LLMSummarizingCondenser(llm, max_size, keep_first)` is
  attached via `Agent(condenser=…)`.
* Stuck detection is on by default
  (`Conversation(stuck_detection=True)`) and covers repeated
  action/observation, repeated errors, monologue, alternation and
  context-window loops.
* Tools: `TerminalTool`, `FileEditorTool`, `TaskTrackerTool` (plus `glob`,
  `grep`, `apply_patch` and others).
* MCP: `Agent(mcp_config={...})`.

### codebase-memory-mcp
* It is a pure C binary with SQLite storage in `~/.cache/codebase-memory-mcp/`
  (override with `CBM_CACHE_DIR`). `CBM_ALLOWED_ROOT` confines indexing.
* The source defines 17 tools, while the README lists 14:
  `index_repository`, `search_graph` (BM25, regex and semantic),
  `query_graph` (openCypher subset), `trace_path`, `get_code_snippet`,
  `get_file_outline`, `get_graph_schema`, `compare_graphs`,
  `get_architecture`, `search_code`, `list_projects`, `delete_project`,
  `index_status`, `check_index_coverage`, `detect_changes`
  (git diff → affected symbols and blast radius), `manage_adr`, and
  `ingest_traces` (which does not create edges).
* It claims cross-service links (HTTP routes, gRPC/GraphQL/tRPC,
  Kafka/SQS/PubSub `ASYNC_CALLS`), cross-repo `CROSS_*` edges, and IaC
  (Docker, K8s, Kustomize). These claims are **measured**, not assumed, in
  the [Phase 3 gap report](../design/repointel-gap-report.md).

### Serena v1.7.0
Verified from the tagged source and the installed wheel (not from `main`):
* CLI: `serena start-mcp-server --project PATH --context FILE --transport
  stdio|sse|streamable-http --enable-web-dashboard false
  --open-web-dashboard false …`; `serena --version` prints `Serena 1.7.0`
  plus `-<HEAD>` of any git repository that encloses the install directory.
* MCP `serverInfo.version` reports the MCP SDK (`1.28.1`), not Serena.
* Tools used: `find_symbol`, `find_referencing_symbols`,
  `find_implementations`, `get_symbols_overview`, `get_current_config`.
  Editing tools (`replace_symbol_body`, `insert_after_symbol`,
  `rename_symbol`, …), shell, file and memory tools are excluded through a
  context file with `fixed_tools`.
* Line numbers in results are 0-based. Go methods have flat name paths
  (`CreatePayment`, not `Service/CreatePayment`); interface methods nest
  (`PaymentRepository/Insert`).
* `SERENA_HOME` (default `~/.serena`) holds the global config, logs and
  language servers it downloads (TypeScript via npm). The per-project folder
  defaults to `<project>/.serena` and is configurable with
  `project_serena_folder_location`; an existing configured folder wins over
  the in-repository one. `trusted_project_path_patterns` gates a project's
  `activation_command`.
* `SERENA_USAGE_REPORTING=false` disables a start-up request to
  oraios-software.de. The dashboard (on by default) listens on a port and
  fetches news.
* Language servers start in a new session with `PR_SET_PDEATHSIG(SIGTERM)`.
* Go needs `gopls` on `PATH`; TypeScript needs `node` and `npm`.

### Codex CLI
* `codex exec` reads the prompt from an argument or stdin, defaults to a
  `read-only` sandbox, and supports `-o/--output-last-message FILE`,
  `--json`, `-m MODEL`, `-C DIR`, `--skip-git-repo-check` and
  `--ephemeral`.
* `codex login status` reports the auth mode. ChatGPT-plan sign-in needs no
  API key. Credentials stay in `~/.codex` on the host.
