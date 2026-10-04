# ADR-0008: Serena v1.7.0 for LSP-backed symbol navigation

Status: accepted (2026-10-03)

## Context
codebase-memory-mcp (ADR-0005/0006) gives breadth: a persistent graph per
repository, search, call tracing, git-diff impact and cross-service links.
Its symbol answers come from tree-sitter and name matching, and they describe
the primary checkout as indexed, not the task worktree. Serena
(github.com/oraios/serena) wraps language servers (gopls, the TypeScript
server, …) behind MCP tools for type-aware symbol lookup, references,
implementations and symbol-level edits.

Licensing: tag `v1.7.0` = commit `949a27ef1e5fda1a6e7b561e777bcece345c6ffd`
is MIT (LICENSE sha256 `16017e50…`). `main` (`2.0.0.dev0`) relicensed the
Serena application to GPL-3.0-or-later; SolidLSP stays MIT but is not
published separately. PyPI `serena-agent==1.7.0` was verified against the
tag: wheel sha256 `6dbf1459…`, all 213 packaged files byte-identical to
`src/` at the commit, with the same LICENSE.

Behaviour observed in v1.7.0's source and binaries (not `main`):
* MCP `serverInfo.version` is the MCP SDK's version (`1.28.1`), not Serena's.
  `serena --version` appends the git HEAD of whatever repository encloses the
  install directory (`1.7.0-dec97a4a` inside our checkout).
* A server has one active project. Without configuration it writes
  `.serena/` (project.yml, pickled symbol caches, memories) into the project.
  A repository's own `.serena/project.yml` can set an `activation_command`
  that Serena runs when the project is trusted (default: all paths).
* Start-up pings `oraios-software.de` unless `SERENA_USAGE_REPORTING=false`;
  the default web dashboard listens on a port and fetches news.
* Language servers start with `start_new_session=True` and
  `PR_SET_PDEATHSIG(SIGTERM)`. The TypeScript server is `npm install`ed on
  first use.

## Decision
1. **Pin and gate.** Only Serena 1.7.0 is accepted. `boundedcode serena
   setup` installs it with `uv sync --frozen` from an embedded lock
   (`configs/serena`, wheel hash pinned), after asking. The version gate
   checks `serena --version` (prefix only), the package metadata of the
   executable's own environment and the installed LICENSE hash; it never
   upgrades or downgrades. `scripts/serenaguard` fails CI if the pin changes
   without the matrix, notices, ADR and a benchmark for the new version, or if
   anything installs Serena unpinned. No updater may touch it.
2. **External process, owned by the control plane.** `internal/repointel/serena`
   starts `serena start-mcp-server` over **stdio** (no listener), one instance
   per checkout root (strategy B: a pool keyed by worktree path, LRU-bounded
   to `max_instances`, default 2, idle stop after 10 min). Strategy A
   (instance per task) is the same pool with a different key and wastes
   language-server warm-up across a task's retries; strategy C (switching
   one instance between projects) serializes all repositories and risks
   answers from the wrong project. Each instance gets a private
   `SERENA_HOME`, our own config, a generated `project.yml` **outside the
   worktree** (so agent-writable files are never trusted and nothing lands in
   the task diff), `trusted_project_path_patterns: []`, no dashboard, no usage
   ping, `GOTOOLCHAIN=local`, `GOPROXY=off`, offline npm and the scrubbed
   host environment. Every process (Serena, language servers and their
   children) carries an instance tag, so stop, cancellation, timeouts and a
   restart after a control-plane crash kill them all.
3. **Read-only (Stage 1).** Instances expose exactly `find_symbol`,
   `find_referencing_symbols`, `find_implementations`, `get_symbols_overview`
   and `get_current_config` (used to verify the active project). The project
   is `read_only`. Symbol-level editing is not enabled for tasks (see
   evidence).
