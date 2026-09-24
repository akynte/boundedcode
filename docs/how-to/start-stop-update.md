# Start, stop, update, and uninstall

The supported lifecycle is session-oriented. There is no separate model-server
or API start step.

## Start

```bash
cd /path/to/your/project
bcode opencode
```

BoundedCode starts the runtime and services required for that session and
stops them when OpenCode exits. See [Install and use BoundedCode](install.md)
for the complete flow.

## Stop

Exit OpenCode normally. The launcher performs ordered cleanup before returning.
For an interrupted process, send `Ctrl-C` once and allow the configured grace
period to elapse. `bcode doctor` can identify a stale lease or an interrupted
workspace, but it is not a substitute for stopping an active editor.

Do not start an independently managed model process and expect
`bcode opencode` to stop it. The setup TUI's embedded mode gives the session a
single owner; an explicitly external endpoint is intentionally outside that
ownership boundary.

## Reconfigure

Run the canonical setup TUI again:

```bash
bcode setup
```

It preserves the data directory, refreshes generated configuration, and
validates the result. Use `bcode config show` for a read-only view of the
effective configuration.

## Update

Back up persistent data before changing a binary or schema:

```bash
bcode backup --all
```

Install the new `bcode` executable using the project's normal release/source
process, then run:

```bash
bcode setup
bcode doctor
```

Schemas migrate forward when the data directory opens. Database downgrades
across schema versions are unsupported; restore a compatible backup or keep
upgrading. Model and runtime artifacts are separate from the BoundedCode
version, so record the runtime revision and model checksum when comparing
results.

## Uninstall

Stop all sessions and back up anything you want to keep. Then remove the
executable and selected data directory:

```bash
rm -f "$HOME/.local/bin/bcode"
rm -rf "${BC_DATA:-$HOME/.local/share/boundedcode}"
```

This removes indexes, ledgers, evidence, cached models, keys, and setup
credentials. Project-side generated files remain in each repository; remove
the managed BoundedCode block from `AGENTS.md` and the BoundedCode MCP entry
from `opencode.json` by hand if the project no longer uses it.
