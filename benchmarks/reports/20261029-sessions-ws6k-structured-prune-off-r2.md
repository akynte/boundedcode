# Direct-mode suite: direct-20261029T003517Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T00:35:17Z, finished 2026-10-29T00:51:31Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=false, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=6000.

**4/5 passed (80%)**, 20.2 model calls and 13.4 tool calls per task, 156900 prompt tokens per task with a 70% prompt-cache hit rate, 4050 generated tokens per task, 61 KB of tool results per task, 195 s per task, 14940 tokens in the largest single prompt; 30 results set aside, 1 recalled, 19 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 19, 294.3 s writing them, 66245 prompt tokens evaluated for them and 108570 by the first call after each. Command outputs pruned: 0, 0 KB → 0 KB. Freshness notes: 1.

Where the agent's time went, per task: 193.9 s in model calls, 0.5 s in tools (with the checks after edits), 0.14 s in the harness itself; time to first token 1869 ms (median). 1106 reasoning tokens per task. 45 prompt-cache invalidations (45 after the history was rewritten), 180491 prompt tokens recomputed; 46307 prompt tokens evaluated by the server per task, in 84.7 s. Cost of a passed task: 22542 processed tokens and 120 s; of a failed one: 161618 and 492 s. Peak memory: harness 36 MiB, model server 28456 MiB, GPU 7155 MiB.

Failures by cause: model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 3/4 | 0 | 22.2 | 14.8 | 171139 | 216.4 | 0.6 | 217 |
| python | 1/1 | 0 | 12.0 | 8.0 | 99943 | 103.8 | 0.2 | 104 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | session | chat, code, code, chat | pass | 16 | 14 | 104765 | 92017 | 8276 | 3784 | 4 | 0 | 0 | 134 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 12 | 8 | 99943 | 66076 | 11904 | 1809 | 5 | 1 | 0 | 104 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 22 | 17 | 150724 | 129766 | 8451 | 4827 | 4 | 0 | 1 | 183 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 74587 | 63616 | 7399 | 1202 | 3 | 0 | 0 | 61 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | FAIL | 39 | 21 | 354479 | 201489 | 14940 | 8628 | 14 | 0 | 0 | 492 | …t ./go/ast/inspector/: --- FAIL: TestHiddenLen (0.00s)     hidden_len_test.go:24: Len() = 66, want 33 FAIL FAIL	golang.org/x/tools/go/ast/inspector	0.119s FAIL  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:bash | tool:edit | tool:glob | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| session-ledger | 7721 | 470 | 395 | 76 | 142 | 101 | 537 | 191 | 24 | 14 | 1095 | 0 | 176 | 2297 | 35 | 2168 |
| session-noisy-tests | 11181 | 316 | 205 | 0 | 0 | 0 | 537 | 5520 | 16 | 0 | 0 | 0 | 96 | 2297 | 986 | 1208 |
| session-resume-constraints | 7806 | 33 | 207 | 0 | 62 | 0 | 537 | 129 | 50 | 0 | 872 | 77 | 611 | 2297 | 1295 | 1636 |
| session-stale-config | 7399 | 268 | 1022 | 131 | 258 | 100 | 537 | 0 | 0 | 0 | 1320 | 0 | 38 | 2297 | 118 | 1310 |
| session-x-tools | 14763 | 221 | 391 | 0 | 190 | 48 | 537 | 0 | 0 | 21 | 8552 | 0 | 50 | 2297 | 1262 | 1194 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
