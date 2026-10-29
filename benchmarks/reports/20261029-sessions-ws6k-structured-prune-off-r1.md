# Direct-mode suite: direct-20261028T234342Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T23:43:42Z, finished 2026-10-28T23:57:57Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=false, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=6000.

**5/5 passed (100%)**, 17.6 model calls and 11.4 tool calls per task, 137652 prompt tokens per task with a 68% prompt-cache hit rate, 3448 generated tokens per task, 70 KB of tool results per task, 171 s per task, 15430 tokens in the largest single prompt; 24 results set aside, 0 recalled, 15 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 15, 225.7 s writing them, 55242 prompt tokens evaluated for them and 91476 by the first call after each. Command outputs pruned: 0, 0 KB → 0 KB. Freshness notes: 1.

Where the agent's time went, per task: 170.1 s in model calls, 0.5 s in tools (with the checks after edits), 0.14 s in the harness itself; time to first token 1801 ms (median). 874 reasoning tokens per task. 41 prompt-cache invalidations (41 after the history was rewritten), 160235 prompt tokens recomputed; 43370 prompt tokens evaluated by the server per task, in 77.6 s. Cost of a passed task: 46818 processed tokens and 171 s; of a failed one: 0 and 0 s. Peak memory: harness 36 MiB, model server 28434 MiB, GPU 7155 MiB.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 4/4 | 0 | 17.2 | 11.2 | 129044 | 163.8 | 0.5 | 165 |
| python | 1/1 | 0 | 19.0 | 12.0 | 172086 | 195.5 | 0.5 | 196 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 63785 | 55400 | 7384 | 2101 | 3 | 0 | 0 | 79 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 19 | 12 | 172086 | 99466 | 15430 | 3121 | 6 | 0 | 0 | 196 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 11 | 92866 | 79992 | 8039 | 3168 | 3 | 0 | 0 | 119 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 74376 | 63039 | 7397 | 1248 | 3 | 0 | 0 | 63 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 33 | 18 | 285149 | 173513 | 14688 | 7602 | 9 | 0 | 0 | 397 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:bash | tool:edit | tool:glob | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | 7384 | 496 | 798 | 76 | 183 | 153 | 537 | 188 | 24 | 0 | 184 | 31 | 430 | 2297 | 110 | 1877 |
| session-noisy-tests | 15430 | 13 | 200 | 65 | 35 | 0 | 537 | 11115 | 0 | 0 | 0 | 0 | 171 | 2297 | 41 | 956 |
| session-resume-constraints | 7713 | 647 | 810 | 76 | 206 | 154 | 537 | 153 | 24 | 0 | 184 | 31 | 350 | 2297 | 126 | 2118 |
| session-stale-config | 7397 | 271 | 1022 | 131 | 258 | 101 | 537 | 0 | 0 | 0 | 1320 | 0 | 38 | 2297 | 118 | 1304 |
| session-x-tools | 14333 | 153 | 391 | 0 | 190 | 49 | 537 | 0 | 0 | 21 | 8552 | 0 | 51 | 2297 | 987 | 1105 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
