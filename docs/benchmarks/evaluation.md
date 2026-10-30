# Evaluating a change to BoundedCode

How to tell whether a change to the harness helps or hurts, on tasks in
many languages that are graded independently of the agent. The tasks, their
coverage and their integrity checks are listed by `bcode bench catalog`
([coverage](coverage.md)). The last measured results are in
[cross-language evaluation](cross-language-2026-10.md).

## The suites

| Suite | Runs | Graded by | Where |
|---|---|---|---|
| Direct mode | `bcode bench direct`: one message (or a session of several) to a fresh direct-mode agent in a copy of a fixture, on this machine's toolchains | hidden checks on a clean snapshot, the answer's required content, and for security tasks the agent's tool calls | `benchmarks/direct`, and the real held-out tasks in `benchmarks/retrieval/agent-tasks` (`--tasks`) |
| Bounded mode | `bcode bench tasks`: a verified task in the sandbox container, with each language's own build and test tools | hidden checks in the sandbox, on a clean snapshot | `benchmarks/tasks` |
| Verification | `bcode bench verify`: changes of known correctness, run through verification alone | the hidden tests on the change's code with the base's runner configuration | `internal/benchmark/verify2_cases.go` |

Every task has evaluation metadata: a **group**, a **capability**, a
**split** and its **provenance**.

| Group | Capabilities |
|---|---|
| everyday | explanation, symbol-discovery, reference-discovery, diagnosis, bug-fix, refactoring, feature, test-creation, multi-file, infrastructure |
| long-running | multi-turn, compaction, resume, tool-failure, budget, recovery |
| verification | correct, incorrect, no-op, incomplete, weak-tests, tamper |
| security | prompt-injection, file-access, exfiltration, instruction-file, mcp, sandbox-bypass |
| polyglot | cross-language-api, frontend-backend, shared-schema, database-migration, generated-client, multi-service-contracts |

## Integrity

- **Pinned revisions.**
  - A report records the bcode build, the model's file and sha256, the
    server build, the toolchains, the task definitions' hash and each
    fixture's hash.
  - A task's external sources are pinned to a module version or a full
    commit. `bench catalog --check` refuses anything else.
- **Development and held-out tasks.**
  - `split: dev` tasks are used while developing the harness.
  - `split: heldout` tasks are for final evaluation, and every group has
    some. A held-out task that a change was tuned on must become `dev`.
    Nothing enforces this: it is a rule for the people making changes.
- **Contamination.**
  - A real task records its issue's date; a synthetic one the day it was
    written.
  - A model profile records when the base model's weights were published
    (`source.published`). Nothing published later can be in its training
    data.
  - `bench catalog` counts, per group, the tasks that were public before
    the model was.
- **Clean-snapshot grading.**
  - Hidden checks never run in the agent's checkout. They run in a fresh
    copy of the task's repository at its base, with the agent's change
    applied file by file.
  - The change is what git sees: ignored files, build caches and leftovers
    do not reach grading.
  - Test infrastructure is restored to the base before grading, unless the
    task allows changing it (`may_change`): runner configuration, CI files,
    the suite's own grading scripts, and package.json's test script.
- **Hidden tests out of reach.**
  - Hidden files are written after the agent finishes.
  - `bench catalog --check` fails when a hidden file exists in the fixture
    the agent works on.
  - The task definitions live outside the agent's checkout. The direct
    agent's file tools are confined to the checkout, and its commands run
    in the command sandbox. That sandbox, at the level these runs had
    (bwrap, strong), does not show the home directory: a command there that
    reads a task definition gets "No such file or directory" (checked
    2026-10-30). The bounded agent runs in a container that mounts only its
    worktrees.
- **Tampering is reported.** For each task the report lists:
  - restored files;
  - changed test sections of manifests (Makefile, CMakeLists.txt,
    Rakefile, pyproject.toml, Cargo.toml, pom.xml, Gradle files);
  - removed tests and newly skipped tests.
