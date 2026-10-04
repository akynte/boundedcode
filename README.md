# BoundedCode

A local-first control plane for AI-assisted software engineering on large,
long-running, multi-repository projects. It runs on commodity hardware and
uses frontier models only for high-value escalation.

Local models do most of the work. The durable value is the system around the
model: task continuity, repository intelligence, deterministic verification,
sandboxing, routing and cost control. Models are replaceable configuration.

**Status: pre-alpha.** See [docs/development/status.md](docs/development/status.md)
for what is *implemented*, *experimental* and *planned*. Benchmark numbers in
this repository are measured on the reference machine and linked to their raw
reports. Nothing is claimed without a measurement.

## How it fits together

```text
boundedcode CLI (Go control plane)
 ├─ task ledger + audit log (SQLite)
 ├─ git worktrees  agent/<task-id>
 ├─ verification engine (gofmt, go test, tsc, eslint, gitleaks, ...)
 ├─ repository intelligence ──> codebase-memory-mcp (external): graph, impact (breadth)
 │                               + cross-service contract analyzers (HTTP, Kafka, env, Terraform)
 │                               + Serena v1.7.0 (optional, MIT-pinned): LSP symbols per worktree (depth)
 ├─ agent runtime ── JSON-RPC/stdio ──> OpenHands SDK adapter (in a container)
 │                                         └─ LLM calls tunnelled back to Go
 ├─ inference runtime ──> llama.cpp llama-server (external, supervised)
 └─ frontier gate (Z1-Z4) ──> codex exec with ChatGPT sign-in (optional)
```

Read [docs/architecture/system-architecture.md](docs/architecture/system-architecture.md)
for details and [docs/product-spec.md](docs/product-spec.md) for goals and
constraints.

## Measured so far

These numbers come from a single laptop (see below), with every result
linked to its raw report. They come from 11 synthetic tasks, so they are
indicative, not general claims.

| | Qwen3.6-35B-A3B | Laguna XS 2.1 | Qwen3-Coder-Next (3-bit) |
|---|---|---|---|
| Decode t/s at 2K / 58K context | 39 / 30 | ~38 / ~31 | 27 / 21 |
| Engineering suite, local only (hidden checks) | 10/11 | 11/11 | 10/11 |
| Hard cross-service task (4 runs) | 1/4 | 4/4 | 0/1 |
| Verified tasks per hour | 18.2 | 7.0 | 9.8 |

* Milestone 1: a multi-repo task survived a forced process kill and two
  context condensations and completed locally
  ([report](benchmarks/reports/milestone-1/)).
* Frontier escalation (Codex, ChatGPT sign-in) fixed the hard task for
  Qwen3.6 with 2 messages ([report](benchmarks/reports/phase7-frontier/)).
* Cross-service contract analysis: an event-field rename that names only
  the producer passed hidden checks 3/3 with the analyzers and 0/3 without
  them. All three runs without them passed per-repo tests while breaking the
  consumer ([evaluation](docs/design/phase9-analyzers.md)).
* Details: [infrastructure reports](benchmarks/reports/) and
  [model evaluation](docs/design/model-evaluation.md).

## Quick start (development)

The full walkthrough (prerequisites with versions, model download, sandbox
image, first task) is in
[docs/usage/getting-started.md](docs/usage/getting-started.md).

```bash
make build
./scripts/build-llama-cpp.sh                 # pinned llama.cpp with CUDA (optional if you have one)
./bin/boundedcode init \
    --models-dir ~/models \
    --llama-server ~/.local/share/boundedcode/runtimes/llama.cpp/v0.5.0/bin/llama-server \
    --llama-bench  ~/.local/share/boundedcode/runtimes/llama.cpp/v0.5.0/bin/llama-bench
./bin/boundedcode doctor
```

Model weights are **not** distributed with this project. Each profile in
[`configs/models/`](configs/models) names the upstream source, the pinned
commit and the license. Download with `scripts/fetch-model.sh` (see the
getting-started guide).

## Limitations

* **Pre-alpha.** Commands and configuration may change between versions.
* **Platform.** Linux x86-64 with an NVIDIA GPU, tested only on the
  reference laptop below. Other platforms and GPUs are untested.
* **CLI only.** There is no daemon and no GUI.
* **Verification presets** exist only for Go and TypeScript. The TypeScript
  preset is limited: `tsc`, `lint`, `test` and `build` run only when the
  repository's `node_modules` and `package.json` scripts provide them. Other
  languages need a `.boundedcode/verification.yaml`.
* **Cross-service analysis** covers HTTP routes and calls, Kafka-style
  topics, environment variables and Terraform topic/queue resources, in Go
  and TypeScript/JavaScript. gRPC, OpenAPI, protobuf and SQL contracts are
  not analyzed (see [cross-service-analysis.md](docs/design/cross-service-analysis.md)).
* **Frontier escalation** works only through the Codex CLI with a ChatGPT
  subscription sign-in, or by pasting packets manually. API keys are
  refused ([ADR-0009](docs/architecture/adr/0009-frontier-escalation.md)).
* **Local-only success rates** are measured only on this repository's
  benchmark suite of 11 synthetic tasks. They say nothing yet about real
  codebases.

## Reference hardware

The project is developed and benchmarked on a laptop with an i7-13620H, an
RTX 4060 Laptop GPU (8 GB), 64 GB DDR5 RAM and Debian 13. It is designed for
this class of machine, not for datacenter GPUs.

## License

Apache-2.0 ([LICENSE](LICENSE)). Third-party components and their licenses
are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and
[docs/licensing/upstream-license-matrix.md](docs/licensing/upstream-license-matrix.md).
Contributions use the [DCO](DCO); see [CONTRIBUTING.md](CONTRIBUTING.md).

This project is independent and is not affiliated with, sponsored by, or
endorsed by the upstream projects or vendors it integrates with.
