# Changelog

## Unreleased (pre-alpha)

Initial implementation. See `docs/development/status.md` for what is
implemented, experimental and planned.

* Fixes from the 2026-10-04 small real-world validation:
  * JavaScript/TypeScript verification now runs: the repository checkout's
    installed `node_modules` are mounted read-only into task worktrees (for
    verification and the agent). A declared `test`/`lint`/`build` script
    with no installed dependencies fails instead of being skipped, which
    had let an untested change pass. npm stages also apply to JavaScript
    projects without `tsconfig.json`.
  * Secret masking no longer hides source code: a Go package named
    `credentials` (grpc-go), `credentials.go` or `kubeconfig.go` are
    visible and editable; non-code files in such directories, and dot
    directories, stay masked.

* Optional Serena v1.7.0 (MIT, pinned) integration for LSP-backed symbol
  navigation per task worktree: `repointel.Navigator`, process manager,
  context-planner routing (graph for breadth, Serena for depth), `serena
  setup|status`, `intel symbol|refs|impls`, doctor checks, `bench intel`
  overlap study, `--serena on|off`, CI pin guard (ADR-0008).
* Benchmark harness: Go module-cache sources for large repositories,
  per-task repository-intelligence metrics; the hidden-check self-test now
  uses the bench PID limit (it previously passed Go tasks for the wrong
  reason).
