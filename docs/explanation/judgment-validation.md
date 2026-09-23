# The Judgment Validation Campaign

[Judgments](judgments.md) describes the eleven registered judgment sites: what
each one reads, what it may affect, and the tier system that bounds it. None of
that establishes that any site is *good* — that its answers carry measurable
predictive skill on this repository's actual diffs, failures, and plans, as
opposed to sounding plausible in the argument for it.

That is what this system, `internal/judgeval` and `bcode judgment eval` /
`bcode judgment eligibility`, is for. The rule it exists to enforce:

> No site may receive authority because its design sounds useful. Authority
> must be earned from reproducible evidence.

Nothing described on this page promotes a site. A promotion is one line
under `sites:` in `judgment.yaml`, written by a person who has read an
eligibility report — never a threshold this package crosses on its own. See
[judgments](judgments.md#authority-tiers-not-every-site-is-trusted-the-same-amount)
for the tier system this evidence feeds.

## Two kinds of evidence, kept apart

`internal/ledger`'s calibration layer (`bcode judgment calibrate`) scores a site
from whatever predictions ordinary task runs happened to pair with an
outcome. It is real evidence, and it is opportunistic: a site accumulates
none of it until tasks run, it cannot be pointed at the hard cases a
promotion decision most needs to see, and four sites
(`intake_profile`, `context_injection`, `note_relevance`, `fact_relevance`)
have no runtime outcome to pair with at all — `judgment.SiteInfo.Paired()`
is false for them, permanently, by the nature of what they judge.

The campaign is the other half: an offline, dataset-driven evaluation that
can be run on demand against a labeled corpus, independent of production
traffic. `bcode judgment eligibility` reads both — live shadow calibration and,
once a held-out campaign has produced one, a manifest of its result — and
neither is a substitute for the other. A site with excellent live shadow
calibration but no held-out campaign evidence is not eligible; a site with
excellent held-out numbers but four resolved production predictions is not
either.

## The registry this campaign validates

`internal/judgment`'s site registry — `judgment.KnownSites()`, printed by
`bcode judgment sites` — is the single source of truth for which sites exist.
This document does not restate a count that could drift from it; run
`bcode judgment sites` for the current list. As of this audit it lists eleven
sites, covering mechanisms M1–M8 (M8 has three). M9, failure-keyed
retrieval, is not a registered site: it extends the pre-existing retrieval
rerank arm (`retrieval.FailureQuery` on `Rerank`) that predates the tier
registry and is promoted through the existing eval-arm mechanism
(`supervised-rerank` vs `supervised`), not through `judgment.yaml`'s
`sites:` map. Confusing the two — asking whether M9 is "eligible for
ordering" the way a registered site is — is a category error this document
exists to prevent.

`note_relevance` and `fact_relevance` are two distinct registered sites, not
one mechanism reported twice: one judges a durable memory note's bearing on
the objective, the other judges a compaction card's already-confirmed fact.
Calibration, the dataset format, and this campaign are all keyed by *site*
name, never by mechanism — a mechanism with three sites (M8) has three
separate calibration reports, three separate datasets, and three separate
eligibility verdicts, because they are different propositions with
different error costs.

## Configuration correctness

An absent `judgment.yaml`, or one with `enabled: false` and no `sites:` map,
is the shipped default: every site reads as `logged`. An explicit,
recognized tier — `logged`, `ordering`, `routing` — is honored, subject to
the site's own declared ceiling (`judgment.SiteInfo.MaxEffect`). An unknown
tier string (`sites: {review_rubric: routng}`) is refused by
`judgment.Config.Validate`, and — since this audit — refused whether or not
`enabled` is set: the check used to short-circuit on a disabled config,
so a typo staged ahead of turning the integration on was invisible until it
was too late to matter. `bcode doctor` now calls `Validate` before its disabled
short-circuit, for the same reason. `Tier.rank()`'s own runtime fallback for
an unrecognized value still reads as `logged` if one somehow reached a call
site anyway (defense in depth); `Validate` is what stops it from reaching
one in the first place, which is the layer an operator actually sees.

## The dataset format

One `Case` (`internal/judgeval/dataset.go`) is one labeled example for one
site:

```go
type Case struct {
    SchemaVersion  int
    Site           string
    SiteVersion    string          // partitions like ledger.CalibrateGrouped does
    CaseID         string          // stable, content-addressed for mutation cases
    Source         Source          // real | mutation | fixture
    Split          Split           // dev | heldout
    State          json.RawMessage // the site's own state shape
    GroundTruth    json.RawMessage // the site's own label shape
    AllowedAnswers []string
    Tags           []string
    Provenance     string
    TaskRef        string
    Difficulty     string
}
```

`State` and `GroundTruth` are opaque JSON rather than one shared struct,
because every site's question is a different proposition over a different
shape of evidence — a diff hunk and an objective for M4, a failure
transcript excerpt for M6. A campaign adapter (see "What is actually wired"
below) is what interprets them for one site; the type only carries them
intact and under version control.

A dataset is a JSONL file — one `Case` per line, blank and `#`-prefixed
lines ignored — at `evals/judgment/datasets/<site>.jsonl`. Loading sorts by
`CaseID`, so a reordered working copy never changes a result, and rejects a
file that mixes two sites or repeats a `CaseID`.

### Development versus held-out

`Split` is `dev` or `heldout`. The rule, enforced by process rather than by
code (nothing stops a person from reading the wrong file — see "What this
does not enforce" below):

- **`dev`** may be used to tune question wording, thresholds, and a site's
  own implementation.
- **`heldout`** may not be used for any of that. Once it has been read for a
  promotion decision, a change that materially affects the site's semantics
  — new criteria, a reworded proposition, a changed threshold — invalidates
  that use and requires a new `SiteVersion` and a fresh held-out evaluation
  before the next promotion decision. `SiteVersion` is exactly the field
  `ledger.CalibrateGrouped` already partitions live evidence on; this
  campaign uses the same field for the same reason.

`bcode judgment eligibility` reads shadow calibration and (once populated) a
held-out campaign manifest; it does not, and cannot, verify that a held-out
split was never peeked at while tuning a site. That discipline lives in how
this repository is worked, documented here so it is explicit rather than
assumed.

## Human annotation

Several sites need a human label rather than a deterministic rule — M1's
sub-predictions, M3's obligation-reason and hypothesis verdicts, M5's
per-hunk conformance category, M6's failure category and same-cause verdict,
M7's rubric answers, M8's relevance and injection judgments on real data.
`internal/judgeval/annotation.go`'s `Annotation` records one person's label
for one `CaseID`:

```go
type Annotation struct {
    CaseID, Label, Annotator, AnnotationVersion, Rationale, Timestamp string
}
```

No field is identity-sensitive; `Annotator` is whatever free-form string an
operator chooses. More than one annotation may exist for one case, so
disagreement is visible rather than silently resolved by whichever label
loaded last. `Resolve` reduces a case's annotations to one ground-truth
label: unanimous agreement wins; any disagreement — including one annotator
saying `ambiguous` — resolves to `ambiguous`, never to a majority vote a
reader would have to reconstruct by re-reading the raw file. `ambiguous` is
a first-class label: `Classify` (see Metrics, below) excludes it from every
precision/recall/F1 figure rather than counting it either way.

Ground truth for a judged site must never come from that site itself, or
from any judgment call: `Resolve` only ever combines `Annotation` rows a
person wrote.

## Mutation generation (M4)

`internal/judgeval/mutation.go` implements design instruction §12's
deterministic mutation generator, currently for M4
(`verification_integrity`) only. Eight named rules
(`RuleRemoveAssertion`, `RuleCommentAssertion`, `RuleWeakenEquality`,
`RuleBroadenErrorMatch`, `RuleIncreaseTolerance`,
`RuleRemoveExpectedField`, `RuleSkipTest`, `RuleDisableTest`) each transform
one line of a real test file into a weakened version and produce a `Case`
whose ground truth (`weakening`) is fixed by which rule ran — never inferred,
never asked of a model. `CaseID` is content-addressed
(`sha256(path, rule, original line)`), so regenerating a dataset from
unchanged source reproduces identical IDs and a diff of the regenerated file
shows only what changed.

**This only produces the weakening side of the dataset.** The companion
"legitimate expected-value update" and "harmless refactor" cases design
instruction §11 asks for need a real before/after pair whose *intent* — not
its shape — is the label, which no deterministic rule can synthesize
honestly; see the package comment in `mutation.go`. Those need a human
annotator reading a real diff beside its objective. The seed dataset at
`evals/judgment/datasets/verification_integrity.jsonl` (27 cases, generated
from a real repository test file) is therefore entirely the `weakening`
class and, by construction, a degenerate population for Brier scoring —
`ledger.Population.Degenerate` and `judgeval.BrierResult.Degenerate` both
already refuse to report a skill figure for it. This is a known, stated gap,
not a bug: see `evals/judgment/README.md`.

## Arms

Design instruction §5's three-arm comparison:

```go
type Arm string
const (
    ArmDeterministicBaseline Arm = "deterministic_baseline"
    ArmLocalControl          Arm = "local_control"
    ArmJev                   Arm = "jev"
)
```

An arm that cannot be run reports `Available: false` with an
`UnavailableReason` naming the exact blocker — never a fabricated result so
three bars appear on a chart. For M4, confirmed by inspection rather than
assumed: **no deterministic classifier for test-weakening exists anywhere in
this repository** (grepped, not guessed), and **no local instruct or
classifier model is wired for this proposition** —
`internal/retrieval/local_rerank.go` provides an embedding-cosine control,
but only for retrieval relevance ranking, a different question with a
different shape. Wiring a new local inference path for M4 was out of this
task's scope (it would be a new capability, not a validation of an existing
one). Both arms report unavailable with these exact reasons; `RunM4Campaign`
does not invent a keyword heuristic to fill the gap.

The Jev arm calls `workflow.CheckTestIntegrity` — the same production
function `internal/task/phases.go` calls at EDIT→VERIFY — never a
reimplementation, so a campaign result describes the code that actually
runs. It needs a `judgment.Judge`: pass `judgment.Off()` or a
`judgment.Fake` for a network-free run, or the configured client via
`--live` for a real (billed) one.

## Metrics

- **Probability calibration** (`stats.go`, `ScoreBrier`): Brier score,
  Brier skill against the site's own base rate, and — beyond what
  `ledger.Population` computes for live rows — a bootstrap percentile
  confidence interval on skill (`BootstrapSkillCI`, seeded for
  reproducibility). A promotion decision reads the *lower* bound
  (`SkillCILow`), never the point estimate — design instruction §9's
  explicit conservative criterion. Below the reporting floor, or for a
  degenerate (single-outcome-value) population, no skill figure is reported
  at all, matching `ledger.Population.Interpretable`.
- **Classification** (`Classify`): per-class precision/recall/F1 and a
  confusion matrix, with `ambiguous`-labeled ground truth excluded from
  every figure rather than counted as a hit, a miss, or its own class.
- **Stability** (`Stability`): mean, standard deviation, max pairwise
  delta, and label-flip rate over repeated answers to the same question
  against the same state — design instruction §17. A flip needs a decision
  threshold; callers pass the site's own `EffectThreshold`
  (`judgment.SiteInfo`) rather than this package guessing one.
- **Retrieval** (`ScoreRetrieval`): Recall@K, MRR, and miss rate, for M9's
  ranked-candidate shape — reserved for when M9 is evaluated through this
  framework (see "What is actually wired").

## Promotion policy

`evals/judgment/policy.yaml` (schema in `internal/judgeval/policy.go`) holds
one `SitePolicy` per site: its effect class (`ordering` or `routing`,
derived from `MaxEffect`), the reporting floor
(`MinReportableSamples` = `ledger.MinCalibrationSample`, 20 — a floor for
*reporting*, never sufficient for *promotion*, per design instruction §8's
explicit distinction), and five `Threshold` values a promotion decision
reads: `MinPromotionSamples`, `MinSkillLowerBound`, `MaxFlipRate`,
`MaxDistractorDelta`, `MaxLatencyP95Seconds`.

Every `Threshold` starts `{Value: 0, RequiresSelection: true}`. This is not
a permissive default that happens to read as zero: `Evaluate`
(`eligibility.go`) treats an unconfigured threshold as an unconditional
blocking reason, on every tier, for every site — not a pass. **No site is,
or can currently be, eligible for promotion**, because `policy.yaml` as
checked in has no configured thresholds. That is the honest state, not an
oversight: choosing `min_promotion_samples: 200` or
`min_skill_lower_bound: 0.05` without held-out evidence to justify it would
be exactly the "authority because the design sounds useful" failure this
system exists to prevent. `bcode judgment policy init` writes the unconfigured
starting file; an operator edits real values in, with a `note` recording
why, once real held-out evidence exists to choose them from.

Routing-class sites (`diff_conformance`, `failure_triage`, `intake_profile`,
`obligation_reason`, `progress_monitor`, `verification_integrity`) default
`RequireBaselineBeaten: true` — design instructions §16 and §28: a false
positive from a routing-capable site changes control flow, so it must clear
a higher bar than an ordering-capable site, whose mistake only changes what
a person reads first.

## Eligibility, never promotion

`judgeval.Evaluate` is a pure function from evidence (`EligibilityInput`) to
a report (`Eligibility`): no store access, no file write, no side effect.
`bcode judgment eligibility <site>` builds the input from live shadow
calibration and `policy.yaml`, and prints something in the shape design
instruction §27 asks for:

```
site:             verification_integrity
current tier:     logged
declared ceiling: routing

ordering eligibility:
  NO

Reasons:
  - no held-out campaign has been run for this site
  - min_skill_lower_bound is not configured in policy.yaml (…)
  - no repeat-evaluation stability trial has been run
  - …
```

A structural safety finding (`EligibilityInput.SafetyViolations`) overrides
every other figure unconditionally — design instruction §26: "safety failure
is not a metric tradeoff." In practice this list is ordinarily empty,
because the tier system's own invariants
(`internal/judgment/monotone_test.go`) already make most of the listed
violations unrepresentable in code, not merely unlikely: no path exists that
lets a judgment accept a task, convert a failed check to a pass, skip
verification, widen a filesystem, network, or allowlist permission, raise a
budget, suppress a security finding, override policy, or auto-approve a
gate. The field exists for a finding a campaign run itself surfaces, not to
duplicate that test.

**Nothing in this command, or anywhere in this package, writes
`judgment.yaml`.** A promotion is a person's decision, made after reading
this report.

## What is actually wired

Honestly, and stated here rather than implied by silence elsewhere: this
audit built the full generic framework — dataset format, split enforcement,
annotation, policy, bootstrap CI, classification/stability/retrieval
metrics, eligibility computation, manifests, the CLI surface — and wired one
complete, working example end to end: **`verification_integrity` (M4)**,
with a real mutation generator, a real (if class-imbalanced) seed dataset,
and a Jev arm that calls the actual production function.

The other ten registered sites do not have a campaign adapter yet.
`judgeval.AdapterRegistered(site)` reports this honestly, and `bcode judgment
eval <site>` for any of them loads and validates the site's dataset (if one
exists — none do yet) and says plainly that no arm can be run against it,
rather than fabricating a generic adapter that would produce numbers no one
designed a proposition for. Wiring the remaining ten — mapping each one's
Site-specific evaluation contract from design instruction §11 (M1's
sub-predictions, M2's Step-boundary loop-detection delay measurement, M3's
two sites, M5's per-hunk categories, M6's failure corpus and its comparison
against the *existing* keyword/fingerprint baselines that do exist for it,
M7's permutation/distractor stability, M8's three sites, M9's retrieval
metrics against the pre-existing rerank arm) — is future work this document
names so it is tracked rather than silently deferred.

