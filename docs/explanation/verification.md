# Verification and completion

BoundedCode's native agent proposes changes; the supervisor decides whether
the recorded checks and workflow permit completion. Calling `done` is a signal
to check the work, not permission to accept it.

## What is checked

The native phase path discovers verification presets at INTAKE and freezes them
in persisted workflow state. A repository can supply `.agent/verify.toml`;
otherwise manifest discovery supplies applicable Go, installed Node, Rust and
Python checks. Discovery does not install dependencies.

Verification levels select checks. The legacy verification-only Go path uses
`GoRecipes` and the level's required kinds. Native tasks use their discovered
presets and candidate identity. Additional `.bc/verify.yaml` generation and
integration checks are included at the applicable levels.
[Recipe reference](../reference/recipe-format.md) ·
[Declared runtime checks](../how-to/declare-runtime-checks.md).

A native plan must resolve affected-consumer obligations, name exact writable
files, and name checks. The model can choose an already frozen generator preset;
it cannot invent a new executable command through a tool call.

## Evidence and decisions

Each recipe produces a status (`pass`, `fail`, `error`, `skipped`), candidate
hash, summary, exit code, and—when captured—an artifact hash for full output.
A tool that cannot start or times out is an environment error, not a verdict on
the patch. A skipped required check does not count as passing.

The runner checks that applicable required evidence describes the current
candidate, that failures are handled, and that scope and protected-path checks
hold. Evaluation configurations can explicitly compare against a recorded
baseline; this is not permission to ignore arbitrary failing tests.

Failed verification can return the task to EDIT, PLAN or LOCALIZE within bounded
counters. A fresh-context model REVIEW follows successful verification. Its
negative verdict can request repairs; its positive verdict cannot substitute
for the checks. The configured human gate is the final approval step.

After approval, FINALIZE rechecks the candidate and commits on the task branch.
It does not merge or cherry-pick into the original branch. Inspect the branch
and use normal git review before applying it.

## What “candidate-bound” means—and does not mean

The worktree content fingerprint lets code identify stale evidence and detect
changes around verification, approval and recovery. Full output is stored as a
content-addressed artifact with read-only permissions.

**Verification does not run in an immutable source snapshot.** It runs commands
against the writable task worktree with writable build caches. There is no
separate always-on verification worker. Repository tests and build scripts can
execute code and modify files within their sandbox grants. Candidate checks
are not proof against every concurrent or temporary modification.

Generated-output checks separately snapshot, run, compare and restore declared
outputs. That narrower mechanism must not be generalized into a claim that all
verification is immutable.

## Two reasons a green run can still be wrong

A test suite can miss a bug, or a patch can weaken the tests. Model review can
miss both. Optional Jev test-integrity findings can flag/taint evidence for the
reviewer at an authorized tier, but `Tainted` does not alter
`recipe.Result.Passed()`. Jev is not a correctness oracle.

The historical graph experiment recorded false acceptances despite the harness.
See [historical results](../benchmarks/results/2026-09-17-graph-contribution.md).
No zero-false-acceptance guarantee or current Bonsai success rate is claimed.

## Existing changes and editor sessions

```bash
bcode task verify --verify standard
```

This creates a verification task and copies uncommitted state into its worktree,
so it checks the candidate you are inspecting rather than silently checking only
HEAD. It uses the verification-only path; do not assume every native
multi-language preset is selected identically.

MCP `bc_verify` and `bc_task_finish` record an editor session's evidence and
review. Editor edits already exist in its checkout; the result is not a
retroactive apply gate.
[Editor boundary](../how-to/use-with-opencode.md).

## Implementation and tests

Read [task phases](https://github.com/akynte/boundedcode/blob/main/internal/task/phases.go),
[completion contract](https://github.com/akynte/boundedcode/blob/main/internal/task/task.go),
[preset checks](https://github.com/akynte/boundedcode/blob/main/internal/recipe/presets.go), and
[recipe execution](https://github.com/akynte/boundedcode/blob/main/internal/recipe/recipe.go).

Tests cover stale evidence, required checks, scope, candidate changes,
continuation/retry state, and judgment authority. These test the harness's
rules. Representative hidden tests are still needed to establish how often
those rules produce correct engineering outcomes.
