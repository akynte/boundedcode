# Use OpenCode with BoundedCode

OpenCode is the supported interactive client. BoundedCode owns the session
lifecycle around it: project registration, local model startup, required
services, sandbox preparation, and cleanup.

## Start

After the one-time [`bcode setup`](install.md), use one command from any
project:

```bash
cd /path/to/your/project
bcode opencode
```

The command is safe to run from a new project. It creates the workspace marker
when needed, refreshes generated project configuration, starts the configured
runtime, waits for readiness, and launches OpenCode. It does not require a
separate `bcode api`, model server, or environment command.

`bcode opencode run` is the explicit spelling for scripts and has identical
behavior. `bcode opencode setup` is only a project-registration refresh.

## What is prepared automatically

For every session BoundedCode creates private OpenCode XDG directories and a
temporary directory, starts the supervisor and configured local model, waits
for `/readyz`, validates the model context and compaction policy, registers
the BoundedCode MCP server, installs the context plugin, selects the strongest
available sandbox, and starts the restricted `bc-editor` agent.

The OpenCode process receives only the environment and paths needed for that
workspace. Its direct shell, file, web, and Code Mode routes are denied; the
supervised `bc_*` tools are the supported path for repository reads, edits,
verification, and task state.

## What happens when OpenCode exits

Cleanup is part of the command, not a separate operator step. BoundedCode:

1. stops OpenCode and its process group;
2. stops the broker and removes the per-session capability;
3. stops the model and other BoundedCode-owned services through the supervisor;
4. closes storage cleanly;
5. removes the session XDG directory, temporary files, and logs.

It waits for termination, so the local LLM is stopped, VRAM is released, and
temporary CPU/RAM consumers are gone before the shell prompt returns. An
inference endpoint explicitly configured as external is not BoundedCode-owned
and is left alone.

## Tool surface

| Tool | Responsibility |
|---|---|
| `bc_status`, `bc_graph_impact`, `bc_search`, `bc_reindex` | Repository status, relationships, retrieval, and index refresh |
| `bc_task_start`, `bc_task_resume`, `bc_task_answer` | Durable task intent, requirements, constraints, and decisions |
| `bc_task_history`, `bc_task_verification` | Candidate-bound historical evidence |
| `bc_read`, `bc_edit` | Supervisor-mediated repository access and task worktree edits |
| `bc_verify`, `bc_task_finish` | Sandboxed verification and completion verdict |
| `bc_task_memory*`, `bc_task_fact`, `bc_note_add` | Typed memory and durable repository notes |

Start a supervised task with explicit requirements, acceptance criteria,
constraints, and non-goals. The model proposes operations; BoundedCode
authorizes them, records them, and decides whether the task may continue or
finish. Passing checks are evidence, not a model's claim of success.

## Configuration and reconfiguration

The first launch refreshes `opencode.json`, the managed block in `AGENTS.md`,
and the context plugin without replacing unrelated user settings. Re-running
`bcode opencode` is idempotent. To change the installation-level runtime or
model, rerun [`bcode setup`](install.md), then start a new session.

If tools do not appear, run `bcode doctor` and confirm the command came from
the same `bcode` executable used for setup. An existing `opencode.jsonc` is
refused rather than rewritten because comments cannot be preserved safely.

## Troubleshooting

- **Runtime does not start:** run `bcode setup`; it validates the executable,
  model, profile, provider, and data directory.
- **Model context mismatch:** rerun `bcode setup` and start a fresh session so
  OpenCode receives the new limits.
- **No supervised tools:** rerun `bcode opencode` from the project root and
  inspect the reported `opencode.json` path.
- **Unexpected resource use after exit:** run `bcode doctor`; a session that
  was not launched through this command is not owned by this lifecycle.
