# Reference inference stack

The reference generator is **Ternary Bonsai 2 27B in PTQ1_0**, served by
PrismML's llama.cpp fork on an 8 GB NVIDIA GPU. It answers every generator role
and stays resident for the whole task. Use the
[installation guide](../how-to/install.md) to reproduce it.

| Phase | Role | Model |
|---|---|---|
| LOCALIZE, PLAN | `planning` | Bonsai 2 27B PTQ1_0 |
| EDIT | `coding` | Bonsai 2 27B PTQ1_0 |
| REVIEW | `review` | Bonsai 2 27B PTQ1_0 |

`providers.yaml` can still route a role to a second provider; see
[configure models](../how-to/configure-models.md). That is a new configuration
that needs its own measurements, not the reference.

This page distinguishes the current reference from optional experiments and
historical measurements. `bcode config init` still writes indexing-only inference
settings and the older `reference-8gb-cuda-64gb-ram` profile; selecting Bonsai
explicitly is part of setup. A fresh install does not download or select it.

## Bonsai: the resident generator

| Property | Reference |
|---|---|
| Exact artifact | `prism-ml/Ternary-Bonsai-2-27B-gguf/Ternary-Bonsai-2-27B-PTQ1_0.gguf` |
| Family | Qwen3.8-27B-derived, dense hybrid-attention language model |
| Packing | PTQ1_0, ternary group-128, approximately 1.75 deployed bits/weight |
| File | 5,946,648,928 bytes (5.95 decimal GB; about 5.54 GiB) |
| SHA-256 | `53107f530aa52eb00912263ab1ee29bd199261c87cd7b4ad4ca1318c1fe33ee3` |
| Runtime observed | PrismML-Eng/llama.cpp `1a07bfa5f4144274c8f1c9963821dd9d9a51854b` |
| Hardware profile | `bonsai-2-27b-8gb-cuda` |
| Context / slots | 32,768 tokens / 1 |
| Placement | All eligible layers requested on GPU; no CPU MoE offload |
| KV / execution | Q8_0 keys and values, flash attention, batch 2048, microbatch 512 |
| Threads | 8 generation / 16 batch |
| Sampling profile | temperature 1.0, top-p 0.95, top-k 20, min-p 0.0 |

