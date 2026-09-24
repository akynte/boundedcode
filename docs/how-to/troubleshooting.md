# Troubleshooting BoundedCode

Start with the setup TUI when installation is incomplete and with `bcode
doctor` when a configured session fails.

```bash
bcode doctor
bcode doctor --json
```

The JSON report contains no repository content and is suitable for a bug
report. Exit status is `0` for clean, `1` for warnings, and `2` for failures.

## Setup does not complete

- Read the last TUI step. It names the failed dependency, runtime, model,
  provider, credential, or validation check.
- Re-run the same canonical command after fixing it:

  ```bash
  bcode setup
  ```

- A model download interrupted by a network failure leaves a `.part` file,
  not a usable model. Re-run setup; the completed file is reused.
- If the optional Prism build stops, check that you are on Linux x86-64 with
  Git, CMake, Ninja, `nvcc`, an NVIDIA driver, and enough free disk. Re-run
  `bcode setup`; the pinned source checkout is reused, while a partial build is
  never reported as a ready runtime. Use `--install-runtime` only when the
  build was explicitly authorized.
- A source checkout with local edits is refused rather than compiled. Restore
  or remove `$BC_DATA/runtime/src` and rerun setup; setup will fetch the pinned
  revision again. A failed compute-capability query, an existing runtime build
  lock, or a non-Linux/x86-64 host is likewise reported before configuration is
  marked complete.
- A decision-plane credential is required for task execution. Prefer
  `TYPESAFE_API_KEY` or the owner-only credential written by the TUI; never
  put the secret in `bcode.yaml`.

## `bcode opencode` cannot start

1. Confirm setup completed:

   ```bash
   bcode setup
   ```

2. Confirm the configured runtime and model are present:

   ```bash
   bcode config show
   bcode doctor
   ```

3. Confirm OpenCode 2 is on `PATH`:

   ```bash
   opencode --version
   ```

4. Start from the project directory. The command is intended to be usable from
   any project and creates the workspace marker on the first run.

## Runtime or model health

`bcode opencode` waits for the supervisor and model readiness endpoint before
launching OpenCode. A long first start can be normal while a GGUF is loaded.
If readiness fails, the session supervisor log is removed during cleanup; run
`bcode doctor` and rerun setup to validate the files. Do not start a second
copy of the model server to work around a failed session.

## Sandbox unavailable

BoundedCode refuses to start a confined session when no real isolation layer is
available. `bcode doctor` explains whether Landlock, bubblewrap, or the
container boundary is active. The explicit `--unconfined` escape hatch is for
diagnostics only and should not be part of a normal workflow.

A host installation does not gain a container boundary merely because the
binary is installed. Use a trusted project and read
[trust boundaries](../explanation/trust-boundaries.md) before weakening
sandbox settings.

## Workspace or index problems

```bash
cd /path/to/project
bcode workspace show
bcode doctor
bcode index
```

If a workspace was moved, use `bcode workspace adopt`; do not delete its
`.bc/workspace.yaml` pin unless you intend to create a new workspace. If a
session was interrupted, use `bcode task recover` before retrying a task.

## OpenCode tools are missing

Run `bcode opencode` again from the project root. Setup merges the BoundedCode
MCP entry into `opencode.json` and refreshes the managed `AGENTS.md` block
without replacing unrelated settings. An existing `opencode.jsonc` is refused
because comments cannot be safely rewritten; merge the printed block by hand.

## Resource usage after exit

A normal `bcode opencode` session owns its model, API, broker, and temporary
state and removes them before returning. If a process remains, first determine
whether it was launched through this command. A process configured as an
external inference endpoint is intentionally not owned. For a suspected stale
session, stop the editor, rerun `bcode doctor`, and include the JSON report and
`bcode version` in an issue.

## Still stuck

Include `bcode version`, `bcode doctor --json`, the selected data-directory
path, the runtime revision, and the exact command you ran. Do not attach model
files, credentials, or repository source.
