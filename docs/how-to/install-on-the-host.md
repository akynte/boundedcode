# Host installer

The reproducible Bonsai/CUDA setup is in [Install](install.md). This page covers
the repository's convenience script for installing the BoundedCode binary
and optional TypeScript sidecar. It does not install the reference model/runtime.

```bash
scripts/install-bare-metal.sh --check
scripts/install-bare-metal.sh
```

The first command reports dependencies without installing anything. The second
builds `bcode`, installs under `$HOME/.local/bin`, and installs/probes the Node
sidecar when available. `--prefix DIR` changes the destination;
`--no-sidecar` skips the sidecar.

```bash
export PATH="$HOME/.local/bin:$PATH"
export BC_DATA="$HOME/.local/share/boundedcode"
export BC_TYPESCRIPT_SIDECAR_DIR="$HOME/.local/share/boundedcode/sidecars/typescript"
bcode doctor
```

A host install gives the supervisor the operator's host access; it has no outer
Docker boundary. Child commands still require an available sandbox. Kernel,
namespace and toolchain-path compatibility must be checked on the actual host.
Do not equate a successful binary install with validated containment.

Then complete the [reference inference setup](install.md) and
[first task](../tutorials/first-task.md). Missing analyzers/linters are reported
as degraded or skipped, not evidence of a passed check.
