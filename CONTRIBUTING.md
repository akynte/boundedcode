# Contributing to BoundedCode

The goal is a coherent, verifiable local engineering workflow on an 8 GB GPU.
Useful contributions reduce required context, improve repository evidence,
strengthen execution boundaries, make failures recoverable, or make the reference
stack easier to reproduce.

For a significant change, open an issue describing the problem and a measurable
success criterion before building a new subsystem.

## Development setup

Use Go 1.27.1 (the `go.mod` toolchain), a C compiler for the cgo tree-sitter
grammars, Git and Make. Docker is needed for container/schema checks and some
integration tests. Node/npm is needed for the TypeScript sidecar. A GPU or model
download is **not** required for the ordinary unit-test suite.

```bash
git clone https://github.com/akynte/boundedcode.git
cd boundedcode
make build
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
go install honnef.co/go/tools/cmd/staticcheck@v0.8.1
make check
make docs-test
```

`make check` covers formatting, vet, the storage analyzer, lint/staticcheck,
race-enabled tests, isolation fault injection, schemas/Compose parsing and
evaluation fixtures. **Lint and staticcheck warn and skip if not installed.**
A successful command with those warnings is not the full CI result.

Other checks:

```bash
make sidecar-test       # installs the Node dependencies and runs its tests
make benchmarks-check  # committed result index is current
make image-smoke       # builds/runs a disposable image; Docker required
make vuln              # may download the vulnerability checker
```

The [reference installation](docs/how-to/install.md) is for live model work.
Do not change your shared inference service or launch a second GPU workload just
to run unit tests. Use scratch `BC_DATA` directories for experiments.

## Find the subsystem

| Area | Start here |
|---|---|
| Task phases, acceptance, repair | `internal/task`, `internal/workflow` |
| Native agent and context log | `internal/engine/native`, `internal/contextpack` |
| Index, graph and retrieval | `internal/index`, `internal/graph`, `internal/retrieval`, `sidecars` |
| Inference, profiles and process lifecycle | `internal/llm`, `internal/procman`, `profiles` |
| Tools, confinement and verification | `internal/firewall`, `internal/broker`, `internal/sandbox`, `internal/recipe` |
| Persistent state and evidence | `internal/store`, `internal/ledger`, `internal/artifacts` |
| Optional typed judgments | `internal/judgment`, `evals/judgment` |
| CLI, editor bridge and deployment | `cmd/bcode`, `internal/mcp`, `deploy`, `.github/workflows` |

Read the [architecture](docs/explanation/architecture.md) and
[verification contract](docs/explanation/verification.md) first. Decisions live
in [ADRs](docs/adr/README.md); superseded records are history, not runtime promises.
When docs and executable behavior disagree, inspect the implementation and
correct the discrepancy. Never preserve a false claim because an older design
document said it should be true.

## Changes worth protecting

- Keep storage APIs workspace-scoped and retrieval slices provenance-checked.
  Persistence exemptions require an explicit reason and review.
- Journal intent and outcome around managed side effects; test uncertain outcomes
  and recovery, not only the happy path.
- Keep write grants, recipe results and completion gates outside model authority.
  New judgment sites start logged; promotion needs domain evidence.
- Keep kernel-dependent tests explicit about skips. A skipped sandbox test is
  not a passing containment test.
- A context/model change needs an evaluation with token cost, failure rate and
  false acceptance, not a single impressive transcript.

## Pull requests

One logical change, a regression test where applicable, and updated docs in the
same PR. Describe the previous behavior, the change, tests actually run and any
skips. Security-boundary changes need a design discussion/ADR.

Use Conventional Commits and a DCO sign-off:

```bash
git commit -s -m "fix(retrieval): preserve evidence when rebuilding a packet"
```

Documentation command tests execute only blocks explicitly marked
`<!-- test:run -->`, not every code fence. Keep new docs in `mkdocs.yml`.
Run `make benchmarks` after changing committed result summaries.

The existing `scripts/record-demo.sh` captures a deterministic CLI walkthrough,
not an 8 GB agent benchmark. For a new coding demo, follow the
[capture protocol](docs/maintainers/github-presentation.md#demo-to-capture).

High-value next work is listed with the [known limitations](docs/explanation/known-limitations.md).
