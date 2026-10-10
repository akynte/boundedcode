# Getting Started

> Public alpha. Commands and configuration may change.

## Fast path

**1. Install.**

Linux and macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/akynte/boundedcode/main/scripts/install.sh | bash
```

Windows (PowerShell; needs Git for Windows and Docker Desktop):

```powershell
irm https://raw.githubusercontent.com/akynte/boundedcode/main/scripts/install.ps1 | iex
```

Where the installer puts the binary:
- `install.sh` puts `boundedcode` and `bcode` in `~/.local/bin`. It uses a
  checksum-verified release binary, or builds with Go when no release has
  one.
- `install.ps1` uses `%LOCALAPPDATA%\Programs\BoundedCode\bin` and adds that
  folder to your user PATH.

Both installers have these properties:
- They never need administrator rights.
- They refuse a binary whose checksum does not match the release's
  `SHA256SUMS`.
- They replace the program atomically, so re-running one upgrades in place.
- `install.sh` refuses to overwrite an unrelated `bcode` or `boundedcode` in
  its target directory (`BC_FORCE=1` overrides).

**2. Choose where the model runs.**

```bash
# Local model (llama.cpp; the validated path; the default model is about 22 GB):
bcode setup

# Or a cloud model API (no GPU needed; your code is sent to the provider):
bcode provider use anthropic --model MODEL   # or openai, gemini, openai-compatible
bcode provider key set anthropic             # stored in the OS credential store
bcode provider test                          # one short request
bcode setup                                  # tools and the sandbox image only
```

`bcode setup` installs the configuration, the pinned tools, llama.cpp, the
model weights and the sandbox image. Each item is skipped when it is already
done or not needed: with a cloud provider, llama.cpp and the model are not
needed. It asks before every download or build.

`bcode setup --check` lists what is still missing. On `main` (after
v0.1.0-alpha.4) it also exits non-zero until everything is in place.

The chat (`bcode` in a repository) offers the same set-up through `/setup`;
see [the terminal interface](tui.md). The rest of this page is the manual
path, and the reference for what setup does.

**3. Run a first task.** See [Your first task](#your-first-task).

## Platforms

Observed evidence for each platform is in the
[platform compatibility matrix](../public-launch/onboarding-validation.md#platform-compatibility-matrix).

| | Linux (x86-64, arm64) | macOS (Apple Silicon, Intel) | Windows (x64) |
|---|---|---|---|
| Status | validated on the reference machine (x86-64); installer and first-run checks pass in clean Debian, Ubuntu and Fedora containers, and for arm64 under emulation | experimental: installer smoke test in CI; the full flow has not been run on a Mac | experimental: installer smoke test in CI; the full flow has not been run on Windows |
| Local inference | llama.cpp built from source (with CUDA when the CUDA toolkit is installed), else the prebuilt release (CUDA 12.8 with an NVIDIA GPU, else CPU) | prebuilt llama.cpp, Metal on Apple Silicon | prebuilt llama.cpp, CUDA 12.4 with an NVIDIA GPU, else CPU |
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

## Your first task

This small example uses the same kind of bug as the README demo. With a
model it takes a minute or two, and it needs only Go, which the sandbox image has. Git must
know who you are (`git config --global user.name "Your Name"` and
`user.email`) for the first commit.

```bash
mkdir shop && cd shop && git init -q -b main
cat > go.mod <<'EOF'
module example.com/shop

go 1.22
EOF
cat > cart.go <<'EOF'
package shop

// Item is one line of a cart.
type Item struct {
	PriceCents int
	Qty        int
}

// Total returns the cart total in cents. Orders of 10 or more items get a
// 10% bulk discount.
func Total(items []Item) int {
	sum, count := 0, 0
	for _, it := range items {
		sum += it.PriceCents * it.Qty
		count += it.Qty
	}
	if count > 10 {
		sum = sum * 90 / 100
	}
	return sum
}
EOF
cat > cart_test.go <<'EOF'
package shop

