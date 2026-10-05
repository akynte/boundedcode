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

## Real-world validation

Results are reported in order, failures included.

**1. Initial validation (2026-10-04).** 8 real public engineering tasks
(SWE-bench Multilingual and Multi-SWE-bench: Go, JavaScript, TypeScript),
selected before execution. The frozen build passed the hidden acceptance
tests on 0/8. Four failures traced to BoundedCode defects, which were fixed;
of the three affected tasks rerun, one passed: **1/8** overall, 1/8 local
only. Three of the eight tasks have acceptance tests that depend on names
only the reference solution defines
([report](docs/benchmarks/small-real-world-validation-2026-10.md)).

**2. Engineering fixes (development evidence, not benchmarks).** The failed
tasks drove general fixes:
- a behavioural-evidence verification gate,
- an agent that can build and run tests offline,
- bounded reasoning and visible output,
- a progress-aware strategy budget,
- a task contract that surfaces material ambiguity,
- ranked retrieval seeds.

On the development tasks this is optimistic by construction: 1/4 passed
([failure-driven pass](docs/benchmarks/failure-driven-engineering-2026-10.md),
[targeted pass](docs/benchmarks/targeted-engineering-pass-2026-10.md)).

**3. Second, independent validation (2026-10-05).** In a fresh small
validation on 6 previously unseen public engineering tasks whose hidden
acceptance criteria were screened for consistency with the issue before
execution, BoundedCode completed 5/6 successfully, with 5/6 completed using
only the local model (Qwen3.6-35B-A3B, no frontier calls, 0 false
verification passes, 10-30 min per task). The sixth was fixed correctly but
left unverified by a gap in BoundedCode's evidence check
([report](docs/benchmarks/second-independent-validation-2026-10.md)).

This is a small practical validation sample, not a statistically
comprehensive benchmark. Screening excluded 10 of 24 candidates whose
hidden tests were not derivable from their issues; real requests are not
screened.

On the largest repositories tested, the model saw a small fraction of the
source:
- Prometheus at 1.8 M source tokens: 1.6 % in the second validation;
- ~2.6 M in the first: ≤ 1.9 %.

Resume after an interruption worked. On a two-task baseline, plain
OpenHands with the same local model failed the same tasks, faster.

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
* **Verification presets** exist only for Go and JavaScript/TypeScript.
  Verification is offline: install a JavaScript project's dependencies in
  your checkout (`npm ci`), and its `node_modules` are mounted read-only
  into task worktrees; a declared `test` script with nothing installed
  fails. `npm test` must work offline without a browser, or set the stages
  in `.boundedcode/verification.yaml`. Other languages need that file.
* **Cross-service analysis** covers HTTP routes and calls, Kafka-style
  topics, environment variables and Terraform topic/queue resources, in Go
  and TypeScript/JavaScript. gRPC, OpenAPI, protobuf and SQL contracts are
  not analyzed (see [cross-service-analysis.md](docs/design/cross-service-analysis.md)).
* **Frontier escalation** works only through the Codex CLI with a ChatGPT
  subscription sign-in, or by pasting packets manually. API keys are
  refused ([ADR-0009](docs/architecture/adr/0009-frontier-escalation.md)).
* **Real-world evidence is small.** One run each of 8 + 6 public tasks (see
  above); stability across reruns and performance on requests whose
  expected behaviour is not derivable from their text are unmeasured.
* **Verification gaps.** Behavioural evidence for Go is a changed test that
  fails on the base:
  - data-driven test files (for example `testdata/*.test`) are not
    attributed, so a correct change can end UNVERIFIED;
  - a test that does not compile on the base counts as failing there;
  - a test can only prove the reading of the request the agent chose.
* **Ambiguity detection is model-derived** and imperfect: it has flagged a
  clear request as ambiguous and misnamed real alternatives. With the
  default `task.ambiguity: ask`, a false positive costs a clarification
  question.

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
