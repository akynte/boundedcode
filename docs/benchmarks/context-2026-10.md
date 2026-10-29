# Context benchmark (2026-10-29)

Does a long direct-mode session keep what it needs when its history is
shortened, and at what cost? This benchmark measures:
- pruning of command output, on a corpus of real tool output with no agent;
- the session state that replaces older history, against the free-text
  summary it replaces, in the agent loop;
- correctness, tokens, compaction latency and the prompt cache.

The behaviour is described in [Context and session state](../usage/context.md).

Machine and model as in the [baseline](baseline-2026-10.md): an RTX 4060
Laptop (8 GB) with 64 GB RAM, Qwen3.6-35B-A3B (UD-Q4_K_M), llama.cpp
b11146, 128K context, the MoE experts of 35 layers in system RAM, offload
threshold 256 ([inference benchmark](inference-2026-10.md)).

## Summary

- **The agent loop.** On the full suite at the defaults the new version
  passed 25 of 26 tasks in both runs, against 23 and 24, with 9% fewer
  prompt tokens and 8% less time per task. On five long-session tasks with
  a working set small enough that most sessions are summarised:
  - the session state with pruning passed 10 of 10 (9 of 10 after the
    cache fix below; that failure is an answer split over two messages);
  - without pruning, 9 of 10;
  - the free-text summary, 7 of 10, taking 396 s per task against 173 s.

  Pruning lost no task, so it stays on.
- **The cache.** The notes request continues the cached conversation. After
  a fix found in these runs, the median summary processed 467 prompt tokens
  instead of 3,971.
- **Pruning, with no agent.** The corpus is 42 outputs of real compilers,
  test runners, linters, package managers, stack traces and searches, in Go,
  Python, JavaScript, TypeScript, Rust, Java and C. At the default budget
  (8 KiB) every line the cases name as evidence was kept, in development
  (23 outputs) and held out (19). Command output came to about half the
  tokens of the 16 KiB head-and-tail cut used before: 50% fewer on the
  development set, 47% on the held-out set.
  - On the held-out set as frozen, the pruner lost one case, a timestamped
    log read as search results. After that was fixed it lost none (see
    below).
  - At 2 KiB it kept 62 of 64 evidence lines; the cut kept 52.
- **Pruning by a model (experimental), rejected.** The model was asked
  which lines to keep, at 4 KiB.
  - A 4B model on the CPU, beside the resident 35B, kept all the evidence
    in 16 of 42 outputs. It took 7–12 s per output and slowed the 35B's
    decoding by 25–47%.
  - The resident 35B kept it in 39 of 42, at 4–6 s per output, on the same
    single slot as the agent.
  - The deterministic pruner kept it in 42 of 42 in under 4 ms (median),
    and in fewer tokens.

## Pruning corpus

`benchmarks/prune/generate.sh` and `benchmarks/prune/heldout/generate.sh`
run real toolchains in the sandbox image. They use small projects with
planted failures, and they download from the public registries, so the
dependency logs are real. Each case lists the lines a developer needs to act
on the output: the failing test, the assertion, the error and its location,
the exception, the outcome.
- **Development set** (`benchmarks/prune/cases.yaml`, 23 outputs). Written
  before any pruning was run. The pruner was developed against it.
- **Held-out set** (`benchmarks/prune/heldout/`, 19 outputs). Other
  projects, tools and failures:
  - make and gcc;
  - the Go race detector and testify;
  - mypy, ruff and unittest;
  - jest and an npm 404;
  - a tsc project;
  - clippy and two Rust panics;
  - a Gradle JUnit failure and a Maven compile error;
  - searches in node_modules and the Python standard library;
  - an npm dependency tree.

  The patterns were written from the raw outputs and frozen before the
  pruner ran on them (digest `2ea6aa4b7dc7fb83`, 2026-10-28 21:31 UTC).

`bcode bench prune` fits each output to each budget with each strategy and
counts the evidence kept. Tokens are estimates (bytes / 3.2 by
`contextplan.EstimateTokens`). Report:
[20261029-prune-corpus](../../benchmarks/reports/20261029-prune-corpus.md).

