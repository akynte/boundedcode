# Changelog

## Unreleased

### macOS (experimental; compiled and unit-tested, not yet run on a Mac)

- Builds for macOS (arm64 and amd64). The llama.cpp manager checks and
  stops its server through the kernel's process table on macOS instead of
  `/proc`; hardware detection reads `sysctl` (Apple Silicon unified memory
  for Metal).
- Set-up installs tools without bash, curl or a compiler: gitleaks,
  codebase-memory-mcp and prebuilt llama.cpp (the release build of the
  pinned commit: Metal on Apple Silicon) are downloaded for this OS and CPU
  and checked against pinned sha256 values. Linux with a compiler keeps the
  validated source build of llama.cpp; without one it uses the prebuilt
  build (CUDA 12.8 or CPU).
- The sandbox caps its CPU and memory requests to what Docker Desktop's (or
  Podman machine's) VM has, instead of failing to start.
- Path checks compare case-insensitively on macOS and Windows and resolve
  `/var` → `/private/var`; the forbidden-mount list adds the macOS keychain
  and the macOS and Windows cloud-CLI credential folders.
- Build-tag checks target Linux (the sandbox), not the host OS.
- `doctor` accepts Metal and Vulkan devices and gives Docker Desktop hints on
  macOS and Windows.
- Contained frontier escalation on macOS and Windows mounts a pinned Linux
  build of the Codex CLI (setup step `frontier`), since the host's codex
  cannot run in the Linux container.
- A verification stage that fails because `node_modules` were installed for
  macOS or Windows says so and how to install Linux dependencies.
- The installer supports macOS and Linux arm64 (`shasum` when `sha256sum`
  is missing).

### Set-up wizard in the interface (experimental)

- `/setup` (and `m` in System, `p` in Runtime, or the palette) opens a
  wizard: local model or cloud API; a model list rated against this machine
  with the suggestion preselected; or a provider, a masked API-key field
  (stored by the backend, never passed to a command or logged), the
  provider's live model list, a connection test; then one confirmation to
  install what is missing (`setup --yes`).
- Runtime view: use (`u`), download (`d`) and delete (`x`) model weights.
  System view: remove the provider's API key (`K`).
- Forms gain masked inputs and vertical option lists with descriptions.

### Model choice for your hardware (experimental)

- Hardware detection on Linux, macOS (Apple Silicon unified memory through
  Metal) and Windows: RAM, CPU, NVIDIA GPUs and free disk space.
- `bcode model recommend` rates every profile against the machine (fits the
  GPU, MoE expert offload, GPU/RAM split, CPU only, or too large; a rule of
  thumb from the file size and an estimated KV cache) and proposes one; the
  validated default is kept whenever it runs on the GPU. `model list` shows
  the size, status and fit of each; `doctor` rates the default model.
- Five new experimental profiles, pinned by commit and sha256 (all
  Apache-2.0, none gated): Qwen3.5-4B, Qwen3.5-9B, gpt-oss-20b, Devstral
  Small 2 and Qwen3.8-27B. They leave GPU offload to llama.cpp's automatic
  fitting (`--fit`, on by default in the pinned v0.5.0). Only Qwen3.6-35B-A3B
  is validated.
- `model fetch|use|remove|token` download (in Go: resumable, sha256-verified,
  disk space checked, no python3 or bash needed), select and delete models;
  `setup` uses the same downloader. Profiles may carry a license notice the
  user sees before downloading; `model token set` stores a Hugging Face token
  for gated repositories.
- A user override written by `bench infra --apply` keeps the built-in
  profile's revision, checksum and status for the same weights.

### Cloud model providers (experimental)

- **New:** the agent can use a cloud model API instead of the local model:
  OpenAI, Anthropic (through the official Go SDK), Google Gemini, or any
  OpenAI-compatible service. `bcode provider use|key set|key delete|models|test|show`
  configures it; the local model stays the default ([ADR-0010](docs/architecture/adr/0010-cloud-model-providers.md)).
- API keys are stored in the OS credential store (or an owner-only file),
  never in `config.yaml` or on command lines, are redacted by value, and are
  used only by the host-side gateway: the agent sandbox keeps no network and
  never sees a key. Vendor variables (`ANTHROPIC_API_KEY`, ...) are not read;
  `BOUNDEDCODE_<PROVIDER>_API_KEY` is.
- With a cloud provider, the agent's conversation, including repository
  content, is sent to that provider.
- Anthropic thinking blocks and Gemini thought signatures are kept on the host
  and replayed verbatim; history condensation is handled with the Anthropic
  `drop_block` binding control.
- A rejected key blocks the task with the fix; rate limits and outages are
  retried without burning attempts.
- `stats` reports model-call usage per provider, and an estimated cost when
  prices are configured; a task that used a cloud model no longer counts as
  local-only.
- New dependencies (MIT, BSD, Apache-2.0): `anthropic-sdk-go` and its
  runtime, `go-keyring`, `godbus/dbus`. Database migration 4 adds the
  provider to `model_calls`.

### Task contract

- A material ambiguity is checked against the request text
  before `task.ambiguity: ask` can block on it. One more local-model call,
  made only when there is a material ambiguity, quotes the request. The
  ambiguity is dropped when a quote that settles it occurs in the request,
  or when fewer than two of its readings have a supporting quote there. The
  quotes are matched in code, ignoring case and whitespace, and a quote must
  have at least 12 non-space characters. Each demotion is recorded as a task
  decision and in a `task.ambiguity_grounded` event. If the check fails, the ambiguity
  stays material (`task.grounding_failed`), as it does when an answer lacks a
  quote slot per reading or is numbered differently from the questions; a
  "settling" quote that is a reading's own support does not settle it. The result is kept in
  `contract.json`, and older contract files still load. This addresses the
  vue and axios false ambiguities of the second validation; it is covered by
  unit tests with scripted replies and has not been run on real tasks.
