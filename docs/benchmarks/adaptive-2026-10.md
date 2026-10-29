# Adaptive execution benchmark (2026-10-29)

What Phase 09 added, measured:
- model profiles that describe capabilities, checked against the weights and
  probed with BoundedCode's own requests;
- local models of four sizes on the same tasks;
- speculative decoding (n-gram, a draft model, MTP);
- routing: thinking chosen per model call, and model choice per kind of
  request;
- escalation decisions;
- the data that escalation sends out.

Machine and runtime as in the [baseline](baseline-2026-10.md): an RTX 4060
Laptop (8 GB) with 64 GB RAM, llama.cpp b11146. The default model is
Qwen3.6-35B-A3B (UD-Q4_K_M), at 128K context with
`GGML_OP_OFFLOAD_MIN_BATCH=256`. This machine's user profile for it has no
`reasoning_budget` ([inference](inference-2026-10.md)), and every run of it
here uses that profile.

## Summary

- **One change is faster end to end: MTP.** On the default model with its
  multi-token prediction layer (`qwen3.6-35b-a3b-mtp`), measured against the
  same profile with speculation off:
  - decode went from 36.6 to **51.9 tokens/s (+42%)**, with 80% of drafted
    tokens accepted;
  - a direct-suite task took **33.3 s instead of 38.7 s (−14%)**;
  - it passed 49 of 52 tasks, against 48;
  - the server took 3.2 GiB more memory and 1.7 GiB more GPU memory.

  Against the default profile (37.1 s per task, 51 of 52 passed), it is
  10% faster. It is a separate, experimental profile: it needs the -MTP
  build of the weights (22.7 GB) and three more expert layers in system
  RAM.
- **n-gram speculation and a draft model did not help in the agent.**
  - n-gram rewrote a file 2.4× faster in isolation, but in the agent loop
    only 23% of its drafted tokens were accepted. Tasks took 40.9 s
    against 37.1 s.
  - A 0.8B draft model was slower even in isolation (0.75×).
- **Routing thinking per call did not pay off.**
  - Answers to questions took 41% fewer generated tokens, and choosing
    what to read 27% fewer, but debugging and editing then took 53–59%
    more.
  - Time per task was the same (38.2 s against 37.1 s).
  - Change tasks passed 18 of 22, against 21 of 22.

  `direct.routing` stays off.
- **The router's model fallback never worked with a thinking model.** In
  80 tokens the model only thought: 0 of 24 unsure requests got its answer,
  and each waited 2.3 s for the rules' choice. With thinking off it
  answered, but it sent 14 of 16 plain questions to a change. Adaptive
  routing therefore skips the call: the same decisions, 2.3 s sooner.
- **Model sizes.** On this machine the default model is the most reliable
  and also the fastest at change tasks:

  | Model | Passed | Time per task |
  |---|---|---|
  | 35B-A3B (default) | 51 / 52 | 37 s |
  | 80B-A3B (3-bit) | 24 / 26 | 47 s |
  | 9B | 24 / 26 | 60 s |
  | 4B | 21 / 26 | 40 s |

  The 4B answered all 15 questions in 23 s each against 34 s. On that
  single run, model routing would switch to it for questions, and keeps
  the default for changes.
- **The adaptive escalation policy, replayed on 135 recorded runs,** makes
  114 escalations against 129 (−12%). It still covers all 16 runs that
  needed help. Most escalations in both (97) are the pre-merge review that
  the configured keywords trigger.
- **Abstract escalation packets** carry none of the repositories' source
  lines, against 670 lines in 19 full packets, and are 53% smaller. They
  still name 17.6 source identifiers each.

**Acceptance.** The MTP profile beats its baseline on time per task (−14%)
and decode speed (+42%), with no loss in tasks passed, over two runs each.
Skipping the router's dead classification call saves 2.3 s per unsure
request with identical decisions. Everything is off or opt-in by default:
`direct.routing: off`, `escalation.policy: rules`, `frontier.data: full`,
no speculative decoding, and the default model unchanged.

## Model capabilities

`bcode bench models` loads each model alone and sends it what the agent
sends:
- a request that needs a tool call;
- the tool's result, to answer from;
- an edit whose arguments hold code with quotes and tabs;
- an answer constrained to a JSON schema;
- the same question with thinking on and off;
- a fact placed in a 32K-token prompt.

It also checks the profile's declared tokenizer, context and MTP layers
against the GGUF header. Reports:
[20261029-models](../../benchmarks/reports/20261029-models.md) and
[20261029-models-mtp](../../benchmarks/reports/20261029-models-mtp.md)
(the MTP profile at `n_cpu_moe 38`).

