# Direct-mode suite: direct-20261029T174237Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T17:42:37Z, finished 2026-10-29T18:00:13Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=ngram-mod, working_set_tokens=0.

**25/26 passed (96%)**, 6.5 model calls and 6.0 tool calls per task, 53215 prompt tokens per task with a 93% prompt-cache hit rate, 1070 generated tokens per task, 12 KB of tool results per task, 41 s per task, 34334 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 143 KB → 12 KB. Freshness notes: 1.

Calls run together: 23 replies, 53 calls, 0.005 s together against 0.007 s one after another.

Decode: 36.1 tokens/s over the run (27818 tokens in 770 s of the server's decode time). Speculative decoding: 10128 tokens drafted, 2499 accepted (25%).

Model calls by role: plan 18 (0 without thinking, 2148 generated tokens, 100 s); explore 32 (0 without thinking, 8441 generated tokens, 293 s); edit 31 (0 without thinking, 4220 generated tokens, 143 s); debug 38 (0 without thinking, 6395 generated tokens, 217 s); answer 49 (0 without thinking, 6614 generated tokens, 291 s).

Where the agent's time went, per task: 40.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1844 ms (median). 328 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3706 prompt tokens evaluated by the server per task, in 10.2 s. Cost of a passed task: 4658 processed tokens and 37 s; of a failed one: 7729 and 132 s. Peak memory: harness 39 MiB, model server 29854 MiB, GPU 7143 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 7.0 | 6.4 | 62815 | 45.4 | 0.3 | 46 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8712 | 8.0 | 0.0 | 8 |
| multi | 1/2 | 0 | 12.0 | 13.0 | 87308 | 71.4 | 0.3 | 72 |
| python | 3/3 | 0 | 5.0 | 4.3 | 31653 | 27.7 | 0.1 | 28 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9078 | 9.9 | 0.0 | 10 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14183 | 14.3 | 0.1 | 14 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 8.0 | 8.0 | 46326 | 88% | 1893 | 64 |
| diagnose | 2/2 | 2.5 | 2.5 | 11356 | 87% | 626 | 21 |
| explain | 5/5 | 2.0 | 1.4 | 9106 | 86% | 316 | 12 |
| feature | 1/2 | 15.0 | 17.5 | 109881 | 97% | 2684 | 92 |
| small-fix | 4/4 | 4.8 | 5.0 | 24351 | 94% | 560 | 22 |
| large-change | 1/1 | 6.0 | 6.0 | 48381 | 91% | 1121 | 40 |
| large-locate | 1/1 | 2.0 | 1.0 | 13554 | 61% | 356 | 19 |
| locate | 3/3 | 2.0 | 1.3 | 9080 | 81% | 211 | 11 |
| refactor | 2/2 | 7.5 | 9.0 | 44501 | 93% | 820 | 33 |
| session | 5/5 | 13.4 | 10.2 | 154734 | 94% | 2347 | 92 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 8 | 8 | 46326 | 40684 | 6881 | 1893 | 0 | 0 | 1 | 64 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8748 | 7919 | 4598 | 284 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 4 | 13964 | 11795 | 5603 | 969 | 0 | 0 | 0 | 32 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10172 | 7996 | 5994 | 640 | 0 | 0 | 0 | 22 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8786 | 7800 | 4743 | 313 | 0 | 0 | 0 | 12 |  |
| feature-currency | feature | code | pass | 8 | 10 | 54370 | 51300 | 8113 | 1559 | 0 | 0 | 0 | 52 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 20169 | 18788 | 5573 | 623 | 0 | 0 | 0 | 23 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 33617 | 31465 | 6559 | 793 | 0 | 0 | 0 | 30 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8712 | 7806 | 4673 | 177 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48381 | 43789 | 9278 | 1121 | 0 | 0 | 0 | 40 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13554 | 8275 | 9125 | 356 | 0 | 0 | 0 | 19 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 9043 | 7885 | 4914 | 118 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 22 | 25 | 165392 | 161472 | 10165 | 3809 | 0 | 0 | 0 | 132 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 1 | 9225 | 6269 | 5100 | 192 | 0 | 0 | 0 | 12 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8782 | 7822 | 4730 | 214 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24225 | 22922 | 5354 | 446 | 0 | 0 | 0 | 19 |  |
| refactor-validate | refactor | code | pass | 9 | 9 | 54801 | 51255 | 7935 | 1037 | 0 | 0 | 1 | 41 |  |
| rename-entries | refactor | code | pass | 6 | 9 | 34201 | 31884 | 6487 | 604 | 0 | 0 | 0 | 25 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9078 | 7873 | 5009 | 236 | 0 | 0 | 0 | 10 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 85790 | 81695 | 9354 | 2822 | 0 | 0 | 0 | 95 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 8 | 6 | 61952 | 55105 | 11266 | 1323 | 0 | 0 | 0 | 56 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 12 | 107967 | 103991 | 10153 | 3068 | 0 | 0 | 0 | 102 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 82526 | 78254 | 8988 | 1201 | 0 | 0 | 0 | 52 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 21 | 16 | 435436 | 407333 | 34334 | 3320 | 0 | 0 | 1 | 156 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19393 | 18026 | 5300 | 378 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8973 | 7828 | 4923 | 322 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:read | tool:read_many | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6881 | 0 | 200 | 76 | 35 | 537 | 102 | 0 | 4 | 0 | 712 | 0 | 0 | 93 | 901 | 2338 | 65 | 1818 |
| diagnose-config | 4598 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 0 | 9 | 2338 | 29 | 912 |
| diagnose-failing-test | 5603 | 19 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 1169 | 0 | 0 | 0 | 59 | 2338 | 29 | 997 |
| explain-event-flow | 5994 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 0 | 20 | 2338 | 33 | 952 |
| explain-ledger-posting | 4743 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 0 | 11 | 2338 | 22 | 898 |
| feature-currency | 8113 | 17 | 198 | 131 | 103 | 537 | 192 | 45 | 5 | 0 | 2212 | 0 | 0 | 0 | 902 | 2338 | 55 | 1378 |
| fix-balance-query | 5573 | 48 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 877 | 0 | 0 | 0 | 117 | 2338 | 32 | 1212 |
| fix-ledger-balance | 6559 | 38 | 204 | 76 | 39 | 537 | 36 | 24 | 7 | 0 | 1622 | 0 | 0 | 0 | 247 | 2338 | 52 | 1339 |
| java-explain-orders | 4673 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 0 | 11 | 2338 | 35 | 908 |
| large-change-txtar | 9278 | 67 | 200 | 286 | 153 | 537 | 17 | 141 | 0 | 0 | 3623 | 0 | 0 | 0 | 616 | 2338 | 79 | 1221 |
| large-locate-preorder | 9125 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 4526 | 0 | 0 | 0 | 55 | 2338 | 42 | 943 |
| locate-topic-name | 4914 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 0 | 10 | 2338 | 25 | 898 |
| multi-feature-discount | 10165 | 0 | 202 | 113 | 68 | 537 | 137 | 163 | 14 | 0 | 1316 | 0 | 440 | 0 | 2554 | 2338 | 117 | 2166 |
| multi-locate-mapping | 5100 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 847 | 0 | 0 | 15 | 2338 | 38 | 941 |
| py-explain-reorder | 4730 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 0 | 15 | 2338 | 40 | 909 |
| py-fix-inventory | 5354 | 44 | 200 | 65 | 36 | 537 | 71 | 16 | 10 | 0 | 785 | 0 | 0 | 0 | 74 | 2338 | 43 | 1135 |
| refactor-validate | 7935 | 0 | 196 | 131 | 34 | 537 | 80 | 96 | 6 | 0 | 2654 | 0 | 0 | 0 | 466 | 2338 | 68 | 1329 |
| rename-entries | 6487 | 17 | 200 | 76 | 47 | 537 | 36 | 24 | 21 | 3 | 1721 | 0 | 0 | 0 | 158 | 2338 | 30 | 1279 |
| rust-explain-ledger | 5009 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 0 | 50 | 2338 | 41 | 906 |
| session-ledger | 9354 | 690 | 798 | 76 | 183 | 537 | 186 | 24 | 14 | 0 | 1791 | 0 | 0 | 31 | 467 | 2338 | 110 | 2109 |
| session-noisy-tests | 11266 | 347 | 607 | 65 | 74 | 537 | 4583 | 16 | 0 | 0 | 809 | 0 | 0 | 0 | 283 | 2338 | 81 | 1526 |
| session-resume-constraints | 10153 | 720 | 810 | 76 | 183 | 537 | 261 | 49 | 14 | 0 | 1791 | 0 | 0 | 31 | 577 | 2338 | 126 | 2640 |
| session-stale-config | 8988 | 293 | 1022 | 131 | 258 | 537 | 74 | 0 | 4 | 0 | 2380 | 0 | 0 | 28 | 375 | 2338 | 118 | 1430 |
| session-x-tools | 34334 | 1117 | 1175 | 286 | 692 | 537 | 20 | 152 | 95 | 0 | 24480 | 0 | 0 | 0 | 913 | 2338 | 209 | 2320 |
| ts-fix-cart-discount | 5300 | 0 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 769 | 0 | 0 | 0 | 66 | 2338 | 44 | 1052 |
| ts-locate-discount | 4923 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 0 | 17 | 2338 | 30 | 915 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