The [model card](https://huggingface.co/prism-ml/Ternary-Bonsai-2-27B-gguf)
describes the lineage and ternary format. Local file inspection confirms the
artifact size and hash; the running server reports PTQ1_0 and a 32K context.
The language model is the only downloaded Bonsai component needed here. The
optional vision projector is not part of this text-coding configuration.

**Role.** Bonsai proposes localization and a structured plan, drives the EDIT
tool loop, and writes a fresh-context review. Review is independent of the
editor's conversation, not of its model.

**Why this model.** Its packed weights leave room for runtime state on the
target card. The reference does not stream dense model layers from host RAM
or require an MoE expert pool. This is a memory/placement rationale, not
evidence that Bonsai is the strongest coding model at this budget.

**Deterministic support.** Index lookups, exact grants, plan validation, bounded
tool output, persisted evidence and executable checks reduce what each model
call must discover or remember. Structured phase output uses JSON Schema through
the inference client; Jev is not responsible for this output contract.

**Residency.** In the external-server setup it loads once, remains resident for
every phase, and the operator stops it. Embedded and managed-provider modes have
separate [lifecycle rules](../explanation/8gb-runtime.md).

**Measured resources.** The September 21 synthetic run observed 27.7 tokens/s
generation and at most 7,691 MiB in whole-device samples after requests.
That includes other GPU users. No continuous task peak or complete process-tree
RAM requirement was measured. See the
[measurement record](../benchmarks/results/2026-09-21-bonsai-runtime.md).

## Why EDIT runs on Bonsai

**What was observed.** EDIT was once routed to a second, coding-tuned model. In a
seeded replay of `SWEBENCH-GIN-1805` (2026-09-20 and 2026-09-21), Bonsai made
59–67 tool calls, all reads and searches, never edited, and was stopped by the
loop guard, while the other model edited and drove verification. Neither solved
the task. That was read as Bonsai being unable to drive a tool loop.

**What it was.** A defect in BoundedCode, found on 2026-09-22 and 2026-09-23 by
recording every request between the harness and the model:

- **EDIT received no code.** EDIT's packet is built from the plan's symbols, and
  plans name them the way people do — `Reconciler.RunOnce`, `pricing.Calculator`,
  `path::Symbol`. The graph stores short names and the lookup was exact, so
  nothing matched and the prompt had no retrieved code at all. Plan validation
  accepted the same names by their last component, so the two layers disagreed
  and nothing flagged it. A model given no code has to rediscover the repository
  with tools; a coding-tuned model starts editing after a few reads, Bonsai kept
  exploring. On the recorded replay the seeded bodies went to PLAN only, so the
  "re-reads of code it had already been given" were reads of code EDIT had never
  been shown.
- **EDIT was told to fix a plan.** A plan correction was left in the feedback
  and reached EDIT as "verification findings from the previous attempt":
  "the previous plan was rejected … return the plan again".

Given code, the same Bonsai edits promptly. Called directly on small bug-fix
repositories with four tools, it fixed and verified 11 of 11, never repeating a
call; handed a plan and the relevant code, it made the edit on its first or
second step.

**What else the recordings found.** Once EDIT had code, other harness limits
showed, and were fixed in the same change:

- EDIT: plans that left `symbols` empty or listed file paths in it still got no
  code (the packet now falls back to the declarations of the files the plan
  writes); the verification budget counted each check kind as a round, so
  build/vet/test of one edit used it all; a tool call cut off at the output limit
  crashed the task on save, and a reply cut off before its tool call ended the
  attempt; a failed `gofmt` check named only the file, so the model re-ran it
  until the loop guard stopped it (it now shows the lines gofmt would change).
- LOCALIZE: every prompt said "0 remaining" for an uncapped task; a guessed file
  path crashed the phase; Go declarations had no end line, so the declarations
  it selected were never shown.
- PLAN: validation refused new files, new tests, new fields and methods named
  `Receiver.Method`; it reported one problem per correction, did not say why an
  obligation the plan had written did not count, and did not restate the scope;
  the call the wall-clock budget interrupted was fed back as a rejected plan; and
  it asked for a written disposition for every consumer of anything the plan
  touched. Obligations are now the direct callers of what the plan changes; a
  breaking change to a type or signature is still enforced after EDIT by the
  exported-signature check.
- The benchmark runner gave the required Jev judge only to its reranking arm, so
  every other supervised arm stopped before its first model call.

**Measured after the fix.** Bonsai for every role, the reference runtime and
profile, the ten-task `evals/tasks` dev set, one run per task per build:

| | Before | After |
|---|---|---|
| EDIT prompt contains retrieved code | never | 4 of 4 tasks that reached EDIT in the last run² |
| `reconcile-steals` EDIT (before) / `nil-deref` EDIT (after) | 17 steps, 0 edits, context reset | 5 steps: read, fix and test, build ✓, test ✓, `done` |
| Tasks solved (hidden acceptance tests) | 0 of the 4 that could run¹ | 2 of 10 in one run (`nil-deref`, `signature-change`), 0 of 10 in the next; 1 of 4 in the last² |

¹ Before the fix `bcode eval run` could not run a supervised task at all: the
solver gave the Jev judge only to the reranking arm, and every other arm stopped
with `JEV_NOT_CONFIGURED`. The "before" column is the old harness with that one
defect fixed.

² A rerun of `restock-silence`, `signature-change`, `nil-deref` and
`config-rename` after the EDIT fixes, before the `gofmt` hint and the PLAN
deadline fix. Earlier runs missed code for plans whose `symbols` were empty or
held file paths; that is what the plan-file fallback fixed.

**What this does not show.** Ten tasks run once per build support no rate, and
consecutive full runs, on builds a few fixes apart, differ by two solves. The unsolved tasks now fail
for reasons other than the tool loop: four tasks have 600 s budgets and Bonsai
spends about 450 s in LOCALIZE and PLAN on this card, so they finish only when
nothing is retried (`nil-deref` solved at 597 s once and timed out in REVIEW
the next time); three failed PLAN validation within two corrections — a
rewritten plan dropped an obligation an earlier round had satisfied, named an
out-of-scope file again, or named a target that does not exist; one
(`damaged-stock`) is stopped at intake by Jev's schema-change site; one
(`discount-floor`) reached EDIT, edited and verified, and did not make its tests
pass in 20 steps; one (`signature-change`) passed verification and was rejected
by its own review on substantive findings, and the rework ran out of time — it
was solved in the runs before and after. In the last run `restock-silence`
reached a candidate whose only failing check was `gofmt`, and `config-rename`
was still editing when its 600 s budget ended. Whether a second model
would edit better once both receive code has not been measured.

## Jev: the required typed decision plane

The TypeSafe integration is named **Jev**, pinned to `jev-1.13.0`, at
`https://api.typesafe.ai/v1/systemone`. It is a hosted System One model, not a
local GGUF. TypeSafe announced it on September 15, 2026.
[Vendor announcement](https://typesafe.ai/blog/introducing-system-one-models-and-jev).

Its job is to answer narrow questions about bounded state: relevance of a
retrieval candidate, plausibility of a plan waiver, possible test weakening,
failure category, or signs of a stalled attempt. It returns probability-shaped
`noul`, `choice`, or `score` answers; it does not generate code.

Why use this shape: lexical scores, graph depth and explicit context have
different units. An optional relevance question supplies a common semantic
signal without asking the coding model for another prose judgment. Other sites
add bounded findings to deterministic fallbacks.

There is **no local weight, quantization, VRAM or model-RAM allocation** for Jev.
Its parameter count and serving hardware are not specified by this repository.
Network latency and API access replace local inference costs; whether this saves
total task time, tokens or failures remains unproven here.

It is a required runtime component: a task run refuses to start without a
usable one. All eleven registered sites default to `logged`; explicit tiers can
permit ordering or bounded routing. Retrieval and localization ranking predate those
tiers. None can fabricate test evidence, expand write authority, or override
the deterministic completion checks.
[Integration and authority](../explanation/judgments.md) ·
[Setup and disclosure](../how-to/use-judgments.md).

## Qwen embedding control: experimental, CPU-only

A service observed on the reference machine uses
`Qwen3-Embedding-0.6B-Q8_0.gguf` (639,150,592 bytes), served by the same Prism
runtime with:

```text
--embeddings --pooling last -c 8192 -ngl 0 -t 4
--host 127.0.0.1 --port 8081
```

The server reports 595,776,512 parameters and 1,024 embedding dimensions.
The [Qwen model card](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B)
describes its embedding role. It produces vectors for objective/candidate cosine
similarity in `supervised-rerank-local`, providing a local experimental control
for judged reranking.

This small model keeps that experiment on CPU; it need not compete with Bonsai
for VRAM. Its process was resident when inspected (about 1.53 GiB RSS at that
instant, not a peak). No task-quality advantage has been established.

**Ordinary `bcode task run` does not use an embedding index or this service.**
The reference provider configuration routes `embedding` to it, but no task
phase asks for an embedding; the code exposes the control in the evaluation
pipeline, and an existing process on port 8081 is not proof that a task uses it.
It is not required for onboarding. Unlike the two generators it is not managed:
it runs with `-ngl 0`, so it costs them no VRAM and never needs to be swapped
out of the process slot mid-task.

## Historical and evaluation-only models

Earlier local measurements used **Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf**, a sparse
35B/3B-active MoE with expert tensors on CPU via `--n-cpu-moe 999`.
Its RAM/VRAM split, cache measurements, and task results are
[historical evidence](../benchmarks/results/README.md), not results for the
current generator.

The committed Evidence Suite v1 runtime names the hosted generator
`claude-sonnet-5`. It is an evaluation control configuration, not the 8 GB
reference and not evidence of local task performance.
[Suite scope](../evidence/evidence-v1.md).

Advanced provider configuration exists, but changing the generator, runtime,
packing or role routing creates a new configuration that needs its own
conformance and task evidence.
