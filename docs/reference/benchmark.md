# BoundedCode benchmark framework

`bcode bench` is a measurement instrument for comparing two control
architectures. It is not a claim that either architecture is better.

## Experimental question

With the same generative model, model/runtime configuration, repository base
revision, task text, limits, network policy, hardware, and independent
evaluator, does the production BoundedCode Supervisor path change the rate of
independently verified task completion relative to a minimally supervised raw
OpenCode workflow?

The framework records the conditions it can measure and marks unknown
conditions as unknown. It never silently gives one arm a different model,
context window, timeout, or task.

## Two modes

### RAW

RAW receives a sanitized task description, a fresh isolated checkout, the
same model/configuration record, and the comparable tool/runtime environment.
It may inspect, edit, test, continue, and finish through normal model-driven
OpenCode behavior. It does not receive a BoundedCode Supervisor, production
ledger, authoritative task worktree, Repo Intelligence context, completion
contract, retry policy, or hidden evaluator.

RAW is not intentionally crippled. Its command is sandboxed with the same
candidate boundary and its OpenCode XDG state is fresh per run. The difference
under study is the control architecture, not the model's basic coding tools.

### BOUNDED

BOUNDED enters the existing production task path. The CLI's production factory
uses the same provider/engine and `internal/supervisor.Runner` assembly as
ordinary BoundedCode task execution, with the benchmark candidate as the
repository root. The ordinary configured production oracle is explicitly
disabled for this path: the benchmark's independent evaluator is the only
oracle. The unattended harness uses the broker's explicit permissive policy so
an accepted task cannot stop at a human gate. That path creates a task, records
the initial candidate and session event, obtains the authoritative worktree and
lease, runs the engine loop, records verification evidence, applies retry and
budget policy, and reaches the production terminal/completion contract.

A benchmark integration must provide a fresh store/runner factory for every
run. The benchmark package does not contain a second Supervisor, a disabled
safety mode, or a benchmark-only completion decision. The deterministic smoke
factory uses a scripted engine only to validate wiring and is explicitly
non-official.

## Independent evaluation and hidden oracles

After the worker stops, the harness:

1. snapshots the candidate and records its content hash and diff;
2. copies that candidate to an evaluator-only workspace;
3. applies the task's hidden oracle files there;
4. runs the declared evaluator command/steps with an independent context and
   timeout; and
5. records `PASS`, `FAIL`, `ERROR`, or `TIMEOUT` plus step output.

The worker request is a separate type that has no evaluator, oracle, expected
labels, or benchmark metadata fields. The worker workspace is materialized
from the fixture/repository with evaluator files excluded. A configured
process sandbox grants the worker its candidate and private temporary state,
not the oracle directory. The loader rejects an oracle inside a fixture and
checks the candidate and its Git history for declared evaluator files.

Before either worker starts, the harness runs the same evaluator against the
immutable post-setup candidate. An official task is rejected unless that
baseline is a genuine evaluator failure; a baseline pass, error, or timeout is
recorded in `independent_evaluator` and makes the result ineligible for
performance evidence. This catches stale or non-discriminating checks before a
model can turn a no-op into a reported success.

The evaluator's `PASS` is authoritative for benchmark correctness. A model's
claim, an OpenCode final response, and BoundedCode's own completion status are
stored separately. A claimed success with evaluator `FAIL` is a
`false_success`; a correct candidate with a claimed failure is a
`false_failure`. The CLI always supplies a selected host confinement layer to
the evaluator; the programmatic evaluator rejects an unset sandbox unless the
caller explicitly sets `AllowUnconfined` for a trusted diagnostic.
`network_policy: allowlist` is rejected at execution time until a task-specific
allowlist can be represented by both adapters; it is never silently widened to
host networking.

## Suite and task format

A suite is a versioned `suite.yaml`; a task is a versioned `task.yaml` beside
its fixture and evaluator material. The canonical shapes are also published
as [`schemas/benchmark-suite.schema.json`](../../schemas/benchmark-suite.schema.json),
[`schemas/benchmark-task.schema.json`](../../schemas/benchmark-task.schema.json),
and [`schemas/benchmark-result.schema.json`](../../schemas/benchmark-result.schema.json).
The frozen artifact is described by
[`schemas/benchmark-manifest.schema.json`](../../schemas/benchmark-manifest.schema.json).

```yaml
schema_version: 1
id: bc-real-001
version: 1
title: Refresh-token race
description: Fix the refresh-token race while preserving the public API.
repository: /path/to/repository-or-source
base_commit: 0123456789abcdef0123456789abcdef01234567
fixture: fixture             # optional immutable local snapshot
languages: [go]
visible_validation:
  - [go, test, ./...]
evaluator:
  oracle: oracle             # outside fixture/
  files: [hidden_refresh_test.go]
  command: [go, test, ./...]
  timeout_seconds: 300
  must_not_change: [internal/auth/hidden_refresh_test.go]
limits:
  wall_clock_seconds: 1200
  max_generation_requests: 20
  max_verification_attempts: 3
network_policy: none
mutable_scope: [internal/auth]
tags: [bug-fix, concurrency]
category: bug_fix
verification: standard
```

Unset wall-clock, generation, and verification limits are normalized to the
same explicit defaults before either worker request is built: 600 seconds, 64
generation requests, and three verification attempts. The normalized values are
recorded in the result and frozen manifest.

`repository` is a string so the same schema can address a local checkout, a
remote, or a materialized fixture. A normal 40/64-character Git object id is
checked exactly. Fixture tasks may use the explicit `base_commit: fixture`
sentinel; their resulting Git commit and content manifest are recorded. A
`content-sha256:<hash>` base is also supported. Setup commands run only after
the immutable starting identity has been checked. If setup creates files, the
harness commits that deterministic post-setup state as the common worker
baseline before either arm starts; both arms therefore see the same generated
state, and the configured base remains separately recorded.

