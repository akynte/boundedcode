# boundedcode

> Temporary codename. The public name is not decided yet.

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
boundedcode (Go CLI / control plane)
 ├─ task ledger + audit log (SQLite)
 ├─ git worktrees  agent/<task-id>
 ├─ verification engine (gofmt, go test, tsc, eslint, gitleaks, ...)
 ├─ repository intelligence ──> codebase-memory-mcp (external)
 ├─ agent runtime ── JSON-RPC/stdio ──> OpenHands SDK adapter (in a container)
 │                                         └─ LLM calls tunnelled back to Go
 ├─ inference runtime ──> llama.cpp llama-server (external, supervised)
 └─ frontier gate (Z1-Z4) ──> codex exec with ChatGPT sign-in (optional)
```

Read [docs/architecture/system-architecture.md](docs/architecture/system-architecture.md)
for details and [docs/product-spec.md](docs/product-spec.md) for goals and
constraints.

## Quick start (development)

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
[`configs/models/`](configs/models) names the upstream source and license.
Download from there.

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
