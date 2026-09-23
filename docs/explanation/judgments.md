# Judgments

Most of this system's decisions were pushed out of the model and into code,
and that is the right default. But some of the decisions code was left holding
are *semantic*, and code makes those with keyword lists, magic constants and
regexes over prose. The codebase is honest about it — nearly every such site
carries a comment admitting the heuristic — but honesty is not accuracy.

A **judgment** is a third option: a narrow, typed question answered by a model
trained to return a calibrated probability rather than text. It is not a chat
completion with a JSON schema bolted on. It returns a number, so code composes
it the way code composes any other number.

## Why this is not another `llm.Provider`

`internal/llm` is chat-shaped: `Chat`, `ChatStructured`, `Embed`, `Infill`. A
judgment backend implements one of those as a lie and the rest as
`UnsupportedError`, and `Capabilities` has no field that could mean "returns
calibrated probabilities" without being meaningful for exactly one kind.

The decisive problem is substitutability. `Router.For(role)` falls through to
the default provider for any unrouted role. Putting a judge behind
`RoleClassify` would mean that when no judge is configured, the local chat
model silently answers classification questions instead — model-voting wearing
a judgment interface, which is the thing §10.1 forbids. A separate package
with its own configuration file makes the absence of a judge look like an
absence, which is what it is.

The two are also different at the egress boundary. A provider sends a prompt
the context packer has already fenced. A judgment sends a structured `state`
object that no existing packer builds and no existing redaction path covers.
Inheriting the chat path's assumptions is how repository text leaves without a
gate.

## What holds, and how

Three rules, each backed by something other than a comment.

**A judgment never accepts work.** `internal/judgment` has an import-direction
test: `internal/policy`, `internal/firewall`, `internal/broker`,
`internal/recipe` and the acceptance path must not reach it, transitively.
Acceptance stays on evidence; a probability from a service outside the machine
is not evidence. This is the rule that did not change when the decision plane
became mandatory, and it is the one that matters most.

**There are two contracts, and the difference is whether a deterministic answer
exists to keep.**

`Consult` is the advisory form. It takes the existing value as a required
argument and returns no error, so a site that gets no answer keeps the answer
the code computed on its own. That is right for reordering candidates: the
deterministic ordering is a real answer, and a worse one is not a wrong one.
Disabled, unreachable, timed out, malformed, unconfident and partially answered
all collapse into one path with different labels.

`Require` and `RequireAll` are the failing form, used by the six sites whose
`MaxEffect` is `routing`. For those there is no deterministic answer to fall
back to — nothing else in the system forms a view on whether an attempt is
circling, or whether a waiver holds — so defaulting is not "keeping the
deterministic answer", it is picking one silently. They return a typed
`RequirementError` carrying a class (`JEV_UNAVAILABLE`, `JEV_AUTH_FAILED`,
`JEV_TIMEOUT`, `JEV_LOW_CONFIDENCE`, `JEV_REFUSED`, …) and the **zero value**,
never the fallback, so a caller that ignores the error gets something obviously
wrong rather than something plausibly wrong.

**What a failed required decision does depends on the site's tier.**
`judgment.Required(site, tier)` is the test, and it is deliberately not
"is this site mandatory" alone. At the default `logged` tier the answer is
journalled and consumed by nothing, so a timeout there stops nothing — failing
a task over a decision no one was going to read would trade real availability
for no safety. At `routing` the same failure stops the task in a resumable
`blocked` state naming the class, because carrying on would mean assuming the
benign answer.

**The plane itself is required either way.** `judgment.Preflight` runs before a
task does any expensive work, and `Runner.Run` refuses to start without a usable
judge whatever the tiers say. A component the system runs perfectly well without
is not a mandatory component. Requiring the plane to *exist* and granting a site
authority over control flow are separate questions, and only the second is
earned from calibration evidence.

**Repository data leaves only through doors that refuse.** `State` has one
door per kind of thing and no generic one. `Objective` and `TrustedFact` carry
operator-authored values, and `TrustedFact` takes scalars only — a composite
is refused, because an `any` that marshalled a slice of retrieval candidates
was exactly the hole the first version had. Repository records go through
`NewRepoItem`, which checks the origin against `policy.EgressSensitive` before
a record exists at all, and whose only JSON encoding is this package's.
Repository *source* additionally needs `repo_text` mode, the credential scan
and neutralisation. A refusal fails the whole request rather than dropping the
field, because a dropped field yields a confident judgment about evidence the
model never saw. An AST test asserts there is no exported method on either
type that takes an unconstrained value.

