# Install and use BoundedCode

This is the **canonical BoundedCode installation and configuration guide**. It
replaces the older host installer, separate model-server recipe, and manual
runtime setup sequence.

## The short version

Install the `bcode` executable, then run the setup TUI once:

```bash
bcode setup
```

The TUI checks dependencies, creates the data directory, selects a hardware
profile, configures the model runtime, prepares the model/provider files,
configures the decision plane, checks the sandbox, and validates the result.
It is safe to run again after an update or hardware change.

After setup, enter any project:

```bash
cd /path/to/your/project
bcode opencode
```

BoundedCode starts and owns the runtime and required services for that
session. You do not start a model server, API, sandbox helper, or environment
process first.

## Requirements

The measured reference is Linux x86-64 with an NVIDIA CUDA GPU, 8 GB VRAM,
64 GB system RAM, and about 20 GB free disk. The setup TUI checks the actual
host and reports mismatches instead of silently applying the reference values.
Smaller measured profiles and a local external OpenAI-compatible endpoint are
available for other machines.

Install these before the TUI:

- Git and ripgrep;
- a C compiler when building BoundedCode from source;
- OpenCode 2;
- a CUDA-compatible `llama-server` and GGUF model, or a local external
  OpenAI-compatible endpoint;
- a TypeSafe Jev credential for task execution.

For the CUDA path, the setup TUI can build the pinned Prism runtime for you.
That build additionally needs CMake, Ninja, `nvcc`, an NVIDIA driver, and
sufficient free disk; it is never started silently. The TUI shows the pinned
source revision and asks for confirmation. In automation, opt in explicitly:

```bash
bcode setup --non-interactive --yes
# or, when the model already exists:
bcode setup --non-interactive --install-runtime --model /path/to/model.gguf
```

`--install-runtime` is the explicit build authorization. `--yes` accepts the
same default and also downloads the reference model when it is missing. A
runtime that is already discovered is reused. To configure a CPU-only or
operator-owned server instead, use `--external-url` and do not request a local
runtime build.

The TUI can discover an existing runtime and model. It never requires you to
edit YAML by hand. A discovered executable is operator state: setup does not
claim that an arbitrary `llama-server` is the pinned Prism build, so use the
approved build or pass the path of a runtime you trust for your model. A model
download is streamed to a temporary file and renamed only after it completes;
an interrupted download is discarded and can be retried, and is never treated
as a valid model.

## Install the executable

From source:

```bash
git clone https://github.com/akynte/boundedcode.git
cd boundedcode
make build
install -Dm755 bin/bcode "$HOME/.local/bin/bcode"
export PATH="$HOME/.local/bin:$PATH"
```

Then run the TUI:

```bash
bcode setup
```

`--data-dir` selects a non-default data directory. `BC_DATA` is also
supported. The default on a host is `~/.local/share/boundedcode`; a container
uses `/data` when `BC_IN_CONTAINER=1`.

## What the TUI configures

### Dependencies and hardware

The first screen reports Git, ripgrep, compiler, OpenCode, bubblewrap, NVIDIA
driver, optional CMake/Ninja/`nvcc` build tools, platform, memory, and
data-directory filesystem checks. Optional components are clearly marked as
optional. A failed required check stops the flow with a fix rather than
producing a half-configured installation.

### Data and configuration

BoundedCode keeps durable state under the selected data directory:

```text
$BC_DATA/
├── config/       # generated configuration and owner-only credentials
├── models/       # downloaded or selected GGUF files
├── runtime/      # optional pinned Prism source and CUDA build
├── workspaces/   # per-project indexes, ledgers, evidence, and state
├── backups/
└── keys/
```

The TUI writes `bcode.yaml`, `providers.yaml`, optional `judgment.yaml`, and
a completion marker. Existing values are preserved unless you choose to
replace them. A marker is written only after validation succeeds.

### Runtime and model

The default local setup selects a `llama-server` executable and a GGUF model,
adds a stable model alias, and records the active hardware profile. The
runtime is embedded: `bcode opencode` starts it through BoundedCode and stops
it when the editor exits.

When you approve the optional build, setup clones Prism revision
`1a07bfa5f4144274c8f1c9963821dd9d9a51854b` into
`$BC_DATA/runtime/src`, configures CUDA for the detected compute capability,
and builds `llama-server` under `$BC_DATA/runtime/build`. The generated
configuration records the resulting executable, so later sessions do not
compile or start it independently. The source and build are installation data,
not a second process owned by an OpenCode session.

An explicitly external endpoint is also supported for CPU-only or otherwise
unsupported machines. An external server is not BoundedCode-owned and is not
terminated by a session. The normal CUDA setup is embedded so cleanup has one
unambiguous owner.

### Decision plane

Jev is a narrow hosted control plane, separate from local code generation. The
TUI configures the pinned model, strict redaction, confidence floor, and
credential environment variable. If you provide a key, it is stored in an
owner-only `config/jev.env` file rather than in YAML; the session launcher loads
it into the supervisor environment. The key is never printed.

Without a usable decision-plane configuration, setup can finish other local
checks but reports that task execution will stop at its required decision-plane
check. Re-run setup with the credential rather than hiding that boundary.

## Start a session

From any project directory:

```bash
cd my-project
bcode opencode
```

BoundedCode automatically:

1. finds or creates the project workspace marker;
2. refreshes idempotent project OpenCode configuration and managed context;
3. creates private per-session XDG state and temporary directories;
4. starts the supervisor, API, local model runtime, and required services;
5. waits for `/readyz` and model readiness;
6. validates the OpenCode model/context/compaction contract;
7. launches OpenCode in the strongest available sandbox.

Arguments after `--` are forwarded unchanged:

```bash
bcode opencode -- --continue
```

`bcode opencode run` is an explicit alias for scripts. It has the same
automatic lifecycle. `bcode opencode setup` only refreshes project-side
registration and is not a separate installation path.

## Cleanup contract

The launcher owns the process tree for the whole editor session. On normal
exit, error exit, or interrupt it:

- stops OpenCode and descendants;
- stops the broker and removes its capability file;
- asks the supervisor to stop the model and services in reverse order;
- escalates to process-group termination after the configured grace period;
- checkpoints and closes storage;
- removes the session XDG directory, temporary files, and diagnostics.

The model process is therefore terminated, its VRAM is released, and temporary
runtime resources are removed before the command returns. The supported
embedded setup leaves no BoundedCode-owned LLM, API, or session process running
after the command exits. A separate process that is explicitly configured as
external remains operator-owned by design.

## Update, reconfigure, troubleshoot, uninstall

Re-run the same command for all three normal maintenance tasks:

```bash
bcode setup
```

It preserves the data directory and refreshes configuration. Use
`bcode config show` for a read-only view and `bcode doctor` for a structured
health report. See [start, stop and update](start-stop-update.md) and
[troubleshooting](troubleshooting.md) for recovery details.

To uninstall, stop active sessions, remove the executable, and remove the
selected data directory:

```bash
rm -f "$HOME/.local/bin/bcode"
rm -rf "${BC_DATA:-$HOME/.local/share/boundedcode}"
```

Back up the data directory first if you want to retain indexes, evidence,
models, or credentials. Project-side generated OpenCode files are not removed
by the data-directory deletion; review and remove the managed block manually
if desired.