| Model | Fit | Load | Tool call | Tool result | Code in arguments | JSON schema | Thinking off | 32K recall | Decode | Prompt |
|---|---|---|---|---|---|---|---|---|---|---|
| Qwen3.5-4B | GPU | 2.5 s | pass | pass | pass | **fail** | toggle | pass | 72.6 t/s | 2,596 t/s |
| Qwen3.5-9B | split | 4.5 s | pass | pass | pass | pass | toggle | pass | 28.9 | 1,356 |
| Qwen3.6-35B-A3B | offload | 7.0 s | pass | pass | pass | pass | toggle | pass | 40.0 | 758 |
| Qwen3.6-35B-A3B MTP | offload | 10.5 s | pass | pass | pass | pass | toggle | pass | 52.6 | 708 |
| Qwen3-Coder-Next (80B-A3B, IQ3) | offload | 14.5 s | pass | pass | pass | pass | none | pass | 26.3 | 601 |
| Laguna XS 2.1 | offload | 9.5 s | pass | pass | pass | pass | toggle | pass | 50.0 | 784 |

Corrections to the profiles that the probe and the weights' headers led to:
- **Qwen3.5-4B:** under a JSON schema it writes on inside a string field
  until the token limit (1,500 tokens). This is now a recorded limitation.
- **Laguna:** it turns thinking off per request; its profile now says so.
- **The MTP profile** ran out of GPU memory at the default's `n_cpu_moe 35`
  and at 36. The MTP context needs another 256 MiB of KV cache plus
  compute buffers. It loads at 38.
- **Declared against the weights:** the default model's file has no MTP
  layer; the -MTP build has one (`blk.40.nextn`). The draft model shares
  the default's tokenizer (`qwen35`). Coder-Next's is `qwen2`.

gpt-oss-20b, Devstral Small 2 and Qwen3.8-27B were not downloaded.
gpt-oss's usage policy has to be accepted by the user. The other two are
dense models of 14–16 GB that would split with system RAM on an 8 GB GPU.

## Models across sizes

`bcode bench direct -m NAME`: 28 tasks, two of them skipped (no Java or
Rust toolchain). The default model's figures are the two baseline runs of
the A/B below; the others are one run each. Reports:
`benchmarks/reports/20261029-direct-p9-{base-r1,base-r2,size-*}`.

| Model | Size | Passed | Questions | Changes | Time per task | Generated tokens per task | Peak GPU / server RSS |
|---|---|---|---|---|---|---|---|
| Qwen3.6-35B-A3B | 35B MoE, 3B active | **51 / 52** | 30 / 30, 33.7 s | **21 / 22**, 41.8 s | **37.1 s** | 1,006 | 7.0 / 28.2 GiB |
| Qwen3-Coder-Next | 80B MoE, 3B active (3-bit) | 24 / 26 | 15 / 15, 35.2 s | 9 / 11, 62.3 s | 46.7 s | 778 | 7.4 / 33.7 GiB |
| Qwen3.5-9B | 9B dense | 24 / 26 | 15 / 15, 53.0 s | 9 / 11, 70.1 s | 60.2 s | 1,279 | 6.5 / 10.7 GiB |
| Qwen3.5-4B | 4B dense | 21 / 26 | 15 / 15, **22.6 s** | 6 / 11, 64.5 s | 40.3 s | 2,396 | 4.9 / 10.3 GiB |

Size does not order the models here:
- The 4B decodes 1.8× faster than the default model and answers questions
  33% sooner. On changes it reasons for twice the tokens and fails 5 of 11.
- The 9B does not fit the GPU with its 64K context, so it runs slower than
  the 35B MoE model.
- The 80B coder model in a 3-bit quant is reliable but 26% slower.

**Model routing on these measurements** (`bcode model route`). Each
model's latest runs are compared on the same kind of request:
- **Changes:** keep the default model. No measured model is better beyond
  chance (90% Wilson intervals) or faster at the same reliability.
- **Questions:** switch to the 4B. It answered 15 of 15 against 30 of 30,
  which cannot be told apart, and over five questions it would save 55 s
  against a 2.5 s load.

That is one run of 15 questions, and a session that switches models was
not run. So the policy only reports what it would choose; it does not
switch the server. A model whose load time was never measured, or that
does not fit the GPU, is never chosen.

## Speculative decoding

`bcode bench spec`: each configuration loads the server alone and answers
four prompts twice, with greedy decoding and the prompt cache off:
- rewriting a 245-line Go file with a small change;
- writing new code;
- explaining the file in prose;
- reasoning with thinking on.

Reports: `benchmarks/reports/20261029-spec-*`.

**On the default model (MoE, experts in system RAM):**

