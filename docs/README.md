# BoundedCode documentation

## Using BoundedCode

| Page | What it covers |
|---|---|
| [Getting started](usage/getting-started.md) | Installers, platforms, prerequisites, the manual path, first task, multi-repository tasks |
| [Terminal interface](usage/tui.md) | `bcode` chat (direct and bounded modes), first-run setup, views and keys |
| [Verification](usage/verification.md) | Result states, behavioural evidence, cross-repository compatibility, known limits |
| [Configuration](usage/configuration.md) | `config.yaml`, model providers, budgets, sandbox, repository verification presets |
| [Languages](usage/languages.md) | What works for any language, the capability model, statuses, the validation matrix, adding a language |
| [Direct-mode command sandbox](usage/direct-sandbox.md) | What commands can reach on each platform, the network proxy, the fallback when containment is weaker, mechanisms evaluated, residual risks |
| [Direct-mode security policy](usage/security-policy.md) | Trust boundaries, sensitive operations and how they are authorized, the verification fallback, MCP trust and pinning, residual risks |
| [Inference protocol](usage/inference.md) | What the gateway does with each request and answer, per provider: capabilities, reasoning in the history, structured output, failures it handles, what each call records, llama.cpp cache settings |
| [Context and session state](usage/context.md) | How long sessions stay within the model's working set: command output pruning, setting results aside, the session state that replaces older history (record and model notes), trust, persistence, stale context |
| [Retrieval](usage/retrieval.md) | What travels with each message and each retry: strategies, when each runs, ranking, language servers, provenance and staleness, compaction and recall, budgets, limits |
| [Serena](usage/serena.md) | Optional LSP-backed symbol navigation |

## Design and architecture

| Page | What it covers |
|---|---|
| [System architecture](architecture/system-architecture.md) | Processes, interfaces, control loop, context planning, package map |
| [Tool capabilities](architecture/capabilities.md) | Direct and bounded mode compared (tools, permissions, context, sessions, execution, verification, errors, events, traces, accounting); what they share and keep apart; direct mode's tool pipeline, concurrent calls, editing, MCP discovery |
| [Everyday development: direct mode](design/everyday-agent.md) | The two-mode product shape, the competitive gap, phases 17–23 and what phase 17 built |
| [Tool coverage](design/tool-coverage.md) | Every Pi, Qwen Code and Claude Code tool and capability, and its BoundedCode equivalent |
| [Sandbox design](design/sandbox.md) | Threat model, controls, adversarial tests, residual risks |
| [Cross-repository compatibility](design/cross-repo-compatibility.md) | The experimental gRPC/protobuf/OpenAPI gate |
| [Cross-service analysis](design/cross-service-analysis.md) | Contract detection across repositories |
| [Upstream components](architecture/upstream-components.md) | Pinned versions and update policy |
| [ADRs](architecture/adr/) | Architecture decisions |
| [Product spec](product-spec.md) | Goals and scope |

## Evaluation

