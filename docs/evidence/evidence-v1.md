# BoundedCode — Standard Evidence Suite v1

> This is a pre-registered compact subset and is not directly comparable to
> full benchmark leaderboard scores.

## Headline

**Standard Evidence Suite v1 execution integration: the reconciliation
problem is solved, proven end to end against a real pinned benchmark
image, and one real bug found in the process is fixed.** The previous
report of this suite said BoundedCode's sandbox model and a benchmark
harness's per-task container model were "not yet reconciled." That framing
was wrong: **the reconciliation mechanism already existed** —
`internal/eval.TaskOrigin` + `internal/sandbox/container.Runner`, built for
exactly this purpose (running an imported SWE-bench-style task against its
own official pinned image) and already exercised by this repository's own
`internal/eval` test suite (`runtime_validation_test.go`). This session
found it by tracing the real code path end to end (`internal/task.Runner`
→ `internal/engine` → `internal/recipe` → `internal/sandbox`), rather than
inferring from names, and used it directly: no new execution architecture
was built.

**Proven for real, not asserted**: `internal/eval/evidencesuite_pipeline_test.go`
runs the real, unmodified `internal/task.Runner` — the same supervisor
every ordinary BoundedCode task goes through — against the real
sanitized future-architect/vuls workspace, with a deterministic fake engine
(PIPELINE_TEST_ONLY, never a benchmark result) performing one known edit
through the real `engine.Engine` interface. Verification ran inside the
real pinned official image via the real `container.Runner`. The task was
accepted. The patch `internal/task.Outcome.Diff` produced was applied,
successfully, to a completely independent fresh pristine container of the
same image. This is design instruction §8's workspace-coherence proof and
§20's pipeline-wiring proof, executed, not merely described.

**Getting there found and fixed a real bug**, previously latent because
`internal/sandbox/container` had zero tests: `Command()` never overrode the
target image's own `ENTRYPOINT`. Every pinned SWE-Bench Pro Verified image
sets `ENTRYPOINT ["/bin/bash"]` with no `CMD`, so the constructed
`/bin/sh -c "..."` became an *argument* to that entrypoint instead of the
command itself — `docker run image /bin/sh -c "go build ..."` actually
executed `/bin/bash /bin/sh -c "go build ..."`, and bash tried to source
`/bin/sh` (a binary) as a script, failing with "cannot execute binary
file." Fixed with one added `--entrypoint /bin/sh` flag; covered by 5 new
tests in `internal/sandbox/container/runner_test.go`, including a live,
Docker-gated proof against a real shell-entrypoint image.

**Also fixed**: `internal/eval.TaskOrigin`-style verification requires the
image reference pinned to a digest (`name@sha256:…`), not a tag — the
architecture's own correctness check, working exactly as designed, caught
this test's own first attempt using a tag.

**Secret isolation is a verified property, not an assumption**:
`internal/sandbox/secret_isolation_test.go` places a fake credential in the
test process's own environment and proves no `sandbox.Runner.Command()`
carries it into a sandboxed child, for the reason read directly in the
source: every runner builds `cmd.Env` strictly from `spec.Env` (never
`os.Environ()`), and Go's `exec.Cmd.Env`, when non-nil, replaces the
child's environment rather than extending the parent's.

**The orchestrator now exists**, as a real Go package
(`internal/evidence`) and three CLI commands. `le evidence status` reads a
durable, append-only, resumable run ledger and reports all 16 planned runs
— real today, not aspirational: `SOLVED`/`FAILED` are never silently
rerun, `INFRA_ERROR` may retry, and an interrupted `RUNNING` record is
recovered as a linked retry rather than restarted from scratch, each
enforced in code and covered by tests (`internal/evidence/ledger_test.go`).
`le evidence run --dry-run` constructs and validates all 16 planned runs
for real: for all 4 SWE-Bench Pro Verified tasks it prepares the sanitized
workspace, re-verifies the anti-leakage invariant immediately before use,
and computes a deterministic content fingerprint; for all 8 task pairs it
confirms the two arms differ only in the Jev treatment
(`internal/evidence/armequality.go`, tested against the real frozen
manifest, not a fixture). `le evidence run --live` requires the generator
credential and `TYPESAFE_API_KEY`, refuses before anything else if either
is absent, and — tested directly, including with fake credentials present
— correctly reaches past the credential boundary and plan validation to
the one honest remaining gap: nothing yet drives `internal/task.Runner`
with a real generator across all 16 planned runs. SWE Atlas workspace
preparation is not yet built either (`internal/evidence/prepare.go` says so
explicitly rather than approximating a harbor-native flow with the
SWE-Bench Pro sanitizer). Both gaps are stated exactly where they are, not
generically. See "Remaining blockers" in
`evals/evidence-v1/reports/PREFLIGHT.md`.

