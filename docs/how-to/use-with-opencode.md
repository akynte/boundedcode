# OpenCode integration

OpenCode 2 is the supported BoundedCode user interface. The Go supervisor
supplies tools, a task ledger and verification; OpenCode owns the model session.
The [context architecture and measured compaction defect](../explanation/opencode-context.md)
explain the boundary in detail.

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

Setup also installs the BoundedCode OpenCode plugin and the Bonsai compaction
policy in `opencode.json`. The plugin reads the Go ledger before each model
request. For a 32K Bonsai slot, the policy uses a 12K buffer and retains 4K
recent tokens, avoiding OpenCode 2.0.15's 15K-retained/12.8K-trigger loop.
Restart OpenCode after changing these settings.

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
| `bc_task_start`, `bc_task_resume`, `bc_task_answer`, `bc_task_history` | Begin or resume supervised work and record or retrieve user decisions |
| `bc_read`, `bc_edit` | Mediated file access in the opened repository |
| `bc_verify`, `bc_task_finish` | Collect checks and record a completion verdict |

The BoundedCode tools are registered in
[`internal/mcp/tools.go`](https://github.com/akynte/boundedcode/blob/main/internal/mcp/tools.go) and
[`supervise.go`](https://github.com/akynte/boundedcode/blob/main/internal/mcp/supervise.go). They call shared Go
subsystems; this is not a read-only four-tool bridge.

Editor edits affect the opened checkout. They do not automatically acquire the
native task runner's separate-worktree lifecycle. A finish verdict evaluates
available evidence; it cannot undo an earlier edit made by another editor tool.

At task start, pass explicit acceptance criteria as `requirements` and scope,
security and performance limits as `constraints`. When a new OpenCode session
has several unfinished tasks, call `bc_task_resume` with the intended task ID.
The original objective, these fields, user decisions and verification are
reconstructed from the ledger after compaction and restart. Read/search output
that was never recorded as durable task state may need to be retrieved again.

## Confinement status

`bcode opencode run` is validated end to end: a loopback broker
(`cmd/bcode/opencode_broker.go`), authenticated by a random per-run capability
and pinned to one workspace's store, gives the sandboxed session's context hook
a narrow state channel without mounting the BoundedCode data root — the
sandbox never sees another workspace's ledger, artifacts or signing keys. See
[the context architecture doc](../explanation/opencode-context.md) for the
end-to-end confined run this was validated against, including the real,
measured VRAM ceiling on an 8 GB card. Review
[trust boundaries](../explanation/trust-boundaries.md) for the regular
editor's security limits.

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