**Nothing external is described before it is authorised.** Guard and the
sensitive-path filter run before the reranker, not after. An earlier version
had that backwards, so a candidate about to be rejected for belonging to
another workspace had already had its path sent. A candidate that is
egress-ineligible does not merely get omitted — it skips the whole rerank,
because a partial candidate set cannot produce the complete ordering this
uses.

## The first use: one comparable relevance scale

`internal/retrieval` scores a lexical anchor with bm25, an expanded node with
`0.5/depth`, and an explicit slice with `1.0`. Those are three different
quantities. The consequence was written into the code long before this existed:

> Sorting inside an origin rather than across all of them because bm25 and
> inverse depth are not the same scale, and comparing them directly would rank
> by units rather than by relevance.

`GraphBudgetFraction = 0.33` exists for the same reason. It is a units
workaround — expansion had to be *fenced off* because it could not be *ranked*
— and its own comment records that the arm with the graph spent 1.8× the
tokens and solved no more.

Asking the same probabilistic relevance proposition of every candidate
provides a **common semantic relevance signal** — one number per candidate,
drawn from one question, rather than three incommensurable scores. That is a
change of kind, and it is all it is. Whether that signal is useful and
sufficiently calibrated to order candidates across origins *in this repository
domain* is what the pilot evaluates. It is not established, and nothing in the
code should be read as assuming it.

On that hypothesis a judged packet sorts across origins, and the graph's share
becomes a floor rather than a ceiling: a graph slice at 0.9 would displace an
anchor at 0.2, while a small reservation keeps a run of confident anchors from
crowding expansion out entirely. If the measurement does not support it, the
deterministic path is still there and still the default.

A partially judged set is treated as unjudged, and so is one too large to
judge completely: the reranker writes every candidate's score or none, makes
no request it cannot use, and records which of those happened. Mixing a 0.9
probability against a raw bm25 value is precisely the units error the old
comment warns about, and a half-answered batch must not create it by accident
— nor may a benchmark count a run that fell back as a treated one.

## Three levels, not two

The arm set has a deterministic baseline, a local semantic reranker, and the
external judge. The middle one is not a courtesy: baseline against judge
establishes only that semantic reranking helps, and that result would be
quoted as if it justified the egress. The comparison that speaks to the egress
is local against judged, where both arms rerank the same candidates under the
same budget rule and the only difference is where the relevance signal came
from.

Be precise about what the control is. `supervised-rerank-local` is **embedding
cosine similarity** between the objective and a rendering of each candidate,
computed through the configured embedding provider. It is one local reranker,
not the best one and not a stand-in for the class: a cross-encoder, a
fine-tuned relevance head or a local instruct model asked the same
proposition would each be a different control and might do better. A result
of "the judge beats embedding cosine" is exactly that, and must not be
written up as "no local method suffices".

## Why it is measured rather than assumed

§3.1 requires the graph's contribution be measured "with an ablation, not by
assumption", and the same applies here with more force, because this one costs
money and sends data. Ladder rung I and the `supervised-rerank` arm exist so
the claim is falsifiable. `eval.Task.Expected` — the files a real fixing commit
touched, recorded since the task format was written and read by nothing —
finally has a consumer in `ScoreLocalization`, so recall is reported beside the
solve rate.

The two fail differently, which is why both are needed. A task can be solved
with poor retrieval, when the model guessed well or the fix was where it first
looked; retrieval can be perfect on a task the model then failed. Reading only
the solve rate cannot tell a retrieval improvement from a model that happened
to have a better day.

The pilot ships if `supervised-rerank` beats `supervised` on the dev set. If
the probabilities turn out not to be calibrated on this domain, the thresholds
are arbitrary and the arm does not get promoted. Cookbook numbers from a
vendor's documentation are examples to evaluate, not results to inherit.

## Authority tiers: not every site is trusted the same amount

The first use, above, is one site with one behaviour: reorder candidates, or
don't. Nine further mechanisms work in the same spirit, and eleven judgment
sites now exist for them,
each reading a bounded, structured piece of ledger state the supervisor
already has and asking a narrow, typed question about it — never a
transcript, never free text. Run `bcode judgment sites` for the live list, with
each site's registered description and its currently configured tier; this
page names where each one reads from and what it may do.

