# Direct-mode suite: direct-20261029T011857Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T01:18:57Z, finished 2026-10-29T01:33:48Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=6000.

**4/5 passed (80%)**, 18.2 model calls and 11.8 tool calls per task, 141370 prompt tokens per task with a 73% prompt-cache hit rate, 3897 generated tokens per task, 62 KB of tool results per task, 178 s per task, 16801 tokens in the largest single prompt; 29 results set aside, 0 recalled, 16 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 16, 195.1 s writing them, 24198 prompt tokens evaluated for them and 93841 by the first call after each. Command outputs pruned: 2, 205 KB → 10 KB. Freshness notes: 1.

Where the agent's time went, per task: 177.2 s in model calls, 0.5 s in tools (with the checks after edits), 0.15 s in the harness itself; time to first token 1902 ms (median). 1231 reasoning tokens per task. 38 prompt-cache invalidations (38 after the history was rewritten), 141695 prompt tokens recomputed; 37758 prompt tokens evaluated by the server per task, in 72.5 s. Cost of a passed task: 21568 processed tokens and 125 s; of a failed one: 122000 and 389 s. Peak memory: harness 37 MiB, model server 28561 MiB, GPU 7155 MiB.

Failures by cause: model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 3/4 | 0 | 19.8 | 12.8 | 155006 | 193.9 | 0.6 | 195 |
| python | 1/1 | 0 | 12.0 | 8.0 | 86825 | 110.3 | 0.3 | 111 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | session | chat, code, code, chat | pass | 15 | 12 | 98335 | 85068 | 8051 | 3054 | 4 | 0 | 0 | 116 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 12 | 8 | 86825 | 58850 | 9530 | 2076 | 5 | 0 | 0 | 111 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 17 | 12 | 114475 | 95272 | 8503 | 5650 | 3 | 0 | 0 | 196 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 13 | 8 | 83437 | 70065 | 7685 | 1676 | 4 | 0 | 1 | 78 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | FAIL | 34 | 19 | 323778 | 208806 | 16801 | 7028 | 13 | 0 | 0 | 389 | turn 6: answer does not match /rewrite\.go/ |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:bash | tool:edit | tool:glob | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | 7427 | 582 | 597 | 76 | 251 | 206 | 537 | 267 | 48 | 0 | 184 | 0 | 244 | 2297 | 92 | 2046 |
| session-noisy-tests | 9031 | 50 | 408 | 0 | 39 | 0 | 537 | 3537 | 16 | 0 | 0 | 0 | 145 | 2297 | 948 | 1054 |
| session-resume-constraints | 7961 | 31 | 207 | 0 | 64 | 0 | 537 | 107 | 25 | 0 | 0 | 31 | 583 | 2297 | 1307 | 2772 |
| session-stale-config | 7685 | 373 | 1022 | 131 | 258 | 99 | 537 | 0 | 0 | 0 | 1320 | 0 | 38 | 2297 | 118 | 1492 |
| session-x-tools | 14649 | 238 | 391 | 0 | 190 | 49 | 537 | 0 | 0 | 21 | 8552 | 0 | 51 | 2297 | 1228 | 1095 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
