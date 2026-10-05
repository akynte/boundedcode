# Implementation Status

Legend: **implemented** (tested, usable), **experimental** (works, but has
limited validation or unstable interfaces), **planned**.

| Area | Status | Evidence / notes |
|---|---|---|
| Repo bootstrap, licensing, DCO, CI | implemented | `make check`; license matrix verified 2026-10-03 |
| Typed config + model profiles | implemented | strict YAML, validation tests |
| SQLite state + redacted audit log | implemented | `internal/store`, `internal/telemetry` |
| `doctor`, `init` | implemented | |
| llama.cpp supervision (start/reuse/stop, health, OOM classification, external server, idle sleep) | implemented | `internal/inference/llamacpp`; idle sleep measured 2026-10-04 (RSS 19.3 → 0.8 GiB, VRAM 7.1 → 0.2 GiB, wake 1.7 s) |
| Infrastructure benchmark (feasibility search, prompt/decode, cache reuse, thermals) | implemented | `bench infra`; reports in `benchmarks/reports/` |
| OpenHands adapter (JSON-RPC/stdio, LLM tunnel, resume, condense, protocol v1 handshake) | implemented | real SDK through the adapter, scripted LLM, host and container tests; malformed/partial/crash/deadline/fuzz tests on the protocol |
| Agent runs against the local model end to end | implemented | milestone 1 (kill/resume, condensation, local-only); 11-task suite on 3 models |
| Workspaces (add/remove/disable, per-workspace config) + codebase-memory-mcp (persistent MCP session, version gate, CLI fallback, per-worktree impact) | implemented | Phase 3 gap report; 5-repo lifecycle test; `detect_changes` limitation and workaround recorded 2026-10-04 |
| Cross-service contract analysis (HTTP, topics, env, Terraform) | implemented | `docs/design/cross-service-analysis.md`: 7/7 fixture links, idiom tests, fuzzing, contract check in the run loop |
| Serena v1.7.0 symbol navigation (optional, MIT-pinned) | implemented | ADR-0008: per-worktree MCP instances, read-only, graph-for-breadth routing with per-symbol fallback; fake and real-Serena tests (Go, TS, worktree freshness, malicious repo config, multi-repo, crash, cancellation, resume); `bench intel` overlap study; Stage 2 edit experiment; CI pin guard. Optional, off by default: A/B 9/9 vs 9/9, efficiency trend only (`benchmarks/reports/*-ab-serena-summary.md`) |
| Task ledger + context planner (resume packs) | implemented | run lease and crash recovery (`TestSIGKILLResume`, real SIGKILL), changed symbols, step progress, confined and redacted packs, lexical (ripgrep) stage |
| Verification engine (task_verified vs tests_green via behavioural evidence, Go build-tag variants, Go/TS/Terraform-fmt/Helm presets, impact-selected Go packages, diff scope, protected paths, gitleaks) | implemented | config and presets read from the base commit (`TestAgentCannotWeakenVerification`); JS/TS stages use the checkout's installed `node_modules` (mounted read-only) and fail when a declared script has no installed dependencies; Terraform/Helm stages are optional and skipped when the tool is absent from the sandbox image |
| Sandbox + policy (container, masks, read-only git, host git hardening) | implemented | adversarial tests incl. admin-dir tampering, planted symlinks, prompt-injected agent, policy bypasses; residual risks in `docs/design/sandbox.md` |
| Frontier escalation Z1–Z4 (Codex subscription, manual) | implemented | live Codex run (Phase 7) fixed a task the local model failed; the contained provider is verified live (`TestCodexContainedLive`) |
| Engineering benchmark harness (14 tasks, hidden checks) | implemented | every task fails on its base (`TestHiddenChecksFailOnBase`, 14/14 on 2026-10-04); the model evaluation used the original 11 |
| Model evaluation (Qwen3.6, Laguna XS 2.1, Qwen3-Coder-Next) | implemented | `docs/design/model-evaluation.md`. Default: Qwen3.6 (ADR-0007, accepted). |
| Success metrics from real task history (`stats`, `frontier list`) | implemented | local-only rate, escalation rate, token share, verified tasks/hour; escalations record model and whether the code changed after the advice |
| Daemon, GUI | planned | |
| Release packaging, SBOM | implemented (unpublished) | `make dist`, `scripts/sbom`. Publishing needs maintainer approval. |
| Local escalation tier (switch to a stronger local model before frontier) | planned | motivated by the 4/4 vs 1/4 result in ADR-0007 |

An implementation audit against both specifications (the original project
specification and the Serena integration) was done on 2026-10-04; see
[audit-2026-10-04.md](audit-2026-10-04.md).

A small real-world validation (8 public SWE-bench-style tasks) was run on
2026-10-04: 0/8 on the frozen build, 1/8 after fixing five defects it found;
see [the report](../benchmarks/small-real-world-validation-2026-10.md).
A failure-driven engineering pass on 2026-10-05 used those failures as a
development corpus (not a benchmark): 1/4 development tasks pass on the
final build (prometheus, previously failing); see
[the engineering report](../benchmarks/failure-driven-engineering-2026-10.md).
