# Development Guide

## Layout

| Path | Contents |
|---|---|
| `cmd/boundedcode` | entry point |
| `internal/cli` | cobra commands (thin), `setup`, and the terminal UI backend (`tui.go`) |
| `internal/tui` | terminal interface and chat (Bubble Tea); a client of the CLI with no logic of its own |
| `assets.go` | setup scripts and the sandbox build context embedded in the binary |
| `internal/orchestrator` | task run loop and escalation |
| `internal/task` | canonical ledger |
| `internal/contextplan` | context packs |
| `internal/verify`, `internal/policy` | deterministic verification and safety rules |
| `internal/agent{,/openhands,/scripted}` | agent runtime boundary and implementations |
| `internal/inference{,/llamacpp}` | inference boundary, client, gateway, llama.cpp supervisor |
| `internal/repointel{,/cbm}` | repository intelligence boundary and codebase-memory-mcp client |
| `internal/sandbox`, `internal/gitops`, `internal/workspace` | isolation, git, workspaces |
| `internal/frontier` | Z1–Z4 policy, packets, providers |
| `internal/benchmark` | infrastructure and engineering benchmarks |
| `adapters/openhands` | Python adapter and sandbox Dockerfile |
| `benchmarks/fixtures`, `benchmarks/tasks`, `benchmarks/reports` | fixtures, task specs, results |

Build and install the local tree as `boundedcode` and `bcode` with
`make install` (`~/.local/bin`; set `PREFIX_BIN` for elsewhere). The UI's logs
go to `<state dir>/tui.log`. UI tests drive the model directly with a fake
backend (`internal/tui/tui_test.go`); the backend tests run the real CLI
against a temporary `BOUNDEDCODE_HOME` (`internal/cli/tui_test.go`). When a
setup script or a file in `adapters/openhands` changes, the embedded copy
changes with it. Check `go list -f '{{.EmbedFiles}}' .` when adding files.

## Tests

```bash
make check                                     # fmt, vet, unit, race, lint, licenses
go test -short ./...                           # fast unit tests only
go test ./...                                  # + integration tests needing go/git/uv/cbm
BC_TEST_DOCKER_IMAGE=boundedcode-openhands:local go test ./internal/sandbox/ ./internal/agent/openhands/ ./internal/benchmark/
(cd adapters/openhands/python && uv run --group dev pytest -q)
go run ./scripts/serenaguard                   # Serena pin and license-review guard (CI)
BC_SERENA_STAGE2=1 go test ./internal/repointel/serena -run TestStage2 -v   # Serena edit experiment
```

`internal/repointel/serena` has unit tests against a fake Serena (always
run) and integration tests against the real Serena v1.7.0 installed by
`boundedcode serena setup` (or `BOUNDEDCODE_SERENA=/path/to/serena`); they
start gopls and the TypeScript server.

Integration tests skip themselves when a tool is missing. Tests that need a
GPU or a real model live behind the `bench` commands, not `go test`.

## Benchmark hygiene

The MoE configuration computes most experts on the CPU, so **any concurrent
CPU load changes decode speed**. Run `bench infra` on an otherwise idle
machine, with the laptop on AC power, and note the conditions in the
report. Numbers measured under load are only valid for feasibility.

## Conventions

* Persist-then-act: write the ledger before long or destructive steps.
* Interfaces only at replaceable boundaries.
* No global mutable state. Pass dependencies explicitly (`cli.App`).
* Never log secrets or full source. Use `telemetry.Redact` for free text.
