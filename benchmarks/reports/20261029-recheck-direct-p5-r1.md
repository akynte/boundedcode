# Direct-mode suite: direct-20261028T202131Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T20:21:31Z, finished 2026-10-28T20:22:02Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**1/1 passed (100%)**, 4.0 model calls and 4.0 tool calls per task, 19105 prompt tokens per task with a 73% prompt-cache hit rate, 727 generated tokens per task, 2 KB of tool results per task, 31 s per task, 5199 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 30.4 s in model calls, 0.2 s in tools (with the checks after edits), 0.02 s in the harness itself; time to first token 2073 ms (median). 340 reasoning tokens per task. 0 prompt-cache invalidations (0 after the history was rewritten), 0 prompt tokens recomputed. Cost of a passed task: 5938 processed tokens and 31 s; of a failed one: 0 and 0 s. Peak memory: harness 31 MiB, model server 28474 MiB, GPU 7141 MiB.

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| fix-balance-query | small-fix | code | pass | 4 | 4 | 19105 | 13894 | 5199 | 727 | 0 | 0 | 0 | 31 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:read | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| fix-balance-query | 5199 | 53 | 202 | 76 | 73 | 537 | 38 | 24 | 877 | 143 | 2199 | 32 | 945 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
