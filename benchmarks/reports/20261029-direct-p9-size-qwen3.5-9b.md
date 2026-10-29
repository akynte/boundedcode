# Direct-mode suite: direct-20261029T190715Z

Model: qwen3.5-9b. Started 2026-10-29T19:07:15Z, finished 2026-10-29T19:33:21Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=65536, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=off, working_set_tokens=0.

**24/26 passed (92%)**, 6.5 model calls and 5.3 tool calls per task, 54076 prompt tokens per task with a 91% prompt-cache hit rate, 1279 generated tokens per task, 14 KB of tool results per task, 60 s per task, 30463 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 205 KB → 10 KB. Freshness notes: 1.

Calls run together: 6 replies, 13 calls, 0.002 s together against 0.002 s one after another.

Decode: 24.9 tokens/s over the run (33249 tokens in 1335 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 2830 generated tokens, 143 s); explore 24 (0 without thinking, 9059 generated tokens, 387 s); edit 31 (0 without thinking, 4109 generated tokens, 189 s); debug 40 (0 without thinking, 8703 generated tokens, 393 s); answer 56 (0 without thinking, 8548 generated tokens, 441 s).

Where the agent's time went, per task: 59.7 s in model calls, 0.3 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 969 ms (median). 477 reasoning tokens per task. 18 prompt-cache invalidations (2 after the history was rewritten), 39723 prompt tokens recomputed; 4682 prompt tokens evaluated by the server per task, in 8.0 s. Cost of a passed task: 6060 processed tokens and 58 s; of a failed one: 4771 and 92 s. Peak memory: harness 40 MiB, model server 10988 MiB, GPU 6663 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 7.4 | 5.9 | 66242 | 72.1 | 0.3 | 73 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8712 | 16.8 | 0.0 | 17 |
| multi | 1/2 | 0 | 8.0 | 9.5 | 59218 | 83.6 | 0.4 | 84 |
| python | 3/3 | 0 | 5.7 | 4.0 | 38648 | 33.5 | 0.2 | 34 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9038 | 10.6 | 0.0 | 11 |
| typescript | 2/2 | 0 | 3.0 | 2.0 | 13868 | 16.0 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 4.0 | 3.0 | 19706 | 75% | 713 | 34 |
| diagnose | 2/2 | 3.5 | 3.0 | 17382 | 90% | 854 | 35 |
| explain | 5/5 | 2.0 | 1.0 | 8942 | 88% | 348 | 14 |
| feature | 1/2 | 11.5 | 13.0 | 87752 | 96% | 2962 | 126 |
| small-fix | 3/4 | 4.2 | 3.2 | 20881 | 94% | 608 | 26 |
| large-change | 1/1 | 6.0 | 5.0 | 43288 | 91% | 1431 | 64 |
| large-locate | 1/1 | 2.0 | 1.0 | 13501 | 61% | 394 | 22 |
| locate | 3/3 | 2.3 | 1.7 | 10559 | 90% | 314 | 13 |
| refactor | 2/2 | 7.5 | 8.5 | 44246 | 94% | 1220 | 53 |
| session | 5/5 | 15.6 | 11.2 | 174163 | 91% | 3105 | 160 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 4 | 3 | 19706 | 14824 | 5455 | 713 | 0 | 0 | 0 | 34 |  |
| diagnose-config | diagnose | chat | pass | 2 | 2 | 8900 | 7835 | 4750 | 404 | 0 | 0 | 0 | 17 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 4 | 25864 | 23325 | 6494 | 1303 | 0 | 0 | 0 | 54 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 1 | 9573 | 7984 | 5395 | 572 | 0 | 0 | 0 | 23 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8789 | 7803 | 4746 | 209 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 10 | 10 | 70991 | 68259 | 8613 | 2431 | 0 | 0 | 2 | 104 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 3 | 19817 | 18750 | 5559 | 879 | 0 | 0 | 0 | 36 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 4 | 26211 | 24402 | 5985 | 563 | 0 | 0 | 0 | 26 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8712 | 7806 | 4673 | 425 | 0 | 0 | 0 | 17 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 5 | 43288 | 39504 | 8842 | 1431 | 0 | 0 | 0 | 64 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13501 | 8222 | 9072 | 394 | 0 | 0 | 0 | 22 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 9064 | 7906 | 4935 | 186 | 0 | 0 | 0 | 9 |  |
| multi-feature-discount | feature | code | FAIL | 13 | 16 | 104513 | 100409 | 10927 | 3492 | 0 | 0 | 0 | 148 | …-- FAIL: TestHiddenDiscount (0.00s)     hidden_test.go:12: {ID:o1 SubtotalCents:1000 TotalCents:700 DiscountCents:0} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 3 | 3 | 13924 | 12840 | 5119 | 508 | 0 | 0 | 0 | 21 |  |
| py-explain-reorder | explain | chat | pass | 2 | 1 | 8600 | 7816 | 4548 | 277 | 0 | 0 | 0 | 11 |  |
| py-fix-inventory | small-fix | code | pass | 4 | 3 | 18450 | 17576 | 4977 | 500 | 0 | 0 | 0 | 20 |  |
| refactor-validate | refactor | code | pass | 7 | 6 | 42043 | 39116 | 7657 | 1283 | 0 | 0 | 1 | 56 |  |
| rename-entries | refactor | code | pass | 8 | 11 | 46448 | 44167 | 6990 | 1158 | 0 | 0 | 0 | 51 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9038 | 7833 | 4969 | 257 | 0 | 0 | 0 | 11 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 8 | 74015 | 67691 | 7665 | 2413 | 0 | 0 | 0 | 109 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 11 | 8 | 88894 | 77675 | 10109 | 1286 | 0 | 0 | 0 | 70 |  |
| session-resume-constraints | session | code, chat, code, code | pass | 15 | 11 | 110069 | 103533 | 9551 | 4817 | 0 | 0 | 0 | 210 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 13 | 8 | 91246 | 84088 | 9282 | 1727 | 0 | 0 | 0 | 87 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 27 | 21 | 506593 | 459179 | 30463 | 5282 | 0 | 0 | 0 | 325 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 3 | 19047 | 17898 | 5242 | 491 | 0 | 0 | 0 | 22 |  |
| ts-locate-discount | locate | chat | pass | 2 | 1 | 8689 | 7821 | 4639 | 248 | 0 | 0 | 0 | 10 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:read_many | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5455 | 0 | 200 | 76 | 35 | 537 | 39 | 0 | 0 | 0 | 0 | 688 | 0 | 31 | 395 | 2338 | 65 | 1051 |
| diagnose-config | 4750 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 77 | 0 | 355 | 0 | 0 | 27 | 2338 | 29 | 969 |
| diagnose-failing-test | 6494 | 0 | 206 | 76 | 58 | 537 | 130 | 0 | 0 | 101 | 0 | 619 | 769 | 0 | 82 | 2338 | 29 | 1549 |
| explain-event-flow | 5395 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 1085 | 0 | 18 | 2338 | 33 | 942 |
| explain-ledger-posting | 4746 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2338 | 22 | 901 |
| feature-currency | 8613 | 94 | 198 | 131 | 103 | 537 | 337 | 186 | 0 | 0 | 0 | 1565 | 0 | 0 | 1634 | 2338 | 55 | 1435 |
| fix-balance-query | 5559 | 54 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 562 | 0 | 297 | 2338 | 32 | 1327 |
| fix-ledger-balance | 5985 | 65 | 204 | 76 | 39 | 537 | 36 | 24 | 0 | 0 | 0 | 1306 | 0 | 0 | 218 | 2338 | 52 | 1090 |
| java-explain-orders | 4673 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2338 | 35 | 908 |
| large-change-txtar | 8842 | 0 | 200 | 286 | 153 | 537 | 532 | 142 | 0 | 0 | 0 | 0 | 2299 | 0 | 1116 | 2338 | 79 | 1160 |
| large-locate-preorder | 9072 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 0 | 15 | 2338 | 42 | 930 |
| locate-topic-name | 4935 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 10 | 2338 | 25 | 919 |
| multi-feature-discount | 10927 | 0 | 202 | 113 | 68 | 537 | 1393 | 24 | 0 | 0 | 132 | 208 | 1631 | 0 | 2275 | 2338 | 117 | 1889 |
| multi-locate-mapping | 5119 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 355 | 0 | 259 | 0 | 0 | 51 | 2338 | 38 | 1157 |
| py-explain-reorder | 4548 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 406 | 0 | 13 | 2338 | 40 | 905 |
| py-fix-inventory | 4977 | 31 | 200 | 65 | 36 | 537 | 19 | 16 | 0 | 0 | 0 | 436 | 0 | 0 | 206 | 2338 | 43 | 1050 |
| refactor-validate | 7657 | 0 | 196 | 131 | 34 | 537 | 93 | 22 | 0 | 0 | 86 | 1131 | 1060 | 0 | 706 | 2338 | 68 | 1255 |
| rename-entries | 6990 | 0 | 200 | 76 | 47 | 537 | 83 | 24 | 21 | 66 | 0 | 1599 | 0 | 0 | 362 | 2338 | 30 | 1607 |
| rust-explain-ledger | 4969 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 8 | 2338 | 41 | 908 |
| session-ledger | 7665 | 333 | 798 | 76 | 206 | 537 | 379 | 24 | 0 | 0 | 0 | 696 | 350 | 31 | 636 | 2338 | 110 | 1151 |
| session-noisy-tests | 10109 | 87 | 404 | 65 | 148 | 537 | 4231 | 16 | 0 | 0 | 0 | 435 | 0 | 0 | 329 | 2338 | 60 | 1459 |
| session-resume-constraints | 9551 | 613 | 820 | 76 | 207 | 537 | 642 | 73 | 0 | 0 | 0 | 0 | 1181 | 31 | 1455 | 2338 | 126 | 1452 |
| session-stale-config | 9282 | 296 | 1022 | 131 | 258 | 537 | 172 | 22 | 0 | 0 | 0 | 2380 | 0 | 28 | 683 | 2338 | 118 | 1297 |
| session-x-tools | 30463 | 804 | 976 | 286 | 492 | 537 | 505 | 94 | 211 | 54 | 0 | 19903 | 0 | 0 | 1579 | 2338 | 179 | 2505 |
| ts-fix-cart-discount | 5242 | 28 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 0 | 551 | 0 | 185 | 2338 | 44 | 1065 |
| ts-locate-discount | 4639 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 494 | 0 | 0 | 8 | 2338 | 30 | 915 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.5-9B-Q4_K_M.gguf (sha256 03b74727a860a56338e042c4420bb3f04b2fec5734175f4cb9fa853daf52b7e8); tasks bdf975b69c63e339.
