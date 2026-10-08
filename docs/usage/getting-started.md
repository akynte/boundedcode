# Getting Started

> Public alpha. Commands and configuration may change.

## Fast path

Linux and macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/akynte/boundedcode/main/scripts/install.sh | bash
cd ~/src/payment-service     # any git repository
bcode
```

Windows (PowerShell; needs Git for Windows and Docker Desktop):

```powershell
irm https://raw.githubusercontent.com/akynte/boundedcode/main/scripts/install.ps1 | iex
cd $HOME\src\payment-service
bcode
```

macOS and Windows support is new and **has not yet been run on those
systems**; see [Platforms](#platforms) below.

The installer puts `boundedcode` and `bcode` in `~/.local/bin`, from a
checksum-verified release binary, or built with Go when the release has none.
On the first run, `bcode` shows what is missing and `/setup` installs it,
asking before every download or build. It covers what sections 2 and 3 do by
hand: the configuration, the pinned tools, llama.cpp, the model weights and
the sandbox image. Run `bcode setup --check` to see the same list from the
shell. Then describe a change in the chat; see
[the terminal interface](tui.md). The rest of this page is the manual path,
and the reference for what setup does.

## Platforms

| | Linux (x86-64, arm64) | macOS (Apple Silicon, Intel) | Windows (x64) |
|---|---|---|---|
| Status | validated on the reference machine (x86-64) | experimental, not yet run on a Mac | experimental, not yet run on Windows |
| Local inference | llama.cpp built from source (CUDA), or the prebuilt release | prebuilt llama.cpp, Metal on Apple Silicon | prebuilt llama.cpp, CUDA 12.4 with an NVIDIA GPU, else CPU |
| Sandbox | Docker or Podman | Docker Desktop or Podman machine | Docker Desktop (WSL 2 backend) |
| Cloud providers | yes | yes | yes |
| Serena (optional) | yes | yes (an interrupted run can leave language servers running) | not supported yet |

**Windows notes.** The agent's sandbox is a Linux container; BoundedCode
mounts your repositories at `/host/<drive>/...` inside it and translates
paths both ways. Task worktrees use relative `.git` pointers, so the
BoundedCode data folder and your repositories must be on the same drive (set
`BOUNDEDCODE_HOME` on that drive if they are not). Worktrees are checked
out without line-ending conversion (`core.autocrlf=false`) and with long
paths enabled. Configuration, credentials and task data folders get an
access list for your user only. JavaScript projects whose `node_modules` were
installed on Windows need Linux dependencies for verification (the stage
explains how).

## 1. Prerequisites

Versions marked "tested" are what the reference machine (Debian 13,
x86-64) runs; older ones may work but are not checked. For macOS and
Windows, see [Platforms](#platforms); the manual steps in section 2 are the
Linux path, and `bcode setup` installs the same pieces on every platform.

| Need | Version | Why | Check |
|---|---|---|---|
| A machine for local inference | default model (Qwen3.6-35B-A3B): NVIDIA GPU (8 GB+) and 32–64 GB RAM, as validated; smaller profiles fit smaller machines | local inference (MoE expert offload for the default); not needed with a cloud model API | `bcode model recommend` |
| Go | 1.27.1+ (`go.mod`) | build the CLI (not needed with a release binary) | `go version` |
| Docker (or Podman) | tested: Docker 29.8; Podman is configurable but not tested on the reference machine; Docker Desktop on macOS and Windows | agent sandbox, contained Codex | `docker version` |
| CUDA toolkit | tested: 13.4 (`nvcc` in `/usr/local/cuda/bin`) | only to build llama.cpp with CUDA on Linux (the prebuilt Windows CUDA build ships its runtime) | `nvcc --version` |
| uv | 0.12.18 (the version the sandbox image and CI use) | adapter development without containers, Serena setup | `uv --version` |
| Python | adapter: >= 3.12 (`requires-python`; the image uses 3.13); Serena: >= 3.11, < 3.15 | adapter outside containers, Serena | `python3 --version` |
| git, ripgrep | git >= 2.17 | worktrees, search | `git --version` |

## 2. Build and install the external pieces

```bash
make build                                   # ./bin/boundedcode
./scripts/build-llama-cpp.sh                 # pinned llama.cpp v0.5.0 with CUDA
./scripts/install-deps.sh ~/.local/bin       # gitleaks + codebase-memory-mcp (checksum-pinned)
```

`install-deps.sh` installs only gitleaks and codebase-memory-mcp. Docker,
uv, the CUDA toolkit, Go and Python come from your system.

Download a model yourself. This project does not redistribute weights.
Review the license on the model page first. The default profile
(`configs/models/qwen3.6-35b-a3b.yaml`) expects
`Qwen3.6-35B-A3B-UD-Q4_K_M.gguf` (about 22 GB, Apache-2.0) from
`unsloth/Qwen3.6-35B-A3B-GGUF` at the commit pinned in its
`source.revision`:

```bash
./bin/boundedcode model recommend            # the model that suits this machine
./bin/boundedcode model fetch qwen3.6-35b-a3b
```

The download is pinned to the profile's commit, resumes if interrupted, and
is checked against the sha256 recorded in the profile. It goes to
`models_dir`, or `~/.local/share/boundedcode/models` when that is unset.
`scripts/fetch-model.sh` does the same from a shell.

## 3. Configure

```bash
L=~/.local/share/boundedcode/runtimes/llama.cpp/v0.5.0/bin
./bin/boundedcode init --llama-server $L/llama-server --llama-bench $L/llama-bench \
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
like any pull request, or bring it into your checkout:

