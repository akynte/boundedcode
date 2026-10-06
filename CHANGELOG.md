# Changelog

## Unreleased

- Documentation: terminology and claim qualifications aligned with published
  evidence; no results changed.

## v0.1.0-alpha.1 (2026-10-05): first public alpha

Release notes: [docs/releases/v0.1.0-alpha.1.md](docs/releases/v0.1.0-alpha.1.md).

### Added
- Go control plane with a persistent task ledger and audit log (SQLite):
  resume after a crash or reboot.
- Git worktree isolation per task.
- Local inference through a supervised llama.cpp server, with measured model
  profiles.
- Agent runtime: the OpenHands SDK in a network-less container, with model
  calls tunnelled through the control plane.
- Repository intelligence:
  - codebase-memory-mcp for breadth;
  - optional Serena v1.7.0 for LSP depth;
  - cross-service contract analysis for HTTP, topics, env and Terraform.
- Bounded context packs with ranked retrieval seeds.
- Deterministic verification with behavioural evidence (`TASK_VERIFIED` vs
  `tests_green`).
- Runaway-generation control: thinking and visible-output caps, and a
  progress-aware strategy budget.
- Task contract with material-ambiguity handling (`task run --clarify`).
- Optional frontier escalation: a Z1-Z4 policy via the Codex CLI with a
  ChatGPT sign-in, or manual packets. API keys are refused. Enabled but not
  triggered in the second validation; in the first, 4 frontier calls were
  sent and no task was accepted.

### Security
- Container sandbox:
  - no network;
  - secret masking;
  - protected paths;
  - a read-only verification config taken from the base commit.
- Hardened git worktree handling.
- Symlink-safe host reads.
- Deterministic command policy.
- Frontier packet sanitization that fails closed.
- See SECURITY.md and docs/design/sandbox.md.

### Validation
- Initial validation (2026-10-04): 0/8, then 1/8 after defect fixes.
- Second validation (2026-10-05): 6 screened tasks not used during
  development and never shown to the agent.
  - 5 of 6 strict `TASK_VERIFIED` successes, all local-only: no frontier
    calls (escalation enabled, not triggered).
  - 6 of 6 of the datasets' hidden acceptance tests (hidden from the agent)
    passed.
  - 0 false verification passes among the 5 `TASK_VERIFIED` tasks on the
    screened set.
  - Not an improvement curve over the first validation: the two sets were
    screened differently.
- Small samples, not statistically comprehensive evaluations.

### Known limitations
- Small validation sample; one machine and one model.
- Model-derived ambiguity detection has false positives.
- Behavioural evidence misses data-driven test files consumed elsewhere.
- Frontier escalation and the strategy governor were not exercised in the
  second validation.

### Development history before the alpha

* Targeted engineering pass (2026-10-05):
  * Strategy governor: an attempt that keeps generating without progress
    (no first edit, new test or failing-to-passing test within
    `agent.strategy.no_progress_tokens`, or past the attempt's token and
    time caps) is stopped, recorded and followed by a materially different
    attempt. `agent.max_output_tokens` makes the per-response cap
    configurable.
  * Task contract: the request is read into required behaviour, explicitly
    allowed alternatives, constraints and material ambiguities before
    implementation. Material ambiguity blocks for clarification
    (`task run --clarify`) or, with `task.ambiguity: proceed`, is recorded
    as SPEC_AMBIGUOUS.
  * Retrieval seeds are ranked: quoted error messages are searched
    literally and located at their origin first; names discussed in prose
    come before identifiers from code samples; placeholder names, URL
    parts, @mentions and names matching a large share of the repository
    are dropped or demoted; file links resolve to the file; lexical hits
    prefer code over docs and build output, and skip source maps and
    minified bundles.
* Second validation (2026-10-05): six public tasks not used during
  development and never shown to the agent, screened for issue-derivable
  acceptance tests and frozen before a single run: 5 of 6 `TASK_VERIFIED`
  successes, all local-only (no frontier calls), 0 false verification passes
  among those 5. The sixth (prometheus) was fixed correctly but left UNVERIFIED:
  the Go evidence check does not attribute data-driven test files
  ([report](docs/benchmarks/second-independent-validation-2026-10.md)).
* From the 2026-10-05 failure-driven engineering pass:
  * Completed tasks now distinguish `task_verified` (checks green and a
    test the change added or modified fails on the base commit and passes
    on the change) from `tests_green` (checks green, nothing demonstrates
    the requested behaviour). Without evidence the agent is asked once for
    a reproduction test; a task that still has none ends unverified, never
    as a verified merge candidate. `stats` reports both.
  * The agent sandbox gets verification's read-only Go module cache and
    offline settings, so the agent can build and test what it is judged
    by (it previously could not run a single test in repositories with
    dependencies), with a separate build cache.
  * Go files behind custom build tags that a change touches are compiled
    under those tags (built-in `go-build-tags` stage).
  * Model profiles can cap thinking per response (`reasoning_budget`);
    the shipped profiles use 4096 tokens. Unbounded thinking ended in
    8K-token runaways costing 31-51% of model time on several tasks.
  * The context pack's change impact now follows the worktree's actual
    changes after an interrupted attempt.
* Fixes from the 2026-10-04 small real-world validation:
  * JavaScript/TypeScript verification now runs: the repository checkout's
    installed `node_modules` are mounted read-only into task worktrees (for
    verification and the agent). A declared `test`/`lint`/`build` script
    with no installed dependencies fails instead of being skipped, which
    had let an untested change pass. npm stages also apply to JavaScript
    projects without `tsconfig.json`.
  * Frontier escalation packets are sanitized of all host paths (task
    state, toolchain caches, stack traces, tool output, the agent's diff,
    and any other path under the home directory), not only the workspace.
    A surviving host path used to block the escalation silently and leave
    no record; a refused packet is now kept locally and recorded as a
    `blocked` escalation, counted by `stats`.
  * Verification no longer changes the candidate it judges: tracked files
    rewritten and untracked files created by its stages (e.g. a build that
    regenerates committed bundles) are undone afterwards and reported.
  * Cross-service scanning no longer runs out of memory: JavaScript
    constant bindings were expanded exponentially (vuejs/core reached
    ~58 GiB) and, in semicolon-free code, swallowed later statements. Both
    analyzers now bound evaluation work and value size.
  * Secret masking no longer hides source code: a Go package named
    `credentials` (grpc-go), `credentials.go` or `kubeconfig.go` are
    visible and editable; non-code files in such directories stay masked,
    and `secrets/` and dot directories stay masked whole, code included.

* Optional Serena v1.7.0 (MIT, pinned) integration for LSP-backed symbol
  navigation per task worktree: `repointel.Navigator`, process manager,
  context-planner routing (graph for breadth, Serena for depth), `serena
  setup|status`, `intel symbol|refs|impls`, doctor checks, `bench intel`
  overlap study, `--serena on|off`, CI pin guard (ADR-0008).
* Benchmark harness: Go module-cache sources for large repositories,
  per-task repository-intelligence metrics; the hidden-check self-test now
  uses the bench PID limit (it previously passed Go tasks for the wrong
  reason).
