# Direct-mode suite: direct-20261029T102659Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T10:26:59Z, finished 2026-10-29T10:42:23Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**25/26 passed (96%)**, 5.8 model calls and 5.5 tool calls per task, 43657 prompt tokens per task with a 93% prompt-cache hit rate, 986 generated tokens per task, 10 KB of tool results per task, 36 s per task, 27603 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 1, 102 KB → 5 KB. Freshness notes: 1.

Calls run together: 24 replies, 56 calls, 0.00 s together against 0.00 s one after another.

Where the agent's time went, per task: 35.1 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1767 ms (median). 275 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3237 prompt tokens evaluated by the server per task, in 8.8 s. Cost of a passed task: 4127 processed tokens and 32 s; of a failed one: 6629 and 116 s. Peak memory: harness 37 MiB, model server 28977 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 6.4 | 5.7 | 50476 | 38.8 | 0.3 | 39 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8719 | 8.0 | 0.0 | 8 |
| multi | 1/2 | 0 | 9.5 | 12.0 | 69184 | 64.2 | 0.3 | 65 |
| python | 3/3 | 0 | 5.0 | 4.3 | 30796 | 27.6 | 0.1 | 28 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9054 | 9.3 | 0.0 | 9 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14234 | 12.7 | 0.1 | 13 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 4.0 | 5.0 | 19977 | 75% | 713 | 28 |
| diagnose | 2/2 | 3.0 | 3.5 | 14537 | 87% | 684 | 23 |
| explain | 5/5 | 2.2 | 1.6 | 10199 | 86% | 329 | 13 |
| feature | 1/2 | 11.5 | 13.5 | 83062 | 97% | 2540 | 78 |
| small-fix | 4/4 | 4.8 | 5.0 | 24153 | 94% | 568 | 21 |
| large-change | 1/1 | 5.0 | 5.0 | 38642 | 88% | 863 | 31 |
| large-locate | 1/1 | 3.0 | 2.0 | 18438 | 70% | 480 | 23 |
| locate | 3/3 | 2.0 | 2.3 | 9170 | 80% | 228 | 11 |
| refactor | 2/2 | 6.5 | 7.5 | 37608 | 94% | 804 | 29 |
| session | 5/5 | 12.4 | 9.2 | 122500 | 94% | 2186 | 81 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 4 | 5 | 19977 | 15060 | 5546 | 713 | 0 | 0 | 0 | 28 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8744 | 7915 | 4594 | 269 | 0 | 0 | 0 | 10 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 6 | 20330 | 17514 | 6325 | 1100 | 0 | 0 | 0 | 36 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10162 | 7986 | 5984 | 646 | 0 | 0 | 0 | 21 |  |
| explain-ledger-posting | explain | chat | pass | 3 | 2 | 14280 | 12593 | 5496 | 373 | 0 | 0 | 0 | 15 |  |
| feature-currency | feature | code | pass | 6 | 6 | 37101 | 34744 | 7107 | 1222 | 0 | 0 | 0 | 39 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 20011 | 18629 | 5483 | 510 | 0 | 0 | 0 | 18 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 32698 | 30532 | 6538 | 772 | 0 | 0 | 0 | 27 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8719 | 7813 | 4680 | 179 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 5 | 38642 | 34164 | 8856 | 863 | 0 | 0 | 0 | 31 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 18438 | 12969 | 9324 | 480 | 0 | 0 | 0 | 23 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 9185 | 7935 | 5056 | 230 | 0 | 0 | 0 | 10 |  |
| multi-feature-discount | feature | code | FAIL | 17 | 21 | 129022 | 126251 | 10071 | 3858 | 0 | 0 | 0 | 116 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9347 | 6289 | 5222 | 250 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8782 | 7822 | 4730 | 228 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24414 | 23111 | 5463 | 581 | 0 | 0 | 0 | 21 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 41829 | 39575 | 6835 | 1054 | 0 | 0 | 1 | 36 |  |
| rename-entries | refactor | code | pass | 6 | 8 | 33388 | 31211 | 6327 | 555 | 0 | 0 | 0 | 22 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9054 | 7849 | 4985 | 219 | 0 | 0 | 0 | 9 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 85548 | 81698 | 9398 | 2658 | 0 | 0 | 0 | 86 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 8 | 6 | 59192 | 54710 | 9030 | 1458 | 0 | 0 | 0 | 53 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 11 | 9 | 79051 | 75652 | 9250 | 2298 | 0 | 0 | 0 | 75 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 82157 | 77965 | 8864 | 1080 | 0 | 0 | 0 | 47 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 14 | 306554 | 284991 | 27603 | 3434 | 0 | 0 | 0 | 143 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19490 | 18123 | 5366 | 410 | 0 | 0 | 0 | 17 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8977 | 7832 | 4927 | 204 | 0 | 0 | 0 | 9 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5546 | 0 | 200 | 76 | 35 | 537 | 37 | 0 | 6 | 0 | 712 | 0 | 31 | 399 | 2338 | 65 | 1110 |
| diagnose-config | 4594 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2338 | 29 | 908 |
| diagnose-failing-test | 6325 | 19 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 1788 | 0 | 0 | 78 | 2338 | 29 | 1067 |
| explain-event-flow | 5984 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2338 | 33 | 942 |
| explain-ledger-posting | 5496 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 1307 | 0 | 0 | 22 | 2338 | 22 | 952 |
| feature-currency | 7107 | 61 | 198 | 131 | 103 | 537 | 167 | 45 | 0 | 0 | 1565 | 0 | 0 | 706 | 2338 | 55 | 1201 |
| fix-balance-query | 5483 | 43 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 877 | 0 | 0 | 107 | 2338 | 32 | 1137 |
| fix-ledger-balance | 6538 | 47 | 204 | 76 | 39 | 537 | 36 | 24 | 21 | 0 | 1622 | 0 | 0 | 250 | 2338 | 52 | 1292 |
| java-explain-orders | 4680 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2338 | 35 | 915 |
| large-change-txtar | 8856 | 24 | 200 | 286 | 153 | 537 | 17 | 43 | 0 | 0 | 3623 | 0 | 0 | 449 | 2338 | 79 | 1107 |
| large-locate-preorder | 9324 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 70 | 4526 | 0 | 0 | 115 | 2338 | 42 | 1012 |
| locate-topic-name | 5056 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 87 | 705 | 0 | 0 | 32 | 2338 | 25 | 931 |
| multi-feature-discount | 10071 | 14 | 202 | 113 | 68 | 537 | 159 | 123 | 11 | 0 | 1316 | 389 | 0 | 2682 | 2338 | 117 | 2002 |
| multi-locate-mapping | 5222 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 947 | 0 | 0 | 27 | 2338 | 38 | 951 |
| py-explain-reorder | 4730 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2338 | 40 | 909 |
| py-fix-inventory | 5463 | 47 | 200 | 65 | 36 | 537 | 71 | 16 | 10 | 0 | 785 | 0 | 0 | 116 | 2338 | 43 | 1199 |
| refactor-validate | 6835 | 20 | 196 | 131 | 34 | 537 | 65 | 74 | 12 | 0 | 1565 | 0 | 0 | 519 | 2338 | 68 | 1276 |
| rename-entries | 6327 | 0 | 200 | 76 | 47 | 537 | 36 | 24 | 21 | 44 | 1599 | 0 | 0 | 151 | 2338 | 30 | 1224 |
| rust-explain-ledger | 4985 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 8 | 2338 | 41 | 924 |
| session-ledger | 9398 | 719 | 798 | 76 | 183 | 537 | 186 | 24 | 14 | 0 | 1791 | 0 | 31 | 478 | 2338 | 110 | 2113 |
| session-noisy-tests | 9030 | 434 | 607 | 65 | 74 | 537 | 2222 | 16 | 0 | 0 | 809 | 0 | 0 | 317 | 2338 | 81 | 1530 |
| session-resume-constraints | 9250 | 681 | 810 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 1791 | 0 | 31 | 565 | 2338 | 126 | 1902 |
| session-stale-config | 8864 | 267 | 1022 | 131 | 258 | 537 | 74 | 0 | 4 | 0 | 2380 | 0 | 28 | 310 | 2338 | 118 | 1397 |
| session-x-tools | 27603 | 1274 | 1175 | 286 | 692 | 537 | 20 | 63 | 95 | 0 | 17804 | 0 | 0 | 601 | 2338 | 209 | 2509 |
| ts-fix-cart-discount | 5366 | 47 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 769 | 0 | 0 | 67 | 2338 | 44 | 1070 |
| ts-locate-discount | 4927 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2338 | 30 | 919 |

Environment: build dev 74e6f0c1c132-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
