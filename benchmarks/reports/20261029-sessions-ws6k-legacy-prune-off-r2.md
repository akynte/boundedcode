# Direct-mode suite: direct-20261029T001158Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T00:11:58Z, finished 2026-10-29T00:35:17Z.

Settings: approval=auto, checks=true, compaction=legacy, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=false, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=6000.

**5/5 passed (100%)**, 18.8 model calls and 12.4 tool calls per task, 114138 prompt tokens per task with a 71% prompt-cache hit rate, 8323 generated tokens per task, 67 KB of tool results per task, 280 s per task, 15354 tokens in the largest single prompt; 27 results set aside, 0 recalled, 16 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 16, 773.2 s writing them, 11273 prompt tokens evaluated for them and 90981 by the first call after each. Command outputs pruned: 0, 0 KB → 0 KB. Freshness notes: 1.

Where the agent's time went, per task: 278.8 s in model calls, 0.6 s in tools (with the checks after edits), 0.10 s in the harness itself; time to first token 2034 ms (median). 5625 reasoning tokens per task. 31 prompt-cache invalidations (28 after the history was rewritten), 42931 prompt tokens recomputed; 33182 prompt tokens evaluated by the server per task, in 62.5 s. Cost of a passed task: 41505 processed tokens and 280 s; of a failed one: 0 and 0 s. Peak memory: harness 37 MiB, model server 28486 MiB, GPU 7155 MiB.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 4/4 | 0 | 19.5 | 12.8 | 115584 | 286.9 | 0.6 | 288 |
| python | 1/1 | 0 | 16.0 | 11.0 | 108355 | 246.3 | 0.4 | 247 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 69628 | 64051 | 7468 | 2034 | 3 | 0 | 0 | 72 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 16 | 11 | 108355 | 67588 | 15354 | 6993 | 6 | 0 | 0 | 247 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 18 | 15 | 108149 | 96641 | 7657 | 5224 | 3 | 0 | 0 | 170 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 15 | 9 | 89312 | 73259 | 7510 | 3613 | 4 | 0 | 1 | 134 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 34 | 18 | 195245 | 103241 | 13102 | 23753 | 11 | 0 | 0 | 776 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | summary | system | tool:bash | tool:edit | tool:glob | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | 7468 | 549 | 395 | 76 | 119 | 0 | 0 | 537 | 153 | 24 | 0 | 1791 | 0 | 141 | 2297 | 35 | 1351 |
| session-noisy-tests | 15354 | 13 | 200 | 65 | 35 | 0 | 0 | 537 | 11119 | 0 | 0 | 0 | 0 | 82 | 2297 | 41 | 965 |
| session-resume-constraints | 7657 | 608 | 810 | 76 | 183 | 145 | 0 | 537 | 153 | 24 | 14 | 184 | 31 | 356 | 2297 | 126 | 2113 |
| session-stale-config | 7510 | 276 | 1022 | 131 | 258 | 96 | 0 | 537 | 0 | 0 | 0 | 1320 | 0 | 38 | 2297 | 118 | 1417 |
| session-x-tools | 13102 | 0 | 196 | 0 | 39 | 0 | 319 | 537 | 0 | 0 | 84 | 8552 | 0 | 44 | 2297 | 72 | 962 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
