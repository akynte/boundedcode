# Optional OpenCode integration

The reference task runner is the native Go engine. OpenCode is an optional
editor/MCP integration, not a required second agent or the default runtime.

## Start OpenCode

Install OpenCode separately, build BoundedCode, and add the build directory to
your shell's `PATH` as shown in the [installation guide](install.md). Then move
to a project directory and run:

```bash
bcode opencode
```

On the first run, this creates a workspace marker if the current directory is
not already in a workspace, registers BoundedCode's MCP server, and writes its
managed context into `AGENTS.md`. It then launches OpenCode. Later runs check
that generated setup and update it only when something changed. The command
also makes the `bcode` executable that launched OpenCode available to its MCP
process, so a stale binary elsewhere on `PATH` is not used.

The MCP entry uses OpenCode 2's `mcp.servers` configuration. If the project has
the older flat `mcp` server map, setup moves those server entries under
`mcp.servers` while preserving MCP-wide settings and the other project config.
See [OpenCode's MCP configuration](https://opencode.ai/v2/docs/mcp-servers/).
BoundedCode sets `codemode: false`, so its MCP tools are exposed as direct tools
instead of being routed through OpenCode's JavaScript `execute` Code Mode. This
avoids asking the model to encode ordinary tool inputs as JavaScript.

Setup also manages a small block in `~/.config/opencode/AGENTS.md` (or the
platform's OpenCode config directory). OpenCode loads global `AGENTS.md`
guidance in every project, so the instructions distinguish JavaScript Code Mode
from the shell and language runtimes across repositories. Only the managed
block is refreshed; your other global instructions are preserved. See
[OpenCode's instruction scope](https://opencode.ai/v2/docs/instructions/).

When BoundedCode has a local inference endpoint configured, setup also registers
it using OpenCode 2's custom provider format. For the reference Bonsai model, it
declares tool support, text-only input, a 32K context, and an 8K output limit.
Bonsai's optional vision projector is not loaded by the reference text-coding
setup, so OpenCode cannot send screenshots to that model. Use a vision-capable
model for image questions.

The workspace marker, `opencode.json`, and managed `AGENTS.md` block are local
project setup; review them before committing. `bcode opencode` does not build
the source index. Run `bcode index` when you want graph-backed retrieval, and
run `bcode opencode` again to refresh the generated context.

For manual setup without launching the editor, use `bcode opencode setup`.

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

If tools do not appear, run OpenCode with `bcode opencode` from the project
directory so its MCP process uses the same binary. Check `bcode version`, and
confirm the workspace marker exists at `.bc/workspace.yaml`. Refresh stale
graph evidence with `bc_reindex` or `bcode index`.

If a task reports `Unknown identifier 'None'` from an `execute` tool, the model
sent Python syntax to a JavaScript execution tool (`None` is Python syntax; use
`null` in JavaScript). That individual tool call failed; it does not mean the
BoundedCode MCP connection or inference server stopped. Run Python through the
shell tool instead. If the assistant then ends after extended reasoning without
a final answer, continue the conversation and ask it to summarize the result.
For image input rejected by the local endpoint, switch to a vision-capable
model; the local Bonsai model is text-only.

Tool paths are confined to the opened repository, including symlink resolution.
This does not confine unrelated tools supplied by another MCP server or an
unconfined editor.
