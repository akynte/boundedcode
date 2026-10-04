# Serena A/B: existing stack vs existing stack + Serena (ADR-0008)

Model `qwen3.6-35b-a3b` (local, llama.cpp), sandboxed OpenHands agent,
identical tasks, `bench tasks --serena on|off`, arms interleaved
(on r1, off r1, on r2, …), 3 repetitions each, 2026-10-04 03:59–06:27Z,
machine otherwise idle. Raw reports: `*-ab-serena-{on,off}-r{1,2,3}.{json,md}`.

| task | arm | verified | attempts (mean) | wall s (per run) | processed tokens (per run) | agent tool calls (per run) | code tokens / pack | Serena calls / ms per task |
|---|---|---|---|---|---|---|---|---|
| go-iface-add-method (fixture, interface + 2 impls + decoy) | off | 3/3 | 1.0 | 192, 206, 255 | 22.7k, 23.3k, 27.3k | 20, 19, 28 | 236 | – |
| | on | 3/3 | 1.0 | 187, 169, 162 | 20.7k, 18.2k, 18.1k | 17, 13, 14 | 391 | 6 / 4.3 s |
| go-large-iface-method (Temporal SDK, 285 files, 5 impls + DataConverter decoys) | off | 3/3 | 2.0 | 954, 1419, 892 | 144k, 185k, 140k | 84, 130, 79 | 1552 | – |
| | on | 3/3 | 2.0 | 591, 1805, 610 | 76k, 290k, 76k | 51, 148, 51 | 3791 | 35 / 12.9 s |
| event-field-rename (3 repositories, cross-service) | off | 3/3 | 1.67 | 264, 249, 228 | 24.1k, 25.2k, 19.8k | 20, 21, 16 | 227 | – |
| | on | 3/3 | 1.67 | 204, 248, 270 | 20.8k, 22.1k, 22.8k | 15, 17, 19 | 514 | 8 / 8.2 s |

Findings:

* **Task success: no difference** (9/9 vs 9/9, no escalations). Every task
  is within the local model's reach without Serena, so success cannot
  improve here.
* **Interface change, small repository:** with Serena, every run used
  fewer tokens (−22 % mean) and fewer agent tool calls (14.7 vs 22.3); wall
  time 173 s vs 218 s.
* **Interface change, large repository:** 2 of 3 Serena runs took ~600 s
  and 76k tokens against 892–1419 s and 140–185k without; the third Serena
  run (1805 s, 290k) failed its first attempt on `gofmt` only and then spent
  20 minutes on the fix, which is unrelated to repository context. Medians
  favour Serena (610 s vs 954 s, 76k vs 144k tokens); means are equal
  within noise (1002 s vs 1089 s). n=3 is too small to claim a significant
  improvement.
* **Multi-repository task:** no measurable difference.
* **Source tokens injected:** Serena *increases* the code placed in the
  pack (bodies, implementation and reference pointers: 391 vs 236 tokens
  per pack, 3.8k vs 1.6k on the large repository) while the agent reads
  less on its own. The hypothesis that Serena reduces injected source
  tokens is not confirmed; it moves exploration from the agent into the
  pack.
* **Incorrect-file edits:** none in either arm (the decoy-file checks
  passed in all 18 runs).
* **Serena cost per task:** 6–35 LSP calls, 4–13 s inside context
  planning (including instance start), plus resident memory while the
  task runs (0.3–1.6 GiB per instance, see the overlap study).

Verdict for ADR-0008: **B — useful but optional.** Retrieval quality is
measurably better (overlap study) and agent effort trends lower on
interface work, but task success did not change and the RAM cost competes
with MoE expert offload on the reference laptop. `repointel.serena.enabled`
stays `false` by default.
