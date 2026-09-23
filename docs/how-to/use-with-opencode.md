# Optional OpenCode integration

The reference task runner is the native Go engine. OpenCode is an optional
editor/MCP integration, not a required second agent or the default runtime.

## Setup

Install OpenCode separately and put `bcode` on PATH. In the repository to inspect:

```bash
bcode workspace init
bcode index
bcode opencode setup
```

Setup merges the BoundedCode MCP entry into `opencode.json` and replaces
only its managed block in `AGENTS.md`. Review those changes before committing.
It does not download a model, launch a sandbox or disable the editor's own
tools. Re-run setup to refresh the managed index/notes context.

## Tool surface

| Tools | Responsibility |
|---|---|
| `bc_status`, `bc_graph_impact`, `bc_search`, `bc_reindex` | Workspace status, reverse-dependency evidence, retrieval, indexing |
| `bc_note_add` | Persist a bounded repository note |
| `bc_task_start`, `bc_task_answer` | Begin editor supervision and answer its gate |
| `bc_read`, `bc_edit` | Mediated file access in the opened repository |
| `bc_verify`, `bc_task_finish` | Collect checks and record a completion verdict |

These eleven tools are registered in
[`internal/mcp/tools.go`](https://github.com/akynte/boundedcode/blob/main/internal/mcp/tools.go) and
[`supervise.go`](https://github.com/akynte/boundedcode/blob/main/internal/mcp/supervise.go). They call shared Go
subsystems; this is not a read-only four-tool bridge.

Editor edits affect the opened checkout. They do not automatically acquire the
native task runner's separate-worktree lifecycle. A finish verdict evaluates
available evidence; it cannot undo an earlier edit made by another editor tool.

## Explicit confinement

Inspect the current flags, then launch the editor through the confinement path:

```bash
bcode opencode run --help
bcode opencode run
```

This path prepares the sandbox, a scrubbed environment and mediated-tool
configuration. It denies built-in tools that would bypass mediation. Review
`bcode doctor` and [trust boundaries](../explanation/trust-boundaries.md);
kernel availability and the deployment still determine OS protection.

## Troubleshooting

If tools do not appear, check `bcode version` in the editor's environment and run
`bcode mcp`: it serves JSON-RPC over stdio, logging to stderr, until stopped.
Open the initialized repository root. Refresh stale graph evidence with
`bc_reindex` or `bcode index`.

Tool paths are confined to the opened repository, including symlink resolution.
This does not confine unrelated tools supplied by another MCP server or an
unconfined editor.
