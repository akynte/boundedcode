# Upstream Components

Each component is used as an **external process or a dependency, never as a
fork**. Licenses are covered in
[upstream-license-matrix.md](../licensing/upstream-license-matrix.md). All
entries were last verified on **2026-10-03**.

| Name | Source | Pinned | Integration | Update policy |
|---|---|---|---|---|
| llama.cpp | github.com/ggml-org/llama.cpp | `v0.5.0` (`7fe450e1`) | external `llama-server`/`llama-bench` binaries, built by `scripts/build-llama-cpp.sh` or supplied by the user | Follow stable `vX.Y.Z` releases, not hourly `bNNNNN` prereleases. Re-run the infra benchmark on upgrade. |
| OpenHands Software Agent SDK | github.com/OpenHands/software-agent-sdk, PyPI `openhands-sdk`, `openhands-tools` | `1.51.0` (`a955aa5d`) | Python dependency of our adapter (`adapters/openhands/python`) | Pin exact versions in `pyproject.toml` and `uv.lock`. Re-run the adapter acceptance test on upgrade. |
| codebase-memory-mcp | github.com/DeusData/codebase-memory-mcp | `v0.11.0` (`8972ea69`) | external binary, used through its `cli <tool> --format json` mode | Pin a release asset by checksum (`scripts/install-deps.sh`). Re-run the repointel benchmark on upgrade. |
| gitleaks | github.com/gitleaks/gitleaks | `v8.30.1` (`83d9cd68`) | external binary, used as a verification stage and in CI | Pin a release asset by checksum. |
| Codex CLI | github.com/openai/codex | user-installed (tested 0.156.1) | external `codex exec` process for frontier escalation, with ChatGPT sign-in | User-managed. We depend only on documented `exec` flags. |

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

### Codex CLI
* `codex exec` reads the prompt from an argument or stdin, defaults to a
  `read-only` sandbox, and supports `-o/--output-last-message FILE`,
  `--json`, `-m MODEL`, `-C DIR`, `--skip-git-repo-check` and
  `--ephemeral`.
* `codex login status` reports the auth mode. ChatGPT-plan sign-in needs no
  API key. Credentials stay in `~/.codex` on the host.