| Site | Reads | Mechanism |
| --- | --- | --- |
| `intake_profile` | the objective, at INTAKE, before localization | M1 |
| `progress_monitor` | the recent tool-call evidence window, after an EDIT step | M2 |
| `obligation_reason` | a plan's `no_change_needed` waivers, at PLAN validation | M3 |
| `hypothesis_check` | the compaction card's working hypothesis, at every phase boundary that rebuilds it | M3 |
| `fact_relevance` | the compaction card's confirmed facts, at the same boundary | M8 |
| `verification_integrity` | the diff's test/fixture hunks, before VERIFY runs | M4 |
| `diff_conformance` | every diff hunk against the plan's stated reasons, before VERIFY runs | M5 |
| `failure_triage` | a failed verification result, in VERIFY | M6 |
| `review_rubric` | every diff hunk against a fixed rubric, at REVIEW | M7 |
| `context_injection` | a retrieval slice's body, once the packet's candidates are final | M8 |
| `note_relevance` | a durable memory note, before it competes for the packet's memory budget | M8 |

A new site earning the same trust the rerank arm has by fiat would be the
model-voting fallback design.md warns against, wearing a tier system's
clothes. So a site's *finding* an *effect* is a further question, answered by
`judgment.Tier`:

- **`logged`** — the site is asked, its answer is journalled by the same
  recorder retrieval's rerank already uses, and nothing consumes it. A run
  behaves exactly as it did before the site existed. This is where every
  ordering-capable site starts, and where a routing-capable one waits when the
  configured redaction mode forbids its subject.
- **`ordering`** — the site's findings may reorder, select, or add a visible
  concern to what a later reader is shown: `verification_integrity`'s
  findings enter the REVIEW call's evidence, `note_relevance` decides which
  notes compete for the memory budget, `diff_conformance`'s `undermines`
  finding is added to the plan's risks. Never a change to control flow and
  never a removal of evidence a deterministic check already produced.
- **`routing`** — the site's findings may taint a verification result
  (`recipe.Result.Tainted`, which never changes `Passed()`), end an attempt
  before its budget, or send a plan or an attempt back for correction within
  the counters those paths already enforce (`ObligationCorrection`'s replan
  budget, `continueEdit`'s continuation cap, `ConformanceReroutes`). Never
  acceptance, never a wider write, never a shortened verification run.

A site's default tier is its own `MaxEffect`, bounded by `redact`: a site that
can change control flow routes unless the configured redaction mode forbids the
thing it reads, in which case it stays at `logged`. That bound is not a
courtesy — defaulting a site to an authority it can never exercise would ship a
configuration that contradicts itself, and every task reaching that site would
be refused locally and stop. `TestDefaultsAreAValidStartingConfiguration`
fails the build if a default ever does that.

An operator's entry in `judgment.yaml`'s `sites` map still wins, in either
direction, and is never a threshold this package crosses on its own:

```yaml
sites:
  obligation_reason: logged         # observe it rather than obey it
  verification_integrity: ordering
```

Be clear about what that default is and is not. It is a *product* decision —
that a decision plane required to exist should also be allowed to matter,
because one that is asked and ignored is indistinguishable from one that is
absent. It is **not** a measured one. A judgment's calibration on *this*
domain — diff hunks, plan waivers, tool-call traces — is not established by
the vendor's benchmark, and at the time of writing no site in this repository
has been shown to improve a decision on real tasks. An operator who wants the
older, stricter posture sets the six to `logged` and reads what they find
first; that is one line each and nothing else changes.

That review has an instrument now. Most sites record each prediction they
make, and this program pairs it with an outcome at the point one becomes
available — a gate decision, the task's own terminal state, a rerun's
result — so `bcode judgment calibrate <site>` reports the site's actual Brier
skill against its own base rate, from ordinary task runs, with no benchmark
spent. Below a small sample it reports the count and declines to report a
skill figure a handful of rows cannot support. Read it before trusting a
site's authority, not instead of reading what it found.

[Judgment validation campaign](judgment-validation.md) is the system that
accumulates and scores that evidence — datasets, arms, metrics, promotion
policy, and `bcode judgment eval` / `bcode judgment eligibility`. Read it before
changing a tier. No site has been run through that measurement yet.

## What is deliberately not judged

`task.Rank` and the completion contract. Candidates are evidence-judged, and
`internal/task/candidates.go` says so in its own words: "Nothing here asks a
model which candidate is better." The import test is what keeps that true.

See also: [trust boundaries](trust-boundaries.md),
[using judgments](../how-to/use-judgments.md).
