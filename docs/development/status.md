# Implementation Status

Legend: **implemented** (tested, usable), **experimental** (works, but has
limited validation or unstable interfaces), **planned**.

| Area | Status | Evidence / notes |
|---|---|---|
| Repo bootstrap, licensing, DCO, CI | implemented | `make check`; license matrix verified 2026-10-03 |
| Typed config + model profiles | implemented | strict YAML, validation tests |
| SQLite state + redacted audit log | implemented | `internal/store`, `internal/telemetry` |
| `doctor`, `init` | implemented | |
| llama.cpp supervision (start/reuse/stop, health, OOM classification, external server) | implemented | `internal/inference/llamacpp` |
| Infrastructure benchmark (feasibility search, prompt/decode, cache reuse, thermals) | implemented | `bench infra`; reports in `benchmarks/reports/` |
| OpenHands adapter (JSON-RPC/stdio, LLM tunnel, resume, condense) | implemented | real SDK through the adapter, scripted LLM, host and container tests |
| Agent runs against the local model end to end | experimental | see `benchmarks/reports/` engineering runs |
| Workspaces + codebase-memory-mcp (persistent MCP session) | implemented | Phase 3 gap report |
| Cross-service impact (HTTP, Kafka) | planned | upstream gaps documented. No custom analyzers until real task failures justify them. |
| Task ledger + context planner (resume packs) | implemented | determinism and budget tests, end-to-end resume test |
| Verification engine (Go/TS presets, impact-selected Go packages, diff scope, gitleaks) | implemented | |
| Sandbox + policy (container, masks, read-only git, host git hardening) | implemented | adversarial tests; residual risks in `docs/design/sandbox.md` |
| Frontier escalation Z1–Z4 (Codex subscription, manual) | experimental | policy, packets and persistence tested with a fake provider. Live Codex calls need maintainer approval. |
| Engineering benchmark harness (11 tasks, hidden checks) | implemented | every task fails on its base (self-check) |
| Model evaluation (Qwen3.6 vs Laguna XS 2.1 vs Qwen3-Coder-Next) | planned | Laguna license under review; Coder-Next Q4 is ~48 GB |
| Daemon, GUI | planned | |
| Release packaging, SBOM | planned | Phase 10 |
