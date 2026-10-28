# Inference benchmark (2026-10-28)

Does BoundedCode's inference protocol give the model the conversation it
actually had, recover from the failures it meets, and use the local
server's cache well? Each change was measured before and after: task
correctness, prompt processing, time to the first token, and how often the
control plane can use what the model returns. The behaviour is described in
[Inference protocol](../usage/inference.md).

Machine and model as in the [baseline](baseline-2026-10.md): an RTX 4060
Laptop (8 GB) with 64 GB RAM, Qwen3.6-35B-A3B (UD-Q4_K_M), llama.cpp
b11146, 128K context, MoE experts of 35 layers in system RAM.

## Summary

- **Reasoning back to the model** (`inference.reasoning_history: auto`).
  Direct-mode suite, 2 runs per arm on one binary:
  - task results unchanged: 42 of 46 in both arms, the same tasks failing
    the same way;
  - the server processed 9% fewer prompt tokens and spent 16% less time on
    prompts (8.2 s instead of 9.8 s per task);
  - median time to the first token 1,662 ms instead of 1,757 ms;
  - the prompts grew 11%: the reasoning stays in the context.
- **Offload threshold** (`op_offload_min_batch: 256`): processing 57–124
  new tokens takes 0.43–0.82 s instead of 0.80–1.15 s, and the same as
  before from 173 tokens up. In the suite, the median small call (fewer than
  150 new tokens) fell from 1,203 ms to 776 ms.
- **Structured output** (`task.structured_output`), on 37 held-out issues:
  - contracts and the ambiguity check were already always parseable, and
    stayed so;
  - counterexample tests were usable 36 times instead of 34, with a median
    of 23 s instead of 27 s. That is 2 better and 0 worse, not significant.
- **Failures:** the three HTTP 502s of the retrieval phase came from
  connections llama-server had closed. In the 10 suite runs since (8 direct
  mode, 2 bounded) and the 12 rechecks, no call
  failed.
- **Bounded suite**, 19 tasks, one run per arm: 19 verified with `drop`, 18
  with `auto`. The `auto` failure is the false verification the baseline
  already showed on the same task (2 of its 3 runs): a correct fix, plus a
  test added to a test file the task forbade changing. Three more runs of
  that task per arm passed 1 of 3 each, every failure the same: 2 of 4
  with `drop`, 1 of 4 with `auto`, 1 of 3 in the baseline.
  With `auto`, the server processed 29% fewer uncached prompt tokens per
  task (21.6k against 30.4k), lower in 16 of 19 tasks, and spent 22% less
  time on prompts.

## Method

Each comparison runs on one binary, with only the setting under test
changed, on the same server.
- **Direct-mode suite.** 25 tasks: edits, explanations, diagnoses and two
  multi-turn sessions. The Java and Rust tasks are skipped (no toolchain on
  this machine), leaving 23. The arms alternated: `drop`, `auto`, `drop`,
  `auto`.
- **Bounded suite.** The 19 tasks of the [baseline](baseline-2026-10.md)
  with their hidden checks, in the container sandbox, one run with `drop`
  and then one with `auto`. The agent (OpenHands) drops reasoning from the
  history; with `auto`, the gateway puts it back from what it kept of each
  answer. Bounded reports count the prompt tokens the server did not take
  from its cache (prompt minus cached), from the task ledger.
- **"Before"** is the direct-mode suite of the retrieval phase on the same
  machine: two runs of the build that phase shipped, with the server's
  default offload threshold.
- **The server's environment.** The user profile on this machine (written
  by `bench infra --apply`) replaces the built-in server section, so the
  threshold was given to the managed server through its environment
  (`GGML_OP_OFFLOAD_MIN_BATCH=256`) for every run of this phase. The same
  profile leaves out the built-in `reasoning_budget`, as it did in every
  earlier benchmark on this machine.

Reports: direct suite
[drop r1](../../benchmarks/reports/20261028-direct-reasoning-drop-r1.md),
[r2](../../benchmarks/reports/20261028-direct-reasoning-drop-r2.md),
[auto r1](../../benchmarks/reports/20261028-direct-reasoning-auto-r1.md),
[r2](../../benchmarks/reports/20261028-direct-reasoning-auto-r2.md);
[structured output](../../benchmarks/reports/20261028-structured-heldout.md);
bounded suite
[drop](../../benchmarks/reports/20261028-tasks-reasoning-drop.md),
[auto](../../benchmarks/reports/20261028-tasks-reasoning-auto.md);
rechecks `20261029-recheck-*`.

## Audit findings

