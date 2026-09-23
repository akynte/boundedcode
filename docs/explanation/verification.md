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

**Verification runs in a fresh snapshot, not in the task worktree.** Before
each run the supervisor checks out the task's base commit into a separate
directory and applies the task's diff (`git diff --binary` against the base,
ignored files excluded). `.bc/` and `.agent/` always come from the base commit,
so a candidate cannot change which checks run. The snapshot is removed after
the run and recreated for the next one. The journal records the base commit,
the SHA-256 of the applied patch and the snapshot's content manifest beside the
worktree candidate.

What this does and does not buy:

- A check that writes files cannot change the task's worktree or what is
  committed to its branch. Build output, ignored files and scratch state from
  the edit loop never reach verification.
- The snapshot is **writable, not immutable**. Tests and build scripts can
  still modify it during a run, and build caches are still shared and writable.
- It runs as the same user, under the same sandbox, in the same supervisor
  process. It is not a separate verifier identity or an always-on
  verification worker.
- The engine's own `run_recipe` tool still runs in the task worktree. It is
  the model's self-check and has no say in acceptance.

Generated-output checks additionally snapshot, run, compare and restore their
declared outputs within the verification snapshot.

When an operator has installed [hidden acceptance
checks](../how-to/add-hidden-acceptance-checks.md), they run in the same
snapshot after the recipes. Their results carry only a check ID and a verdict,
and the completion contract requires each one that applies to pass. A hidden
check that could not run blocks acceptance.

## Tests that reach the change

The test preset answers whether the suite passes. It does not say whether
anything in the suite exercises the code that changed, and it cannot tell a test
that passed from one the change taught to skip. After the recipes, and before
any hidden check is installed, verification:

1. finds the Go functions and methods whose lines the patch touches, new ones
   included;
2. finds the tests that reach each one: through the code graph (callers across
   packages, up to three hops, as of the base commit), and in the candidate's
   own test files in the same package, by name;
3. runs exactly those tests with `go test -json`, one run per package;
4. records one result, `impact: tests reaching the change`, with a report in
   the artifact store naming each declaration, its tests and how each was found.

That result fails, and blocks acceptance, when a test that reaches the change
**now skips and its file was changed by the task**, when such a test was
**removed or renamed** by the task, or when a
[`require_tests` policy](../how-to/write-a-policy-rule.md#require-test-evidence)
covers a changed declaration and no reaching test passed. A reaching test that
fails is noted but judged by the test preset, with its baseline allowance.
Everything else, such as a declaration no test reaches outside a policy, is
reported, not enforced.

This is static reach, not coverage. The graph misses calls through function
values, reflection and generated code. The same-package scan matches by name,
so a test that mentions a same-named identifier counts. Neither sees other
languages. It is skipped at the `low` level and when the build fails.

## The evidence chain

Every verification run appends one record to an append-only, hash-linked chain
in the ledger: the base commit, the SHA-256 of the exact patch applied to the
snapshot, the snapshot's content manifest, the oracle digest and hidden-check
IDs, and each check's verdict and artifact hash. Each record carries the hash
of the one before it and a signature by the verifier key under `keys/`.

`bcode task attest` verifies every hash, link and signature. What that detects,
and what it does not:

- A record edited, removed, inserted or reordered after the fact.
- A chain rewritten and re-hashed: the signatures cannot be redone without
  the key.
- **Not** truncation of the newest records, on its own. The task's commit
  carries an `Evidence-Head:` trailer; `attest --head` checks the chain still
  reaches it.
- **Not** a dishonest supervisor. The key is readable by the user the
  supervisor runs as. The signature says a record came from the supervisor or
  someone with its credentials, not that the verification was honest.

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
