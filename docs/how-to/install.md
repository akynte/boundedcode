# Install the 8 GB reference configuration on Linux

This walkthrough reproduces the **Linux x86-64, NVIDIA RTX 4060 Laptop, Bonsai
PTQ1_0** inference configuration. It uses the native Go agent and a separately
started local model server.

**You will need a TypeSafe Jev credential.** Generation runs on your GPU; the
decision plane does not. Jev is a required runtime component, and a task run
refuses to start without a usable one — see
[step 7](#7-configure-the-decision-plane-required) and
[what leaves the machine](../explanation/judgment-data-flow.md). Every other
command, including `bcode doctor`, works without it, which is how you diagnose it.

**Scope.** This builds the whole 8 GB reference: Bonsai answers every
generator role (localization, planning, editing and review) from one resident
server, with nothing swapped in or out.
[The model stack](../reference/model-stack.md) says what each role does and
what has been measured.

This is a **host install**: BoundedCode and the Prism inference server run
directly on Linux. The container boundary is not installed, so `bcode doctor`
will report that layer as absent. The shipped container image does not include
the Prism kernels required by this Bonsai model; a tested container recipe for
this exact setup is not yet available. Use this workflow for local development
with repositories you trust, and read the [trust boundaries](../explanation/trust-boundaries.md)
before running tasks against other people's code.

The reference has 64 GB system RAM. Budget at least 20 GB of free disk for the
model, runtime build and basic caches, plus space for your repositories and
toolchains; this is a planning estimate. [Hardware details](../explanation/8gb-runtime.md).

## 1. Prerequisites

Install Go **1.27.1** from [Go downloads](https://go.dev/dl/), a working NVIDIA
driver, and the CUDA toolkit. The observed build used **CUDA 13.3**, driver
**595.84**, and compute capability **8.9**. Those are reference versions, not
a claimed universal minimum.
[NVIDIA's Linux installation guide](https://docs.nvidia.com/cuda/cuda-installation-guide-linux/).

On Debian/Ubuntu, the remaining build tools are:

```bash
sudo apt-get update
sudo apt-get install -y build-essential cmake ninja-build pkg-config \
  libssl-dev git curl ripgrep bubblewrap
go version
nvidia-smi
nvcc --version
```

If `go` or `nvcc` is not found, the tool is usually installed but not on
`PATH`. The Go tarball and NVIDIA's toolkit do not add themselves. Add them
(and to your shell profile) before building:

```bash
export PATH="/usr/local/go/bin:/usr/local/cuda/bin:$PATH"
```

Optional tools. Without them BoundedCode still runs, with less:

| Tool | Without it |
|---|---|
| `bubblewrap` (installed above) | No DR-3 layer 3; concurrent tasks share a PID view |
| Node 22 and `npm` | TypeScript and Python are read lexically, with no call graph ([step 8](#8-prepare-a-repository-and-run-a-task)) |
| `golangci-lint`, `semgrep` | Their verification recipes skip, which is not the same as passing |

`ripgrep` is strongly recommended; without it lexical search falls back to a
slower path.

A Linux kernel with Landlock or a working bubblewrap setup is needed for host
task confinement. `bcode doctor` reports availability. Host mode does not have an
outer container boundary; use only repositories whose build/test commands you
are prepared to execute. [Trust boundaries](../explanation/trust-boundaries.md).

## 2. Build BoundedCode

```bash
git clone https://github.com/akynte/boundedcode.git
cd boundedcode
make build
if ! grep -Fq "export PATH=\"$PWD/bin:\$PATH\"" "$HOME/.bashrc"; then
  printf '\nexport PATH="%s/bin:$PATH"\n' "$PWD" >> "$HOME/.bashrc"
fi
grep -Fq 'export BC_DATA=' "$HOME/.bashrc" || \
  printf 'export BC_DATA="%s/.local/share/boundedcode"\n' "$HOME" >> "$HOME/.bashrc"
grep -Fq 'export BC_PRISM_DIR=' "$HOME/.bashrc" || \
  printf 'export BC_PRISM_DIR="%s/.local/share/boundedcode-runtime"\n' "$HOME" >> "$HOME/.bashrc"
source "$HOME/.bashrc"
bcode config init
```

This adds the checkout's `bin` directory and the data/runtime paths to Bash's
startup file if they are not already present, so they are available from new
terminals and other project directories. If you move the checkout later, update
its `PATH` entry in `~/.bashrc`. The binary requires cgo; do not build with
`CGO_ENABLED=0`.

After installing OpenCode, run `bcode opencode` from any project directory to
initialize BoundedCode there and launch OpenCode with its MCP tools ready. The
first run creates the workspace marker and editor configuration; later runs
check and refresh generated setup only when needed. The command makes the exact
`bcode` executable it launched available to OpenCode's MCP process, including
when the checkout is built locally with `make build`.

`config init` creates `$BC_DATA/config/bcode.yaml` and `providers.yaml`; it
refuses to overwrite existing configuration. Existing installations should edit
the relevant fields below, not use `--force` indiscriminately.

<!-- test:run -->
```console
$ bcode version
boundedcode …
schemas: index=… ledger=… telemetry=…
```

## 3. Build the Prism runtime

Bonsai's PTQ1_0 requires Prism's ternary kernels, not stock llama.cpp. Build
the latest [Prism release](https://github.com/PrismML-Eng/llama.cpp/releases/latest):

```bash
export BC_PRISM_DIR="$HOME/.local/share/boundedcode-runtime"
BC_PRISM_TAG="$(curl -fsSLo /dev/null -w '%{url_effective}' \
  https://github.com/PrismML-Eng/llama.cpp/releases/latest)"
BC_PRISM_TAG="${BC_PRISM_TAG##*/}"
echo "Prism release: $BC_PRISM_TAG"
git clone --depth 1 --branch "$BC_PRISM_TAG" \
  https://github.com/PrismML-Eng/llama.cpp.git "$BC_PRISM_DIR"
cmake -S "$BC_PRISM_DIR" -B "$BC_PRISM_DIR/build" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release -DGGML_CUDA=ON \
  -DCMAKE_CUDA_ARCHITECTURES=89
NINJA_STATUS="[%f/%t %p, %es] " \
  cmake --build "$BC_PRISM_DIR/build" --target llama-server llama-bench -j 8
```

The build prints a `[done/total percent, elapsed]` counter as it compiles each
file. The CUDA kernels take the longest.

Record the printed release tag with any results you report. The reference run
used revision `1a07bfa5` (see [the model stack](../reference/model-stack.md)).
This assumes a new runtime directory. Architecture `89` is for the reference
Ada GPU; use the correct target architecture for another GPU and record that
configuration separately. See [Prism's runtime guide](https://github.com/PrismML-Eng/Bonsai-demo)
for upstream build/platform changes.

## 4. Download the model

Download the latest 1-bit PTQ1_0 file from
[prism-ml/Ternary-Bonsai-2-27B-gguf](https://huggingface.co/prism-ml/Ternary-Bonsai-2-27B-gguf).
Only the language model is needed; do not download the F16 or PQ2_0 weights or
the vision projector for this setup.

```bash
BC_MODEL_URL="https://huggingface.co/prism-ml/Ternary-Bonsai-2-27B-gguf/resolve/main/Ternary-Bonsai-2-27B-PTQ1_0.gguf"
BC_MODEL="$BC_DATA/models/Ternary-Bonsai-2-27B-PTQ1_0.gguf"
mkdir -p "$BC_DATA/models"
BC_MODEL_HEADERS="$(curl -fsSI "$BC_MODEL_URL")"
BC_MODEL_REV="$(printf '%s' "$BC_MODEL_HEADERS" | tr -d '\r' |
  awk -F': ' 'tolower($1) == "x-repo-commit" { print $2 }')"
BC_MODEL_SHA="$(printf '%s' "$BC_MODEL_HEADERS" | tr -d '\r"' |
  awk -F': ' 'tolower($1) == "x-linked-etag" { print $2 }')"
echo "Model revision: $BC_MODEL_REV"
curl -fL --retry 3 \
  "https://huggingface.co/prism-ml/Ternary-Bonsai-2-27B-gguf/resolve/$BC_MODEL_REV/Ternary-Bonsai-2-27B-PTQ1_0.gguf" \
  -o "$BC_MODEL"
echo "Verifying SHA-256 (this takes a moment for a 6 GB file)..."
echo "$BC_MODEL_SHA  $BC_MODEL" | sha256sum -c -
```

`curl` prints the percentage downloaded, speed and time remaining while it
runs. The expected SHA-256 comes from Hugging Face for the same revision that is
downloaded. Stop if `sha256sum` does not print `OK`. Record the printed model
revision with any results you report. The reference run used revision
`6ed5e12b` (see [the model stack](../reference/model-stack.md)).

## 5. Start inference

In a dedicated terminal, the paths from Step 2 are already set. Confirm the
model and server binary exist before launching:

```bash
test -f "$BC_DATA/models/Ternary-Bonsai-2-27B-PTQ1_0.gguf" || {
  echo "Model not found under $BC_DATA/models; check BC_DATA and complete step 4" >&2
  exit 1
}
test -x "$BC_PRISM_DIR/build/bin/llama-server" || {
  echo "llama-server not found under $BC_PRISM_DIR/build/bin; check BC_PRISM_DIR and complete step 3" >&2
  exit 1
}

"$BC_PRISM_DIR/build/bin/llama-server" \
  --model "$BC_DATA/models/Ternary-Bonsai-2-27B-PTQ1_0.gguf" \
  --alias boundedcode-bonsai \
  --ctx-size 32768 --parallel 1 --n-gpu-layers 99 \
  --flash-attn on --cache-type-k q8_0 --cache-type-v q8_0 \
  --batch-size 2048 --ubatch-size 512 --threads 8 --threads-batch 16 \
  --temp 1.0 --top-p 0.95 --top-k 20 --min-p 0.0 --jinja \
  --checkpoint-min-step 512 \
  --host 127.0.0.1 --port 8080
```

Wait for the model to load. In a second terminal:

```bash
curl -fsS http://127.0.0.1:8080/health
```

Expected: `{"status":"ok"}`. Keep one generation server/slot active and leave
GPU headroom for runtime allocations. Ctrl-C in the server terminal stops
inference; `bcode` does not stop an externally owned server.

## 6. Configure the reference

Keep the inference server running in its dedicated terminal. In a second
terminal, run this one command:

```bash
bcode config reference
```

It sets the Bonsai profile, local inference URL, port, and provider routing in
both configuration files, then checks that the server responds `local ... ok`.
It preserves other settings in `bcode.yaml`; it replaces `providers.yaml` with
the single-provider reference setup. Run it again any time you want to restore
these reference settings.

The profile's published budgets are starting settings, not measurements from
your machine. Step 7 runs `bcode doctor` after configuring judgment. On this
host install, the missing outer container boundary and documented
network-containment limitation are expected warnings. The profile-fit estimate
remains until you benchmark it, and index freshness remains until Step 8 builds
the index.

## 7. Configure the decision plane (required)

BoundedCode requires TypeSafe Jev to run tasks. From the checkout, run:

If you pulled newer BoundedCode changes since Step 2, rebuild first with
`make build` so the doctor check uses the updated executable.

```bash
scripts/configure-judgment.sh && source "$HOME/.bashrc"
```

It prompts for the API key without displaying it, writes the reference
`judgment.yaml`, and runs `bcode doctor`. The key is stored outside the
repository in `~/.config/boundedcode/jev.env` with owner-only permissions
(mode `600`); it is plaintext on disk, and `~/.bashrc` loads it in new Bash
terminals. If a judgment config already exists, the script backs it up before
replacing it. Keep the private key file out of untrusted backups and sync
services; use an OS secret manager instead if you require encrypted storage.

The reference uses a pinned Jev model and `redact: strict`, which sends
repository metadata only, not source lines. Read
[what leaves the machine](../explanation/judgment-data-flow.md) before changing
the redaction mode. `bcode judgment sites` lists the decisions that can affect a
task; [Use judgments](use-judgments.md) explains their authority tiers.

## 8. Prepare a repository and run a task

Repositories need a commit, an installed toolchain and locally available
dependencies. Native tasks start from committed code; commit or separately
preserve existing work before creating the task. The
[first-task tutorial](../tutorials/first-task.md) uses a disposable copy of the
included Go service and needs no third-party Go modules.

For TypeScript analysis, install the sidecar from this checkout:

```bash
npm --prefix sidecars/typescript ci
export BC_TYPESCRIPT_SIDECAR_DIR="$PWD/sidecars/typescript"
```

For Python analysis, install the pinned `scip-python` sidecar the same way:

```bash
npm --prefix sidecars/python ci
export BC_PYTHON_SIDECAR_DIR="$PWD/sidecars/python"
```

Use Node 22 for both sidecars. Without them, those languages are read lexically,
with no call graph; see
[repository intelligence](../explanation/repository-intelligence.md).
These tools do not consume model VRAM.

With the model server running and `TYPESAFE_API_KEY` set, register the
repository, index it and run a task from inside it:

```bash
cd /path/to/your/repository
bcode workspace init
bcode index
bcode doctor
bcode task create --title "Fix the failing parser test without changing its expected behavior" \
  --scope "internal/parser/**" --verify standard
bcode task run <task-id> --diff
```

Replace the title and scope with ones that fit your repository, and use the
task ID `task create` prints. If work reaches a gate, inspect it with
`bcode gate list` and `bcode gate show <gate-id>`, approve it explicitly, then
run `bcode task retry <task-id>`. The
[first-task tutorial](../tutorials/first-task.md) walks through this end to end.

## Keep the environment across terminals

Every step above relies on environment variables set in the current shell.
Add the ones you use to your shell profile, adjusting paths to your checkout:

```bash
export PATH="$HOME/boundedcode/bin:/usr/local/go/bin:/usr/local/cuda/bin:$PATH"
export BC_DATA="$HOME/.local/share/boundedcode"
export BC_PRISM_DIR="$HOME/.local/share/boundedcode-runtime"
export BC_TYPESCRIPT_SIDECAR_DIR="$HOME/boundedcode/sidecars/typescript"
export BC_PYTHON_SIDECAR_DIR="$HOME/boundedcode/sidecars/python"
```

Keep `TYPESAFE_API_KEY` in a secrets manager or a file only you can read
rather than in a shared profile.

## Container deployments

Docker is useful for bounding host access and for reproducible evaluation
runtimes, but **the shipped CUDA image downloads stock llama.cpp b11042**, not
the Prism build above. Do not expect its embedded server to load this GGUF.

The existing compose files remain available for CPU tooling and external
inference, or an operator-built image containing Prism. Build from source with
`make image DOCKER_TARGET=cpu` to inspect the tooling image. It runs as UID
10001; its data volume and repository mounts must be writable by that user.
Do not mount the Docker socket into the supervisor container.

On Linux, a host server bound to `127.0.0.1` is not reachable from a bridge
container merely by adding `host.docker.internal`. Arrange a deliberately
restricted reachable inference endpoint or co-locate the server; do not expose
an unauthenticated model/API service broadly just to make the connection work.

A pinned, tested Bonsai container recipe remains a packaging follow-up.
[Storage setup](persistent-storage.md) ·
[Host installer helper](install-on-the-host.md).
