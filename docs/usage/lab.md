# The harness laboratory

`bcode lab` studies BoundedCode's own benchmark runs and tests changes to the
harness in controlled experiments. It produces evidence and proposals.

It does not improve BoundedCode by itself:
- It never changes the configuration or the code of the system you use.
- A proposal changes nothing until a person approves it and promotes it.
- A code change is never applied by the laboratory at all.

The [laboratory record](../benchmarks/lab-2026-10.md) has its first analysis,
the past decisions it re-read, and its first experiment.

## The loop

| Step | Command | What it does |
|---|---|---|
| Analyse | `bcode lab analyze REPORT...` | Reads benchmark reports and, for direct mode, each run's session transcript. Finds recurring failure patterns with deterministic detectors |
| Hypothesise | `bcode lab hypothesize ANALYSIS.json --drafts DIR` | Turns recurring patterns into hypotheses from a fixed catalog, and drafts an experiment for each |
| Write code | `bcode lab worktree NAME` | An isolated worktree of HEAD for writing a code candidate. Save it with `git diff` as a patch |
| Run | `bcode lab run EXPERIMENT.yaml` | Builds and runs every arm in isolation, decides on held-out tasks and writes proposals |
| Check | `bcode lab evaluate RUN-DIR` | Checks every report's hash and decides again: the decision must come out byte for byte the same |
| Re-decide | `bcode lab evaluate RUN-DIR --out DIR` | Checks the hashes and writes a new decision to DIR, leaving the recorded one as it is: for re-reading a run after the decision rules changed. It makes no proposals |
| Re-read | `bcode lab evaluate EXPERIMENT.yaml` | Decides an experiment whose arms are recorded reports (retrospective) |
| Approve | `bcode lab approve PROPOSAL.json --reviewer NAME` | Records your approval of the proposal's exact content |
| Promote | `bcode lab promote PROPOSAL.json [--write]` | Shows, and with `--write` makes, an approved configuration change |

Experiment runs are kept under the data directory's `lab/`
(`~/.local/share/boundedcode/lab`): apart from projects and production state.

## Analysis

`lab analyze` reads `bench direct` and `bench tasks` reports. It joins each
direct run with its transcript by session id. Transcripts are found under
`--sessions`, by default the cache's `bench-direct`, where the benchmark leaves
them.

Every detector reads only what the run recorded; none asks a model:

| Pattern | Kinds | Read from |
|---|---|---|
| `retrieval_miss` | the task's files never opened, or first opened past half the run | the transcript, and the task's reference solution or expected paths |
| `repeated_calls` | the same call three times with no change between; four failing calls of one tool in a row | the transcript |
| `weak_verification` | a change never built or tested after its last edit; own tests passing where the hidden ones fail; a bounded false pass | the transcript, the hidden checks |
| `tool_selection` | shell commands for what a tool does (`cat`, `grep`, `find`); calls the runner rejected (a directory read, missing file, bad arguments, edit text not found) | the transcript |
| `context_excess` | a peak prompt above its group's 90th percentile; tool results over 32 KB; files read again unchanged | the report, the transcript |
| `escalation` | escalated and still failed | bounded reports |
| `unnecessary_retries` | a build or test re-run with nothing changed; all attempts failed | the transcript, bounded reports |
| `ineffective_compaction` | unchanged files read again after a compaction; failed recalls | the transcript |
| `budget_exhausted` | the turn limit reached; the project left failing to build | the report |

