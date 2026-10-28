# Direct-mode suite: direct-20261028T123157Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T12:31:57Z, finished 2026-10-28T12:46:04Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**23/23 passed (100%)**, 5.2 model calls and 5.4 tool calls per task, 38348 prompt tokens per task with a 91% prompt-cache hit rate, 985 generated tokens per task, 6 KB of tool results per task, 37 s per task, 27903 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 36.4 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1848 ms (median). 327 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 748 prompt tokens recomputed. Cost of a passed task: 4573 processed tokens and 37 s; of a failed one: 0 and 0 s. Peak memory: harness 37 MiB, model server 28601 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2. Stalls: empty_answer 1.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/15 | 0 | 5.8 | 5.5 | 47749 | 42.2 | 0.2 | 43 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8348 | 6.4 | 0.0 | 6 |
| multi | 2/2 | 0 | 6.0 | 11.0 | 38101 | 56.7 | 0.3 | 57 |
| python | 2/2 | 0 | 3.0 | 3.0 | 12982 | 15.9 | 0.0 | 16 |
| rust | 1/1 | 1 | 6.0 | 9.0 | 28267 | 29.2 | 0.0 | 29 |
| typescript | 2/2 | 0 | 3.0 | 2.5 | 13494 | 11.8 | 0.1 | 12 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 4.0 | 4.0 | 18886 | 91% | 1212 | 38 |
| diagnose | 2/2 | 3.5 | 3.0 | 16618 | 89% | 897 | 29 |
| explain | 5/5 | 2.8 | 3.0 | 12654 | 89% | 401 | 15 |
| feature | 2/2 | 8.0 | 12.0 | 51110 | 92% | 2246 | 73 |
| small-fix | 4/4 | 4.2 | 4.8 | 20372 | 92% | 592 | 23 |
| large-change | 1/1 | 7.0 | 8.0 | 66923 | 86% | 1451 | 57 |
| large-locate | 1/1 | 3.0 | 2.0 | 17598 | 69% | 281 | 18 |
| locate | 3/3 | 2.0 | 2.0 | 8675 | 86% | 213 | 9 |
| refactor | 2/2 | 7.0 | 8.5 | 39451 | 92% | 948 | 37 |
| session | 2/2 | 15.5 | 12.0 | 196728 | 92% | 3260 | 129 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 4 | 4 | 18886 | 17218 | 5187 | 1212 | 0 | 0 | 0 | 38 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8380 | 7519 | 4388 | 225 | 0 | 0 | 0 | 9 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 5 | 24855 | 21926 | 6066 | 1569 | 0 | 0 | 0 | 49 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 9784 | 7547 | 5764 | 501 | 0 | 0 | 0 | 17 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8432 | 7412 | 4547 | 263 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 6 | 6 | 35037 | 31809 | 6739 | 1286 | 0 | 0 | 1 | 45 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 18934 | 17340 | 5113 | 695 | 0 | 0 | 0 | 25 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 7 | 26355 | 23800 | 6070 | 841 | 0 | 0 | 0 | 31 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8348 | 7408 | 4467 | 115 | 0 | 0 | 0 | 6 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 7 | 8 | 66923 | 57610 | 12820 | 1451 | 0 | 0 | 0 | 57 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 17598 | 12171 | 8950 | 281 | 0 | 0 | 0 | 18 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8689 | 7498 | 4718 | 136 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | pass | 10 | 18 | 67183 | 62021 | 8657 | 3205 | 0 | 0 | 0 | 101 |  |
| multi-locate-mapping | locate | chat | pass | 2 | 4 | 9019 | 7494 | 5052 | 337 | 0 | 0 | 0 | 13 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8437 | 7421 | 4543 | 357 | 0 | 0 | 0 | 13 |  |
| py-fix-inventory | small-fix | code | pass | 4 | 4 | 17528 | 16362 | 4685 | 501 | 0 | 0 | 0 | 19 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 33499 | 30573 | 6437 | 870 | 0 | 0 | 1 | 34 |  |
| rename-entries | refactor | code | pass | 8 | 11 | 45403 | 41887 | 7019 | 1025 | 0 | 0 | 0 | 40 |  |
| rust-explain-ledger | explain | chat | pass | 6 | 9 | 28267 | 26209 | 5569 | 770 | 0 | 0 | 0 | 29 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 78299 | 73034 | 8390 | 2603 | 0 | 0 | 0 | 92 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 14 | 315156 | 290712 | 27903 | 3917 | 0 | 0 | 0 | 167 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 18670 | 17089 | 5100 | 330 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 1 | 8318 | 7419 | 4426 | 165 | 0 | 0 | 0 | 8 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:multi_edit | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5187 | 0 | 200 | 76 | 35 | 537 | 38 | 0 | 0 | 4 | 0 | 0 | 0 | 688 | 0 | 31 | 382 | 2199 | 65 | 932 |
| diagnose-config | 4388 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2199 | 29 | 841 |
| diagnose-failing-test | 6066 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 70 | 2199 | 44 | 973 |
| explain-event-flow | 5764 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2199 | 33 | 861 |
| explain-ledger-posting | 4547 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 6739 | 23 | 198 | 131 | 103 | 537 | 81 | 98 | 0 | 0 | 0 | 0 | 0 | 1565 | 0 | 0 | 734 | 2199 | 55 | 1015 |
| fix-balance-query | 5113 | 0 | 202 | 76 | 73 | 537 | 38 | 24 | 0 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 108 | 2199 | 32 | 947 |
| fix-ledger-balance | 6070 | 0 | 204 | 76 | 39 | 537 | 37 | 24 | 0 | 7 | 0 | 0 | 0 | 1622 | 0 | 0 | 247 | 2199 | 52 | 1026 |
| java-explain-orders | 4467 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 12820 | 6 | 200 | 286 | 153 | 537 | 18 | 141 | 0 | 0 | 0 | 0 | 0 | 7381 | 0 | 0 | 761 | 2199 | 79 | 1059 |
| large-locate-preorder | 8950 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 46 | 0 | 0 | 0 | 0 | 4526 | 0 | 0 | 32 | 2199 | 42 | 884 |
| locate-topic-name | 4718 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 10 | 2199 | 25 | 841 |
| multi-feature-discount | 8657 | 90 | 202 | 113 | 68 | 537 | 161 | 18 | 0 | 11 | 0 | 0 | 112 | 1316 | 194 | 0 | 2106 | 2199 | 117 | 1413 |
| multi-locate-mapping | 5052 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 5 | 0 | 0 | 0 | 947 | 0 | 0 | 36 | 2199 | 38 | 906 |
| py-explain-reorder | 4543 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2199 | 40 | 861 |
| py-fix-inventory | 4685 | 45 | 200 | 65 | 36 | 537 | 69 | 16 | 0 | 10 | 0 | 0 | 0 | 436 | 0 | 0 | 68 | 2199 | 43 | 961 |
| refactor-validate | 6437 | 35 | 196 | 131 | 34 | 537 | 81 | 74 | 0 | 6 | 0 | 0 | 0 | 1565 | 0 | 0 | 501 | 2199 | 68 | 1010 |
| rename-entries | 7019 | 88 | 200 | 76 | 47 | 537 | 37 | 24 | 0 | 21 | 88 | 0 | 0 | 2289 | 0 | 0 | 188 | 2199 | 30 | 1195 |
| rust-explain-ledger | 5569 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 13 | 0 | 20 | 0 | 1119 | 0 | 0 | 238 | 2199 | 41 | 1077 |
| session-ledger | 8390 | 742 | 798 | 76 | 183 | 537 | 188 | 24 | 0 | 14 | 0 | 0 | 0 | 1791 | 0 | 31 | 495 | 2199 | 110 | 1202 |
| session-x-tools | 27903 | 1260 | 1175 | 286 | 692 | 537 | 21 | 63 | 0 | 21 | 31 | 0 | 0 | 19243 | 0 | 0 | 776 | 2199 | 209 | 1390 |
| ts-fix-cart-discount | 5100 | 38 | 200 | 82 | 36 | 537 | 123 | 54 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 63 | 2199 | 44 | 955 |
| ts-locate-discount | 4426 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 494 | 0 | 0 | 8 | 2199 | 30 | 841 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
