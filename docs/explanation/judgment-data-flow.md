# What leaves the machine

BoundedCode runs its coding model on your hardware. It has exactly one required
remote dependency: the **Jev decision plane**, a hosted service that answers
typed questions about work in progress. This page says precisely what is sent
to it, what is not, and what is kept afterwards.

It is a description of the code, not a policy statement. Where a claim is
enforced by something, that thing is named.

## The short version

| Where | What |
| --- | --- |
| Runs locally | The coding model, the code graph, retrieval, the sandbox, every verification command, the ledger, the index, telemetry. |
| Leaves the machine | One HTTPS request per judgment site consultation, to your configured Jev endpoint, carrying a structured `state` object and a set of typed questions. |
| Never leaves | Your credentials, environment variables, whole files, whole repositories, the conversation with the coding model, verification output under the default mode, anything from a path marked egress-sensitive. |
| Kept afterwards | Local metadata about each consultation, and — when the cache is on — the decoded answers. Never a request body. |

## What is sent, by redaction mode

A judgment sends a `state` object, never free text and never a transcript.
`redact` in `judgment.yaml` decides how much of the repository that object may
describe. The modes are cumulative and the default is the strictest.

**`strict`** — the default. Repository *metadata* only: the task objective as
you wrote it, file paths, symbol names, symbol kinds, line ranges, counts and
statuses.

No line of source leaves under this mode. Be clear about what that does and
does not promise: a path and a symbol name *are* repository information, and
they do go. They are what makes the question answerable.

**`repo_text`** — additionally permits function signatures, short excerpts and
diff hunks. This is an explicit operator decision, and `bcode doctor` reports it
on every run.

**`output`** — additionally permits normalized output from your own tools and
tests. It is a separate tier above `repo_text` on purpose: program output is
neither source nor metadata, and it can contain anything a running program
printed, including a secret it was holding when it failed.

Six of the eleven sites need more than `strict` to run at all: five need
`repo_text`, and `failure_triage` needs `output`. Under a mode below what they
need they do not send a reduced version of the question — they do not run, and
say so. A site configured to *route* on a decision it can never
make is a configuration error `bcode doctor` and preflight both refuse.

## What is never sent

Enforced, not merely intended:

- **Credentials and secrets.** Every field is scanned by
  `firewall.CheckContentSecrets` before it can become part of a request. A hit
  refuses the entire request rather than dropping the field — a dropped field
  would produce a confident judgment about evidence the model never saw.
- **Egress-sensitive paths.** A repository record is checked against
  `policy.EgressSensitive` before the record exists at all. An ineligible
  candidate is not merely omitted from the question; the site does not describe
  it, including its path.
- **Whole files, whole repositories, the model conversation.** There is no door
  for them. `judgment.State` has one method per kind of thing and no generic
  one, and `TrustedFact` takes scalars only
  (`TestStateHasNoGenericSerializationDoor` walks the package's AST and fails on
  any exported method that would accept an unconstrained value;
  `TestTrustedFactRefusesCompositeValues` covers the scalar rule).
- **Your API key.** It travels as an `Authorization: Bearer` header, is read
  from an environment variable, and never enters the `state` object
  (`TestTheCredentialIsSentAsAHeaderAndNeverInTheState`). Failures do not echo
  the response body either (`TestNon200IsAFailureThatDoesNotEchoTheBody`).

## Where the credential lives

`judgment.yaml` holds the *name* of an environment variable, never a secret:

```yaml
api_key_env: TYPESAFE_API_KEY
```

The default is `TYPESAFE_API_KEY`. BoundedCode reads it at startup, keeps it in
memory for the process lifetime, and does not write it to the ledger, the
telemetry database, the journal, any log line, or any error message. Nothing in
the repository persists it, and rotating it is an environment change with no
BoundedCode-side state to clear.

## What is kept afterwards

One row per consultation, locally, in the workspace's ledger:

- which site was consulted, under which task and phase
- whether a request actually left, and if not, the site's own reason
- the outcome status, how many questions were asked, how many distinct subjects
  they were about, and how many findings came back
- the model identifier and endpoint that answered
- latency, a timestamp, and links to any predictions recorded

Sites that make predictions additionally record the prediction, its subject as
a short identifier (`hunk:<path>`, `waiver:<path>::<symbol>`), the probability,
and the site's tier at the time — so `bcode judgment calibrate` can later pair it
with what actually happened.

**Never kept:** the request body. Nothing writes the `state` object that was
sent to disk, so the repository content a request described is not recoverable
from BoundedCode's own records. A failed request records its class —
`JEV_UNAVAILABLE`, `JEV_AUTH_FAILED`, `JEV_TIMEOUT` and the rest — and
deliberately not the response body, because a service may echo the request back.

**Kept when `cache: true`** (the default): the decoded answers — the
probabilities, choices and scores — in a local content-addressed cache under
your data directory. Not the raw HTTP response, and not the state that produced
it. The key mixes the endpoint, model id, redaction mode, confidence floor,
state and question set, which is why an unpinned model alias makes cached
judgments meaningless and `bcode doctor` warns about one. Deleting the cache costs
you repeated requests and nothing else.

## Reading it yourself

```console
$ bcode judgment sites
$ bcode trace <task-id>
$ bcode doctor
```

`bcode doctor` prints the endpoint, the model, the redaction mode and which sites
have been promoted past the default tier. It is intended to be the answer to
"what is this configuration actually sending", without reading source.

See also: [judgments](judgments.md) · [trust boundaries](trust-boundaries.md) ·
[telemetry schema](../reference/telemetry-schema.md).