| Configuration | All prompts | Rewrite | New code | Explain | Reasoning | Accepted | Answers unchanged | Peak GPU |
|---|---|---|---|---|---|---|---|---|
| none | 39.1 t/s | 38.7 | 39.8 | 38.8 | 39.4 | – | 8 / 8 | 7.0 GiB |
| ngram-mod | 51.7 (1.32×) | **94.0 (2.43×)** | 39.8 | 39.2 | 38.9 | 81% | 6 / 8 | 7.0 |
| ngram-simple | 51.9 (1.33×) | 101.8 (2.63×) | 38.8 | 39.7 | 38.0 | 76% | 4 / 8 | 7.0 |
| ngram-map-k | 44.2 (1.13×) | 55.9 (1.44×) | 38.8 | 39.7 | 38.1 | 62% | 4 / 8 | 7.0 |
| draft: Qwen3.5-0.8B, `n_cpu_moe` 40 | 29.1 (0.75×) | 41.4 | 30.1 | 23.2 | 23.3 | 70% | 2 / 8 | 7.6 |
| MTP, `n_cpu_moe` 38 | **51.1 (1.31×)** | 59.1 (1.54×) | **52.4 (1.32×)** | **44.4 (1.14×)** | **46.1 (1.17×)** | 79% | 2 / 8 | 7.2 |
| MTP, `n_cpu_moe` 40 | 49.4 (1.27×) | 57.6 | 51.5 | 41.9 | 44.3 | 77% | 2 / 8 | 6.3 |

The second baseline run (38.9 t/s overall) is in the draft and MTP report.

**On the 9B (dense, on the GPU):**

| Configuration | All prompts | Rewrite | New code | Accepted | Answers unchanged |
|---|---|---|---|---|---|
| none | 27.7 t/s | 27.3 | 28.7 | – | 8 / 8 |
| ngram-mod | 47.4 (1.71×) | 85.5 (3.13×) | 33.3 | 78% | 4 / 8 |
| draft: Qwen3.5-0.8B | 21.9 (0.79×) | 26.1 | 21.3 | 83% | 2 / 8 |

What this shows:
- **n-gram speculation drafts only what is already in the prompt.** It makes
  a rewrite 2.4–3.1× faster and leaves everything else unchanged.
- **MTP drafts from the model itself.** It speeds up every kind of output,
  by 1.14–1.54×.
- **A separate draft model is slower on both models.** On the MoE model,
  its verification batches use more experts in system RAM. On the dense
  model, the draft and the target share the one GPU.
- **Speculation changed greedy answers.** In theory greedy speculative
  decoding produces the same answer. Here, verifying drafted tokens in
  batches changed later tokens of most answers that were not rewrites,
  which differ in floating point from one-token steps. Only the suite says
  whether that matters.

**In the agent loop.** `bcode bench direct`, two runs per arm, interleaved:

| Arm | Passed | Time per task | Decode | Accepted | Questions | Changes | Peak GPU / server RSS |
|---|---|---|---|---|---|---|---|
| Default profile | 51 / 52 | 37.1 s | 37.7 t/s | – | 30 / 30, 33.7 s | 21 / 22, 41.8 s | 7.0 / 28.2 GiB |
| Default + `ngram-mod` | 49 / 52 | 40.9 s | 36.1 | 23% | 29 / 30, 36.6 s | 20 / 22, 46.8 s | 7.0 / 29.2 |
| MTP profile, speculation off | 48 / 52 | 38.7 s | 36.6 | – | 29 / 30, 35.1 s | 19 / 22, 43.5 s | 5.6 / 29.8 |
| **MTP profile** | **49 / 52** | **33.3 s** | **51.9** | **80%** | 28 / 30, 31.0 s | **21 / 22, 36.4 s** | 7.3 / 32.6 |

- **The MTP profile against itself without speculation** (the same weights,
  `n_cpu_moe 38` and server settings):
  - time per task −14%: −12% on questions, −16% on changes;
  - decode +42%;
  - one more task passed.
- **MTP against the default profile:** −10% per task.
- **The failures are the usual ones in every arm:** `fix-balance-query`
  (about half the runs since Phase 02), `multi-feature-discount`, and one
  session task each.
- **n-gram in the agent:** a turn writes little that its prompt already
  holds, so 23% of drafts were accepted and the turn paid for the
  verification.

## Routing

**Thinking per call** (`direct.routing: adaptive`). Each model call gets a
role from what the turn has done:
- `plan`: the first call of a change;
- `explore`: before the first edit;
- `edit`: after an edit;
- `debug`: after a failed command, check or edit;
- `answer`: a call of a question.

Thinking is off for `explore` and `answer`, and the model's own for the
rest. Calls by role, over two runs (reports
`20261029-direct-p9-{base,routing}-r*`):

