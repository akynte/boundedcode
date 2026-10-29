# Direct-mode suite: direct-20261028T235757Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T23:57:57Z, finished 2026-10-29T00:11:58Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=6000.

**5/5 passed (100%)**, 17.6 model calls and 11.8 tool calls per task, 132068 prompt tokens per task with a 71% prompt-cache hit rate, 3548 generated tokens per task, 61 KB of tool results per task, 168 s per task, 14752 tokens in the largest single prompt; 26 results set aside, 0 recalled, 14 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 14, 228.9 s writing them, 50554 prompt tokens evaluated for them and 80908 by the first call after each. Command outputs pruned: 2, 205 KB → 10 KB. Freshness notes: 1.

Where the agent's time went, per task: 167.3 s in model calls, 0.5 s in tools (with the checks after edits), 0.17 s in the harness itself; time to first token 1820 ms (median). 936 reasoning tokens per task. 38 prompt-cache invalidations (38 after the history was rewritten), 149516 prompt tokens recomputed; 38269 prompt tokens evaluated by the server per task, in 71.9 s. Cost of a passed task: 41818 processed tokens and 168 s; of a failed one: 0 and 0 s. Peak memory: harness 37 MiB, model server 28073 MiB, GPU 7155 MiB.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 4/4 | 0 | 19.8 | 13.0 | 151044 | 195.3 | 0.5 | 196 |
| python | 1/1 | 0 | 9.0 | 7.0 | 56165 | 55.6 | 0.2 | 56 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | session | chat, code, code, chat | pass | 16 | 13 | 101816 | 90451 | 8198 | 3334 | 3 | 0 | 0 | 121 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 9 | 7 | 56165 | 45758 | 7862 | 1207 | 4 | 0 | 0 | 56 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 16 | 13 | 105358 | 90907 | 8578 | 3423 | 4 | 0 | 0 | 130 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 74302 | 63372 | 7348 | 1207 | 3 | 0 | 0 | 61 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 35 | 19 | 322701 | 178507 | 14752 | 8571 | 12 | 0 | 0 | 472 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:bash | tool:edit | tool:glob | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | 7418 | 87 | 404 | 0 | 145 | 0 | 537 | 116 | 25 | 0 | 688 | 31 | 571 | 2297 | 997 | 1520 |
| session-noisy-tests | 7862 | 271 | 404 | 65 | 74 | 149 | 537 | 2402 | 16 | 0 | 0 | 0 | 216 | 2297 | 60 | 1371 |
| session-resume-constraints | 7970 | 689 | 604 | 76 | 119 | 152 | 537 | 230 | 24 | 14 | 872 | 0 | 156 | 2297 | 81 | 2119 |
| session-stale-config | 7348 | 251 | 1022 | 131 | 258 | 95 | 537 | 0 | 0 | 0 | 1320 | 0 | 38 | 2297 | 118 | 1281 |
| session-x-tools | 14697 | 181 | 391 | 0 | 190 | 50 | 537 | 0 | 0 | 21 | 8552 | 0 | 51 | 2297 | 1357 | 1070 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
