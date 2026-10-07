# Product Specification

Status: living document. Product name: **BoundedCode**. The CLI binary,
module and data directories use `boundedcode`.

## 1. Mission

BoundedCode is a control plane for AI-assisted software engineering on
large, long-running, multi-repository projects. It runs a local model on
commodity hardware by default, or a cloud model API the user chooses
(ADR-0010, 2026-10-07), and keeps dependence on paid frontier subscriptions
low.

> With a local model, local models handle the overwhelming majority of token
> volume. Frontier models are used only where their marginal intelligence is
> worth consuming scarce subscription quota. A user may instead run the agent
> on a cloud model API; that is their explicit, metered choice.

The primary KPI is **verified engineering tasks completed per dollar per
wall-clock hour**. It is not model size, context length, synthetic benchmark
score, or degree of autonomy.

## 2. Users and constraints

| Constraint | Value |
|---|---|
| Reference machine | Lenovo LOQ 15IRH8: i7-13620H (10C/16T), RTX 4060 Laptop 8 GB (CC 8.9), 64 GB DDR5, NVMe, Debian 13 x86-64 |
| Frontier budget | at most one ~USD 20/month subscription (initially ChatGPT Plus via Codex). No paid API credits are assumed. |
| Workload target | Design goal, not a demonstrated result: 95 %+ of the workload runs locally and under 5 % goes to the frontier, to be *measured* on real task history (`boundedcode stats`), not assumed. Current evidence is limited to small validation samples (see the README). |
| Repository scale | Multi-million-token workspaces. The model sees a task-specific context pack of about 10K–30K tokens; the exact value is benchmark-driven. |

Hard rules:

* No silent paid-API usage. An API key is only used if the user explicitly
  configures one (a cloud model provider, ADR-0010); keys never enter the
  agent sandbox. The frontier route never uses an API key (ADR-0009).
* No model weights in Git and no redistribution of model artifacts.
* No destructive infrastructure operations (`terraform apply/destroy`,
  production `kubectl`).
* The agent never pushes, force-pushes, resets or merges protected branches.
* The LLM is never the sole judge of correctness.

## 3. Scope

### In scope (the product owns)

* task lifecycle and canonical task ledger
* workspace (multi-repo) lifecycle
* model, routing, escalation, verification, safety and resource policy
* repository-intelligence orchestration
* Git worktree isolation
* sandboxed execution
* benchmarking (infrastructure and engineering)
* CLI, local daemon (later), telemetry and audit log

### Delegated to upstream (integrated, not forked)

| Concern | Initial component | Boundary |
|---|---|---|
| Inference | llama.cpp `llama-server` | `inference.Runtime` (supervised external process, OpenAI-compatible HTTP) |
| Agent loop, tools, conversation persistence, condensation, stuck detection | OpenHands Software Agent SDK | `agent.Runtime` (thin Python adapter over JSON-RPC/stdio) |
| Code graph, search, impact | codebase-memory-mcp | `repointel.Intelligence` (external MCP process) |
| Symbol navigation: definitions, references, implementations (optional) | Serena v1.7.0 (MIT, pinned) over language servers | `repointel.Navigator` (external MCP processes per worktree, ADR-0008) |
| Frontier reasoning | Codex CLI with ChatGPT sign-in | `frontier.Provider` |
| Secret scanning | gitleaks | verification stage (external binary) |

### Explicitly excluded

* TypeSafe Jev, in any form. Bounded decisions use deterministic rules,
  repository metadata, verification outcomes, constrained local-model
  decisions, and frontier escalation, in that order.
* The Serena application from v2 on (GPL-3.0-or-later). The optional
  integration is pinned to the MIT-licensed v1.7.0 and runs as a separate
  process (ADR-0008); moving past it needs a legal and architectural review.
* A GUI before the core is stable. The terminal interface (`bcode`, experimental)
  is not a GUI in this sense: it is a client of the CLI's operations and adds
  no behaviour of its own.
* A zoo of hot-swapped large local models.

## 4. Functional requirements

### 4.1 Workspaces
A workspace groups repositories and tracks origins, branches/worktrees,
languages, services, APIs, data stores, event contracts, infrastructure
definitions, ADRs, graph references and task history. Repositories are added
incrementally.

### 4.2 Repository intelligence
The system answers *"if I change this, what else can break?"* at symbol,
service, contract (HTTP/gRPC/OpenAPI/protobuf), event (Kafka/outbox),
schema (SQL/migrations) and configuration (env → Helm → K8s → Terraform)
level. Upstream capabilities are used first. Custom analyzers are written
only for gaps that a documented gap report proves.

Retrieval preference: exact symbol lookup → dependency/impact graph → exact
lexical search → hybrid lexical+semantic → reranking → LLM exploration.

Implementation status (2026-10-04):

| Level / stage | Status |
|---|---|
| Exact symbol lookup (Serena/LSP, optional; graph fallback) | implemented |
| Dependency and change-impact graph (codebase-memory-mcp, per task worktree) | implemented |
| Exact lexical search (ripgrep) when symbol and graph lookups miss | implemented |
| Hybrid lexical+semantic retrieval, reranking | not implemented; no measured gap yet |
| LLM exploration | the agent's own tools inside the sandbox |
| Symbol, call and interface level | implemented (Serena, graph) |
| HTTP routes and calls, Kafka-style topics, env → Helm values/K8s/compose/Dockerfile, Terraform topic resources | implemented (`internal/xservice`) |
| gRPC/protobuf, OpenAPI, outbox, SQL schema/migrations, Helm templates, Terraform beyond topics | not implemented (no analyzer; Phase 9 found no proven gap, see `docs/design/phase9-analyzers.md`) |