| Role | Calls (default / adaptive) | Generated tokens (default / adaptive) |
|---|---|---|
| answer | 101 / 97 | 14,404 / **8,434** (−41%) |
| explore | 57 / 70 | 15,594 / **11,431** (−27%) |
| plan | 36 / 36 | 4,921 / 5,223 |
| edit | 57 / 68 | 7,056 / 10,812 (+53%) |
| debug | 69 / 80 | 10,337 / 16,402 (+59%) |

Without thinking while exploring, the model read more and then made more
edits and fixes. The tokens saved moved to later calls, so time per task
stayed level (38.2 s against 37.1 s). Questions all passed in both arms;
changes passed 18 of 22 against 21 of 22.

**The router's classification** (`bcode bench router`). This covers 12
requests the rules cannot classify with confidence: questions about how to
change something, and corrections stated without a verb. Each ran twice.
The labels are the author's judgement. Report:
[20261029-router](../../benchmarks/reports/20261029-router.md).

| Classification call | Model's answer used | Matches the label | Time |
|---|---|---|---|
| As before (the model's own thinking, 80 tokens) | 0 / 24 | 16 / 24 (the rules') | 2.34 s |
| Thinking off | 24 / 24 | 10 / 24 | 1.35 s |
| Adaptive: no call | – | 16 / 24 | 0 s |

## Escalation

`bcode bench escalation` replays both policies on the 135 bounded-mode
runs whose ledgers are on disk. It uses their attempts, failures and
contracts, at the points where the task runner evaluates escalation.
Replaying the rules reproduces exactly the triggers each run recorded, in
all 135. A run needed help when its hidden checks failed for a reason in
the change. Report:
[20261030-escalation](../../benchmarks/reports/20261030-escalation.md).

| Policy | Escalations | On runs that passed | Runs needing help with an escalation they could act on | By trigger |
|---|---|---|---|---|
| rules | 129 | 104 | 16 / 16 | Z1 25, Z2 7, Z3 97 |
| adaptive | **114** | **91** | 16 / 16 | Z1 11, Z2 6, Z3 97 |

The adaptive policy differs from the rules in four ways:
- **Architectural review (Z1)** waits for a failed attempt, unless the
  contract has unresolved ambiguities.
- **A failure is escalated once (Z2)**: one run had escalated the same
  failure five times.
- **No escalation once no attempt is left** to apply the advice.
- **An attempt that changed nothing escalates at once.**

The pre-merge review (Z3) is unchanged. It fires on 82 of the 118 runs that
passed, because the configured high-risk keywords (payment, ledger, …)
match most of these fixtures. No recorded signal separated the 14 false
passes from real passes: behaviour evidence, mutation status, changed
repositories and changed tests were all checked. The replay keeps each run's recorded attempts:
what advice would have changed is not known.

## Privacy

`bcode bench packets` rebuilds, for the latest recorded run of each of the
19 tasks, the escalation packet the runner would send after its last
attempt, in both data modes. It uses the task, the strategies, the
verification results and the agent's patch applied to the repositories.
The code section is absent, because the recorded runs' code index is not
kept, so full packets here understate what a full packet carries. Report:
[20261030-packets](../../benchmarks/reports/20261030-packets.md).

| `frontier.data` | Tokens per packet | Lines repeating the source | Packets with any | Source identifiers per packet | Repository paths per packet | Pass the abstract gate |
|---|---|---|---|---|---|---|
| full | 1,623 | 670 | 18 / 19 | 50.7 | 2.5 | 1 / 19 |
| abstract | **762** | **0** | 0 / 19 | 17.6 | 2.5 | 19 / 19 |

An abstract packet keeps the task, the failures and the strategies tried,
with every line that repeats the repositories withheld. The change is
reduced to files and line counts. Even so, it names a third of the
identifiers a full packet names, and every path. Error messages and the
task describe the code. Withholding source lines is a filter, not proof
that the code cannot be inferred, so the fail-closed gate only refuses a
packet that still repeats a line.

A repository that must not reach any external model says so in
`.boundedcode/data-policy.yaml`: escalation is then blocked and recorded,
and direct mode refuses a cloud provider in it (tests:
`TestDataPolicyBlocksEscalation`, `TestDataPolicy`).

## Limits

- **Scope:** one machine, one 8 GB GPU.
  - The A/B arms ran twice each; the other model sizes once.
  - The speculative micro-benchmark ran each prompt twice.
- **The model-routing decision for questions** rests on 15 questions, and
  switching was not exercised.
- **Router labels** are the author's.
- **Escalation and packets are replays.** The packets lack the code section.
- **Not measured:**
  - MTP on bounded tasks;
  - MTP with `ngram-mod` together (llama.cpp accepts a list of types);
  - gpt-oss-20b, Devstral Small 2 and Qwen3.8-27B (not downloaded).
