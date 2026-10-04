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
| Agent runs against the local model end to end | implemented | milestone 1 (kill/resume, condensation, local-only); 11-task suite on 3 models |
| Workspaces + codebase-memory-mcp (persistent MCP session) | implemented | Phase 3 gap report |
| Cross-service contract analysis (HTTP, topics, env, Terraform) | implemented | `docs/design/cross-service-analysis.md`: 7/7 fixture links, idiom tests, fuzzing, contract check in the run loop |
| Task ledger + context planner (resume packs) | implemented | determinism and budget tests, end-to-end resume test |
| Verification engine (Go/TS presets, impact-selected Go packages, diff scope, gitleaks) | implemented | |
| Sandbox + policy (container, masks, read-only git, host git hardening) | implemented | adversarial tests; residual risks in `docs/design/sandbox.md` |
| Frontier escalation Z1–Z4 (Codex subscription, manual) | implemented | live Codex run (Phase 7) fixed a task the local model failed; the contained provider is verified live (`TestCodexContainedLive`) |
| Engineering benchmark harness (11 tasks, hidden checks) | implemented | every task fails on its base (self-check) |
| Model evaluation (Qwen3.6, Laguna XS 2.1, Qwen3-Coder-Next) | implemented | `docs/design/model-evaluation.md`. Default: Qwen3.6 (ADR-0007, accepted). |
| Daemon, GUI | planned | |
| Release packaging, SBOM | implemented (unpublished) | `make dist`, `scripts/sbom`. Publishing needs maintainer approval. |
| Local escalation tier (switch to a stronger local model before frontier) | planned | motivated by the 4/4 vs 1/4 result in ADR-0007 |