Task selection is frozen, the Jev experimental profile is frozen, and the
manifest binding both to a specific repository commit is frozen and hashed.
None of the 16 planned end-to-end runs (8 tasks × {Control, Experimental
Jev Full-Authority}) has executed. This document reports exactly what was
completed, what real external data it rests on, and the precise commands
needed to finish.

## Benchmark Selection

Eight tasks, drawn from two real, currently published benchmarks — never
from BoundedCode's own history, never synthetic:

| Track | Benchmark | Count |
|---|---|---|
| SWE-Bench Pro Verified | [opencompass/SWEBench-Pro-Verified](https://huggingface.co/datasets/opencompass/SWEBench-Pro-Verified) (arXiv:2609.08149, Apache-2.0) | 4 |
| SWE Atlas Test Writing | [scaleapi/SWE-Atlas](https://github.com/scaleapi/SWE-Atlas) `data/tw/` (arXiv:2605.08366, Apache-2.0) | 2 |
| SWE Atlas Refactoring | [scaleapi/SWE-Atlas](https://github.com/scaleapi/SWE-Atlas) `data/rf/` | 2 |

Selection was deterministic and frozen **before** any BoundedCode run,
per `evals/evidence-v1/selection/select_tasks.py`: objective eligibility
filters (chosen before any selection outcome existed), a deterministic sort,
and a seeded min-hash pick within each stratum (language for SWE-Bench Pro
Verified, repository for SWE Atlas). See
[`selection/fetch_notes.md`](https://github.com/akynte/boundedcode/blob/main/evals/evidence-v1/selection/fetch_notes.md)
for the exact fetch commands against the official sources, and
`selection/selection.json` for the full selection record including the
filter parameters and eligible-set sizes.

### Selected tasks

| Task | Benchmark | Language | Repo | Files changed |
|---|---|---|---|---|
| `instance_future-architect__vuls-bff6b7552370b55ff76d474860eead4ab5de785a-v1151a6325649aaf997cd541ebe533b53fddf1b07` | SWE-Bench Pro Verified | Go | future-architect/vuls | 1 |
| `instance_element-hq__element-web-ca58617cee8aa91c93553449bfdf9b3465a5119b-vnan` | SWE-Bench Pro Verified | JavaScript | element-hq/element-web | 2 |
| `instance_internetarchive__openlibrary-1894cb48d6e7fb498295a5d3ed0596f6f603b784-v0f5aece3601a5b4419f7ccec1dbda2071be28ee4` | SWE-Bench Pro Verified | Python | internetarchive/openlibrary | 3 |
| `instance_tutao__tutanota-b4934a0f3c34d9d7649e944b183137e8fad3e859-vbc0d9ba8f0071fbe982809910959a6ff8884dbbf` | SWE-Bench Pro Verified | TypeScript | tutao/tutanota | 3 |
| `task-6902ef3ab97fe23e2ad271f5` | SWE Atlas TW | — | Automattic/wp-calypso | — |
| `task-6902ef3ab97fe23e2ad2727a` | SWE Atlas TW | — | drakkan/sftpgo | — |
| `task-694b4b99829f00e24fd11891` | SWE Atlas RF | JavaScript | Automattic/wp-calypso | — |
| `task-69b7c2a04b6f8ff9ed98812c` | SWE Atlas RF | C++ | MariaDB/server | — |

All four languages named as a priority in the selection rule (Go, Python,
JavaScript, TypeScript) are represented among the four SWE-Bench Pro
Verified tasks — the eligible set (489 of 731 instances, after the
files-changed and FAIL_TO_PASS filters) supported this without forcing it.
Every SWE Atlas task is from a distinct repository.

Eligibility filters, chosen before selection ran (see
`select_tasks.py`'s `SWEBENCH_PRO_FILTER`): at least one `FAIL_TO_PASS`
test, a published container image, and a 1–5 file gold patch — a scope
proxy chosen to keep per-task engineering and review cost bounded, per this
suite's own stated objective, not to make any particular task easier.

## Anti-Leakage Isolation

Official SWE-Bench Pro Verified images retained their full upstream git
history, including whatever commit carries the gold patch — this was
detected during preflight, directly, before any live model execution:
every one of the 4 selected images was pulled and started, and each showed
209 to 301 refs reachable via ordinary `git log --all`/`git branch -a`,
with no network call needed. This is not a claim that the original images
are somehow defective — AgentCompass, the official evaluator, mitigates
exactly this by stripping `.git` at agent-exposure time, and this section
does not imply the images themselves were clean without that step. It is a
positive methodological finding about what an unmitigated `docker pull` +
`docker run` of these images would expose, found before it mattered, and it
is published transparently here rather than left implicit.

The remediation: **BoundedCode Evidence Suite performs an additional
agent-workspace history-sanitization step before inference, while
preserving pristine official grading.** For each SWE-Bench Pro Verified
task, the agent never runs against the official image's own checkout.
Instead:

1. A disposable container starts from the exact, pinned official image.
2. Every future commit `git log --all` can see inside it is captured
   first — the concrete leakage surface, not an assumption about it.
3. The repository root is copied out, excluding `.git` entirely (symlinks
   and executable bits preserved).
4. The copy is turned into a fresh, single-commit git repository with a
   deterministic identity and timestamp — no remotes, no tags, no prior
   reflog, no alternates pointing anywhere.
5. **This is done independently for the control and the jev-assisted arm**
   — two separate sanitizations from the same base container, never one
   arm copied from the other — and their resulting source-tree fingerprints
   are verified identical before either is used.
6. The agent (BoundedCode, either arm) only ever sees this sanitized
   copy. It never sees the gold/reference patch, the official `test_patch`
   field, or any commit after the benchmark's own base state.
7. Official grading happens separately, in a **fresh, pristine** container
   from the same original image — never the sanitized copy, never a
   container the agent had shell access to. The agent's own patch is
   extracted (`git diff --binary HEAD` against the sanitized workspace's
   one commit) and applied there for scoring. The official evaluator's
   result, not BoundedCode's own verification, remains the published
   score.
8. Network restrictions specified by the benchmark (AgentCompass's
   code-hosting-domain block) remain active and unmodified throughout;
   sanitizing git history is a complement to that policy, not a
   replacement for it — an agent that could still `git fetch` the real
   upstream would defeat the point regardless of local history.

Implementation: `evals/evidence-v1/tooling/sanitize_workspace.py` (the
sanitizer and its probes) and `materialize_task.py` (per-task
orchestration against real containers). This is this suite's own
additional step, layered on top of the official infrastructure — not a
claim of being the official OpenCompass/AgentCompass implementation, which
it is not, and does not attempt to be.

**Verified against real data, not only a fixture.** All 4 selected
instances were sanitized for real; every probe passed on all 8 resulting
workspaces (4 tasks × 2 arms): exactly 1 reachable commit, 0 remotes, 0
tags, every captured future commit unreachable via both `git cat-file -e`
and `git show`, 0 unreachable objects via `git fsck --full --unreachable
--no-reflogs`, no object-alternates escape hatch. A red-team probe
(`redteam_probe.py`) additionally searched every workspace for stray
`*.patch` files, oracle-shaped filenames, and other non-git leakage;
the only matches were legitimate `node_modules` build-tooling patches and
one project's own `scanner/oracle.go` (future-architect/vuls's OS-oracle
vulnerability scanner, unrelated to this suite's concept of oracle
*validation*) — recorded, not silently filtered, and confirmed harmless.
Patch portability was proven end-to-end against a live, fresh pristine
container for all 4 tasks: a harmless test edit, extracted as a patch,
applied cleanly with `git apply` in a brand-new container from the same
image. Full results: `evals/evidence-v1/sanitization-results.json` and
`redteam-probe-results.json`. 30 + 14 automated regression tests
(`test_safe_cleanup.py`, `test_sanitize_workspace.py`) cover the mechanism
itself against a constructed fixture repository (base/future/solution
commits, a tag, a remote-tracking branch, a reflog) independent of any
live container.

SWE Atlas needed no equivalent step: all 4 selected images were already
single-commit, history-clean at the image-build stage (Scale AI's own
pipeline), confirmed by the same direct-probe method.

**Since resolved**: real oracle validation (the actual gold patch, applied
in a fresh pristine container, scored by each project's own official test
command) has now run for all 4 SWE-Bench Pro Verified tasks — see "Real
oracle validation," below. A real agent-runtime mount audit has also run,
against an actual container with a sanitized workspace bind-mounted in —
see "Real agent-runtime mount audit."

**What this still does not cover**: `le evidence run --live`
(`cmd/le/evidence.go`) verifies frozen hashes, credentials, and every
artifact above, and correctly refuses before spending a credential — but it
does not yet call `internal/task.Runner` against a materialized workspace.
BoundedCode's own sandbox is one persistent container plus per-task
Landlock/bubblewrap (design v3 §6), not a per-task Docker container the way
a benchmark harness expects; reconciling the two is real integration work
this task does not resolve unilaterally. See "Remaining blockers" in
`evals/evidence-v1/reports/PREFLIGHT.md`.

## Real oracle validation

For all 4 SWE-Bench Pro Verified tasks: a fresh pristine container from the
pinned image; the dataset's own `before_repo_set_cmd` (checks out the
FAIL_TO_PASS test file, using the image's still-full history — only inside
this disposable oracle container, never an agent workspace); the project's
own official test command with no fix (BASELINE, expected to fail); the
real gold `patch` field applied with `git apply`; the same test command
again (ORACLE, expected to pass). No scoring logic was reimplemented.

| Task | Baseline | Oracle |
|---|---|---|
| vuls (Go, `go test`) | FAIL as expected | **PASS** |
| element-web (JS, `yarn jest`) | FAIL as expected | **PASS** |
| openlibrary (Python, `pytest`) | FAIL as expected | **PASS** |
| tutanota (TS, `node test.js`) | BUILD_FAIL as expected | **PASS** (8,681 assertions) |

Full detail: `evals/evidence-v1/oracle-validation-results.json`. Isolation
re-confirmed afterward: the red-team probe was re-run against all 8 agent
workspaces post-oracle-validation and still reports all-PASS — the gold
patches and oracle containers never touched an agent workspace.

SWE Atlas has no separate "gold patch" concept the way SWE-Bench Pro
Verified does; its analogous official validation is each task's own
harbor-native, rubric-graded evaluator (Claude Opus 4.5 as judge). Running
that was not completed in this session — recorded as a specific, named gap
rather than assumed equivalent to the result above.

## Real agent-runtime mount audit

Not inferred from configuration: an actual container was instantiated the
way a future agent run would need to be — the pinned official image, with
the sanitized control workspace bind-mounted at `/app` in place of the
image's own checkout — and its real mount table, filesystem visibility, and
environment were inspected directly. Result: exactly one mount (the
sanitized workspace, read-write), no oracle/grading/other-arm/host-repo
path reachable, `cd /app && ls -la ..` shows the container's own root
filesystem rather than any host path above the mount, and only standard
toolchain/shell environment variables are visible. Full detail:
`evals/evidence-v1/mount-audit-results.json`.

## Jev Ablation

Two arms, executed on identical configuration except the Jev site
authorities. Calling Arm B simply "Jev ON" would understate what it
actually is, so this document and `jev-profile.yaml` both use the more
precise term:

- **Arm A — Control.** `judgment.yaml` absent/`enabled: false`. Every site
  reads as `logged` (design default); nothing is consulted.
- **Arm B — Experimental Jev Full-Authority Profile**
  (`EXPERIMENTAL_JEV_ASSISTED` in tooling). A dedicated profile,
  [`evals/evidence-v1/jev-profile.yaml`](https://github.com/akynte/boundedcode/blob/main/evals/evidence-v1/jev-profile.yaml),
  read only by this suite's harness — never written into, and never derived
  from, a workspace's normal `judgment.yaml`. Nothing here promotes a
  production Site. This is a materially stronger condition than ordinary
  "Jev on": a production deployment leaves every site at `logged` until it
  is promoted individually on real evidence (see
  [judgments](../explanation/judgments.md)' authority tiers and the
  [judgment validation campaign](../explanation/judgment-validation.md)); this
  arm promotes every site to its own ceiling at once, for the duration of this
  experiment only.

Every included site's unattended behavioral effect was audited against its
actual code consumer (not merely the design document) —
`evals/evidence-v1/reports/PREFLIGHT.md`'s "Jev treatment semantics" table
has the full per-site result; none is observational-only in this benchmark.

Every one of the eleven registered judgment sites is included, each
promoted to its own registered ceiling (`judgment.SiteInfo.MaxEffect`) —
never higher, and the profile cannot grant higher, because it is subject to
the same `judgment.Config.Validate` over-grant refusal production
configuration is. Each site's inclusion is justified in the profile file
itself against its actual, code-verified consumer in
`internal/task/phases.go` — not against what the design document merely
says the site does. Notably, `review_rubric`'s ordering effect **was**
included: this harness's "reviewer" step (`Runner.decide`, the review-phase
call) is itself a model call, not a literal human waiting at a gate, so
reordering what it reads first is not the "impossible interactive human
gate" the source instructions rule out.

The profile is frozen: its SHA-256 is recorded in `manifest.json`, and per
the suite's own rule, any change to it after task 1 begins is Evidence
Suite v2.

## SWE-Bench Pro Verified Results

Not yet run. See "Environment" below.

## SWE Atlas Test Writing

Not yet run.

## SWE Atlas Refactoring

Not yet run.

## Jev Intervention Analysis

Not yet run — the ledger this section would read
(`internal/judgeval`/`internal/ledger` predictions recorded during Arm B)
does not exist until Arm B executes.

## Jev Decision Quality

One of three planned component evaluations is built: **`verification_integrity`
(M4)**, 34 cases (`evals/evidence-v1/judgments/m4-dataset.jsonl`), generated
deterministically by `internal/judgeval`'s mutation rules against two real,
external, neutral test files —
[trufflesecurity/trufflehog's `stripe_test.go`](https://github.com/trufflesecurity/trufflehog/blob/main/pkg/detectors/stripe/stripe_test.go)
and
[grafana/k6's `hosts_test.go`](https://github.com/grafana/k6/blob/master/lib/types/hosts_test.go) —
neither of which is a BoundedCode historical fix, and neither of which
is one of the eight selected end-to-end tasks. Ground truth is exact by
construction (the mutation rule that produced a case is its label), so no
human or model annotation step was needed for this one site. 12 of 34 cases
are held out (`~1/3`, deterministic index rule).

`failure_triage` (M6) and `review_rubric` (M7) component datasets are **not
built**. Both need real ground truth a deterministic rule cannot produce —
a real failure's category and same-cause verdict, or a real diff hunk's
rubric answers — which needs either genuine historical failure/review
transcripts (excluded as a source by this suite's own rules) or careful
human annotation, which was not attempted rather than rushed with
low-confidence labels. This is named here as a specific, scoped remaining
task, not silently dropped.

No metrics (precision/recall/F1/Brier/latency/cost) are reported for any
site, because running the campaign against the M4 dataset needs a live Jev
credential this environment does not have — see `le judgment eval
verification_integrity --dataset evals/evidence-v1/judgments --live`.

## Cost and Latency

Not measured — no live run occurred.

## Reproduction

```bash
# 1. Selection (already run and frozen; re-running reproduces the same
#    output byte-for-byte from the same cached metadata and seed).
cd evals/evidence-v1/selection && python3 select_tasks.py

# 2. Manifest freeze (already run and frozen).
cd evals/evidence-v1 && python3 freeze_manifest.py

# 3. Provide credentials (required for everything below; none were present
#    when this suite was prepared).
export TYPESAFE_API_KEY=...      # for the Jev-assisted arm
export <GENERATOR_PROVIDER>_API_KEY=...   # whichever provider le.yaml/providers.yaml names

# 4. Materialize each selected task's environment (not yet implemented —
#    see "Limitations"): pull each SWE-Bench Pro Verified dockerhub_tag,
#    and run `harbor` (github.com/laude-institute/harbor, pinned v0.18.0)
#    against each SWE Atlas task directory.

# 5. Run Arm A (Jev OFF) then Arm B (Jev Assisted) for every selected task,
#    one run per task per arm, in that order (§13/§37) — a harness for this
#    step is not yet built; see "Limitations".

# 6. Component evaluation, once #3 is satisfied:
le judgment eval verification_integrity \
  --dataset evals/evidence-v1/judgments --split heldout --live --json
```

## Raw Artifacts

- `evals/evidence-v1/manifest.json` — frozen manifest.
- `evals/evidence-v1/jev-profile.yaml` — frozen experimental profile.
- `evals/evidence-v1/selection/` — selection script, cached metadata,
  `selection.json`, `fetch_notes.md`.
- `evals/evidence-v1/judgments/m4-dataset.jsonl` — M4 component dataset.
- `evals/evidence-v1/raw/{control,jev-assisted}/` — reserved, empty; where
  per-task run artifacts land once Arm A/B execute.
- `evals/evidence-v1/preflight.json`, `evals/evidence-v1/reports/PREFLIGHT.md`
  — machine- and human-readable live-run preflight (§ above).
- `evals/evidence-v1/tooling/safe_cleanup.py` — the cleanup guard every
  destructive filesystem operation in this suite's tooling must go through,
  and its regression tests.
- `evals/evidence-v1/tooling/sanitize_workspace.py`,
  `materialize_task.py`, `redteam_probe.py` — the anti-leakage isolation
  mechanism, its per-task orchestration, and its adversarial probe.
- `evals/evidence-v1/sanitization-results.json`,
  `evals/evidence-v1/redteam-probe-results.json` — real, measured results
  from running the mechanism above against all 4 selected SWE-Bench Pro
  Verified instances.

## Limitations

- **No live execution occurred.** All 16 end-to-end runs and the M4/M6/M7
  component campaigns remain to be run; the numbers this report structure
  reserves sections for do not exist yet.
- **Eight tasks is a deliberately small, non-representative sample.** No
  general claim about coding-agent capability, or about Jev's effect,
  should be drawn from it even once results exist — see "What this does not
  prove."
- **The anti-leakage gap found by an earlier preflight is now remediated
  and verified against real data** — see "Anti-Leakage Isolation" above.
  What remains outstanding: BoundedCode's own task-execution path does
  not yet call the sanitizer automatically (an operator or a future `le
  evidence run --live` must invoke it first), and real oracle/gold-patch
  grading (as opposed to this preflight's harmless portability-test patch)
  has not been run end-to-end in a pristine container.
- **Environment validation is now substantially more complete than at
  first preparation, but still not finished.** All 8 task images are
  pulled and their base checkout confirmed directly (a real repository at
  the expected commit, working toolchain present). `harbor==0.18.0` is
  installed (pinned, isolated venv under the guarded run root) and
  AgentCompass is cloned (commit `f3b95d0`), but neither has been run
  end-to-end against a selected task, so oracle/gold-patch validation and
  evaluator-stability checks remain outstanding for every task.
- **`failure_triage` and `review_rubric` component datasets do not exist
  yet** (see "Jev Decision Quality").
- **SWE Atlas TW/RF task completeness was spot-checked on one task, not all
  160** (`complete: true` in the cache is inferred from every task's
  `task.toml` and `environment/Dockerfile` parsing successfully, not from a
  full directory listing per task — see `fetch_notes.md`).
- **No local-model control was available or built** for any site (design
  instruction §22): `Local-model control unavailable in Evidence Suite v1.`

## What This Does Not Prove

Nothing in this document establishes that BoundedCode can resolve any of
the eight selected tasks, that Jev changes outcomes on them, or that any
judgment site is calibrated. It establishes only that a reproducible,
pre-registered, leakage-conscious selection and configuration exists and is
frozen, ready for the credentialed party who runs it to produce evidence
that either confirms or refutes those questions — including a negative or
mixed result, which this suite's design treats as valid evidence rather
than a failure to report.
