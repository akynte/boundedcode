# CLI reference

```
bcode [global flags] <command> [flags]
```

## Global flags

| Flag | Default | |
|---|---|---|
| `--data <dir>` | `$BC_DATA`, then `/data` | The data directory |
| `--log-json` | off | Structured JSON logs |
| `-v, --verbose` | off | Debug logging |
| `-q, --quiet` | off | Errors only |

## Environment

| Variable | |
|---|---|
| `BC_DATA` | Data directory |
| `BC_API_ADDR` | Overrides `api.addr`. The image sets `0.0.0.0:7777` |
| `BC_PROFILE` | Overrides the active hardware profile |
| `BC_IN_CONTAINER` | Set by the image; used for container detection |

---

## `bcode version`

Build, schema, indexer and workspace-id-scheme versions. `--json` for machine
output.

## `bcode doctor`

Reports what is actually in effect: isolation layers, the data directory's
filesystem, index freshness, profile fit, and stale leases.

| Flag | |
|---|---|
| `--json` | Machine-readable; contains no repository content |
| `--deep` | Full `PRAGMA integrity_check` (slow on a large index) |

**Exit status:** `0` all clear, `1` warnings, `2` failures.

## `bcode workspace`

| Command | |
|---|---|
| `init [path]` | Create `.bc/workspace.yaml` and register the workspace |
| `adopt [path]` | Re-bind the pinned id after the directory moved |
| `list` | Every workspace known to this data directory |
| `show` | The workspace containing the working directory |

`init` flags: `--name` (default: the directory name), `--repo` (repeated, for a
multi-repository workspace), `--force`.

Commit `.bc/workspace.yaml`. It pins the identity to the code.

## `bcode index`

Walks every repository in the workspace and writes files, directories,
containment edges and lexical chunks. `--json` for statistics.

## `bcode graph`

| Command | |
|---|---|
| `stats` | Node and edge counts by kind and evidence category |
| `impact <symbol>…` | What a change to these symbols would affect |
| `search <query>` | Retrieve context the way a task step would |

`impact --change` takes one of: `signature`, `behaviour`, `remove`, `rename`,
`add_field`, `schema`, `config`, `route`. The verdict for each consumer comes
from a deterministic table over the change kind and the edge kind.

`search --expand N` sets the graph expansion depth from the lexical anchors;
`0` disables expansion.

## `bcode plan`

```
bcode plan <requirement…> [--apply] [--max-steps N]
```

Decomposes a requirement into independent, verifiable steps, each with the
narrowest scope that contains its change. Nothing is created without `--apply`.

The plan is produced by a model but is not trusted by one. A step is rejected
before anything runs when it declares no scope, names a scope escaping the
repository, uses an unknown verification level, or depends on a step that does
not come before it. Those are the failures that would otherwise be discovered
mid-execution, with a half-applied change in a worktree.

## `bcode gate`

| Command | |
|---|---|
| `list` | Gates waiting for a decision (`--all` includes decided ones) |
| `show <id>` | The gate and the evidence behind it (`--diff` for the full diff) |
| `approve <id> --note "…"` | Approve |
| `reject <id> --note "…"` | Reject |

A gate is a point where a decision leaves the system. Each carries the
deterministic evidence — an impact report, a diff, the verification findings —
so answering means reading what the supervisor computed rather than trusting a
summary.

Gates are journalled *before* they block, so an interrupted approval is a
pending gate on restart rather than a lost one.

Which decisions open a gate is configuration (`gates:` in `bcode.yaml`). The
shipped default gates breaking changes, out-of-scope writes and applying a
change; it does not gate plans. A budget increase is always a person's call —
the budget exists precisely so a task cannot decide to keep going.

The `--note` matters more than the verdict: it is what the gate is worth six
months from now.

## `bcode task`

| Command | |
|---|---|
| `verify` | Put the current worktree under the completion contract |
| `create` | Create a task |
| `run <task-id>` | Run a task to a terminal state |
| `list` | Tasks in this workspace |
| `journal <task-id>` | The operation journal; `UNCERTAIN` marks a missing outcome |
| `recover` | Reconcile every non-terminal task and report resumable state |
| `retry <task-id>` | Return a failed task to pending, keeping its id, journal and worktree |
| `attest <task-id>` | The task's signed verification records, and a check of the whole evidence chain |

### `bcode task retry`

A failed task is not always work that could not be done. An output budget too
small for the model's reasoning, a request longer than the provider's timeout,
or a machine under memory pressure all produce a failed task whose work was
never really attempted — and fixing the cause does not help on its own, because
`bcode task run` refuses a task in a terminal state.

Retry returns it to `pending`. The id, the journal and the worktree are kept, so
the record of what was already tried survives; creating a new task with the same
description loses it.

An accepted task is refused: its change has been through the completion contract
and may already be merged, so running it again would redo approved work against
evidence that no longer describes the worktree.

| Flag | |
|---|---|
| `--reason` | What you changed so this run goes differently. Recorded in the journal as a decision |

### `bcode task attest`

Every verification run appends one record to the workspace's evidence chain:
the base commit, the SHA-256 of the exact patch verified, the snapshot's content
manifest, the oracle digest and hidden-check IDs, and each check's verdict and
artifact hash. Each record carries the hash of the one before it and a signature
by the verifier key in `keys/` under the data directory, created on first use.