```bash
./bin/boundedcode task apply <id>            # staged, for you to review and commit
./bin/boundedcode task apply <id> --commit   # committed on your current branch
./bin/boundedcode task create "Also log each retry" --from <id> --run   # follow-up on its branch
```

The same is available in the chat (`bcode`), where each message becomes a
task.

### Multi-repository tasks: the compatibility report

When a task changes a gRPC, protobuf or OpenAPI contract between task
repositories, the full gate also runs the
[cross-repository compatibility gate](../design/cross-repo-compatibility.md)
(experimental). Each affected link gets `compatible`, `broken` or
`untested`, with the commits, commands and reason. `task status <id>` shows
the latest report and marks results recorded for earlier commits as stale.
`verify <id> --full` re-checks the current commits. The TUI shows the same
report in the task's Verification tab.

For example, on the [`contract-break` fixture](../../benchmarks/fixtures/contract-break/README.md),
with the Docker sandbox, after `protos` renamed a field that `checkout` still
uses (excerpt; every repository's own full gate passed):

```text
cross-repository compatibility: BROKEN (3 broken, 2 untested, 0 compatible)
  BROKEN     grpc_def     grpc shop.payments.v1.PaymentService/Charge [changed]
             checkout@a5f2bdbb99 internal/pay/client.go:23 -> protos@f70b3b206e payments/v1/payments.proto:9
             checkout's checks fail with protos's candidate (checkout@a5f2bdbb99 + protos@f70b3b206e) and pass with protos's base commit: internal/pay/client.go:23:77: unknown field AmountCents in struct literal of type paymentsv1.ChargeRequest
             breaking: field 2 of shop.payments.v1.ChargeRequest renamed amount_cents -> amount_minor
             resolve checkout: `go list -m -f {{.Dir}} example.com/shop/protos` in checkout@a5f2bdbb99 + protos@f70b3b206e => pass; example.com/shop/protos => protos@f70b3b206e
             candidate checkout: `go test -count=1 -covermode=set -coverpkg=example.com/shop/checkout/internal/pay example.com/shop/checkout/internal/pay` in checkout@a5f2bdbb99 + protos@f70b3b206e => fail
             control checkout: `go test -count=1 -covermode=set -coverpkg=example.com/shop/checkout/internal/pay example.com/shop/checkout/internal/pay` in checkout@a5f2bdbb99 + protos@b7ec3cc04e => pass
  UNTESTED   grpc_def     grpc shop.payments.v1.PaymentService [changed]
             payments@6a984796cc internal/server/server.go:25 -> protos@f70b3b206e payments/v1/payments.proto:8
             the task's repositories pass against the candidate, but the definition change is breaking for code built from the base definition (services already deployed, or consumers outside the task); nothing tests that compatibility
             breaking: field 2 of shop.payments.v1.ChargeRequest renamed amount_cents -> amount_minor
             candidate payments: `go test -count=1 -covermode=set -coverpkg=example.com/shop/payments/internal/server example.com/shop/payments/internal/server` in payments@6a984796cc + protos@f70b3b206e => pass; internal/server/server.go:29-42 ran
  ...
```

An `untested` link withholds `TASK_VERIFIED`; the reason says which
evidence is missing. Supported: Go sides of gRPC and protobuf links, built
against the provider's committed generated code, and OpenAPI sides whose
tests read the specification. Other shapes are reported `untested`.

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
