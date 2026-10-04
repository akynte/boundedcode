# Getting Started

> Pre-alpha. Commands may change.

## 1. Prerequisites

| Need | Why | Check |
|---|---|---|
| Linux x86-64, NVIDIA GPU (8 GB+), 32–64 GB RAM | local inference with MoE expert offload | `boundedcode doctor` |
| Go 1.27+ | build the CLI | `go version` |
| Docker or Podman | agent sandbox | `docker version` |
| CUDA toolkit (to build llama.cpp) | inference runtime | `nvcc --version` |
| uv | adapter development without containers | `uv --version` |
| git, ripgrep | worktrees, search | |

## 2. Build and install the external pieces

```bash
make build                                   # ./bin/boundedcode
./scripts/build-llama-cpp.sh                 # pinned llama.cpp v0.5.0 with CUDA
./scripts/install-deps.sh ~/.local/bin       # gitleaks + codebase-memory-mcp (checksum-pinned)
```

Download a model yourself. This project does not redistribute weights. For
example, the default profile expects `Qwen3.6-35B-A3B-UD-Q4_K_M.gguf` from
`unsloth/Qwen3.6-35B-A3B-GGUF` (Apache-2.0).

## 3. Configure

```bash
L=~/.local/share/boundedcode/runtimes/llama.cpp/v0.5.0/bin
./bin/boundedcode init --models-dir ~/models --llama-server $L/llama-server --llama-bench $L/llama-bench \
    --adapter-dir $PWD/adapters/openhands/python
./bin/boundedcode sandbox build --dir adapters/openhands
./bin/boundedcode doctor
```

Tune the model for *your* machine. This measures; it does not guess. Run it
on an idle machine:

```bash
./bin/boundedcode bench infra --ctx 65536 --ubatch 512,1024,2048 --apply
```

## 4. Create a workspace and index it

```bash
./bin/boundedcode workspace create payments
./bin/boundedcode workspace add ~/src/payment-service
./bin/boundedcode workspace add ~/src/ledger-service
./bin/boundedcode index
./bin/boundedcode intel search CreatePayment
```

## 5. Run a task

```bash
./bin/boundedcode task create "Make the ledger consumer idempotent per payment_id" \
    -c "duplicate events post once" -c "go test ./... passes" --run
./bin/boundedcode task status <id>
./bin/boundedcode task events <id>      # audit log
./bin/boundedcode task diff <id>
./bin/boundedcode task resume <id>      # after Ctrl-C, crash or reboot
```

The result is a branch `agent/<id>` in each repository, verified by the
deterministic pipeline. Nothing is pushed or merged automatically. Review it
like any pull request.

## 6. Frontier escalation (optional)

Escalation is off by default. To use a ChatGPT subscription through the
Codex CLI, with no API key:

```yaml
# ~/.config/boundedcode/config.yaml
frontier:
  enabled: true
  provider: codex      # or "manual": the packet is written to disk; answer with `frontier answer`
  require_approval: true
```

Run `codex login` once. Each escalation shows the packet path and asks
before sending. `boundedcode frontier status` shows the history and
outcomes.

## 7. Serena symbol navigation (optional)

Serena v1.7.0 (MIT, pinned) adds type-aware definitions, references and
implementations from language servers, read against each task's worktree.
It needs `uv`, and `gopls` (Go) and/or `node` + `npm` (TypeScript):

```bash
./bin/boundedcode serena setup        # asks before installing the locked environment
./bin/boundedcode serena status       # version, license check, MCP start, Go/TS language servers
```

Then set `repointel.serena.enabled: true` (or pass `--serena on` to `task
run` / `bench tasks`). See [serena.md](serena.md) for configuration,
troubleshooting and the upgrade policy.