`attest` lists a task's records and verifies the whole chain: every hash, every
link, every signature. With a key, an unsigned record counts as broken, because
rewriting the chain and recomputing its hashes is exactly what leaves one.

| Flag | |
|---|---|
| `--pub` | Verify against an exported public key (`keys/verifier.ed25519.pub`) instead of this data directory's |
| `--head` | A chain hash recorded elsewhere that the chain must still contain |
| `--json` | Machine-readable report |

A task's commit carries an `Evidence-Head:` trailer with the hash of its last
verification record. Passing it as `--head` detects a chain whose newest records
were removed, which the hashes alone cannot. Exit status is 1 when the chain is
broken or the head is missing.

### `bcode task verify`

Creates a task, gives it a fresh git worktree, **syncs your uncommitted
changes into it**, runs the verification recipes inside a sandbox, records
every result as evidence tied to the exact content hash it describes, and
reports whether the completion contract is met.

| Flag | |
|---|---|
| `--verify` | `low` (build only), `standard` (build, vet, test, format), `high` (adds race and — where the repository declared them — lint, semgrep, generator checks and integration steps) |
| `--committed` | Verify the last commit instead of your working tree |
| `--oracle` | Absolute directory of [hidden acceptance checks](../how-to/add-hidden-acceptance-checks.md); overrides `oracle.dir` |
| `--json` | Machine-readable outcome |

Exit status is 1 when the contract is not met, so it composes in a script.

The output always says which state it examined. A verdict that does not say
what it looked at is not usable evidence — and verifying the last commit while
you are looking at uncommitted changes would produce a pass describing code
nobody is running.

### `bcode task create` / `bcode task run`

`create` flags: `--title` (required), `--verify`, `--requirement`, `--scope`
(allowed paths, directory prefixes or globs), `--attempts`.

`run` flags: `--json`, `--diff`, `--oracle` (hidden acceptance checks; see
`task verify`).

A task runs in its own worktree; your working copy is never touched. Every
action is journalled intent-first, so an interruption at any point leaves a
state `bcode task recover` can reconcile.

**The native editor requires an explicit `--scope` before it may write.** Paths
outside that scope, secrets, generated files and protected paths are rejected
before execution. Scope accepts exact paths, directory prefixes and globs.

**A change outside `--scope` also blocks acceptance** even when every recipe passes.
For other editing engines, the out-of-scope check detects such writes
by inspecting the final diff.

### The completion contract

A task is accepted only when:

1. every recipe kind its verification level requires has a result,
2. that result is a **pass** — a skip or an error satisfies nothing,
3. it was produced against the **current** candidate, so evidence for an older
   state cannot be reused,
4. **no check that ran found a problem**, including the conditional kinds
   (`lint`, `analyzer`) the level does not demand a result from,
5. no file changed outside the declared scope,
6. **every [hidden acceptance check](../how-to/add-hidden-acceptance-checks.md)
   that ran passed** on the current candidate; for these an `error` blocks too,
