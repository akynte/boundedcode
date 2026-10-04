# Serena (optional LSP symbol navigation)

BoundedCode can use [Serena](https://github.com/oraios/serena) **v1.7.0** to
answer precise symbol questions (where is X, who references X, what
implements X) with real language servers, against the task's own worktree.
The design is in [ADR-0008](../architecture/adr/0008-serena-symbol-navigation.md);
when Serena or the code graph answers which question is in
[system-architecture.md §7.1](../architecture/system-architecture.md).

## Why only v1.7.0

Serena v1.7.0 (tag `v1.7.0`, commit `949a27ef`) is MIT-licensed. Serena's
`main` branch relicensed the application to GPL-3.0-or-later for v2. That
change is not retroactive, so v1.7.0 stays MIT. BoundedCode therefore pins
v1.7.0, verifies the installed license file at runtime, and refuses other
versions. **Serena upgrades are manual and require license review.**

## Install

```bash
boundedcode serena setup          # interactive confirmation; --yes for scripts
```

This checks for `uv`, installs `serena-agent==1.7.0` with `uv sync --frozen`
from the lock file embedded in the binary (exact versions and hashes) into
`~/.local/share/boundedcode/tools/serena-1.7.0/`, verifies the version and
license, then starts Serena once per language: Go is checked (needs `go`
and `gopls` on `PATH`), and the TypeScript language server is installed by
Serena with npm. This is the only step that uses the network.

An existing Serena of yours (`uv tool install`, `~/.serena`) is never
modified or reused implicitly. To use your own installation, set
`repointel.serena.command`; it must be exactly 1.7.0.

## Configure

```yaml
# ~/.config/boundedcode/config.yaml
repointel:
  serena:
    enabled: true          # default: false
    version: "1.7.0"       # must be the pinned release
    auto_upgrade: false    # must stay false
    transport: stdio       # the only supported transport; nothing listens on a port
    max_instances: 2       # Serena + language servers kept resident at once
    idle_timeout: 10m
    startup_timeout: 90s
    call_timeout: 30s      # a call that takes longer stops that instance
    command: ""            # empty: the install made by `serena setup`
```

`--serena on|off` on `task run`, `task resume` and `bench tasks` overrides
`enabled` for one run. Serena's own options are generated per instance;
you do not edit Serena's config files.

## Use

Tasks use Serena automatically for symbols named in the request. You can
also query it directly:

```bash
boundedcode intel symbol CreatePayment --body          # workspace repositories
boundedcode intel refs PaymentRepository/Insert
boundedcode intel impls PaymentRepository --root ~/.local/share/boundedcode/tasks/<id>/work/billing-service
```

`--root` points at any checkout, such as a task worktree. Name paths follow
Serena: nested symbols are `Type/method` (TypeScript classes, Go interface
methods); Go methods are top-level (`CreatePayment`).

## Troubleshooting

`boundedcode serena status` (also part of `doctor`) shows what is wrong.
Instance logs are in `~/.local/state/boundedcode/serena/`.

| Symptom | Cause and fix |
|---|---|
| `serena: not installed` | Run `boundedcode serena setup`. Tasks continue without Serena. |
| `Serena 2.x is not supported` | A v2 (GPL-3.0-or-later) or other version was found at `repointel.serena.command`. It is left unchanged; remove the setting to use the pinned install, or point it at a 1.7.0. |
| `package metadata disagrees` / `installed LICENSE does not match` | The environment was modified. `boundedcode serena setup --reinstall`. |
| `serena go LSP: missing gopls` | `go install golang.org/x/tools/gopls@latest`, make sure it is on `PATH`. |
| `serena typescript LSP: … failed` | Install Node.js and npm, then rerun `serena setup` (it installs the TypeScript server). Tasks never download it. |
| `mcp initialize: … timeout` | A large repository's language server can take long to start (grpc-go: ~15 s). Raise `startup_timeout`. Check the instance log. |
| `failed to start 2 times … retrying after` | Serena is paused for that worktree for 5 minutes after repeated start failures, so tasks are not slowed down; the code graph is used meanwhile. |
| `serena serves project "…", want "…"` (wrong project) | An instance answered for a different project; it is stopped and not used. Each worktree has its own instance and configuration under `~/.cache/boundedcode/serena/instances/`; deleting that directory is safe. |
| Results from the main checkout instead of the task | Should not happen: instances are keyed by the worktree path. `intel … --root <worktree>` shows what Serena sees there. Report it with the instance log. |
| Leftover `gopls`/`tsserver` processes | Every instance's processes carry `BOUNDEDCODE_SERENA_INSTANCE`; they are killed on stop, timeout, cancellation and on the next start after a crash. `ps eww | grep BOUNDEDCODE_SERENA_INSTANCE` lists them. |
| A `.serena/` directory appeared in a repository | Not created by BoundedCode (its Serena data lives under the cache directory). A repository's own `.serena/` is ignored. |

## Upgrading Serena

Serena upgrades are manual and require license review. Do not change the
pin to try a newer version. The process (docs/licensing/policy.md):

1. A written upgrade request.
2. License review of the new tag: LICENSE at the tag, the PyPI artifact,
   and the license of every bundled component. Serena v2 is
   GPL-3.0-or-later for the application, which the project policy does not
   permit in the core.
3. Compatibility review against that tag's source (CLI flags, tool names
   and result formats, config keys), not `main`.
4. `boundedcode bench intel` and the task A/B on the new version.
5. Explicit maintainer approval, then update together: the constants in
   `internal/repointel/serena/version.go`, `internal/config.SerenaVersion`,
   `configs/serena/` (pyproject + `uv lock`), `LICENSES/upstream/serena.txt`,
   the license matrix, `THIRD_PARTY_NOTICES.md`, upstream-components and
   the ADR. CI (`scripts/serenaguard`) rejects partial changes.
