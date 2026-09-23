# Use judgments

A *judgment* is a narrow, typed question answered by an external model that
returns a calibrated probability instead of text — today, [TypeSafe's System
One](https://docs.typesafe.ai). `bcode` uses them where deterministic code has to
make a semantic call and can only guess: "is this file relevant to what the
user asked?" is not a question a keyword count answers well, and it is not a
question worth a whole chat completion either.

The decision plane is **required**: a task run refuses to start without a
usable one. The six sites that can change control flow route by default, as far
as your `redact` mode lets them function; the other five are observational.
This page is about configuring the plane and about turning individual sites up
or down.

## What they are allowed to do

A judgment never accepts or rejects work, and the boundary is enforced by a
test rather than by intent. A judgment may:

- reorder retrieval candidates, so the most relevant code is read first;
- change how the context budget is divided between lexical hits and graph
  expansion;
- shorten a candidate list the structure pass is shown;
- flag a diff hunk or a plan waiver for a person to read, or require the gate
  to show it, before finalization proceeds past it silently.

A judgment may **not** accept work, reject work, decide a task is done, widen a
path policy, or change what a verification recipe reports. §10.1 requires that
candidates be "evidence-judged, not model-voted", and a probability from a
service outside your machine is not evidence. `internal/judgment` has an
import-direction test that fails the build if `internal/policy`,
`internal/firewall`, `internal/broker`, `internal/recipe` or the acceptance
path ever gains a dependency on it.

### Authority tiers

A site's tier says what its findings may do. The default is the site's own
`MaxEffect`, bounded by `redact`: a site that can change control flow routes,
unless the configured redaction mode forbids the very thing it reads, in which
case it stays at `logged` rather than shipping a configuration that would
refuse every task reaching it.

| `redact` | routes by default |
| --- | --- |
| `strict` | `intake_profile`, `obligation_reason` |
| `repo_text` | those two, plus `verification_integrity`, `diff_conformance`, `progress_monitor` |
| `output` | all six, adding `failure_triage` |

The other five sites are ordering-capable at most and stay at `logged`: there
is a real deterministic answer to keep when they cannot be asked, so nothing is
lost by leaving them observational.

Any entry you write wins, in either direction:

```yaml
sites:
  obligation_reason: logged     # observe it instead; it will not stop a task
  verification_integrity: ordering  # its findings enter the reviewer's evidence
```

`bcode doctor` prints which sites can stop a task under your configuration, and
`bcode judgment sites` lists every site with its tier.

`ordering` lets a site's findings change what a reviewer or a packet is shown.
`routing` additionally lets a site taint a verification result or send a plan
back for correction — never accept, reject, or change what a recipe reports.
Promote a site only after reading what it has been finding at `logged`; see
[judgments](../explanation/judgments.md#authority-tiers-not-every-site-is-trusted-the-same-amount).

## Turn them on

Create `/data/config/judgment.yaml`:

```yaml
enabled: true
model: jev-1.13.0
api_key_env: TYPESAFE_API_KEY
redact: strict
min_confidence: 0.75
cache: true
```

and put the key in the environment, never in the file:

```console
$ docker run -d -e TYPESAFE_API_KEY=… … ghcr.io/akynte/boundedcode:latest
```

Check what is in effect:

```console
$ bcode doctor
judgment (external)   WARN   enabled: jev-1.13.0 at api.typesafe.ai, redact
                             strict, min_confidence 0.75; paths and symbol
                             names are sent to that host, no source
```

`bcode doctor` reports this as a **warning** when it is live. That is not a fault
— it is the one line on a report otherwise full of things that *do not* leave
your machine, and it should be read twice.

## What leaves your machine

Everything is built through a type that refuses rather than redacts, so a field
that should not go out fails the whole request instead of being silently
dropped. A silently dropped field would produce a confident judgment about
evidence the model never saw.

`redact: strict` — the default — sends **no repository source**. It does send
repository *metadata*, because that is what makes the question answerable:

```json
{
  "objective": "make the daily account limit configurable",
  "candidates": [
    {"id": "c0", "path": "internal/billing/limit.go", "symbol": "DailyLimit",
     "kind": "function", "lines": "41-68"}
  ]
}
```

In strict mode a request may contain exactly these, and nothing else:

| Field | Example |
| --- | --- |
| the objective, as typed | `make the daily account limit configurable` |
| repository-relative path | `internal/billing/limit.go` |
| symbol name | `DailyLimit` |
| declaration kind | `function` |
| line range | `41-68` |
| a count | `matching_lines: 3` |

No file contents, no signatures, no diffs, no test output. Note the honest
reading: a path list is still information about your repository, and it is a
weaker disclosure than source rather than none at all. What strict mode
guarantees is that no line of anybody's code leaves.

`redact: repo_text` additionally permits signatures and short excerpts. It is
an explicit decision, and `bcode doctor` names it on every run. Even then, four
checks run before anything is sent:

1. `policy.Sensitive` — the same predicate that keeps a path out of the local
   model's context. A third-party host is strictly more exposed, so anything
   the local model may not see certainly may not leave.
2. `.git/`, `.bc/` and `.agent/` are refused outright.
3. The firewall's credential patterns, the same ones that guard the commit
   gate. The error names the field and never repeats the value.
4. Size caps: 8 KiB per field, 64 KiB per request.

Retrieval also deliberately withholds *how* a candidate was found — its origin,
score and graph depth. A judgment that knew a candidate came from graph
expansion would be agreeing with the ranking it exists to second-guess.

## What it costs, and what happens when it fails

One request per retrieval build, roughly 40 questions in it, on the order of
1.5 k input tokens. Answers are cached content-addressed by state, model and
question set, so a repeated build is free.

What a failure does depends on the site.

**At an advisory site** — retrieval rerank, note relevance, hypothesis and fact
hygiene, review rubric — nothing fails because a judgment did not arrive.
Disabled, unreachable, timed out, malformed, below the confidence floor, or
partially answered all collapse to the same outcome: the deterministic answer
the code already had. A rerank has a 1.5 s budget, and a slow judge is treated
as an absent one.

**At a site configured to route**, there is no deterministic answer to collapse
to, so the task stops rather than assume one. It lands in `blocked`, which is
resumable, with the class named:

| Class | Meaning | Retry helps |
| --- | --- | --- |
| `JEV_NOT_CONFIGURED` | no usable configuration or credential | no |
| `JEV_AUTH_FAILED` | the credential was rejected | no |
| `JEV_UNAVAILABLE` | unreachable, or a server error | yes |
| `JEV_TIMEOUT` | the request outlived its deadline | yes |
| `JEV_RATE_LIMITED` | the service asked for less traffic | yes |
| `JEV_INVALID_RESPONSE` | a response this build cannot read | no |
| `JEV_LOW_CONFIDENCE` | answered, below the configured floor | no |
| `JEV_REFUSED` | this system declined to ask | no |

The last one is always a local fault, never the service's: a malformed
question, or state the configured `redact` mode forbids this site from sending.

**Before any of that**, a task run refuses to start at all without a usable
decision plane. `judgment.Preflight` checks the configuration, the credential,
and whether any site has been promoted to a tier whose questions its `redact`
mode forbids — a combination that would otherwise refuse every task that
reached that site. The check is local and makes no request, so it costs
nothing to run on every task.

`bcode doctor` reports all of this, and keeps working when the plane is down,
which is when you need it.

## Pin the model, and verify what was served

These are **two separate pieces of evidence**, and a benchmark needs both.

**A pinned request identifier** says what you asked for. `model: jev-1.13.0`
rather than `jev-latest` means the configuration names one build, so the cache
key is stable and two runs describe the same thing.

**A verified served identifier** says what answered. The service reports it in
the response's `model` field, documented by the vendor as "the model that
performed the evaluation".

Acceptance of the first is not evidence of the second. A `200` for
`jev-1.13.0` establishes that the service recognised the identifier; the
vendor documents nothing about how an alias resolves or whether a pinned
request is honoured, so there is no contract from which "it was accepted" gets
you to "it served the request".

`bcode judgment smoke` makes one live call carrying no repository content and
prints both. Run it before a benchmark depends on a configuration.

### What that means for a benchmark

| Requested | Served | Production | `--benchmark` |
| --- | --- | --- | --- |
| pinned | same | runs | valid, publishable |
| pinned | different | runs | **invalid** |
| pinned | absent | runs | **invalid** — `MODEL_IDENTITY_UNVERIFIABLE` |
| alias | any | runs | **invalid** |
| none | — | runs | valid |

Production stays permissive throughout, and should: a judge that answered from
a different snapshot produced a slightly different ordering, which is advisory
anyway, so there is nothing to invalidate. An experiment cannot absorb the same
uncertainty, because the artifact would name a model on no evidence.

`--allow-unverified-model` is the development escape. It keeps every other
benchmark check and permits an unobservable served model; the run proceeds, the
preflight says it will not be publishable, and the report marks it so. It does
not apply to a mismatch or an alias — one would name the wrong model, the other
no model at all. The model id is part of the cache key,
which is what makes a cached judgment reproducible; an alias that moves
underneath makes yesterday's `bcode eval` run and today's incomparable while the
configuration looks unchanged. `bcode doctor` warns when it sees one.

## Measure before you trust it

The reranking arm exists so this is a measurement rather than a belief:

First, the dataset. Tasks must be admissible, their starting states verified,
and the split decided before any model is run:

```console
$ bcode eval admit
$ bcode eval integrity
$ bcode eval audit --protocol
$ bcode eval audit
$ bcode eval readiness
```

Then ground truth. Localization cannot be measured without it, and nothing may
infer it:

```console
$ bcode eval rubric
$ bcode eval annotations
$ bcode eval annotate nil-deref-001
$ bcode eval reliability
```

Then check what each arm will see, and the configuration, before spending
anything on it:

```console
$ bcode eval parity
$ bcode judgment smoke
$ bcode eval preflight --set dev --benchmark --arms supervised,supervised-rerank-local,supervised-rerank
```

Then tune on dev, freeze, and measure on held-out:

```console
$ bcode eval run --set dev --benchmark --arms supervised,supervised-rerank-local,supervised-rerank
$ bcode eval run --set heldout --benchmark --require-clean --cache cold --arms supervised,supervised-rerank-local,supervised-rerank
$ bcode eval report
```

And, on dev, the two diagnostics that say whether the score means what
production treats it as meaning:

```console
$ bcode eval stability "make the daily account limit configurable"
$ bcode eval calibrate
```

Three arms, not two. A deterministic baseline against the judged reranker says
only that semantic reranking helps. Whether sending anything to a third party
is what bought the difference is the `supervised-rerank-local` comparison, and
without it a positive result will be quoted as if it answered a question it
never asked. That control is embedding cosine similarity over the same fields the judged
arm receives under strict redaction — path, symbol, kind and line range — so
`bcode eval parity` reports the pair as an equal-information, model-level
comparison. It is one local method and not a claim about every possible local
reranker: a cross-encoder or a local instruct model asked the same proposition
would each be a different control.

`--benchmark` is what stops a run measuring nothing. At an advisory site an
unavailable judge falls back to the deterministic ordering, which is correct in
production; in a benchmark the same fallback would publish the baseline's
numbers under the treatment's name, so it stops instead. The campaign runner
applies the same rule to cases it could not get an answer for: they are counted
as undecided and excluded from the classification rather than scored as a
confident negative.

`RECALL` is the share of the files the real fixing commit touched that the
context packets carried. It measures retrieval rather than the model: a task
can be solved with poor recall, and good recall does not solve it. Fit any
threshold on the dev set only — a constant chosen because it scored well on a
held-out task is no longer measured by that task.
