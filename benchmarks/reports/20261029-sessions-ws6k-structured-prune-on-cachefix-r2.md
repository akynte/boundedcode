# Direct-mode suite: direct-20261029T013348Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T01:33:48Z, finished 2026-10-29T01:47:10Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=6000.

**5/5 passed (100%)**, 18.0 model calls and 12.8 tool calls per task, 133968 prompt tokens per task with a 76% prompt-cache hit rate, 3571 generated tokens per task, 63 KB of tool results per task, 160 s per task, 17073 tokens in the largest single prompt; 28 results set aside, 0 recalled, 12 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 12, 173.9 s writing them, 24801 prompt tokens evaluated for them and 64269 by the first call after each. Command outputs pruned: 2, 205 KB → 10 KB. Freshness notes: 1.

Where the agent's time went, per task: 159.4 s in model calls, 0.6 s in tools (with the checks after edits), 0.16 s in the harness itself; time to first token 1908 ms (median). 858 reasoning tokens per task. 34 prompt-cache invalidations (34 after the history was rewritten), 110711 prompt tokens recomputed; 32181 prompt tokens evaluated by the server per task, in 63.6 s. Cost of a passed task: 35752 processed tokens and 160 s; of a failed one: 0 and 0 s. Peak memory: harness 37 MiB, model server 28315 MiB, GPU 7155 MiB.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 4/4 | 0 | 19.2 | 13.5 | 144386 | 173.4 | 0.6 | 174 |
| python | 1/1 | 0 | 13.0 | 10.0 | 92297 | 103.3 | 0.5 | 104 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | session | chat, code, code, chat | pass | 15 | 12 | 98451 | 88865 | 8269 | 2992 | 3 | 0 | 0 | 109 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 13 | 10 | 92297 | 67952 | 10318 | 2150 | 5 | 0 | 0 | 104 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 19 | 15 | 124212 | 109626 | 8098 | 4626 | 3 | 0 | 0 | 164 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 13 | 8 | 81653 | 68684 | 7481 | 1436 | 4 | 0 | 1 | 72 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 30 | 19 | 273228 | 173807 | 17073 | 6650 | 13 | 0 | 0 | 353 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:bash | tool:edit | tool:glob | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | 7734 | 573 | 597 | 76 | 183 | 149 | 537 | 267 | 49 | 0 | 184 | 31 | 699 | 2297 | 92 | 2000 |
| session-noisy-tests | 9657 | 0 | 205 | 0 | 39 | 0 | 537 | 4315 | 16 | 0 | 0 | 0 | 145 | 2297 | 1053 | 1050 |
| session-resume-constraints | 7491 | 20 | 207 | 0 | 64 | 0 | 537 | 108 | 50 | 0 | 184 | 31 | 1166 | 2297 | 1190 | 1637 |
| session-stale-config | 7481 | 350 | 1022 | 131 | 258 | 98 | 537 | 0 | 0 | 0 | 1320 | 0 | 38 | 2297 | 118 | 1312 |
| session-x-tools | 14627 | 166 | 391 | 0 | 190 | 51 | 537 | 0 | 0 | 21 | 8552 | 0 | 51 | 2297 | 1307 | 1064 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