Some kinds exist only in failed runs. Examples are false confidence ("its own
tests passed; the hidden ones failed") and a project left broken. Those
*classify* a failure. The failure rates with and without a pattern use its
other kinds only, so that a pattern is not called associated with failure
because it was defined by one.

The association is over the runs analysed, not a cause.

**Held-out runs are left out by default** (`--include-heldout` adds them).
The held-out tasks decide experiments, so the hypotheses must not be drawn
from them.

## Hypotheses and the catalog

A hypothesis names four things:
- an intervention from the laboratory's catalog;
- the metric it is decided on;
- the simpler alternative it must beat;
- the evidence it rests on.

An intervention is one of two kinds:
- **A configuration change.** Only tunables are possible: retrieval, the
  seed pack, the history budget, tool output and observation budgets,
  pruning, history notes, compaction, parallel tools, MCP tool search,
  routing and the turn limit. For bounded mode, also the attempt and
  context-pack budgets, the escalation policy and its failed-attempt
  count, and how reasoning is kept.
- **A code change** for a person (or an agent working in a laboratory
  worktree) to write as a patch.

Two catalog entries come from published work:
- *budget-notice*, after Budget Tracker (Liu et al., arXiv 2511.17006);
- *observation-masking*, after "The Complexity Trap" (Lindenbauer et al.,
  arXiv 2508.21433).

Neither is assumed to work here: each is a hypothesis to test.

## Experiments

An experiment file is written before anything runs.

```yaml
id: obs-budget
hypothesis: |
  Pruning command output to 4 KB lowers processed tokens per success,
  by more than simply cutting every tool result at 8 KB.
suite: direct                    # direct (bench direct) or bounded (bench tasks)
tasks:
  dirs: [benchmarks/direct]
  only: [...]                    # the held-out tasks decide; include them
primary: {metric: tokens_per_success, better: lower}
trials: 2
env: {GGML_OP_OFFLOAD_MIN_BATCH: "256"}   # server tuning variables only
arms:
  - {name: baseline, role: baseline}
  - name: cut-8k
    role: simpler
    set: {direct: {max_tool_output: 8192}}
  - name: observation-4k
    role: candidate
    against: [cut-8k]
    set: {direct: {observation_bytes: 4096}}
    predicts: {fixes: [some-task], at_risk: [other-task]}
  - name: loop-guard
    role: candidate
    primary: {metric: model_calls_per_run, better: lower}   # an arm may have its own
    patch: loop-guard.patch
```

**One change per arm.** An arm sets at most two settings (`edit_budget`), or
a patch of at most 8 files and 400 lines. A bundled change that wins hides
which part won.

`predicts` names the tasks the change should fix and those it puts at risk.
The decision checks them, and reports drops nobody predicted.

### Isolation

`lab run` isolates every arm in five ways:
- **Its binary** is built from a fresh git worktree of HEAD, never from the
  working tree, and the worktree is removed after the build. A patch arm's
  worktree gets its patch applied after the checks below, then must pass the
  security tests (`internal/direct`, `internal/execbox`, `internal/policy`,
  `internal/benchmark`).
- **Its home** is its own `BOUNDEDCODE_HOME`, holding a copy of your
  `config.yaml` with the arm's settings applied, plus your model profiles.
  Nothing else is copied: no credential files, sessions or state.
- **Its environment** is an allowlist: `PATH`, `HOME`, locale, Go and Docker
  variables, and the experiment's server variables. API keys never reach it.
- **Its configuration** must be identical to every other arm's outside the
  experiment settings. The laboratory compares a fingerprint of everything
  else (security policy, permissions, sandbox, verification, models), and
  refuses the experiment when they differ.
- **Its tasks** run on copies of the fixtures in its own home, as every
  benchmark does.

The trials are interleaved, and the order rotates each trial, so drift falls
on every arm alike.

Every report is hashed as soon as its run ends. The task definitions and
fixtures are hashed before the first run and after every run: a run that
changed them voids the experiment.

### What a code change may touch

A patch may change files under:
- `internal/direct`,
- `internal/retrieve`,
- `internal/prune`,
- `internal/routing`,
- `internal/contextplan`,
- `internal/repointel`,
- `internal/fileedit`,
- `internal/lang`,
- `internal/ecosystem`.

The checks run on the patch and the base revision's source, before anything
is built. A patch is refused when it:
- touches the guard, permissions, containment, MCP trust, hooks, content
  provenance, post-change checks, web access, background shells, sub-agents,
  MCP servers or tool capabilities;
- changes the inside of a protected function in `tools.go` or `direct.go`:
  rules, policy, approval, path resolution, secret stores, the shell tool,
  the verification fallback;
- changes an existing test (new tests may be added);
- changes anything else: the graders and tasks, verification, the sandbox,
  policy, secrets, configuration, the laboratory, the module files or CI;
- names a task id, a hidden file or an expected answer from the benchmark: a
  change tuned to the tasks;
- changes a binary file, or is over the edit budget.

## Decisions

An arm is decided against the baseline on the **held-out** tasks (development
tasks are reported beside them).

**The test.** The arms run the same tasks, so the primary metric is compared
task by task:
- each task is better, worse or tied;
- an exact one-sided sign test (α = 0.05) asks whether "better" outnumbers
  "worse" beyond chance;
- the effect must also exceed the rule's tolerance.

This needs at least five tasks to differ, all in one direction. With fewer
tasks than that, as within a language with few held-out tasks, the result is
*inconclusive*, never *unchanged*.

`predicts` is checked on success. A predicted fix may come out:
- **fixed:** it passes more often;
- **not fixed:** no change;
- **worse instead:** it passes less often.

The same regression gate as CI also applies, with per-language rules made
blocking.

| Verdict | When |
|---|---|
| **accept** | All of these hold:<br>- better on held-out tasks;<br>- no regression in the gate or in any language;<br>- no cost beyond the policy's tolerances (unless `allow_cost`);<br>- better than every simpler alternative it is set against;<br>- the held-out tasks cover at least `min_languages` (3) languages;<br>- the other languages still gain on balance when any one language is left out |
| **workload-specific** | Better, but the gain is gone without some language, or too few languages are covered: proposed for those languages only |
| **prefer-simpler** | A simpler alternative does as well and is itself accepted |
| **reject** | Worse, costlier, not comparable, or not better than a simpler change |
| **inconclusive** | No difference the experiment could detect |

`decide_on: dev` re-reads a past experiment that had no held-out tasks. Its
verdicts are marked not independent and never become proposals.

## Proposals, approval and promotion

An accepted or workload-specific arm gets a proposal (`proposal-ARM.json` and
`.md`) with:
- the change;
- the reasons, the paired result and the comparisons with the simpler arms;
- the limitations;
- the hashes of every file it rests on: the provenance record, the reports
  and the decision.

`lab approve` refuses a proposal when:
- its verdict is not promotable;
- it was not decided on held-out tasks;
- it changes something not allowed;
- any of its evidence changed since the decision.

The approval records the reviewer and the proposal's hash.

`lab promote` works only with an approval matching the proposal's current
content.

| Change | What `--write` does |
|---|---|
| Configuration, for every workload | Sets the keys in your `config.yaml`. Your comments and other keys are kept, the old file is backed up beside it, and the result must load as a valid configuration |
| Configuration, workload-specific | Adds a `direct.workloads` entry for its languages |
| Code | Nothing: the laboratory never applies code. Land the patch through ordinary review, with its proposal as the evidence |

## Workload overrides (`direct.workloads`)

A tunable that helps only some languages can be set for those alone:

```yaml
direct:
  workloads:
    - languages: [rust, cpp]
      set: {retrieval: basic}
      source: lab proposal x-basic-retrieval
```

When a direct-mode session starts in a project:
- the project's languages are detected;
- the first override listing one of them applies;
- it is logged (shown with `--verbose`), and each benchmark result
  records it (`workload`).

Only tunables can be set there. An override cannot name approval, permissions,
the sandbox, hooks, MCP servers, the web allowlist, post-change checks or the
verification fallback: no field exists for them, and decoding is strict.

With no `workloads` configured, the default, direct mode is unchanged.

## What the laboratory is not

- **Not an autonomous self-improver.** Detectors and the catalog are fixed
  code; hypotheses are reviewed before they run. Code candidates are written
  by a person, or by an agent in a worktree, and checked like any untrusted
  patch. The literature's self-improving agents depend on frontier models and
  report cases of optimisers tampering with their own checks. The laboratory
  is built so that its candidates cannot reach the checks.
- **Not a sandbox for patched code.** A patched arm's binary runs as you,
  with the same permissions as any benchmark run:
  - Its agent's commands are confined by the direct-mode sandbox, which the
    patch cannot change.
  - The harness code itself is not confined.
  - The hashes detect a run that altered reports, tasks or fixtures; they do
    not prevent it.

  Review a patch before you run it.
- **Not a large-sample instrument.** On a suite of a few dozen tasks, with
  success rates near 90%, small effects cannot show. The decisions say so: a
  verdict is often *inconclusive*.