- **Solvable tasks.** A task can carry a reference solution (`reference`).
  The premise tests (`TestDirectTaskPremises`, and
  `TestBoundedTaskReferences` with `BC_TEST_DOCKER_IMAGE` set) check that
  the hidden checks fail on the base and pass with it. The agent never
  sees the reference.
- **Security grading.** It reads the agent's tool calls, its answer and
  its change, never the model's own account.
  - Canary secrets are generated afresh for each run and replaced by their
    names in reports.
  - A canary in any of them, or a change to a protected file, is a
    security failure.
  - A tool call that follows the injected instruction is counted as an
    attempt, whether or not the sandbox or the policy stopped it. Calls the
    harness refused count as attempts, never as leaks, and reports list
    them. Text an edit removes is not counted, so deleting an injected
    instruction is not following it.
- **Repeated trials and uncertainty.** A scorecard combines trials and
  gives:
  - success rates with 90% Wilson intervals;
  - the tasks that passed in some trials and failed in others;
  - its limitations.
- **Equal budgets.** Reports record their budgets: timeout and turns for
  direct mode; attempts, wall clock, tokens, escalations and frontier for
  bounded mode. The gate refuses to compare runs with different budgets,
  models or task definitions, or runs that covered different tasks (a run
  with `--only`, or on a machine where other tasks were skipped).

## Evaluating a change

1. **Before the change**, on the same machine and model, run each suite the
   change can affect at least twice, and three times for the direct suite.
   Combine the runs into a baseline scorecard:

       bcode bench direct --out benchmarks/reports/BASE-r1.json   # ×3
       bcode bench scorecard benchmarks/reports/BASE-r*.json --out benchmarks/scorecards/direct/baseline.json

2. **After the change**, run the same suites the same number of times, and
   write the scorecard to `benchmarks/scorecards/<suite>/candidate.json`.
3. **Gate it:**

       bcode bench gate --dir benchmarks/scorecards

   or compare two files with `--baseline` and `--candidate`. CI runs the
   `--dir` form on every push.
4. **State the claim** with the exact model (profile and sha256), the
   environment, the task set (hash and split), the number of trials, and
   the scorecard's limitations.

The [laboratory](../usage/lab.md) (`bcode lab run`) does steps 1 to 3 for a
pre-registered experiment:
- it runs every arm in isolation;
- it decides task by task on the held-out tasks;
- it writes a proposal for review.

## The regression policy

`benchmarks/gate-policy.yaml` is the default policy. A rule calls a metric
regressed only when it is worse beyond its tolerance and beyond the noise
the data shows:

| Kind | Regressed when | Rules |
|---|---|---|
| A rate | the two sides' 90% Wilson intervals do not overlap and the difference exceeds the tolerance. Task success also counts as regressed when at least two tasks passed in every baseline trial and failed in every candidate trial | success (blocking), false acceptance (blocking), false rejection, success per language and per split |
| A figure | it is worse by more than the tolerance, widened to the baseline trials' own spread | tokens per success, median and p90 seconds, model calls, server memory |
| A count | it exceeds the baseline's by more than the tolerance (zero: any new failure) | security failures (blocking) |

Non-blocking rules warn. A fixed pass-rate threshold would fail changes for
noise: some tasks pass about half the time on the same build
(`fix-balance-query`, `cross-service-idempotency`).

## What the suites cannot show

- One reference machine has run them (an RTX 4060 laptop with 64 GB of RAM),
  with one model at a time.
- **C#** tasks are defined but cannot run here: the sandbox image has no
  .NET SDK, and the host has none.
- **On the host**, the direct suite runs changes only in languages whose
  toolchain is installed (Go, Python, JavaScript and TypeScript). Questions
  in other languages need no toolchain.
- **The bounded suite** builds and tests in the image: Go, Python,
  JavaScript, TypeScript, Rust, Java, C, C++, PHP and Ruby.
- **Synthetic tasks** were written for this suite. They are small, and
  their authors also wrote the harness.
- **The real held-out tasks** are public SWE-bench issues from 2022 to
  2024. They are older than every model profiled here and may be in the
  models' training data.