7. **the tests that reach the change** were not taught to skip or removed by
   it, and every `require_tests` policy covering a changed declaration has a
   passing test that reaches it (see
   [verification](../explanation/verification.md#tests-that-reach-the-change)).

Rules 1 and 4 answer different questions. A level says which kinds must have
produced evidence; `lint` and `analyzer` cannot be on that list because they
run only where the repository committed a configuration, and demanding them
unconditionally would fail every repository that committed neither. But a check
that *did* run and *did* find something is evidence about this code, so it
disqualifies regardless of level. An `error` still does not: a tool that could
not run says nothing about the code.

An engine's claim that it finished is an input to that decision and never the
decision itself.

`recover` reports, per task: the current candidate hash, whether the worktree
drifted, each uncertain operation's classification, how many validations are
stale, whether it is safe to resume, and the next action.

## `bcode backup` / `bcode restore`

`backup` writes a consistent online snapshot. `--all` covers every workspace;
`--to` sets the destination.

`restore --from <dir>` restores into the current workspace. The supervisor must
not be running. `--force` overwrites. Restored files are re-opened and verified
before the command returns.

## `bcode models`

| Command | |
|---|---|
| `bench` | Measure this machine and propose a hardware profile |
| `health` | Probe every declared provider |
| `conformance` | Check a provider against what `providers.yaml` declares about it |
| `needle` | Measure the packet size this model can actually retrieve from |

`bench` flags: `--write` (save the profile), `--iterations`, `--prompt-tokens`,
`--output-tokens`, `--context`, `--profile-name`, `--json`.

`conformance` flags: `--provider`, `--json`. DR-4 makes a provider's
`Capabilities()` something callers rely on rather than a hint, so the
declarations in `providers.yaml` are worth verifying rather than trusting. A
capability a provider accepts but does not declare is reported as `unproven`,
not as a failure: the contract is that a declaration must hold, not that
everything working must be declared.

`needle` flags: `--sizes` (packet sizes to try, in tokens), `--write` (save the
measured cap into the active profile), `--json`.

`needle` is what §8.3 means by "the needle test sets the hard packet cap per
model profile". A window a model *accepts* and a window it *retrieves from* are
different sizes, and `max_packet_tokens` derived as half the context window is a
statement about arithmetic rather than about the model. The command hides a
random access code at several depths in a packet of Go-shaped filler and asks
for it back.

Two things about how it reports:

- **The cap is the largest size where every depth is recalled**, not where the
  average is good. A packet builder cannot choose where in a packet the needed
  slice lands, so a size that works at the edges and fails in the middle is a
  size that fails.
- **Every size is the provider's own token count**, not the size asked for. The
  sweep calibrates against the provider first — two probe-shaped requests solved
  for characters-per-token and fixed prompt overhead — because a cap derived
  from an estimate is an estimate wearing a measurement's clothes. Against a
  provider that reports no usage the numbers are the sizes requested, the report
  says so in its header, and `--write` refuses.

A provider refusing a request larger than its window is not a recall failure:
the model was never asked. That is reported as running out of context rather
than out of recall, and the cap it yields is a floor rather than a ceiling.

## `bcode judgment`

| Command | |
|---|---|
| `smoke` | Make one live call to confirm the configured model is accepted |
| `sites` | List judgment sites this build defines, and each one's configured tier |
| `calibrate` | Report a judgment site's calibration from paired predictions and outcomes |
| `consultations` | Report how often each site was consulted, and what came back |
| `replay` | Replay a task's recorded judgments against the configuration in force now |
| `eval` | Run the offline Judgment Validation Campaign for one site against a labeled dataset |
| `eligibility` | Report whether a site's evidence supports a promotion, without promoting it |
| `policy` | Manage the judgment promotion-policy file (`policy init`) |

Judgments are typed, calibrated answers from a service outside this machine,
used where deterministic code has to make a semantic call — how relevant a
retrieval candidate is, whether a diff hunk weakens the test it changes,
whether a plan's stated reason for a waiver actually holds, and more. They
are off by default, advisory everywhere, and an import-direction test
prevents them reaching acceptance, the write firewall or the human gate. See
[use judgments](../how-to/use-judgments.md).

`smoke` sends one trivial question carrying no repository content and prints
both the model you requested and the model the service reports running. Run it
before a benchmark depends on a configuration: the unit tests answer from a
local stub and cannot tell a real version from an invented one.

`consultations` prints the denominator every other judgment number is a rate
over: how many times each site was reached, how many of those sent a request,
how many skipped before asking and why, how many returned nothing, and how many
produced a finding. A site consulted on clean work records a row here and
nowhere else, so without it "the site was never reached" and "the site was
asked and found nothing" are the same absence — and no fire, skip, decline or
finding rate can be computed. A site with no row was never reached; that is the
one thing absence means. It reads records only and promotes nothing.

`sites` lists every judgment site the running binary defines, with a one-line
description and its currently configured authority tier (`logged`,
`ordering`, or `routing` — see [use judgments](../how-to/use-judgments.md)).
A site with no line under `judgment.yaml`'s `sites:` map still appears here,
at `logged`, which is the default every site starts at.

`calibrate <site>` reads the predictions a site has made and, for the ones
this program later observed an outcome for, reports the site's calibration:
a Brier skill score against the site's own base rate, and a reliability
table of predicted probability against what was actually observed. It reads
what ordinary task runs have already recorded — nothing here spends a
judgment or runs a task — and reports the resolved count honestly, declining
to report a skill figure below a sample floor. See
[judgments](../explanation/judgments.md#authority-tiers-not-every-site-is-trusted-the-same-amount)
for which sites pair predictions with which outcome, and where.

`replay <task>` reads the judgments a finished task recorded — which site
asked, about what, the probability it composed, the model, the question
version, the authority in force, and whether an effect was applied — and
reports what the configuration in force *now* would do with the same
predictions. It is the question a promotion decision asks: on this task,
under this tier, what would this site have done? It is read-only and
offline: no model call, no tool, no repository write, and the task it reads
is left exactly as it was. It replays composed predictions rather than the
model's answers, because the journal records that a request happened and a
digest of its state, not the answers themselves — the command says so rather
than implying more.

`eval <site> [--split dev|heldout] [--dataset dir] [--live] [--json]` runs
the offline Judgment Validation Campaign: it loads
`evals/judgment/datasets/<site>.jsonl`, runs every configured arm
(deterministic baseline, local control, Jev) against the requested split, and
reports per-arm metrics plus a reproducibility manifest. Without `--live` it
makes no network call — the Jev arm runs against `judgment.Off()` and reports
itself unavailable rather than silently answering nothing. It never promotes
anything. See
[judgment validation](../explanation/judgment-validation.md).

`eligibility <site> [--policy path] [--json]` reads a site's live shadow
calibration and `evals/judgment/policy.yaml`, and reports whether the
evidence clears every configured requirement for `ordering` and for
`routing` — never whether it *should* be promoted, only whether the numbers
support it. It never writes `judgment.yaml`; a promotion is a person's
decision after reading this report. A threshold `policy.yaml` has not set
reads as a blocking reason, never as a pass.

`policy init [--path]` writes a starting `evals/judgment/policy.yaml`: one
entry per registered site, with the right effect class and reporting floor,
and every promotion threshold marked as requiring a real, evidence-based
value rather than a guessed default.

## `bcode evidence`

| Command | |
|---|---|
| `preflight` | Verify Evidence Suite v1 is ready for a live run, without spending any credential |
| `run` | Run Evidence Suite v1 (refuses to spend a credential unless every other check passes) |
| `status` | Show the 16 planned runs' durable, resumable status |

Evidence Suite v1 is a frozen, pre-registered set of 8 tasks drawn from real,
published external coding-agent benchmarks (SWE-Bench Pro Verified, SWE
Atlas Test Writing, SWE Atlas Refactoring) — never this repository's own
history. See [Standard Evidence Suite v1](../evidence/evidence-v1.md). It is
deliberately separate from `bcode eval`, which runs this repository's own task
set; neither reads the other's tasks or config.

Every subcommand checks both frozen artifacts
(`evals/evidence-v1/manifest.json`, `evals/evidence-v1/jev-profile.yaml`)
against the hashes recorded when the suite was frozen before doing anything
else, and neither file is ever written by this command.

`preflight [--json]` checks, in order: both frozen artifacts' hashes,
generator and Jev credential presence (name and presence only, never a
value), and every prior verification artifact the suite has produced
(sanitization, oracle validation, mount audit, red-team probe). Makes no
network call and no model call.

`run [--suite v1] [--live] [--dry-run] [--json]`: without either flag,
re-runs every `preflight` check. `--dry-run` constructs and validates all
16 planned runs for real — preparing and re-verifying every SWE-Bench Pro
Verified workspace, fingerprinting it, and confirming every task's two arms
differ only in the Jev treatment — with no model or Jev call; it is the
same construction path `--live` uses, up to the credential boundary.
`--live` requires the generator credential and `TYPESAFE_API_KEY` and
refuses before doing anything else if either is absent; with both present
it still refuses today, because nothing yet drives
`internal/task.Runner` with a real generator across the 16 planned runs —
it says so explicitly rather than proceeding incorrectly. See "Known
limitations" in [Standard Evidence Suite v1](../evidence/evidence-v1.md).

`status [--suite v1] [--json]` reads the durable run ledger
(`.boundedcode-runs/evidence-v1/ledger/`, outside the repository) and
reports each of the 16 planned runs' status — `PENDING`, `RUNNING`,
`SOLVED`, `FAILED`, `INFRA_ERROR`, or `INVALID` — useful for checking on a
multi-hour live campaign without interrupting it. A `SOLVED` or `FAILED`
run is never silently rerun; only `INFRA_ERROR` may retry automatically,
and an interrupted `RUNNING` record is recovered as an infrastructure
retry rather than ignored or restarted as a fresh attempt.

## `bcode eval`

| Command | |
|---|---|
| `run` | Run the task set and report the results |
| `tasks` | List and validate the task set |
| `report` | Render a saved result file |
| `arms` | Describe the configurations being compared and what each isolates |
| `qualify` | Report whether the evidence yet supports a claim that the system helps |
| `results` | Generate `summary.json` and `RESULTS.md` from saved runs |
| `preflight` | Check whether a benchmark configuration would produce interpretable numbers |
| `rubric` | Print the localization annotation rubric |
| `annotations` | Report localization annotation coverage |
| `annotate` | Record one independent, blind localization reading |
| `adjudicate` | Resolve disagreements and gaps between independent annotations |
| `admit` | Report why each task is eligible or ineligible for the benchmark |
| `integrity` | Verify each task starts from its declared pre-fix state |
| `runtime` | Prove each task's verification environment is usable before a batch |
| `audit` | Audit the dev/held-out split for imbalance and related task families |
| `reliability` | Report agreement between independent annotators |
| `parity` | Show what evidence each reranking arm receives |
| `readiness` | Report which benchmark stage this dataset has reached |
| `stability` | Measure whether a Jev score is stable under irrelevant request structure |
| `calibrate` | Measure whether Jev scores have useful probability semantics here |
| `judges` | Measure how often each acceptance judge accepts a change that does not solve its task |

`judges` puts a corpus of candidate changes before two judges and scores each
against the task's hidden acceptance command. `ci` runs the visible build, vet,
test and format checks on the patched tree. `bcode` runs the completion contract
on an indexed repository: the same checks in a snapshot, plus the tests that
reach the change. Candidates are edit scripts in `<corpus>/*.yaml` or unified
diffs at `<corpus>/<task-id>/<name>.patch`, which is how another agent's patch
is judged. Flags: `--tasks`, `--corpus` (default `evals/judges`), `--json`, and
`--oracles <dir>`, which adds a `bcode+oracle` judge using one hidden acceptance
suite per task from `<dir>/<task-id>/` and reports whether each suite fails on
the unfixed code.

`run` flags: `--arms`, `--set` (`dev` or `heldout`), `--tasks` (the task set
directory), `--task` (run only these ids), `--repeat`, `--raw` (write every run,
including failures, as its own file), `--out`, `--json`.

`--benchmark` applies the stricter rule: a treatment that is not available
stops the run instead of falling back. That distinction decides whether a
number means anything.

```text
production:  the judge is unavailable      → fall back, carry on
benchmark:   the treatment is unavailable  → the experiment is invalid, stop
```

Falling back is correct in a task and a silent lie in an experiment: an arm
named `supervised-rerank` that quietly produced the baseline's ordering would
publish the baseline's numbers under the treatment's name, and nothing
downstream could tell. `--require-clean` refuses a dirty working tree, for a
run whose numbers will be published.

`preflight` runs the same checks without spending anything, and prints the
experiment id the run would record. For an arm that uses the external judge it
makes one authenticated smoke call, so a benchmark discovers at second three
rather than minute forty that its treatment was never available.

That smoke call establishes that the requested model identifier was *accepted*.
It does not establish that the identifier *served* the request — the two are
separate pieces of evidence, and the vendor documents no guarantee connecting
them. A publication benchmark therefore requires the service to name the model
it ran; when it does not, preflight reports `MODEL_IDENTITY_UNVERIFIABLE` and
refuses. `--allow-unverified-model` permits a development diagnostic whose
results are marked non-publishable.

`--cache cold|warm|disabled` controls every cache. Warm is what production
does and disqualifies the cost figures: request counts, token totals and
latencies then mix cache hits with live calls and understate the real cost by
an unknown amount. Semantic quality is unaffected — a cached answer is the
answer the service gave. Run cold for cost, or report cold and warm separately.
The policy is recorded in provenance and is part of the experiment identity.

`--order-seed` seeds the per-cell arm rotation. Arms are run innermost and
rotated per task and repetition, so no arm is systematically first: running a
whole arm before the next would give the later one warmer caches, a warmer
model server and whatever thermal state the first pass left, none of which is
the treatment. The order used is recorded on every run.

`--set` is what makes the tuning/held-out split real rather than decorative.
Thresholds and top-K values are fitted on `dev`; a parameter fitted on a task
is no longer measured by that task, so the final comparison runs on `heldout`
with the configuration frozen. The report records that configuration, so a
held-out run can be shown to have used what the dev run settled on. `bcode eval
tasks` lists which set each task is in and which record localization ground
truth.

Two properties are what make the numbers mean anything. A task's acceptance
tests are never in the worktree while the task runs, so a model cannot satisfy a
test it can read. And the system's own verdict is recorded separately from the
ground truth, so "claimed success and was wrong" is its own number rather than
something averaged away — read a solved rate without the false-acceptance rate
beside it and you are reading half the result.

`report` takes `--tasks` as well. A result file written before per-test grading
existed holds each run's acceptance output but no counts; given the set it was
run against, `bcode eval report results.json --tasks evals/tasks` recovers them
without re-running anything.

### Building a dataset

`admit` applies the task-admission protocol: a reproducible pre-fix state, an
objective derived from real project history, a known correct outcome,
reproducible acceptance evidence, no mandatory external dependency, and no
future-fix information the solver can see. Some rules it cannot decide — whether
an objective really came from real history is a judgment — and those are
reported as needing review rather than passed. It admits nothing on its own.

`integrity` checks that the fixture the solver gets is the declared base state
and holds no patch, annotation or version-control history that reveals the
change. `--print-digest` produces the `origin.base_digest` to record, after
which an edited fixture stops matching instead of quietly measuring a different
starting state. Benchmark preflight fails when this cannot be established.

`audit --protocol` prints the dev/held-out assignment rule; `audit` applies it.
Assignment is deterministic from the task id and a recorded seed, decided
before any model is run, so a split cannot be adjusted after seeing results
without the membership digest changing. The audit surfaces likely duplicate
families across the split for review and never reassigns one.

A **declared family** — somebody's explicit statement that two tasks are
variations of one problem — must not span dev and held-out: tuning on one
member partly tunes the other. `audit` reports a collision, and held-out
benchmark preflight refuses until a curator moves the whole family to one side
and re-freezes the split. Nothing is moved automatically, and never after
results exist. Heuristic near-duplicate detection stays advisory.

`readiness` reports the stage: `READY_FOR_DATASET_CONSTRUCTION`,
`READY_FOR_DEV_TUNING` or `READY_FOR_HELDOUT_EVALUATION`, and what stands
between this one and the next.

### Diagnostics

`parity` prints what evidence each reranking arm receives. If one arm sees
more, a conclusion of the form "X is a better reranker" is not available — the
run supports a statement about two systems as configured. The table says, per
pair, which claim is licensed.

`stability` measures whether a candidate's Jev probability is stable under
permutation, batch composition and added distractors. Production compares that
number against an absolute floor and against other candidates, and both uses
assume it is a property of the candidate rather than of the request. No
pass/fail bar is imposed: collect the distribution on dev and decide a limit
from it.

`calibrate` measures whether the scores have probability semantics here.
`REQUIRED` against `NOT_REQUIRED` is the subset; `USEFUL` is reported
separately and never folded into either. Scores are stratified by retrieval
origin, which is the assumption cross-origin ordering rests on.

Both need a live judgment service and refuse when authentication is
unavailable: a stability measurement against a stub reports perfect stability,
which is both true and worthless.

### Localization ground truth

`rubric` prints the labelling rubric; `annotate <task>` walks one task and
records your decisions; `annotations` reports coverage.

The labels answer "would a careful engineer need to **read** this to implement
or verify the objective correctly", which is not "did the fix change it". The
interface that constrained the change, the caller whose expectations it had to
preserve, the test that defined the required behaviour and the migration it
depended on are all read and none appear in the diff. A gold patch is evidence
an annotator weighs, never truth a tool applies — `annotate` will not extract
paths from one and will not pre-fill a label, because an annotation the tool
suggested and a human clicked through is the tool's opinion with a name on it.

`annotate` is blind by construction. It shows no fixing revision, nothing
derived from one, no other annotator's labels, no adjudicated label and no
suggestion; each reading is saved under its own annotator's name so a second
cannot overwrite a first. Gold evidence belongs in `adjudicate`, after both
readings are in — a reading made while looking at the patch is a reading of
the patch.

`adjudicate` resolves two distinct kinds of thing. A **disagreement** is a file
both annotators labelled differently. A **gap** is a file only one considered:
not a disagreement, and not free to vanish either. Held-out evaluation requires
every file in the union of considered files to carry two matching labels or an
adjudicated one, and preflight refuses a held-out run with unresolved gaps.

Three labels: `REQUIRED`, `USEFUL`, `NOT_REQUIRED`. Recall is computed over
`REQUIRED` alone; `USEFUL` files are excluded from precision's denominator
rather than counted against the packet, so a reranker is not penalised for
surfacing genuinely helpful context. Every annotation records who wrote it,
when, and under which rubric version, and a rubric change marks old labels
stale rather than silently reinterpreting them.

`qualify` reads saved runs and checks them against the bar this project set for
itself before any of it was measured: held-out tasks, repeats, hidden
verification, a baseline, consecutive ladder rungs, ablations, false acceptance,
paired statistics, retained raw runs, complete environment metadata, and an
independent reproduction that agrees. It does not decide whether the system is
good; it decides whether anyone is entitled to an opinion yet. A failing gate is
not a bug, it is the next piece of work — flags are `--tasks`, `--json`,
`--evaluation-run`, `--reproduction-run` and `--reproduction-agrees`.

`results` generates the published document. Every figure in it is computed from
the runs, so no number in this repository's benchmark documentation was typed by
a person; the output carries a header saying not to edit it. Flags are
`--tasks`, `--out` (a directory), `--provenance` (a label rendered above every
number, for example that these are pilot runs) and `--seed`.

Every run is graded as well as judged. `SOLVED` is the only measure of whether a
task was done; `TESTS` is the mean share of a task's hidden tests a run
satisfied, and exists because a binary outcome carries one bit per run and
cannot separate two arms that differ slightly without more runs than the
hardware can produce. Comparisons report both: an exact paired test on solved,
and a bootstrap interval on the graded difference.

`--repeat` **defaults to 3** because one run of a cell is a sample rather than
a measurement. The first real run of this harness changed verdict on 4 of 12
task/arm cells between passes, which is why repetition is the default rather
than something to remember. `--repeat 1` is still accepted, and the report says
plainly that a single pass measures nothing. See
[the published results](../benchmarks/results/2026-09-14-tasks.md) and
[the methodology](../benchmarks/METHODOLOGY.md).


### `bcode eval runtime`

`runtime` answers a question the official grader does not. The grader says
whether a known-good change resolves an instance; it says nothing about
whether *this* system can verify that instance, because the checks belong to
the upstream project and need its interpreter, its compiler and its installed
packages.

A task may name the environment its own verification runs in
(`origin.runtime_image`, pinned to a digest). `runtime` proves that
environment is usable: the image is present, it is named by digest rather
than a mutable tag, the worktree can actually be mounted into it, every
preset the repository declares can start there, and all of them pass on the
untouched fixture. A preset already failing before the solver touches
anything makes success unreachable, so the task is refused rather than
measured.

`--prepare` derives a runtime from the official one with the repository's
dependencies already fetched, and records it as
`origin.runtime_prepared_image`. Fetching happens here, once, so the measured
run needs no network. Nothing in this command reads a task's acceptance key.

```console
$ bcode eval runtime --prepare --task SWEBENCH-CADDY-4943
[prep] SWEBENCH-CADDY-4943   bc-runtime/swebench-caddy-4943@sha256:9508b105…
[ok  ] SWEBENCH-CADDY-4943   bc-runtime/swebench-caddy-4943@sha256:9508b105…
```

## `bcode opencode`

| Command | |
|---|---|
| *(no subcommand)* | Initialize this directory as a workspace if needed, refresh OpenCode setup, and launch OpenCode |
| `setup` | Register the MCP server and write AGENTS.md for this repository |
| `run` | Start OpenCode confined to this workspace |

Running `bcode opencode` from a project directory initializes its workspace
marker if one is not already present, applies the setup below, and launches
OpenCode. The setup is idempotent: later runs update generated files only when
their contents need refreshing. The launched editor inherits the directory of
the invoking `bcode` binary first on `PATH`, so its MCP process uses that same
build rather than another installation elsewhere on the machine.

Setup registers `bcode mcp` in `opencode.json`, merging rather than replacing
so an existing model choice or another MCP server survives, and writes a block
into `AGENTS.md` — which OpenCode reads into every session — naming the tools,
saying which questions they answer better than search, and carrying what this
repository has recorded about itself.

Only the block between its markers is replaced, so anything you write in
`AGENTS.md` yourself is left alone. An existing `opencode.jsonc` is refused
rather than rewritten, because marshalling it would delete its comments; the
block to paste is printed instead.

Re-run after recording notes or re-indexing, so the generated block matches what
the tools can actually answer.

`run` starts the session instead of leaving you to start it. That is the
difference between the tools being reachable and the process being confined:
a session you start yourself inherits your home directory, your agent sockets
and every variable your shell exported, and OpenCode's own permission system is
not a boundary — its enforcement has documented bypasses, which is why the
architecture review puts the firewall outside the shell.

A session started here runs under the strongest layer `bcode doctor` reports, can
write only its worktree, its tmp and this workspace's OpenCode state, reads only
the toolchain paths the operator granted, and gets an environment built from
nothing rather than filtered. Its shell, web-fetch, web-search, subagent and
external-directory tools are refused, so verification goes through `bc_verify`,
where the command is one the operator froze and the result is tied to a content
hash. Arguments after `--` reach OpenCode unchanged.

When no sandbox layer is available the command refuses and says why, because
reporting a confinement that is not there is worse than not confining. Pass
`--unconfined` to start anyway.

## `bcode trace`

```
bcode trace <task-id> [--jsonl]
```

Prints why a task did what it did: every operation in order, what it intended
before the side effect, and what it recorded afterwards.

An operation with an intent and no outcome reads as `UNCERTAIN`, not as a
failure. That is the state the journal exists to make visible — the supervisor
was interrupted between deciding and recording, so whether the side effect took
hold has to be established by inspection, which `bcode task recover` does.

`--jsonl` writes one JSON object per line. That is the export form: it appends,
it streams, and a trace cut off by a full disk or a killed pipe is still
parseable up to the cut, which a single JSON array is not. Redirect it with `>`
to keep a trace beyond the workspace.

`bcode task journal` prints the same rows as a terse table when all you want is
which operations ran.

## `bcode mcp`

Serves BoundedCode's tools to an MCP client over stdin and stdout, so an
editor or agent can ask about a repository without the operator typing
commands. It takes no subcommands.

Register it with OpenCode in `opencode.jsonc`:

```jsonc
{ "mcp": { "boundedcode": {
    "type": "local", "command": ["bcode", "mcp"], "enabled": true } } }
```

| Tool | |
|---|---|
| `bc_status` | Whether BoundedCode is set up here, what it tracks, how fresh the index is, and what the graph holds |
| `bc_graph_impact` | What a change to a symbol would affect, with evidence category and migration per consumer |
| `bc_search` | The code most relevant to a question: lexical anchors then graph expansion |
| `bc_reindex` | Re-analyse the repository so the index matches the working tree |
| `bc_task_start` | Open a supervised task: journals the intent before the work, returns the repository's protected paths |
| `bc_task_answer` | Record a question the executor asked the user and the answer given |
| `bc_verify` | Run the verification recipes in a sandbox and apply the completion contract |
| `bc_task_finish` | Close the task and produce its final review |
| `bc_note_add` | Record something durable the next session should not have to rediscover |

Together these put an editor's work under the same controls a CLI task gets. The
intent is journalled before the work; questions the user answers are recorded as
decisions rather than left in a chat transcript; the completion contract — not
the agent's account of itself — decides whether the work is done; and the task
ends in a review that states what was asked, what was decided, what changed and
what was checked.

**The review is a record, not an approval.** By the time it exists the agent has
already edited the working tree, so there is nothing left to withhold and a gate
that blocked here would block nothing. `git diff` is the authoritative view of
the change; the review is the part git cannot reconstruct. A task finished
without a verification on record is reported UNVERIFIED rather than fine.

The agent does the editing, and that division is forced rather than chosen. An
MCP server cannot ask a user a question: OpenCode declares only the `roots`
capability, not `elicitation`, so there is no channel for one. The agent talking
to the user is the only component that can pause and ask, so it edits and this
supervises.

`bcode task run`, which drives BoundedCode's own model through a bounded loop,
stays on the CLI. So does every destructive operation. Nesting a model's
multi-minute loop inside another agent's tool call would put one budget under
another's control with no gate between them.

stdout carries the protocol, so the command prints nothing there; diagnostics go
to stderr. Each call rebinds to the workspace containing the path it was given,
which performs §2.2's switch — a server is long-lived and will be asked about
more than one repository.

## `bcode memory`

| Command | |
|---|---|
| `add` | Record a note |
| `list` | Show the notes kept with this repository |
| `remove` | Delete a note |

`add` flags: `--kind` (required), `--source`, `--evidence`, `--tag`.
`list` flags: `--kind`, `--json`. `remove` flags: `--kind` (required).

Notes come in three kinds because they are not interchangeable:

| Kind | |
|---|---|
| `intent` | Why something is being done: a requirement, a constraint |
| `observation` | Something seen once — evidence, not a rule |
| `advice` | A rule meant to steer future work |

Every note records where it came from, and `--source` cannot be dropped. A rule
with no source cannot be judged, and a system that writes rules about its own
work will write flattering ones. Notes live under `.bc/memory/` so they travel
with the repository; there is no global store.

## `bcode lessons`

| Command | |
|---|---|
| `export` | Write this repository's notes to a file |
| `import` | Copy notes from a file into this repository, after showing them |

`export` flags: `--kind` (defaults to `advice`), `--to`. `import` flags:
`--kind`, `--yes`.

There is no global memory and no automatic channel between projects. These two
commands are the channel: `export` writes a file you can read, and `import`
shows you what it would copy before copying it. Imported notes are marked as
imported and keep their original source, so a rule learned elsewhere never reads
as one this repository established — the difference between a lesson and a
rumour.

`export` defaults to `advice` alone: `intent` is usually specific to the
repository that recorded it, and `observation` is evidence about one codebase
rather than a rule about any.

## `bcode deps`

The dependency provisioning lane of §6.1: a confined process whose only route
out is the allowlisting egress proxy.

| Command | |
|---|---|
| `sync` | `go mod download all` inside the deps lane |
| `run -- <cmd>` | Any command inside the deps lane |

**These are not part of a task, and that is the design.** A task sandbox has no
egress at all: verification runs with `GOPROXY=off`, and a task's Landlock
ruleset grants the inference endpoint and assigned test ports and nothing else.
There is no per-task flag that changes this — the only function that builds a
spec containing the proxy port takes a lane, and a task runner cannot construct
one.

So a change that needs a new dependency is an operator running `bcode deps sync`
between tasks, with the `go.mod` diff visible before any task verifies against
it.

Both lanes require `egress.enabled` in `bcode.yaml`, which is **off by default**.
Each lane has its own listener and its own slice of the allowlist, so a
documentation host cannot be used to fetch code.

A refused host is reported by the supervisor as `egress refused` with the host,
the lane and the reason. The fix is an entry in `egress.allowlist` with a `why`
— the reason is required so the decision is legible later. `bcode doctor` reports
the proxy's state and warns about wildcard rules.

## `bcode docs`

The documentation provisioning lane, separate from `bcode deps` so that a
documentation host cannot be used to fetch code.

| Command | |
|---|---|
| `fetch <url>` | Fetch one URL through the docs lane; `--output` writes a file |
| `run -- <cmd>` | Any command inside the docs lane |

The same rules apply as for `bcode deps`: `egress.enabled` must be on, the lane is
confined, and only hosts the allowlist names for the `docs` lane are
reachable.

## `bcode tui`

A repainting status view for use inside the container (§4.1):

```console
$ docker exec -it boundedcode bcode tui
```

It shows what `bcode doctor` cannot — what is happening *now*: the supervisor's
children and their restart counts, the active sandbox layers, non-terminal
tasks, and any gate waiting for a decision.

| Flag | |
|---|---|
| `--interval` | refresh period (default 2s) |
| `--once` | render a single frame and exit |

Deliberately not a full-screen application. Without a TTY it appends plain
blocks instead of repainting, so it can be piped or redirected to a log — and a
cursor-addressed UI would add a dependency and break under `docker exec`
without a terminal.

**It is read-only, and that is not an omission.** Approving a gate is
`bcode gate approve`, with the diff and the impact report in front of you. A key
that approved from a status screen would be a way to approve without reading,
which is the failure the gates exist to prevent.

## `bcode telemetry`

| Command | |
|---|---|
| `show` | counters for the workspace containing the working directory |
| `aggregate build` | collect counters from every workspace |
| `aggregate show` | report counters across workspaces |
| `aggregate forget <id>` | remove one workspace from the aggregate |

Telemetry is per workspace, like everything else (§2.2). The aggregate is the
**one** exception §2.2 permits — "an optional aggregate with workspace ids
only", holding "counters, never content" — and it is bounded accordingly:

- It stores a workspace id, a metric name, a UTC day and two numbers. There is
  nowhere in the row shape to put a path, a symbol or a task title, which is how
  "never content" is enforced rather than promised. A test asserts the column
  set, because adding a column is exactly how that would stop being true.
- A **day** is the finest resolution. Per-second counters across workspaces
  would be a timing channel between projects, which §2.2's slot clearing is
  careful to avoid elsewhere.
- It is built by `build`, never written continuously, so nothing accumulates
  across your projects while you are not looking. `show` says out loud that its
  numbers are as of the last build.
- It is derived and disposable: deleting the file loses nothing that is not
  still in each workspace's own telemetry.

## `bcode config`

| Command | |
|---|---|
| `init` | Write default `bcode.yaml` and `providers.yaml` |
| `reference` | Configure the local Bonsai reference and check inference health |
| `show` | Effective configuration, providers and role routing |
| `profiles` | Available profiles; `*` is active, and each says shipped or yours |

## `bcode api`

Runs the supervisor: migrates schemas, validates configuration, selects the
sandbox, starts children, serves `/healthz` and `/readyz`. This is the
container's default command.

`--addr` overrides the listen address. Inside a container bind `0.0.0.0` and
restrict exposure with `-p 127.0.0.1:7777:7777`.

## Hidden commands

`__sandbox-exec` is the Landlock re-exec helper. It is internal, and fails if
invoked without the environment the runner sets.

`verify-declared --kind <generate|integration> --name <step>` runs one step
from `.bc/verify.yaml` and prints its result as JSON. It is hidden because it
is not an interface: it is how a recipe expresses a *sequence*. A recipe is
argv and never a shell string — a shell inside the sandbox would make the
argument boundary meaningless — and neither declared kind is one command. An
integration step is up-then-test-then-down with the teardown guaranteed; a
generate check is snapshot-run-compare-restore. See
[declare runtime and generation checks](../how-to/declare-runtime-checks.md).