| Set | Strategy | Budget | Cases with all evidence | Evidence lines kept | Tokens |
|---|---|---|---|---|---|
| development | head and tail (before) | 16 KiB | 23 / 23 | 71 / 71 | 57.6k |
| development | prune | 8 KiB | 23 / 23 | 71 / 71 | **28.5k** |
| development | prune | 4 KiB | 23 / 23 | 71 / 71 | 18.4k |
| development | head and tail | 4 KiB | 21 / 23 | 69 / 71 | 21.2k |
| held out | head and tail (before) | 16 KiB | 19 / 19 | 64 / 64 | 35.0k |
| held out | prune | 8 KiB | 19 / 19 | 64 / 64 | **18.6k** |
| held out | prune | 4 KiB | 19 / 19 | 64 / 64 | 14.1k |
| held out | head and tail | 4 KiB | 19 / 19 | 64 / 64 | 15.8k |
| held out | prune | 2 KiB | 17 / 19 | 62 / 64 | 8.9k |
| held out | head and tail | 2 KiB | 12 / 19 | 52 / 64 | 10.4k |

What the development set exposed, and was fixed while building the pruner:
- Error words inside qualified names (`std::panic::catch_unwind`) made every
  Rust library frame outrank the failure.
- A build tool repeating the compiler's messages (Gradle) counted each
  error twice.
- Go's `t.Logf` lines, which carry `file:line`, were kept as located
  messages, and the markers for gaps between them were not counted against
  the budget.
- Compiler error lists have the shape of search results.

The held-out set, as frozen, exposed one more. A timestamped log
(`2026-10-28 21:30:24,983 INFO …`) was read as `path-line-` search output,
so its 12 `ERROR … upstream returned 503` lines were lost at every budget.
Search detection now requires a path with a file extension. After that fix
the held-out results are those in the table. They are no longer unseen
numbers, and that one fix is the only change made after the freeze.

The slowest case, a 115 KB Python traceback after 400 repeated warnings,
took 63 ms. The median was about 1 ms.

## Agent loop

`bcode bench direct` on one binary, two runs per arm. Every report is in
`benchmarks/reports/20261029-*`.

### The full suite, at the defaults

28 tasks; two (Java, Rust) are skipped here for the missing toolchain and
count as failures in the pass rate. Before: free-text summary, head-and-tail
cut. After: the session state and pruning.

| Arm | Run | Passed | Prompt tokens per task | Processed by the server per task | Time per task |
|---|---|---|---|---|---|
| before | 1 | 23 / 26 | 47.7k | 3,467 | 37.2 s |
| before | 2 | 24 / 26 | 53.1k | 3,804 | 37.2 s |
| after | 1 | **25 / 26** | 48.7k | 3,301 | 34.7 s |
| after | 2 | **25 / 26** | 43.4k | 3,019 | 33.7 s |

- `multi-feature-discount` failed in all four runs: the model's discount
  exceeds the subtotal. It is the same failure as in the baseline.
- Before, run 1 also failed `fix-balance-query` and `session-noisy-tests`;
  run 2 failed `session-ledger`. After, neither run failed any other task.
- `session-noisy-tests` is the case pruning was built for: the failing test
  is about 480 lines into 107 KB of output. The head-and-tail cut lost its
  name in one of the two runs.
- No session in the suite reached the working set (32K), so none was
  summarised. Pruning shortened 2 outputs in one run and 4 in the other.

### Long sessions, summarised

The five session tasks, three to five messages each, with a 6K working set.
Three of the five are summarised in nearly every run (`session-x-tools` 10
to 29 times); the other two at most once. Three arms on the binary measured
above:

| Arm | Passed | Time per task | Prompt tokens per task | Processed per task | Summaries | Time per summary | Processed by the first call after each |
|---|---|---|---|---|---|---|---|
| free-text summary, no pruning | 7 / 10 | 396 s | 143k | 57.7k | 49 | 42.2 s | 8,388 |
| session state, no pruning | 9 / 10 | 183 s | 147k | 44.8k | 34 | 15.3 s | 5,884 |
| session state, pruning | **10 / 10** | **173 s** | **135k** | **39.9k** | 30 | 16.0 s | 5,732 |

