# Cancellation, cleanup and evidence

A task can stop for reasons that have nothing to do with the code it was
changing: a wall-clock budget expires, an operator interrupts, a deadline
fires. What happens next is the difference between a system that can be
operated and one that merely runs.

Three properties matter, and they pull against each other:

- work should **stop** when the task stops,
- cleanup should **still happen** when the work is stopped,
- the evidence explaining **why** it stopped must survive, without being able
  to hang the exit itself.

Satisfying the first alone is easy and produces leaks. This page describes how
the three are separated.

## Cancellation reaches the subprocesses

An evaluation shells out constantly — `git clone`, `docker run`, `docker exec`,
a Python interpreter, `cp -r`. Until recently none of those calls carried a
context, so cancelling a run stopped only the Go code that was waiting: the
clone kept cloning and the container kept sleeping.

The context now originates at the real top-level operation and is threaded
down. In `internal/evidence` that is `ExecuteRun`, `gradeSWEAtlas` and
`gradeSWEBenchPro`; for the dry run it is the cobra command's own context. It
passes through `Prepare`, `prepareSWEAtlas`, `prepareSWEBenchPro`,
`startEntrypointOverriddenContainer`, `verifySanitizedInvariant`,
`EnsurePatchedHarborRuntime`, `harborPackageDir`, `StartAtlasEnvironment`,
`EnsureAtlasTaskDirectory`, and in `internal/judgeval` through `GitRevision`
and `runGit`.

No intermediate function invents a `context.Background()` or a `context.TODO()`
to satisfy a signature. Where a function needed a context, its caller was given
one first. Subprocesses use `exec.CommandContext`, so ending the context ends
the process.

**What this does not claim.** A cancelled `docker` CLI invocation is not the
same thing as a cancelled container — see the next section. Nothing here
promises that a process which ignores signals dies promptly; it promises that
the request is made and that the Go side stops waiting.

`internal/evidence/cancellation_test.go` covers an already-cancelled context
starting no subprocess, cancellation while one runs, deadline expiry, and that
each returns rather than hangs, with the success path asserted unchanged.

## Cleanup is detached, and bounded

The obvious next step is wrong. If the run's context also reaches the commands
that *undo* its work, then every cancelled run leaks the container it started —
`docker stop` would be cancelled along with everything else. Trading a hung
subprocess for a leaked container is not an improvement.

So cleanup gets its own context. `cleanupContext` drops the parent's
cancellation and keeps its values, under a 30-second ceiling:

```go
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
}
```

The bound is not a budget anyone should reach. `docker stop -t 2` gives the
container two seconds before the daemon kills it, so the command finishes in
about that plus one round trip; the ceiling exists so a wedged daemon cannot
hang a run's exit.

The principle generalises: **work stops with the task; cleanup gets a separate,
bounded opportunity to finish.**

## Evidence survives the thing it explains

The same tension appears at the other end. A judgment site records one
consultation row per invocation, and the task's context carries the wall-clock
deadline. Writing that row through the task's context means that when the
budget expires — exactly when the record explaining the stop is most useful —
the write is cancelled with everything else.

`Observation.Finish` therefore detaches too, and bounds it:

```go
writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), observeTimeout)
```

`observeTimeout` matches `ledger.recordTimeout`, which bounds a detached
journal write for the same reason. One small `INSERT` against a local file does
not need longer, and an unbounded detached write would let a wedged database
hang a task's exit on a diagnostic.

`internal/ledger`'s own rule is worth reading beside this: a detached write
applies to `Interrupted` and nothing else. `Complete` and `Fail` deliberately
keep the caller's context, because writing an outcome makes an operation
*certain*, and under a cancelled context the side effect is precisely what
nobody knows. Refusing that write is what keeps an interrupted operation
uncertain, so recovery still inspects it.

## Budget exhaustion says what it is

Because the wall-clock budget bounds the task context, a task that runs out of
time fails inside whatever call was in flight and reports *that call's* error.
One recorded run read:

```text
native: step 3: llm: local /v1/chat/completions: context deadline exceeded
```

which sends an operator to debug an inference server that was working
correctly. The reason is now resolved through one helper used by both the phase
loop and EDIT, and reads:

```text
wall-clock budget exhausted after 30m0s in EDIT; the interrupted call was: …
```

The underlying error is kept — which call was interrupted is still worth
knowing — but the budget is named as the cause.

## Filesystem containment for evaluation writes

`EnsurePatchedHarborRuntime` copies the installed `harbor` package with
`cp -r` and then writes a patched compose file into the copy. The file names
are literals and the run root is operator configuration, but the copied tree is
not this project's to trust: a symlink at `environments/docker` would redirect
that write outside the run root entirely.

`containedPath` resolves the path before anything is written. It takes the
absolute form, follows every symlink in the chain, and refuses a result that
falls outside the run root. For a path that does not exist yet it walks up to
the nearest ancestor that does — that ancestor is the part a link could have
redirected — and re-attaches the remainder, which cannot contain a link because
it does not exist. Both sides are resolved, so a symlinked run root compares
equal to itself.

**Scope.** This is a check on specific evaluation writes, not a general
filesystem sandbox. Task edits are confined separately, by the worktree and the
write firewall described in [trust boundaries](trust-boundaries.md) and
[the isolation model](isolation-model.md). Do not read this section as a claim
that every write in the system is containment-checked.

## Retries and state

Failure handling is bounded rather than open-ended, and the bounds are
persisted so they survive a restart:

- verification failures feed a repair loop with its own attempt budget;
- plan validation refuses a plan and returns a correction naming real
  alternatives, twice, before the task stops;
- context exhaustion permits a small number of explicit replanning boundaries,
  and a continuation is not counted as an attempt;
- exhausted budgets, unavailable prerequisites and uncertain recovery stop the
  task in an inspectable state rather than retrying forever.

[Crash recovery](crash-recovery.md) covers what happens when the process dies
between an intent and its outcome.
