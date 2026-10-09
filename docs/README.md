# BoundedCode documentation

## Using BoundedCode

| Page | What it covers |
|---|---|
| [Getting started](usage/getting-started.md) | Installers, platforms, prerequisites, the manual path, first task, multi-repository tasks |
| [Terminal interface](usage/tui.md) | `bcode` chat, first-run setup, views and keys |
| [Verification](usage/verification.md) | Result states, behavioural evidence, cross-repository compatibility, known limits |
| [Configuration](usage/configuration.md) | `config.yaml`, model providers, budgets, sandbox, repository verification presets |
| [Serena](usage/serena.md) | Optional LSP-backed symbol navigation |

## Design and architecture

| Page | What it covers |
|---|---|
| [System architecture](architecture/system-architecture.md) | Processes, interfaces, control loop, context planning, package map |
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
| [Second (held-out) validation](benchmarks/second-independent-validation-2026-10.md) | 6 screened public tasks, 5 of 6 `TASK_VERIFIED` |
| [Initial validation](benchmarks/small-real-world-validation-2026-10.md) | 8 public tasks, 0/8 then 1/8, with the failures |
| [Failure-driven engineering](benchmarks/failure-driven-engineering-2026-10.md) and [targeted pass](benchmarks/targeted-engineering-pass-2026-10.md) | Development evidence, including false verification passes |
| [Raw reports](../benchmarks/reports/) | Per-run JSON, logs and manifests |

## Project

| Page | What it covers |
|---|---|
| [Release notes](releases/) and [CHANGELOG](../CHANGELOG.md) | What changed, per release |
| [Implementation status](development/status.md) | What is implemented, experimental or planned |
| [Readiness audit (2026-10)](public-launch/readiness-audit.md) · [README review](public-launch/readme-review.md) | Evidence-checked findings, fixes and open risks; first-visitor review of the README |
| [Licensing](licensing/upstream-license-matrix.md) | Upstream license matrix |
| [Security policy](../SECURITY.md) · [Contributing](../CONTRIBUTING.md) | Reporting vulnerabilities, development setup |