| Page | What it covers |
|---|---|
| [Evaluation overview](benchmarks/README.md) | What was measured, how to read it, and its limits |
| [Comparative evaluation (2026-10)](../benchmarks/comparative-2026-10/report.md) | BoundedCode vs. the same agent and model without it: protocol, results, deviations |
| [Baseline (2026-10-27)](benchmarks/baseline-2026-10.md) | Architecture inventory, language-support matrix, three runs per mode with pinned environment, where time, tokens and memory go, run-to-run noise, and how to compare a change with it |
| [Verification benchmark (2026-10)](benchmarks/verification-benchmark-2026-10.md) | False acceptance and false rejection of the verification pipeline, before and after test integrity and mutation, in Go, Python, JavaScript, TypeScript, Rust, Java and a mixed repository |
| [Retrieval benchmark (2026-10)](benchmarks/retrieval-2026-10.md) | Retrieval measured apart from patch correctness on 267 real issues in ten languages (development and held-out repositories), then in the agent loop: recall, precision, context, latency, tool calls, redundant reads, completion |
| [Inference benchmark (2026-10)](benchmarks/inference-2026-10.md) | Reasoning in the history, the offload threshold and structured output, each measured before and after: task results, prompt tokens the server evaluated, prompt time, time to the first token, parseable replies |
| [Context benchmark (2026-10)](benchmarks/context-2026-10.md) | Command-output pruning on 42 real outputs (development and held out), the session state against the free-text summary in the agent loop, the cache-aligned notes request, and pruning by a model (rejected): evidence kept, tokens, pass rate, summary latency, cache |
| [Adaptive execution benchmark (2026-10)](benchmarks/adaptive-2026-10.md) | Model capabilities probed with BoundedCode's own requests, four model sizes on the same tasks, speculative decoding (n-gram, draft model, MTP) in isolation and in the agent loop, thinking routed per call, model choice, escalation policies replayed on recorded runs, and what escalation packets carry |
| [Orchestration benchmark (2026-10)](benchmarks/orchestration-2026-10.md) | Calls of one reply run together, MCP tools found on demand, and both modes' suites before and after the shared tool architecture: tool latency, tool calls, tokens, time, determinism and policy consistency |
| [Direct-mode suite (2026-10)](benchmarks/direct-mode-suite-2026-10.md) | The everyday agent on 13 tasks and 2 sessions with the local model: pass rate, tool calls, cache hits, time, per change |
| [Project detection](benchmarks/ecosystem-detection-2026-10.md) | Projects found in any language on 12 public repositories, with what was missed and fixed |
| [Lease study](benchmarks/lease-study-2026-10.md) · [trail study](benchmarks/trail-study-2026-10.md) | Whether a request's text, or a repository's history, predicts what a change touches; measured on real commits before building on it |
| [Evidence demo](../demo/evidence/README.md) | The README demo: reproducible scenario, recordings, what it does and does not show |
| [Second (held-out) validation](benchmarks/second-independent-validation-2026-10.md) | 6 screened public tasks, 5 of 6 `TASK_VERIFIED` |
| [Initial validation](benchmarks/small-real-world-validation-2026-10.md) | 8 public tasks, 0/8 then 1/8, with the failures |
| [Failure-driven engineering](benchmarks/failure-driven-engineering-2026-10.md) and [targeted pass](benchmarks/targeted-engineering-pass-2026-10.md) | Development evidence, including false verification passes |
| [Raw reports](../benchmarks/reports/) | Per-run JSON, logs and manifests |
| [Submitting an evaluation result](../benchmarks/submitting-results.md) | How an external result must be run, reported and submitted |

## Project

| Page | What it covers |
|---|---|
| [Release notes](releases/) and [CHANGELOG](../CHANGELOG.md) | What changed, per release |
| [Implementation status](development/status.md) | What is implemented, experimental or planned |
| [Readiness audit (2026-10)](public-launch/readiness-audit.md) · [README review](public-launch/readme-review.md) | Evidence-checked findings, fixes and open risks; first-visitor review of the README |
| [Onboarding validation](public-launch/onboarding-validation.md) | Installation and first-task tests: environments, commands, results, platform matrix |
| [Licensing](licensing/upstream-license-matrix.md) | Upstream license matrix |
| [Security policy](../SECURITY.md) · [Contributing](../CONTRIBUTING.md) | Reporting vulnerabilities, development setup, tool versions, what CI checks |
| [Contributor onboarding](development/onboarding.md) | From a fresh clone to a pull request; first-contribution opportunities |
| [Engineering case study](public-launch/engineering-case-study.md) | The problem, architecture, trade-offs, evaluation results and limitations, with references to code and evidence |
| [Repository readiness (2026-10)](public-launch/repository-readiness.md) | GitHub configuration audit, metadata recommendations, changes that need the maintainer |
| [Adoption metrics](public-launch/adoption-metrics.md) · [feedback process](public-launch/feedback-process.md) · [14-day launch checklist](public-launch/launch-evaluation-checklist.md) | What counts as external adoption and where it is measured (no telemetry); how reports are reproduced, prioritized and turned into work |
