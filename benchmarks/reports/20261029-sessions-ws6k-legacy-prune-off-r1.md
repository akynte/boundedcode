# Direct-mode suite: direct-20261028T230104Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T23:01:04Z, finished 2026-10-28T23:43:42Z.

Settings: approval=auto, checks=true, compaction=legacy, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=false, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=6000.

**2/5 passed (40%)**, 25.8 model calls and 16.6 tool calls per task, 171911 prompt tokens per task with a 52% prompt-cache hit rate, 14494 generated tokens per task, 66 KB of tool results per task, 512 s per task, 20882 tokens in the largest single prompt; 37 results set aside, 2 recalled, 33 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 33, 1292.4 s writing them, 17630 prompt tokens evaluated for them and 320011 by the first call after each. Command outputs pruned: 0, 0 KB → 0 KB. Freshness notes: 1.

Where the agent's time went, per task: 510.5 s in model calls, 0.7 s in tools (with the checks after edits), 0.12 s in the harness itself; time to first token 1806 ms (median). 11201 reasoning tokens per task. 56 prompt-cache invalidations (54 after the history was rewritten), 61334 prompt tokens recomputed; 82149 prompt tokens evaluated by the server per task, in 132.8 s. Cost of a passed task: 10998 processed tokens and 72 s; of a failed one: 153739 and 804 s. Peak memory: harness 40 MiB, model server 27799 MiB, GPU 7155 MiB.

Failures by cause: model 3. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 2/4 | 0 | 28.8 | 18.2 | 191302 | 576.8 | 0.8 | 578 |
| python | 0/1 | 0 | 14.0 | 10.0 | 94346 | 245.3 | 0.3 | 246 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 70566 | 64938 | 7519 | 2316 | 3 | 0 | 0 | 79 | switched to code. |
| session-noisy-tests | session | chat, code, chat | FAIL | 14 | 10 | 94346 | 53628 | 15417 | 6970 | 5 | 0 | 0 | 246 | switched to code. turn 3: answer does not match /OK\|no failures\|all (?:tests )?pass\|none failed\|0 fail/ |
| session-resume-constraints | session | code, chat, code, code | FAIL | 15 | 12 | 90971 | 78223 | 7893 | 4662 | 4 | 1 | 0 | 155 | turn 4: answer does not match /API\|exported/ |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 13 | 8 | 81073 | 68219 | 7342 | 1198 | 4 | 0 | 1 | 65 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | FAIL | 76 | 44 | 522600 | 183804 | 20882 | 57324 | 21 | 1 | 1 | 2013 | turn 1: answer does not match /event/; turn 1: answer does not match /inspector\.go/ |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | summary | system | tool:bash | tool:edit | tool:glob | tool:read | tool:recall | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | 7519 | 467 | 395 | 76 | 119 | 0 | 0 | 537 | 153 | 24 | 0 | 1791 | 0 | 141 | 2297 | 35 | 1484 |
| session-noisy-tests | 15417 | 13 | 200 | 65 | 35 | 0 | 0 | 537 | 11077 | 0 | 0 | 0 | 0 | 164 | 2297 | 41 | 988 |
| session-resume-constraints | 7893 | 726 | 604 | 76 | 119 | 149 | 0 | 537 | 115 | 59 | 14 | 184 | 696 | 319 | 2297 | 81 | 1917 |
| session-stale-config | 7342 | 256 | 1022 | 131 | 258 | 103 | 0 | 537 | 0 | 0 | 0 | 1320 | 0 | 38 | 2297 | 118 | 1262 |
| session-x-tools | 20882 | 281 | 196 | 0 | 39 | 405 | 79 | 537 | 484 | 251 | 84 | 3160 | 0 | 3608 | 2297 | 72 | 9389 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