| Area | Found | Change |
|---|---|---|
| Reasoning, local | Neither mode sent the model's reasoning back. Qwen3.6's template renders it (`reasoning_content`), and llama-server preserves it in every turn by default. The model read empty reasoning blocks in its own turns, and every call processed the previous answer again | Sent back where the template renders it, decided per server from `/props`. Bounded mode, whose agent drops it, gets it restored by the gateway |
| Reasoning, cloud | Anthropic thinking blocks and Gemini thought signatures were already replayed verbatim from the host | Unchanged; never sent to an API that does not take it |
| Streams | A stream that broke off was taken as a complete answer, with its partial text and half-written tool calls. An error event came back as a 200 | Detected, discarded and sent once more; error events keep their status |
| Local transport | The three 502s of the retrieval phase reached no slot (the server's log has no trace of them): a request raced llama-server closing an idle connection. A malformed tool call ended a bounded task with llama.cpp's 500 | Local requests are replayable (`Idempotency-Key`); connection failures and 500s are retried, up to 3 attempts |
| Context overflow | Direct mode recognised llama.cpp's and OpenAI's wording, not Anthropic's or Gemini's, so a cloud overflow failed the turn instead of summarising | One shape, `context_length_exceeded`, for every provider |
| Malformed answers | A 200 without choices failed later with "no choices" | A 502 that names it; tool calls get an id, type and JSON arguments |
| Cache attribution | A cache miss could not be told from a rewrite of the history | Each call records the first message that changed and the tokens the server evaluated |
| Prefill | Every prompt step of 32 tokens or more paid about 0.7 s to copy the experts to the GPU | Measured; threshold 256 in the profile |
| Checkpoints | A hybrid model rolls back to its latest checkpoint. In the suite this cost 1,071 extra tokens over 97 follow-up calls: negligible | Settings exposed, defaults unchanged |
| Structured output | Contract, grounding and counterexample replies were free text parsed for a JSON object | The schema is sent as each provider takes it |
| Router | Rule-based; it makes no model call | Nothing to constrain |
| Main loop | Native tool calling | Kept; no alternative was measured |

## Reasoning in the history

| Direct suite (23 tasks × 2 runs) | Before (retrieval phase) | `drop` | `auto` |
|---|---|---|---|
| Passed | 44 / 46 | 42 / 46 | 42 / 46 |
| Model calls per task | 5.2 | 5.3 | 5.5 |
| Prompt tokens per task (cached included) | 36.4k | 40.5k | 45.1k |
| Served from the cache | 91% | 92% | 93% |
| Prompt tokens the server evaluated, per task | 3,448 | 3,387 | **3,079** |
| … per follow-up call | | 662 | **591** |
| Prompt processing, s per task | 10.2 | 9.8 | **8.2** |
| Time to the first token, median | 1,763 ms | 1,757 ms | **1,662 ms** |
| Largest prompt, mean per task | 6.9k | 6.8k | 7.3k |
| Generated tokens per task | 1,115 | 972 | 982 |
| Seconds per task, mean (median) | 41 (26) | 36 (23) | 35 (21) |

- **Correctness.** In both arms the same four runs failed, all with a
  wrong fix of the model's own: `fix-balance-query` twice and
  `multi-feature-discount` twice. Neither is new: over every earlier
  direct-mode report they passed 15 of 26 and 7 of 18 runs. In the retrieval phase's two runs they
  passed 2 of 2 and 1 of 2. Three more runs of `fix-balance-query` on the
  same server, alternating the builds, passed 3 of 3 with the retrieval
  phase's build and 3 of 3 with this one (`auto`).
- **Cache.** On follow-up calls the server evaluated 71 fewer tokens per
  call. The previous answer was 175 tokens, so about 40% of it is reused,
  not all. A possible reason, not verified: the template renders tool calls
  again from their JSON arguments, which need not match the model's own
  tokens.
- **Cost.** The reasoning stays in the context, so prompts are 11% larger
  and the largest prompt 7% larger. Direct mode counts it toward the
  working set.
- **Multi-turn sessions.** `session-ledger` evaluated 26% fewer tokens
  (3.8k against 5.2k). `session-x-tools` ran for different lengths in the
  four runs, and its largest prompt was 30–35k tokens with `auto` against
  22–28k with `drop`.

## Offload threshold

The prompt's new tokens processed after a cached conversation, in the shape
of an agent loop: each request extends the previous one exactly, with a
tool result of the given size. Median of 3 runs per size and threshold.

| New tokens | Default (32) | 256 | 1,024 | 4,096 |
|---|---|---|---|---|
| 57 | 801 ms | **433 ms** | 451 ms | 461 ms |
| 94 | 1,014 ms | **645 ms** | 660 ms | 665 ms |
| 124 | 1,104 ms | **821 ms** | 805 ms | 824 ms |
| 173 | 1,324 ms | 1,179 ms | 1,242 ms | 1,236 ms |
| 287 | 1,479 ms | 1,482 ms | 1,973 ms | 2,023 ms |
| 532 | 1,706 ms | 1,698 ms | 3,328 ms | 3,485 ms |
| 1,273 | 2,050 ms | 2,048 ms | 2,070 ms | 7,390 ms |
| 2,595 | 4,004 ms | 4,000 ms | 5,784 ms | 14,787 ms |

Below the threshold the experts are computed on the CPU, which is faster
than copying them to the GPU (about 0.7 s) until about 170 tokens. 256 is
never slower than the default within noise.

A first sweep that put the new tokens inside the user message was
discarded. That prompt differed from the cache inside a message, and this
hybrid model (`qwen35moe`: recurrent layers with full attention every
fourth layer) rolled back to its latest checkpoint, processing about 700
more tokens than were new.

In the suite, the median prompt step of fewer than 150 new tokens took
1,203 ms before (43 calls) and 776 ms with the threshold (44 calls,
`drop`). With `auto`, such small steps are twice as common (97 calls) and
took 400 ms at the median.

## Structured output

37 held-out issues of the [retrieval benchmark](retrieval-2026-10.md),
up to 4 per language, picked by a seeded order fixed before the run. Each
call ran once without the schema and once with it, alternating which went
first. The decisive question is whether the parser of a task run accepts
the reply.

| Call | Prompt only | With schema | Paired: better / worse |
|---|---|---|---|
| Contract | 35 / 37 usable; 2 named nothing required | 35 / 37, the same 2 | 0 / 0 |
| Ambiguity check | 9 / 9 | 7 / 7 | 0 / 0 (5 pairs) |
| Counterexample tests | 34 / 37: 1 malformed JSON, 2 cut at the 3,000-token limit | 36 / 37: 1 cut at the limit | 2 / 0 (p = 0.5) |
| Counterexample, median time | 27.3 s | 23.2 s | |
| Counterexample tests proposed (named as test files) | 42 (41) | 46 (46) | |

The local model already writes parseable contracts without the schema.
For whole test files inside JSON strings, the grammar prevents malformed
escaping, but not a reply cut at the output limit. The setting stays on: it
lost nothing measured, and the difference is too small to be significant.

## Bounded suite

| Bounded suite (19 tasks) | Baseline, 3 runs (earlier build) | `drop` | `auto` |
|---|---|---|---|
| Verified by the hidden checks | 16, 17, 16 | 19 | 18 |
| Self-verified, hidden checks failed | 2, 1, 2 | 0 | 1 |
| Attempts | 36, 38, 37 | 38 | 36 |
| Model calls per task | 18.6–22.5 | 21.6 | 20.6 |
| Prompt tokens per task (cached included) | 282k–372k | 386k | 341k |
| Uncached prompt tokens per task | 23.7k–31.8k | 30.4k | **21.6k** |
| Prompt processing, s per task | 52.5–67.9 | 65.4 | **50.7** |
| Generated tokens per task | 6.3k–8.6k | 6.9k | 5.8k |
| Tokens processed again after a cache miss, all tasks | 17.0k–58.0k | 40.8k | 19.7k |
| Seconds per task, mean (median) | 282–364 (227–237) | 321 (215) | 269 (235) |
| Failed model calls | 0 | 0 | 0 |

- **Correctness.** The one failure, `go-debug-ledger-balance-query` with
  `auto`, is the false verification the baseline documented: the code fix
  was right (`if e.AccountID == accountID`), and the agent added a test to
  `ledger_test.go`, which the request forbade and a hidden check enforces.
  Three more runs per arm (`20261029-recheck-tasks-*`): 1 of 3 passed with
  each, and each of the four failures was this one check. Over all runs
  the task passed 2 of 4 with `drop`, 1 of 4 with `auto` and 1 of 3 in the
  baseline: the model's habit, not the setting. Verification does not read
  criteria written in prose, so the gate cannot stop it.
- **Cache.** Per task, `auto` processed fewer uncached prompt tokens in 16
  of 19 tasks; the three others differ by under 12%. Most of the saving is in
  the long tasks: `cross-service-idempotency` 149k → 70k (3 attempts with
  `drop`, 2 with `auto`), `go-large-iface-method` 76k → 42k.
- **Time.** The mean fell from 321 s to 269 s, mostly from
  `cross-service-idempotency`'s extra attempt with `drop`; the median rose
  from 215 s to 235 s. One run per arm does not separate the setting from the
  model's variance in wall time. The tokens and the prefill time are the
  measured effect.

## Limits

- One model on one machine. The offload threshold depends on the GPU's bus
  and the CPU; `bench infra` does not tune it yet.
- Two runs per arm in the direct-mode suite; the model's variance between
  runs is one or two tasks.
- The cloud translations (Anthropic `output_config.format`, Gemini
  `responseJsonSchema`, the context-overflow wording) are covered by tests
  against recorded API shapes. They were not run against the live APIs,
  which would spend money.
- The reasoning round trip for OpenAI-compatible services that require it
  (`keep`) is tested against a fake server only.
