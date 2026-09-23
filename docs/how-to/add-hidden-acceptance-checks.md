# Add hidden acceptance checks

A hidden acceptance check is a test or command you keep **outside the
repository**. The supervisor applies it to every candidate during verification,
and the coding model never sees it.

The repository's own tests are written, read and editable by the same model
whose work they judge. A check the model cannot read cannot be satisfied by
editing it, weakened by the patch, or special-cased by code that knows what it
asserts.

## Write a suite

A suite is a directory of YAML files. Each file holds one or more checks:

```yaml
checks:
  - id: refresh-token-single-use
    applies_to: ["services/auth/**"]
    must_not_change: ["services/auth/refresh_test.go"]
    files:
      services/auth/hidden_refresh_test.go: |
        // bc-canary-4f1d93a2e7
        package auth

        import "testing"

        func TestHiddenRefreshIsSingleUse(t *testing.T) {
            // ...
        }
    argv: ["go", "test", "-run", "TestHiddenRefreshIsSingleUse", "./services/auth/"]
    timeout_seconds: 300
    canaries: ["bc-canary-4f1d93a2e7"]
```

| Field | Meaning |
|---|---|
| `id` | Required. The only part of the check the model is ever told. |
| `argv` | Required. Runs in the verification snapshot, in the verification sandbox. Exit 0 passes. |
| `files` | Written into the snapshot after the candidate is applied, replacing any candidate file at the same path. |
| `applies_to` | Run only when the change touches these paths (exact paths, directory prefixes, globs, `dir/**`). Empty means every change. |
| `must_not_change` | The check fails if the change modifies any of these paths. |
| `timeout_seconds` | Default 600. |
| `canaries` | Strings of at least 12 characters that appear in the check's files and nowhere else, such as a comment with a random token. See below. |

Loading is strict. Unknown fields, duplicate IDs, a check without `id` or
`argv`, and file paths that are absolute or escape the repository are all
refused.

## Install it

Point `oracle.dir` in `bcode.yaml` at the directory, or pass `--oracle` to
`bcode task run` or `bcode task verify`. The path must be absolute. The
supervisor refuses a directory inside the repository, including through a
symlink, because the model can read the repository.

At start the supervisor logs the number of checks and the suite's digest. The
journal records that digest and the IDs of the checks that applied, never their
content.

## What happens during verification

1. The candidate is materialized in a fresh snapshot (see
   [verification](../explanation/verification.md)).
2. The repository's recipes run.
3. Each applicable hidden check is installed and run. If the build failed, the
   check is recorded as skipped.
4. The completion contract requires every hidden check that ran to pass on the
   current candidate. Unlike a visible check, a hidden check that **could not
   run or timed out blocks acceptance**. No baseline allowance applies: a
   hidden check states what done means, not a pre-existing condition.

The hidden checks do not run in the baseline verification at INTAKE, before the
task has been attempted.

## What the model sees

Only `hidden acceptance check <id> passed`, `did not pass`,
`could not complete`, or `modifies a path it protects`. The check's output,
test names, messages and files are removed from every result the model or the
review and triage steps can read. The full output is kept in the artifact store
for a person; the result's `artifact_hash` is in `bcode task run --json` and in
each record of `bcode task attest <id> --json`.

## Feedback budget

Each rejected candidate tells the model which checks failed. Over enough
attempts that is a search against the suite. `oracle.feedback_rounds` (default
3) bounds it: after that many distinct candidates have failed hidden checks,
the next failing candidate ends the task as failed instead of going back to the
model. An unchanged rerun does not count. A candidate that passes is accepted
and needs no budget. The model therefore learns about at most that many
candidates, one verdict per applicable check each.

## Canaries

Redaction keeps hidden content out of results. Canaries catch it if it arrives
anyway, by a route redaction does not cover: an operator pasting part of a
hidden test into a task's title, or a bug.

Put a random token in each hidden file, for example as a comment, and list it
under `canaries`. Every request the runner sends to a model goes through a
guard: the planning, review and critic models, and the editing engine. A request
containing a canary is refused before it leaves the process, and the task is
blocked. The refusal names the check, never the canary.

## Evidence

Each verification run is appended to the signed evidence chain with the
suite's digest and the IDs of the checks that applied, never their content.
`bcode task attest <id>` shows it.

## Limits

These are real, and they are why "hidden" is not the same as "secret":

- **The candidate's code runs next to the hidden files.** Hidden test files are
  written into the snapshot and compiled with the candidate. Code the model
  wrote can read them at test time, detect that it is under test, or print them
  into output. The output does not reach the model, but a determined candidate
  can still signal through timing or pass/fail. Prefer black-box checks that
  exercise the change through its public interface or a separate process.
- **Every reported rejection still leaks one bit per check.** The feedback
  budget bounds the total; it does not make it zero. Rotate or extend suites
  that are used often.
- **Canaries catch only what they mark.** Content paraphrased or summarized
  before it reaches a prompt does not contain the token. They also do not cover
  the hosted judgment service, which is not a model provider.
- **The same user and sandbox.** The snapshot runs as the supervisor's user
  under the verification sandbox, not as a separate verifier identity.
- **Checks can interfere.** Every applicable check's files are installed into
  the same snapshot, one after another.
