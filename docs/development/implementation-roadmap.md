# Implementation Roadmap

Each phase ends with tests passing, a recorded benchmark where relevant,
updated docs/ADRs, and small local commits. Progress is tracked in
[status.md](status.md).

| Phase | Goal | Exit criteria |
|---|---|---|
| 0 | Bootstrap and upstream audit | repo builds; tests and CI run; LICENSE, NOTICE, THIRD_PARTY_NOTICES; upstream versions and licenses verified and pinned; ADR-0001..n |
| 1 | Local runtime and hardware benchmark | `model`/`runtime` commands discover, start, health-check and stop llama-server; infrastructure benchmark sweeps flags for Qwen3.6-35B-A3B; results persisted under `benchmarks/reports/` |
| 2 | OpenHands adapter | JSON-RPC adapter; local-model tool calls work; task ↔ conversation id mapping; kill → restart → resume continues the same task |
| 3 | Workspace and repository intelligence | workspace commands; codebase-memory-mcp integration; index and impact measured on Go, TS and mixed workspaces; written gap report |
| 4 | Task ledger and context planner | full ledger; deterministic context packs; multi-condensation/resume test stays coherent |
| 5 | Verification engine | configurable stages (Go, TS, generic); structured, persisted results; impact selection; full gate |
| 6 | Sandbox and safety | worktrees, containerized execution, mounts, command/path policy, gitleaks, adversarial tests, residual-risk doc |
| 7 | Frontier escalation | Z1–Z4 policy; compact packets; approval; persistence; local → fail → frontier → local → verify loop measured |
| 8 | Model evaluation | engineering suite on candidate models; default selected from measurements |
| 9 | Cross-service intelligence | analyzers only for gaps proven by Phase 3/8 failures, each with fixtures, tests and a measured improvement |
| 10 | OSS readiness | README, docs, benchmark report, install script, release process, DCO, license scan, secret scan, SBOM. **No publishing without maintainer approval.** |
| 11–16 | Model choice, cloud providers, TUI set-up, macOS and native Windows | see [multiplatform-plan.md](multiplatform-plan.md) |
| 17 | Direct mode: a native agent loop for everyday development in the checkout (ADR-0011) | **done (experimental)**: `internal/direct`, `bcode chat` / `bcode -p`, the chat defaults to direct mode with `/task` for bounded mode; unit, TUI and CLI tests; see [everyday-agent.md](../design/everyday-agent.md) |
| 18–23 | Streaming and transcript; extensibility (commands, skills, MCP, hooks); repository intelligence in direct mode; session-to-task hand-off; editor integration; an everyday-development benchmark | see [everyday-agent.md](../design/everyday-agent.md#3-phases) |