## Reproducibility

Every result this framework writes (`bcode judgment eval ... --results-dir ...
--manifests-dir ...`) is paired with a `Manifest`
(`internal/judgeval/manifest.go`): schema version, git revision and dirty
flag, site, site version, dataset path and content hash, split, model id
(only when `--live`), policy schema version and a hash of the resolved
`SitePolicy` actually used, arm, seed, and whether the run was live. A
result is reproducible from (repository revision + the dataset file at its
recorded hash + the manifest) alone — design instruction §21 — and nothing
in this package stamps wall-clock time into a value that would otherwise be
deterministic; `Timestamp` is caller-supplied metadata, not an input to any
computation.

## Network and cost

Every test in `internal/judgeval` passes `judgment.Fake`, `judgment.Off()`,
or `nil` — never a live client — so `go test ./...` stays exactly as
network-free as the rest of this repository (design instruction §20).
`bcode judgment eval` defaults to `judgment.Off()` for the same reason; `--live`
is the one explicit, opt-in path to a real, billed call, matching every
other live judgment command this program already has (`bcode judgment smoke`).

## Fallback under instrumentation

`RunFallbackTrials` (`runner.go`) confirms design instruction §19's
requirement directly: with a disabled judge, a nil judge, an unavailable
fake, a fake that answers nothing, or an already-expired context,
`workflow.CheckTestIntegrity`'s ordinary skip behavior is unchanged —
`Attempted` stays false and no `Finding` is produced. Validation
instrumentation may fail; the R2 deterministic-fallback guarantee it wraps
may not, and does not, under any of the trials this test runs.

