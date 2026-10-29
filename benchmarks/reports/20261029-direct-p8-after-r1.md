# Direct-mode suite: direct-20261029T094915Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T09:49:15Z, finished 2026-10-29T10:05:57Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**25/26 passed (96%)**, 6.3 model calls and 6.1 tool calls per task, 47998 prompt tokens per task with a 93% prompt-cache hit rate, 1083 generated tokens per task, 10 KB of tool results per task, 39 s per task, 29799 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 4 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 1, 102 KB → 5 KB. Freshness notes: 1.

Calls run together: 25 replies, 59 calls, 0.00 s together against 0.00 s one after another.

Where the agent's time went, per task: 38.1 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1712 ms (median). 370 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3258 prompt tokens evaluated by the server per task, in 9.1 s. Cost of a passed task: 4327 processed tokens and 38 s; of a failed one: 4710 and 61 s. Peak memory: harness 38 MiB, model server 28712 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 7.2 | 6.6 | 59894 | 45.9 | 0.3 | 46 |
| java | 1/1 | 1 | 3.0 | 2.0 | 13549 | 11.0 | 0.0 | 11 |
| multi | 1/2 | 0 | 5.5 | 9.5 | 33155 | 35.6 | 0.3 | 36 |
| python | 3/3 | 0 | 5.3 | 5.0 | 33041 | 29.0 | 0.1 | 29 |
| rust | 1/1 | 1 | 5.0 | 4.0 | 22327 | 13.7 | 0.0 | 14 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14228 | 14.0 | 0.1 | 14 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 10.0 | 10.0 | 63075 | 91% | 2576 | 81 |
| diagnose | 2/2 | 3.5 | 3.5 | 17524 | 90% | 731 | 25 |
| explain | 5/5 | 2.8 | 2.2 | 12721 | 90% | 352 | 13 |
| feature | 1/2 | 7.5 | 11.5 | 47448 | 95% | 1576 | 51 |
| small-fix | 4/4 | 5.0 | 5.2 | 25754 | 94% | 594 | 22 |
| large-change | 1/1 | 6.0 | 6.0 | 48253 | 91% | 1116 | 39 |
| large-locate | 1/1 | 2.0 | 1.0 | 13505 | 61% | 299 | 17 |
| locate | 3/3 | 2.0 | 2.0 | 9034 | 87% | 278 | 11 |
| refactor | 2/2 | 8.0 | 11.0 | 46680 | 94% | 1010 | 35 |
| session | 5/5 | 13.4 | 10.4 | 141218 | 95% | 2514 | 91 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 10 | 10 | 63075 | 57665 | 7865 | 2576 | 0 | 0 | 1 | 81 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8753 | 7924 | 4603 | 268 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 26296 | 23467 | 6508 | 1194 | 0 | 0 | 0 | 39 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10161 | 7985 | 5983 | 564 | 0 | 0 | 0 | 19 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8787 | 7801 | 4744 | 314 | 0 | 0 | 0 | 12 |  |
| feature-currency | feature | code | pass | 6 | 6 | 37267 | 34910 | 7144 | 1251 | 0 | 0 | 0 | 40 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 20191 | 18809 | 5571 | 606 | 0 | 0 | 0 | 20 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 33606 | 31440 | 6547 | 750 | 0 | 0 | 0 | 28 |  |
| java-explain-orders | explain | chat | pass | 3 | 2 | 13549 | 12557 | 4836 | 267 | 0 | 0 | 0 | 11 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48253 | 43697 | 9194 | 1116 | 0 | 0 | 1 | 39 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13505 | 8226 | 9076 | 299 | 0 | 0 | 0 | 17 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 9447 | 7814 | 5318 | 249 | 0 | 0 | 0 | 10 |  |
| multi-feature-discount | feature | code | FAIL | 9 | 17 | 57628 | 54819 | 8170 | 1901 | 0 | 0 | 0 | 61 | …-- FAIL: TestHiddenDiscount (0.00s)     hidden_test.go:12: {ID:o1 SubtotalCents:1000 TotalCents:700 DiscountCents:0} FAIL FAIL	example.com/shop/api	0.001s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 2 | 8682 | 7964 | 4557 | 271 | 0 | 0 | 0 | 11 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8782 | 7822 | 4730 | 280 | 0 | 0 | 0 | 11 |  |
| py-fix-inventory | small-fix | code | pass | 6 | 6 | 29738 | 28431 | 5438 | 631 | 0 | 0 | 0 | 23 |  |
| refactor-validate | refactor | code | pass | 8 | 8 | 49170 | 45806 | 7898 | 1096 | 0 | 0 | 1 | 39 |  |
| rename-entries | refactor | code | pass | 8 | 14 | 44191 | 41748 | 6994 | 925 | 0 | 0 | 0 | 32 |  |
| rust-explain-ledger | explain | chat | pass | 5 | 4 | 22327 | 20946 | 5320 | 333 | 0 | 0 | 0 | 14 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 75443 | 71611 | 8712 | 2035 | 0 | 0 | 0 | 69 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 8 | 7 | 60603 | 55901 | 9291 | 1499 | 0 | 0 | 0 | 54 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 13 | 11 | 100239 | 96708 | 10026 | 2967 | 0 | 0 | 0 | 95 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 13 | 8 | 94084 | 89528 | 9609 | 1475 | 0 | 0 | 0 | 59 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 22 | 17 | 375722 | 353714 | 29799 | 4592 | 0 | 0 | 1 | 179 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19483 | 18116 | 5344 | 389 | 0 | 0 | 0 | 17 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8973 | 7828 | 4923 | 314 | 0 | 0 | 0 | 12 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 7865 | 42 | 200 | 76 | 35 | 537 | 379 | 0 | 4 | 0 | 0 | 712 | 93 | 901 | 2338 | 65 | 2483 |
| diagnose-config | 4603 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2338 | 29 | 917 |
| diagnose-failing-test | 6508 | 15 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 80 | 2338 | 29 | 1252 |
| explain-event-flow | 5983 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2338 | 33 | 941 |
| explain-ledger-posting | 4744 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2338 | 22 | 899 |
| feature-currency | 7144 | 50 | 198 | 131 | 103 | 537 | 167 | 45 | 0 | 0 | 0 | 1565 | 0 | 708 | 2338 | 55 | 1247 |
| fix-balance-query | 5571 | 51 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 79 | 2338 | 32 | 1245 |
| fix-ledger-balance | 6547 | 38 | 204 | 76 | 39 | 537 | 36 | 24 | 21 | 0 | 0 | 1622 | 0 | 245 | 2338 | 52 | 1315 |
| java-explain-orders | 4836 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 616 | 0 | 21 | 2338 | 35 | 988 |
| large-change-txtar | 9194 | 24 | 200 | 286 | 153 | 537 | 17 | 105 | 0 | 0 | 0 | 3623 | 0 | 664 | 2338 | 79 | 1168 |
| large-locate-preorder | 9076 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 15 | 2338 | 42 | 934 |
| locate-topic-name | 5318 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 303 | 0 | 705 | 0 | 77 | 2338 | 25 | 932 |
| multi-feature-discount | 8170 | 0 | 202 | 113 | 68 | 537 | 630 | 104 | 14 | 0 | 0 | 1394 | 0 | 1008 | 2338 | 117 | 1645 |
| multi-locate-mapping | 4557 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 5 | 0 | 0 | 259 | 0 | 19 | 2338 | 38 | 977 |
| py-explain-reorder | 4730 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2338 | 40 | 909 |
| py-fix-inventory | 5438 | 40 | 200 | 65 | 36 | 537 | 59 | 16 | 10 | 0 | 0 | 785 | 0 | 101 | 2338 | 43 | 1208 |
| refactor-validate | 7898 | 0 | 196 | 131 | 34 | 537 | 80 | 96 | 6 | 0 | 0 | 2631 | 0 | 504 | 2338 | 68 | 1277 |
| rename-entries | 6994 | 23 | 200 | 76 | 47 | 537 | 36 | 24 | 36 | 3 | 12 | 1833 | 0 | 267 | 2338 | 30 | 1532 |
| rust-explain-ledger | 5320 | 0 | 205 | 89 | 31 | 537 | 11 | 0 | 0 | 0 | 0 | 930 | 0 | 80 | 2338 | 41 | 1058 |
| session-ledger | 8712 | 458 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 31 | 466 | 2338 | 110 | 1714 |
| session-noisy-tests | 9291 | 458 | 607 | 65 | 74 | 537 | 2434 | 16 | 0 | 0 | 0 | 809 | 0 | 400 | 2338 | 81 | 1472 |
| session-resume-constraints | 10026 | 704 | 810 | 76 | 183 | 537 | 261 | 49 | 0 | 0 | 0 | 1791 | 31 | 778 | 2338 | 126 | 2342 |
| session-stale-config | 9609 | 354 | 1022 | 131 | 258 | 537 | 72 | 0 | 4 | 0 | 0 | 2733 | 28 | 324 | 2338 | 118 | 1690 |
| session-x-tools | 29799 | 1133 | 1175 | 286 | 692 | 537 | 20 | 153 | 95 | 31 | 0 | 18695 | 0 | 1423 | 2338 | 209 | 3012 |
| ts-fix-cart-discount | 5344 | 32 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 65 | 2338 | 44 | 1065 |
| ts-locate-discount | 4923 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2338 | 30 | 915 |

Environment: build dev 74e6f0c1c132-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