- An ambiguity that affects only the implementation is no
  longer material.
- Derivation: the prompt now says what `required` holds for a
  bug report (the expected behaviour) and for a question or proposal (the
  change it asks for). HTML comments are stripped from the request. A
  contract with nothing required is retried once with a corrective
  follow-up. `task.contract_failed` now includes the redacted raw reply,
  truncated to 2000 characters.

### Verification

- Behavioural evidence attributes changed test data (for example
  `promql/testdata/*.test`) to the Go package whose tests read it, and
  compares Go test failures on the base with and without the changed tests,
  so a failure the base already had no longer counts.
- **Behaviour change:** a changed test that does not compile or load on the
  base (it calls code the change adds), or a stage that times out there, is no
  longer behavioural evidence. Such a task ends `tests_green` unless a test
  that also runs on the original code shows the change.
- Directories such as `latest/` or `contest/` are no longer taken for test
  directories.
- `docs/usage/configuration.md` documents `.boundedcode/verification.yaml`.

### Missing or broken dependencies

- Task runs (`task run`, `task create --run`, chat) check the container
  engine and the sandbox image before starting. Docker or Podman not
  installed, a daemon that is down or refuses the user, and a missing image
  are now separate errors, each naming its fix (`setup --only sandbox`).
  Before, these surfaced as `adapter exited before ready: exit status 125` or
  `exec: "docker": executable file not found`.
- An adapter that exits during startup is reported with the last lines of its
  redacted log, not only the log's path.
- Verification: a missing container engine is no longer reported as the
  stage's tool missing (`go not installed`), and optional stages are no
  longer skipped because of it. Engine failures (daemon down, image missing)
  are verification errors with a fix instead of stage failures. Neither ever
  passes the gate.
- Missing model weights and a missing `llama-server` name `setup --only model`
  and `setup --only inference`. In external inference mode, a task run fails
  at once with the URL when the server does not answer, instead of blocking
  later on "local model server unavailable".
- **Behaviour change:** codebase-memory-mcp missing or at the wrong version: `index` fails with one
  message naming `setup --only tools`; a task run warns once and continues
  without graph context.
- The full gate's missing-gitleaks error points to `setup --only tools`
  instead of a script that needs a source checkout.
- Frontier: a missing Codex CLI is reported as not installed, not as
  "not logged in".
- `doctor` tells an engine that is not installed from one whose daemon is
  down, no longer reports the sandbox image as "not built" when it cannot
  check, and points each missing dependency to its `setup --only` step.
  `sandbox build` (and the terminal interface's build action) use the build
  context embedded in the binary when no checkout is configured.
- Hints name the program as you invoked it (`bcode` or `boundedcode`).
- `setup --only` rejects an unknown step name instead of doing nothing and
  reporting success. New `setup --only STEP --force` runs a step that already
  looks complete (for example `--only inference` to rebuild llama.cpp after
  installing the CUDA toolkit); it never rewrites the configuration.
  `doctor`'s llama.cpp CUDA hints point to it.
- Verification diagnoses a broken container engine once a minute at most, not
  once per failing stage.

### Release and install

- `make dist` builds the licenses archive (reproducible: commit time, neutral
  ownership) and includes it in `SHA256SUMS`.
- The installer's uninstall hint uses the actual install directory and lists
  every data directory and the sandbox image.
- `--version` prints the version, like the `version` command.

## v0.1.0-alpha.2 (2026-10-06): `bcode`, chat and guided set-up

Release notes: [docs/releases/v0.1.0-alpha.2.md](docs/releases/v0.1.0-alpha.2.md).

### Added
- `bcode`: one-command install (`scripts/install.sh`), installing `boundedcode`
  and the short name `bcode`. Running either with no arguments opens the
  terminal interface in a chat for the current git repository. The repository
  is registered and indexed on first use. Each message becomes a task, answers
  a task that is waiting, or starts a follow-up from the previous task's
  branch.
- `setup`: checks and, with permission, installs every prerequisite (tools,
  llama.cpp, model weights, sandbox image). It uses pinned installers and a
  sandbox build context embedded in the binary, so no source checkout is
  needed.
- `task apply`: brings a completed task's changes into your checkout, staged
  or committed (`--commit`). It refuses a checkout with uncommitted changes.
- `task create --from TASK`: follow-up tasks start from the previous task's
  branch.
- Terminal interface (`boundedcode tui`): a full-screen client for every CLI
  capability. It has a live task activity timeline, diffs, verification
  stages, workspaces and indexing, repository intelligence, runtime and model
  profiles, frontier escalations, stats, `doctor` and Serena set-up, and a
  console for any command, including benchmarks. Actions run the CLI commands
  in-process, and approvals appear as dialogs. See
  [docs/usage/tui.md](docs/usage/tui.md).
- The audit log's `agent.event` records now include a short, redacted summary
  of each agent action and its stated reason (`action`, `thought`, at most
  300 characters each), and an `is_error` flag on failed tool results.

### Changed
- `boundedcode` (or `bcode`) with no arguments in a terminal opens the
  terminal interface; without a terminal it prints help as before.
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