4. **Behind `repointel.Navigator`.** A new interface, keyed by checkout root,
   next to `repointel.Intelligence`. The context planner routes symbols named
   in the task: Serena first for definition, references and implementations
   in each **task worktree**; in multi-repository tasks the code graph first
   decides which repositories mention the symbol (breadth), then Serena
   answers inside those (depth). On any Serena error, timeout or miss the
   planner uses the graph's snippet and callers for that symbol. Impact,
   cross-service contracts and architecture stay with codebase-memory and
   `internal/xservice`.
5. **Not an agent tool.** The agent runs in a container; Serena runs on the
   host. Serena context reaches the agent only through control-plane packs,
   rebuilt on every attempt and on resume. There is no tool-selection prompt
   for the model to forget (no Serena hooks or client-specific guidance are
   installed).
6. **Optional, off by default.** `repointel.serena.enabled: false`;
   `serena setup` plus `enabled: true` (or `--serena on`) turns it on. A
   missing, unsupported or failing Serena never blocks a task. Rationale: the
   agent A/B below shows no change in task success and only a trend toward
   less agent effort, while each instance costs 0.3–1.6 GiB RAM next to an
   MoE model whose experts already run from system RAM.

## Consequences
* Symbol context reflects the agent's current edits; codebase-memory's
  snippets reflect the indexed primary checkout.
* Cost per active repository: one Python process plus language servers
  (measured below), started lazily; at most `max_instances` stay resident.
* Serena runs outside the agent sandbox, like codebase-memory-mcp and
  gitleaks. Mitigations: read-only tool set, not reachable by the model,
  repository-supplied Serena config ignored, caches outside agent-writable
  paths, the sandbox's secret masks passed as `ignored_paths`, no network for
  toolchains. Residual: gopls runs `go list` on the worktree on the host
  (cgo directives are restricted by Go's flag allowlist; toolchain downloads
  are disabled).
* Upgrading to Serena 2.x is a GPL-3.0-or-later decision and is out of scope
  for the core; it would need a separate legal and architectural review.

## Evidence
* Overlap study `benchmarks/reports/20261004T035902Z-intel-overlap.md`
  (`boundedcode bench intel`, 17 ground-truth questions on the Go and
  TypeScript fixtures, grpc-go v1.84.0 (938 files) and the Temporal Go SDK
  v1.49.0 (285 files)): Serena recall 1.00, precision 0.99, no decoys,
  2.0k context tokens in total; codebase-memory recall 1.00, precision 0.90,
  2 decoys (method-name-only `IMPLEMENTS`; a same-named `RunF` in another
  package); grep-and-read recall 1.00, precision 0.76, 4 decoys, 99k
  tokens. Serena's one miss of precision is gopls listing an interface that
  structurally satisfies `PayloadConverter`. Serena is slower: warm
  0.1–3 s per question (the first references query per server 2–5 s),
  start 3.3 s (fixtures), 5.8 s (Temporal), 14.9 s (grpc-go); 0.3–1.6 GiB
  RSS per instance including the language server.
* Stage 2 edits `benchmarks/reports/20261004T031140Z-serena-stage2-edits.md`:
  Go inserts, replacements and a cross-file rename were exact and
  gofmt-clean; a broken body was caught by build verification; **a
  TypeScript rename on a cold server changed only the declaration** (silent
  partial edit) and was correct only after the server had warmed up.
  Editing therefore stays disabled for tasks; OpenHands' own editor remains
  the only writer.
* Agent A/B `benchmarks/reports/20261004T063000Z-ab-serena-summary.md`
  (Qwen3.6 local, 3 tasks × 3 repetitions per arm, interleaved): verified
  9/9 with and 9/9 without Serena. On the small interface change every
  Serena run used fewer tokens (−22 %) and tool calls; on the Temporal SDK
  (285 files) medians favour Serena (610 s / 76k tokens vs 954 s / 144k)
  but one Serena run was the slowest overall (an unrelated gofmt detour), so
  the difference is not significant at n=3. Serena increases the source
  tokens placed in packs (it moves exploration from the agent into the
  pack). No decoy files were edited in either arm.
* grpc-go could not be used for the agent A/B: the sandbox's secret-path
  heuristic masks its `credentials/` packages, so it cannot build in the
  container (documented in docs/design/sandbox.md; not changed here).