## Known limitations

- The campaign validates one site fully (M4) and ten sites structurally
  (dataset loading, but no arm). See "What is actually wired."
- The M4 seed dataset is entirely the `weakening` class; Brier scoring on
  it is correctly refused as degenerate. Classification precision/recall on
  the `weakening` class is still meaningful and is what
  `bcode judgment eval verification_integrity` reports today.
- `CheckTestIntegrity`'s public result only exposes a hunk as a `Finding`
  once it clears `WeakenFloor` and `LegitimateCeiling`; it does not expose
  the raw per-hunk probability for a hunk that did not clear them. A full
  Brier-scored campaign (once negative cases exist) will need either a
  small, additive accessor on that result or an equivalent adapter change —
  not a redesign, but not yet built either.
- Held-out discipline (never tuning against `heldout` cases) is a process
  rule this document states; nothing in the code enforces it beyond keeping
  the two splits in clearly separate `Case.Split` values.
- No site currently has, or can currently be given, a `RequiresSelection:
  false` promotion threshold without an operator making an evidence-based
  choice first. This is the intended state, not a gap: see "Promotion
  policy."

## No claims

Nothing in this document, this package, or its tests claims that any
judgment site improves task outcomes, that Jev is calibrated on this
repository's diffs or failures, or that any site is ready for promotion.
The infrastructure is built; the campaign it makes possible has not been run
at scale, and no threshold has been chosen. Read
[judgments](judgments.md)' authority-tier section alongside this one before
changing a tier.