- The free-text summary failed three tasks, all in run 1:
  - `session-noisy-tests`: the answer after the summary no longer named the
    failing test;
  - `session-resume-constraints`: after the resume, the answer dropped one
    of the user's two rules;
  - `session-x-tools`: the model did not find the type it was asked about
    and the session was summarised 29 times (76 model calls). Most of that
    arm's time and summaries are this one task.
- The session state without pruning failed `session-x-tools` once: the fix
  made a hidden test fail (`Len() = 66, want 33`). It is a wrong fix, not
  lost context.
- Pruning on against pruning off: one more pass and 11% fewer processed
  tokens, with 389 KB of command output shown as 30 KB. No task did worse
  with pruning, so it stays on.
- A free-text summary took 42 s on average, almost all of it writing: about
  1,600 generated tokens with thinking on. Its prompt is short (590 tokens)
  because its transcript keeps 400 characters of each tool result. The
  notes took 16 s: 356 generated tokens, and 6.4 s processing the prompt.
- The first call after a summary processed 32% fewer tokens with the
  session state (5,732 against 8,388).

### The notes request and the cache

The notes request is meant to process only its own tokens
([below](#cache-aligned-compaction)). In these runs it processed 3,600 per
summary on average. The call log shows why. A summary follows the pass that
sets older results aside, and that pass changes the history near its start,
so only the system prompt was still cached. Where nothing had been set
aside since the last call, a notes request processed 440–740 tokens.

The notes request now sends results set aside in that same pass as the last
call sent them, when the request still fits the context window. It then
continues the cached conversation. `TestNotesRequestContinuesAfterSettingAside`
covers it.

Measured again on the default setting (session state, pruning), two runs:

| Notes request | Passed | Time per task | Processed per task | Summaries | Processed per summary (median, mean) | Prompt time per summary | Time per summary |
|---|---|---|---|---|---|---|---|
| as measured above | 10 / 10 | 173 s | 39.9k | 30 | 3,971, 3,612 | 6.4 s | 16.0 s |
| sending results set aside in the same pass | 9 / 10 | 169 s | **35.0k** | 28 | **467**, 1,750 | 4.0 s | 13.2 s |

- Every notes request reused the whole previous request from the cache.
  The summaries that still processed thousands of tokens did so for the
  tool results added since that request, such as a large file read; those
  are new to the server on any path.
- The failure is `session-x-tools`, turn 6, a question about two earlier
  answers. The model gave the first ("`go/ast/astutil/rewrite.go`") from
  the session state, then searched for the second, and its last message
  gave only the second. The bench scores a turn's last message, so it
  counts as a failure. It is not lost context, but it is scored as
  measured.
- What is left of a summary's time is the notes themselves: about 350
  generated tokens, 9 s at this machine's decode speed.

## Cache-aligned compaction

The free-text summary sent a new request: its own system prompt and a
transcript of the history. The server had to process all of it again. The
session state's notes request continues the conversation instead: the same
messages and tools, then one request, with `tool_choice: none` and a JSON
schema for the reply.

Measured on the server, on a 7,151-token conversation:

| Request | Prompt tokens processed | Prompt time |
|---|---|---|
| The conversation again (cache) | 4 | 97 ms |
| The notes request (continues it) | 27 | 295 ms |
| A summary request with its own prefix (no tools) | 6,924 | 9,723 ms |
| The conversation again, after either | 4 | 87–92 ms |

The server's RAM prompt cache keeps the conversation's state, so a summary
request does not cost the next call its cache.

Two server behaviours shaped the request:
- With tools in the request and thinking on, the model called a tool
  instead of writing notes. `tool_choice: none` alone did not stop it: the
  model wrote the tool call as text.
- llama.cpp b11146 rejects a JSON schema together with tools ("failed to
  parse grammar") unless `tool_choice` is `none`. With both, the reply is
  the schema's JSON and the cache is used.

### Precomputing summaries

Not implemented. Precomputing would write the notes ahead of time, while
the session is idle, so a summary costs nothing when it is needed. On this
setup it would not pay:
- The server has one slot (`--parallel 1`). A notes request made ahead of
  time runs on the same slot as the user's next request, which waits for
  it: up to the 9 s it takes to write the notes.
- At the default working set (32K) no session in the full suite was
  summarised, so most precomputed notes would never be used.
- With the cache-aligned request a summary costs 13 s (median prompt 467
  tokens), once per summary. Precomputing could at best move that time,
  and it would spend it again whenever the session changes before the
  summary.

A server with a second slot, or a provider, would change the first point.
That remains to be measured.

## Pruning by a model (experimental)

Run after the deterministic pruner was measured, on the same corpus at
4 KiB. The model is shown each output with numbered lines (each cut to 300
bytes) and the case's goal. It answers with the line ranges to keep, as
JSON. When the reply cannot be used, the bench falls back to the head-and-
tail cut and counts an error. Reports:
[20261029-prune-model-small](../../benchmarks/reports/20261029-prune-model-small.md),
[20261029-prune-model-resident](../../benchmarks/reports/20261029-prune-model-resident.md).

| Pruner | Cases with all evidence | Evidence lines kept | Tokens shown (dev, held out) | Median time per output | Errors |
|---|---|---|---|---|---|
| deterministic (`internal/prune`) | **42 / 42** | **135 / 135** | **18.4k, 14.1k** | **3.8 ms, 0.5 ms** | 0 |
| Qwen3-4B-Instruct-2507 (Q4_K_M), CPU, 8 threads | 16 / 42 | 59 / 135 | 63.3k, 52.9k | 11.5 s, 6.8 s | 2 |
| the resident Qwen3.6-35B-A3B, thinking off | 39 / 42 | 130 / 135 | 70.3k, 47.3k | 5.9 s, 3.8 s | 0 |

**The small model's cost, beside the resident 35B:**
- It loaded in 6.1 s.
- Its resident memory was 9,727 MiB when loaded and 17,859 MiB after it
  read the corpus, with a 48K context.
- GPU memory rose by 467 MiB, although no layer was offloaded.
- While it read a long output, the 35B decoded at 20–30 tokens/s instead
  of 38–40, so the agent's own replies slowed by 25–47%.

**The resident model's cost.** The 35B runs on the agent's only server
slot, so each pruning call delays the agent's next request by the whole
call: 4–6 s in the median, and 40 s for the 85 KB Go build log.

**What the models lost:**
- The 4B model lost evidence in 26 cases, most of it the errors
  themselves: compiler errors, failing tests, assertions and outcome lines
  (the report lists them).
- The 35B lost three cases:
  - a Go panic's frames and exit status;
  - one warning of a C build;
  - the root of an npm dependency tree.
- Both showed more tokens than the deterministic pruner: the ranges they
  chose kept more lines.

The first run of the 35B had thinking on. It spent its replies on
reasoning, and 40 of 42 came back without JSON (scored with the cut;
[report](../../benchmarks/reports/20261029-prune-model-resident-thinking.md)). The
request now turns thinking off, and the table is the second run.

Goal-conditioned pruning by a model is therefore not part of this phase.
It cost seconds per output and either memory and decode speed (the small
model) or the agent's slot (the resident one), and it lost evidence that
the deterministic pruner kept.

## Limits

- One model on one machine; two runs per arm in the agent loop.
- Some of the session tasks are new to this phase, and the runs used them
  as they were written. They are short (three to five messages), so the
  stress arm uses a 6K working set to make every session compact. That is
  far below the default of 32K, which these sessions do not reach.
- The bench scores a session turn by its last message. One failure
  (`session-x-tools` after the cache fix) was an answer split over two
  messages, and it is counted as a failure.
- Two follow-ups came after the measurements: `recall` of an elided pruned
  result shows what the model was shown, and the pruning header's plurals.
  Neither changes what the model sees on the measured paths.
- The before arm of the full suite keeps the originals of cut outputs for
  recall, as the after arm does; only pruning and the summary differ.
- Pruning was measured on command output (`bash`). Other tools' results
  over the cap keep the head-and-tail cut, with the original kept for
  recall.
- Bounded mode's observations are OpenHands' own and were not changed. Its
  context is rebuilt from the task ledger, not from the conversation.
- The cloud providers' handling of the notes request (a schema with
  `tool_choice: none`) is covered by the translation tests. It was not run
  against the live APIs.