### 4.3 Task continuity
The *task* is the source of truth, not the conversation. The ledger stores:
task/workspace ids, original request, goal, acceptance criteria, phase,
completed/remaining steps, attempt count, strategies tried/rejected,
decisions, verification state, changed repos/files/symbols, agent-runtime
session id, frontier escalations and timestamps. It stores *references* to
runtime conversation state and does not copy it.

On resume or condensation the working context is rebuilt from authoritative
sources: ledger, Git state, graph, ADRs, diff, verification results,
relevant source, and remaining work.

### 4.4 Git
Every autonomous task runs on an isolated worktree on branch
`agent/<task-id>`. A task survives process crash, context reset, reboot and
agent restart.

### 4.5 Verification
A deterministic, per-repository configurable pipeline: format, build, static
analysis, lint, unit tests, impact-selected tests, integration tests,
security and secret scans, diff scope, full suite. Impact selection is used
during iteration. The full gate runs before a task becomes a merge candidate.
Results are structured and persisted.

### 4.6 Safety
Execution happens inside a real sandbox (container). Only the task worktree is
mounted read-write. Host `$HOME`, SSH agent, cloud credentials, kubeconfig
and secrets are not exposed. Path denylist covers `.env*`, `secrets/`,
credentials, keys and kubeconfigs. Deterministic command policy blocks
dangerous operations. The LLM cannot override policy. Frontier credentials
never enter agent environments.

### 4.7 Frontier escalation
Triggers:

* **Z1 architectural risk**: multiple services, DB/event contracts,
  ledger/accounting semantics, idempotency, outbox, distributed transactions,
  authn/z, crypto, payments.
* **Z2 repeated local failure**: repeated verification failure, several
  materially different strategies failed, or a loop was detected. Thresholds
  are configurable.
* **Z3 high-risk pre-merge review**: authn/z, crypto, money, permissions, or
  high-blast-radius infrastructure.
* **Z4 explicit user request.**

Escalation sends a compact packet: task, acceptance criteria, ADRs, impact
summary, relevant code, diff, verification results, failure summary,
strategies tried, and a specific question. Advice returns to the local
workflow, and the local model implements it. Escalations, their type, their
outcome, and whether they changed the result are all tracked. No fixed
calls/day quota is hard-coded.

### 4.8 Models
One resident strong local model is the default. Models are configuration
(`configs/models/*.yaml`), not code. The default is chosen by *measured*
verified-task success, then reliability, then speed. Candidates are
Qwen3.6-35B-A3B, Laguna XS 2.1 and Qwen3-Coder-Next.

### 4.9 Observability
Structured events cover task lifecycle, model calls (tokens, latency,
prompt-processing and decode rates), retrieval, verification, retries,
escalations, condensations, resumes, resources and wall-clock time. Secrets
and full proprietary source are not logged by default.

### 4.10 Benchmarks
* *Infrastructure*: load time, VRAM/RAM, prompt processing, decode, cold vs
  cached prompt, context sizes, sustained thermals. Flags are swept, not
  guessed.
* *Engineering*: real or realistic tasks with machine-verifiable acceptance
  criteria across Go, TypeScript, SQL migrations, API contracts,
  cross-service, Docker, Kubernetes, Terraform, debugging, refactoring and
  multi-repo work.

### 4.11 Success metrics
Verified tasks/hour, verified tasks/dollar, local-only completion rate,
frontier escalation rate, attempts per successful task, wall-clock per task,
generated local tokens per task, retrieved source tokens per task, context
resets per task, successful resume rate, verification failure rate,
regression rate, user intervention rate.

## 5. Non-functional requirements

* **Install**: a single static Go binary plus external tools. Control-plane
  state lives in embedded SQLite, with no database server.
* **Failure**: state is persisted before destructive transitions, and
  operations are idempotent where practical. No task is lost to a crash of
  the model, llama.cpp, the adapter or the daemon, or to a reboot.
* **Config**: explicit, typed, validated YAML, versionable and overridable
  per workspace.
* **Licensing**: Apache-2.0 for our code. Upstream licenses are verified
  per pinned version (see [license matrix](licensing/upstream-license-matrix.md)).
  Contributions use DCO.
* **Claims**: no capability is claimed without a measurement. Features are
  labelled *implemented*, *experimental* or *planned*.

## 6. First serious milestone

1. Initialize a multi-repo workspace.
2. Index it.
3. Submit a non-trivial task.
4. OpenHands runs against local Qwen.
5. Repository intelligence supplies targeted context.
6. Changes happen in an isolated worktree.
7. Verification finds problems and feeds them back.
8. The task survives an agent/runtime restart.
9. The task survives a condensation/resume cycle.
10. Final changes pass deterministic verification.
11. The task completes without frontier use.
12. A separate hard task triggers a controlled escalation, gets guidance,
    returns to local execution and passes verification.
13. Everything is visible in the audit log.
14. No secrets are exposed.
15. Upstream license compliance is maintained.