import "testing"

func TestTotalSmallCart(t *testing.T) {
	if got := Total([]Item{{PriceCents: 100, Qty: 5}}); got != 500 {
		t.Fatalf("Total = %d, want 500", got)
	}
}
EOF
git add -A && git commit -qm "shop"

bcode workspace create shop && bcode workspace add . && bcode index
bcode task create "Orders of exactly 10 items do not get the 10% bulk discount: Total([]Item{{PriceCents: 100, Qty: 10}}) returns 1000 but should be 900." \
    -c "go test ./... passes" --run
```

**Expected result.** `go test ./...` already passes on the buggy code. The
task should end with a line like this:

```text
task t…: status=completed phase=review verification=task_verified attempts=… tokens=… escalations=0 (…)
```

`bcode task diff <id>` then shows `count > 10` changed to `count >= 10`,
plus a new test for 10 items.

If the agent changes the code but adds no test, BoundedCode asks for one
once. Without a test, the task ends `verification=tests_green`
(UNVERIFIED).

The result depends on the model, and a model can fail this task.

The README demo is a recorded, reproducible version of this scenario, with
the evidence re-checked by hand. Its script, [`demo/evidence/demo.sh`](../../demo/evidence/README.md),
runs with your model, or without one using a scripted stand-in.

## When a step fails

| Symptom | Cause and fix |
|---|---|
| `bcode: command not found` after installing | `~/.local/bin` is not on your PATH. The installer prints the line to add for your shell. Open a new terminal afterwards. |
| `install.sh` says an existing `bcode` "is not a link to BoundedCode" | Another program named `bcode` is in the target directory. Move it, or install elsewhere with `BC_BIN_DIR=DIR`. |
| `checksum mismatch` | The download was corrupted or altered, so nothing was installed. Re-run the installer; if it persists, report it. |
| `docker is installed but not usable` | Start Docker (Docker Desktop, or `sudo systemctl start docker`). On Linux, also add your user to the `docker` group and log in again. Then run `bcode setup --only sandbox`. |
| `sandbox image … is not built`, or "needs a rebuild" | Run `bcode setup --only sandbox`. It builds the image locally (about 5 GB) and never pushes it. |
| `llama-server not found` | Run `bcode setup --only inference`, or use a cloud provider (`bcode provider use NAME`). |
| llama.cpp built without CUDA despite an NVIDIA GPU | Install the CUDA toolkit, then run `bcode setup --only inference --force`. Without the toolkit, set-up on `main` uses the prebuilt CUDA build. |
| `no workspace selected` (on `main`: `no workspace yet`) | Run `bcode` in the repository (the chat sets one up), or `bcode workspace create NAME && bcode workspace add . && bcode index`. |
| `rejected the API key` | Run `bcode provider key set NAME` again; `bcode provider test` checks it. |
| A task ends `blocked` | It ran out of attempts, tokens or time. `bcode task status <id>` says why; `bcode task resume <id>` continues it. |

`bcode doctor` checks everything at once and prints the fix for each
failure.

## 1. Prerequisites

Versions marked "tested" are what the reference machine (Debian 13,
x86-64) runs; older ones may work but are not checked. For macOS and
Windows, see [Platforms](#platforms); the manual steps in section 2 are the
Linux path, and `bcode setup` installs the same pieces on every platform.

| Need | Version | Why | Check |
|---|---|---|---|
| A machine for local inference | default model (Qwen3.6-35B-A3B): NVIDIA GPU (8 GB+) and 32–64 GB RAM, as validated; smaller profiles fit smaller machines | local inference (MoE expert offload for the default); not needed with a cloud model API | `bcode model recommend` |
| Go | 1.27.2+ (`go.mod`) | build the CLI (not needed with a release binary) | `go version` |
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
deterministic pipeline (see [Verification](verification.md) for what
`tests_green` and `TASK_VERIFIED` mean). Nothing is pushed or merged automatically. Review it
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
