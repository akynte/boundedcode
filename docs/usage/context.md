# Context and session state

How direct mode keeps a long session within what the model can attend to
without losing what the session needs: command output is pruned before it
enters the history, older tool results are set aside, and when that is not
enough the history is replaced by a structured session state. Nothing is
deleted: every original stays in the session and `recall` shows it.

Measured in [Context benchmark (2026-10)](../benchmarks/context-2026-10.md).

## Command output

Command output (`bash`, `bash_output`) that has something to sort is pruned
to the observation budget (`direct.observation_bytes`: 8 KiB for a local
model, 24 KiB for a cloud provider). Something to sort means errors, failing
tests, stack traces, warnings, progress, downloads, or passing tests.
Pruning is deterministic and language-agnostic. Lines are classified by the
formats `internal/lang` knows, which are the located messages and stack
frames of every registered language, and by words that build tools, test
runners, linters and package managers share.

**Always:**
- Escape sequences and progress lines redrawn in place are removed.
- Runs of the same line, differing only in times, durations, percentages or
  addresses, are folded into the first, the last and a count.

**Over the budget, lines are kept in this order:**
1. The last lines, which hold the runner's summary and exit status. Errors
   among them come first.
2. Errors, failing tests and assertions, with the line before and three
   after each.
3. Located messages: compiler, linter and test-runner `file:line` lines.
4. Stack frames in the project. A frame is in the project when the file is
   inside the project and not in a dependency directory. Library frames keep
   the first two of each run.
5. The first lines, which show what ran.
6. Warnings.
7. The rest.

**Dropped first:**
- progress;
- download, resolution and install lines;
- passing tests;
- lines the output already showed, such as a build tool repeating the
  compiler's messages.

**Many places with the same message:** the first five are kept, and one line
names the other places (`"…Cannot find name 'logger'." at 25 more places:
cart.ts:6, …`).

**Search results** (`path:line:` lines with a file extension) keep the first
matches of every file and count the rest per file.

**Output with nothing to sort,** such as a diff, a file or a program's own
results, is content. It is not pruned under the cap (`direct.max_tool_output`).
Output over the cap, from any tool, still keeps its head and tail.

**What the model sees:** a pruned result starts with what was done (here, a
Python traceback after 400 repeated warnings):

```
[output shortened: 1227 lines (115 KB) → 89 lines; kept 1 error line,
1 located message, 5 stack frames in the project, the last lines; dropped
1137 lines (repeated message 1133, progress 4); every error and located
line was kept]
```

It ends with how to see the rest:

```
[the whole output (1227 lines) is kept: recall id "call_7" with offset and
limit, or a pattern, shows any part of it]
```

When errors alone exceed the budget, the header says how many did not fit.

**Where originals are kept:** next to the session file
(`<session>.outputs/<call id>.txt`, up to 8 MiB each), so they survive a
resume. `recall` takes the id with `offset` and `limit` (lines) or a
`pattern` (matching lines with two lines around each).

`direct.prune: false` turns pruning off. Output is then cut at the cap as
before, and the original is still kept.

## Setting results aside

Unchanged. Before each model call, once the history exceeds the working set
(`direct.working_set_tokens`), the oldest large tool results the model has
already seen are replaced by a one-line placeholder. The two newest are
never replaced. `recall` brings a result back.

## Session state

When setting results aside is not enough, the older history is replaced by
the session state. `/compact` does this on request. The last messages, from
a user message on, stay as they were.

### The record

The record is built from the session itself. The archive keeps every message
a summary replaced, so the record is built from the original messages each
time, never from an earlier summary. It holds:
- the user's requests, verbatim. The first one is the goal and is always kept
  in full;
- the constraints the user stated, verbatim, with the request they came
  from. A later request can lift one;
- the files read and changed, each with the git blob of its content then.
  A file that changed since is marked: read it again before relying on it;
- what git sees changed, including changes made by the agent's commands;
- the symbols looked up;
- the test, build and check commands run, each with its outcome, the first
  lines that report a problem, and its recall id. A run is marked when files
  changed after it;
- the checkout (branch and commit).

### The model's notes

The model's notes cover what the record cannot say:
- the status of the task and its parts;
- what was learned, with paths and symbols;
- the decisions made;
- what is pending;
- open questions;
- the next step.

**How they are requested:** the notes request continues the conversation
exactly as the model last saw it. It has the same messages, then one
request, with the same tools (none may be called) and a JSON schema for the
reply. A server with a prompt cache (llama.cpp, and the providers that cache
prefixes) processes only the request's own tokens, not the conversation
again.

**When that request does not fit or fails,** the notes are asked for from a
shortened transcript. When no notes can be had, the record alone is the
summary and says so. The session goes on either way.

**An interrupted compaction** changes nothing.

### Trust

The summary is data, like any summary: it never authorizes anything
([Security policy](security-policy.md)).
- Only the user's own messages fill the requests and constraints. A message
  counts only when the user typed it, and attached files and piped input
  are named there, not copied.
- What the model wrote is labelled as the model's notes, and what tools
  returned as the record or terminal data.
- Model text is written one line per item and cannot start a section or
  close the state.
- Sensitive operations are authorized by the current message's words, the
  user's rules or their approval, as before. A summary cannot stand in for
  them.

### Persistence

The state is also written to the session file as a `state` line before the
compaction it belongs to.
- A resumed session has the state as it was, and the summary message is in
  its history.
- Another model or provider can continue the session: the summary is an
  ordinary user message.
- A later compaction builds the record again from the archive and asks for
  new notes.

### Stale context

- Each tool result that shows or writes a file records the file's blob.
- Before each message, the model is told which files changed since the
  conversation last saw them, and whether the checkout moved.
- `recall` says when a result's file changed.
- The state marks files that changed between being read and being
  summarised.

A file the session read is therefore never silently treated as current
after it changed.

## Settings

| Key | Default | Meaning |
|---|---|---|
| `direct.prune` | `true` | Prune command output that has something to sort. |
| `direct.observation_bytes` | `0` (8 KiB local, 24 KiB cloud) | The budget such output is pruned to. |
| `direct.compaction` | `structured` | `structured` (the session state) or `legacy` (a free-text summary of a transcript, as before). |
| `direct.working_set_tokens` | `0` (32K local, 96K cloud) | The history budget. |
| `direct.max_tool_output` | `0` (16 KiB local, 48 KiB cloud) | The cap on any tool result. |

`bcode bench direct` takes `--prune on|off`, `--compaction`,
`--observation-bytes` and `--working-set`. `bcode bench prune` scores
pruning strategies on the corpus in `benchmarks/prune`.

## Bounded mode

Bounded mode does not keep its working context in the conversation. Each
attempt's context pack is built from the task ledger, git, verification
results and the code graph (`internal/contextplan`), so a task resumes
after a crash, a restart or OpenHands' condensation with an equivalent
pack. Its observations are OpenHands' own. Changing them in the gateway
would make the agent's history disagree with what it was sent, so this
phase leaves them unchanged.