`oracle/` is evaluator-only. It is copied into the evaluator workspace after
the worker exits, never into the worker workspace. Oracle and evaluator hashes
are part of the suite/task identity.

## Result schema and artifacts

Every execution writes `result.json` with schema version 1. It includes:

- suite/task/mode/run/pair/seed identity, physical execution identity, and
  frozen-manifest identity;
- model/provider/runtime/file-hash/sampling/context fields;
- BoundedCode/OpenCode/CPU/GPU/OS/runtime environment observations;
- starting/final candidate hashes, base commit/tree, changed files and diff
  statistics;
- externally enforced start/end/wall-clock/termination data;
- available generation, turn, tool, edit, verification, retry, recovery, and
  token measurements;
- the system's reported status and failure reason;
- independent evaluator status, exit code, duration, and step evidence; and
- independently verified success, false success, and false failure.

Unavailable measurements are pointers serialized as JSON `null`, not zero.
Artifacts are deterministic by suite/task/mode/run and include the sanitized
configuration/task snapshot, stdout/stderr, events, patch, evaluator output,
and retained candidate. Credentials are rejected from task environments and
redacted again before result serialization.

## Fingerprints and model locking

The common comparability fingerprint excludes the mode and covers suite/task
hashes, base commit, model/provider/runtime/file hash, quantization, context,
sampling, limits, network policy, runner configuration, and measured
environment. A separate mode fingerprint records the arm. Reports refuse to
present a known model/context/limit mismatch as a fair pair and warn when a
model file hash or served runtime identity cannot be verified. A configured
model route is never reported as proof of what a provider served; the BOUNDED
adapter must observe the provider's model identity before an official task can
run. An official RAW arm must likewise provide a worker-level served-model
attestation; the CLI's un-attested `--raw-command` adapter is rejected rather
than producing a superficially paired result.

## Scheduling, pairs, resume, and timeouts

`bench plan` is side-effect free. Runs are generated per task and repetition,
then RAW/BOUNDED order is deterministically rotated from `--seed`; the default
is sequential so local model/GPU contention cannot systematically favor an
arm. Each cell has a stable `pair_id` and each execution a stable `run_id`.
The schedule is written before execution. The runner's explicit unattended gate
policy is `permissive`; it is recorded in the suite hash, fingerprints, and
frozen manifest so a future human-gate change cannot silently alter a run.

`bench run` skips valid completed result IDs. `--rerun` explicitly replaces
all of them; `--rerun-id` replaces selected cells. A physical rerun receives a
new execution ID and a new production store, while its stable `run_id` and
`pair_id` remain the schedule identity. An incomplete run directory is preserved
under an `interrupted-*` name and a new candidate is materialized; no candidate,
transcript, ledger, or OpenCode state is reused. A worker has an external
context deadline. On `TIMEOUT`, the current candidate is snapshotted and
independently evaluated when safe; timeout and evaluator status are recorded
separately.

Recovery events are optional evidence (`verification_failure`, `failed_patch`,
`session_interruption`, `fresh_session_resume`, `compaction`,
`repeated_failure`, `provider_interruption`, and recovery attempted/succeeded
events). The reporter derives recovery rates only when tasks emit those events;
it never invents them for ordinary tasks.

## Freeze and reporting

`bench freeze` writes a manifest containing suite/task/evaluator hashes, base
commits, model and runner configuration, limits, code revision, and seed. A
run can verify and retain that manifest with `--manifest`; changing a task,
oracle, or evaluator invalidates it. Reports warn when result directories carry
different manifest/suite identities.

`bench report <results>` shows RAW and BOUNDED side by side:

- independently verified completion rate;
- reported success, false-success, false-failure, worker timeout, evaluator
  timeout, and infrastructure rates;
- median/p90 wall time, input/output/total tokens, generations, tool calls,
  retries, verification attempts, and recovery metrics;
- a paired task-clustered bootstrap interval and conservative verdict;
- per-task/per-pair visibility;
- an explicit performance-evidence eligibility flag, which is false for
  smoke, mixed, incomplete, or incomparable result sets; and
- `schedule_verified`, which is true only when every cell in the durable
  schedule has exactly one identity-matching result.

The bootstrap resamples tasks, not individual attempts. With too few tasks it
prints `insufficient sample size for meaningful statistical conclusion` rather
than a significance claim. A timeout remains visible; infrastructure and
evaluator errors are not converted into ordinary task failures.

## Smoke fixture

`benchmarks/smoke` contains one tiny Go fixture and one hidden test. It is
labelled `smoke`, `official: false`, and `infrastructure-only`. Its scripted
workers prove the harness can execute both adapters, start from identical
base state, hide the oracle, collect candidates, run the independent evaluator,
persist results, and render a paired report. Its numbers are **NON-OFFICIAL
INFRASTRUCTURE SMOKE RESULT** and must not be cited as a BoundedCode versus
OpenCode performance result.

## Creating a real task later

1. Choose an immutable repository revision and record its exact object ID.
2. Put only the starting repository in `fixture/`, or use a local/remote
   repository plus `base_commit`; never include `oracle/` in it.
3. Write task text that states behavior, not the patch.
4. Put hidden regression/build/static checks under an evaluator-only oracle
   directory and declare them as argv steps.
5. Declare language, limits, network policy, safe environment, mutable scope,
   tags, and provenance.
6. Run `bcode bench validate`, `bcode bench plan`, and a non-official dry run.
7. Freeze the suite, then run paired repetitions sequentially on a quiescent
   machine. Review comparability warnings before interpreting any rate.

The corpus is intentionally not populated by this implementation. The smoke
fixture validates the instrument only.
