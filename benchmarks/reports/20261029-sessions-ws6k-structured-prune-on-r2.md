# Direct-mode suite: direct-20261029T005131Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T00:51:31Z, finished 2026-10-29T01:06:21Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=6000.

**5/5 passed (100%)**, 18.0 model calls and 11.6 tool calls per task, 137557 prompt tokens per task with a 70% prompt-cache hit rate, 3714 generated tokens per task, 56 KB of tool results per task, 178 s per task, 14801 tokens in the largest single prompt; 28 results set aside, 0 recalled, 16 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 16, 252.3 s writing them, 57799 prompt tokens evaluated for them and 91064 by the first call after each. Command outputs pruned: 3, 184 KB → 20 KB. Freshness notes: 1.

Where the agent's time went, per task: 177.1 s in model calls, 0.5 s in tools (with the checks after edits), 0.16 s in the harness itself; time to first token 1942 ms (median). 857 reasoning tokens per task. 42 prompt-cache invalidations (42 after the history was rewritten), 162895 prompt tokens recomputed; 41456 prompt tokens evaluated by the server per task, in 77.4 s. Cost of a passed task: 45171 processed tokens and 178 s; of a failed one: 0 and 0 s. Peak memory: harness 37 MiB, model server 28468 MiB, GPU 7155 MiB.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 4/4 | 0 | 19.8 | 12.8 | 151722 | 194.5 | 0.5 | 196 |
| python | 1/1 | 0 | 11.0 | 7.0 | 80893 | 107.4 | 0.2 | 108 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | session | chat, code, code, chat | pass | 13 | 11 | 82697 | 74130 | 7564 | 2532 | 3 | 0 | 0 | 93 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 11 | 7 | 80893 | 54551 | 9151 | 2248 | 5 | 0 | 0 | 108 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 16 | 13 | 104425 | 92416 | 7942 | 3670 | 4 | 0 | 0 | 133 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 74884 | 61865 | 7550 | 1438 | 4 | 0 | 0 | 71 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 38 | 20 | 344884 | 197539 | 14801 | 8684 | 12 | 0 | 0 | 484 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:bash | tool:edit | tool:glob | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | 7564 | 538 | 798 | 76 | 183 | 153 | 537 | 223 | 24 | 14 | 184 | 31 | 508 | 2297 | 110 | 1888 |
| session-noisy-tests | 8672 | 21 | 200 | 65 | 35 | 0 | 537 | 4322 | 0 | 0 | 0 | 0 | 181 | 2297 | 41 | 973 |
| session-resume-constraints | 7788 | 114 | 408 | 0 | 64 | 0 | 537 | 111 | 49 | 0 | 688 | 31 | 823 | 2297 | 1171 | 1495 |
| session-stale-config | 7550 | 358 | 1022 | 131 | 258 | 100 | 537 | 0 | 0 | 0 | 1320 | 0 | 38 | 2297 | 118 | 1371 |
| session-x-tools | 14571 | 190 | 391 | 0 | 190 | 52 | 537 | 0 | 0 | 21 | 8552 | 0 | 51 | 2297 | 1246 | 1044 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
