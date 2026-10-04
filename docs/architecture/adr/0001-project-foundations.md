# ADR-0001: Project foundations

Status: accepted (2026-10-03); §7 amended by [ADR-0008](0008-serena-symbol-navigation.md)

## Context

Developers working on large, multi-repository systems want AI help with
real engineering work: cross-service changes, debugging, refactoring and
infrastructure. Frontier subscriptions are expensive and quota-limited. The
target budget is at most one ~USD 20/month plan. Open-weight sparse MoE
models such as Qwen3.6-35B-A3B (~3B active parameters) now run usefully on
an 8 GB GPU with experts offloaded to system RAM. Mature open-source parts
already cover inference (llama.cpp), agent loops (OpenHands SDK) and code
graphs (codebase-memory-mcp). What is missing is the **system around the
model**: task continuity, verification, safety, routing, cost control and
reproducible measurement.

## Decision

1. **Why the project exists.** It is a local-first control plane that owns
   the task lifecycle, workspace, verification, policy, escalation and
   benchmarking. Local models handle most token volume, and a frontier model
   is consulted only for high-value cases.
2. **Not a fork.** We integrate upstream components as dependencies or
   external processes behind small interfaces (`inference.Runtime`,
   `agent.Runtime`, `repointel.Intelligence`, `frontier.Provider`,
   `sandbox.Sandbox`). Forking would make us maintain fast-moving upstream
   code we have no advantage in. Upstream improvements then reach us through
   a version bump.
3. **Go owns the control plane.** It ships as a single static binary, which
   makes installation easy. It gives explicit error handling, good process
   supervision (`os/exec`, signals, contexts) and cheap concurrency with
   cancellation. Python appears only where an upstream library requires it.
4. **OpenHands SDK is the agent runtime.** It already provides the tool loop,
   conversation persistence and resume, LLM-summarizing condensation, stuck
   detection, MCP support and LiteLLM-based local-model access. We wrap it
   in a thin adapter (ADR-0004) and keep product logic in Go, so it stays
   replaceable.
5. **codebase-memory-mcp is the repository-intelligence engine.** It is MIT
   and a single C binary. It already claims symbol graphs, call tracing,
   git-diff impact, cross-service links, semantic search and ADR storage. We
   measure those claims and fill only proven gaps.
6. **llama.cpp is the inference runtime.** It is MIT, has CUDA support and
   MoE expert offload (`--n-cpu-moe`), and exposes an OpenAI-compatible
   server with per-request timings. We supervise it and do not reimplement
   it.
7. **Serena is excluded from core.** Its application license moves to
   GPL-3.0-or-later from v2. The current v1.7.0 release is still MIT; this
   was verified 2026-10-03 and differs from our original expectation. We
   want a permissively licensed core with no GPL coupling. Any future
   support must be an optional, separately installed process with its own
   review.
   *Amended by ADR-0008: Serena v1.7.0 (MIT) is now an optional, separately
   installed external process, pinned to that release.*
8. **Models are downloaded separately.** We do not redistribute weights.
   This avoids model-license redistribution obligations and multi-GB
   artifacts in releases, and it keeps the model choice with the user.
   Profiles record source, revision and license.
9. **TypeSafe Jev is excluded.** Bounded decisions use deterministic rules,
   repository metadata, verification outcomes, constrained local-model
   calls and, only when justified, frontier escalation.

## Consequences

* Each boundary needs an adapter and integration tests against pinned
  upstream versions.
* Upstream API changes can break us. Pinning plus the upgrade policy in
  `docs/licensing/policy.md` contains that risk.
* A Python environment is required for the agent runtime. It is isolated in
  a container image or a uv-managed venv.
