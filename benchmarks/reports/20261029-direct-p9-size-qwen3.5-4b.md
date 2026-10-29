# Direct-mode suite: direct-20261029T184941Z

Model: qwen3.5-4b. Started 2026-10-29T18:49:41Z, finished 2026-10-29T19:07:09Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=65536, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=off, working_set_tokens=0.

**21/26 passed (81%)**, 7.2 model calls and 6.2 tool calls per task, 60182 prompt tokens per task with a 90% prompt-cache hit rate, 2396 generated tokens per task, 15 KB of tool results per task, 40 s per task, 34060 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 4 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 205 KB → 10 KB. Freshness notes: 1.

Calls run together: 13 replies, 29 calls, 0.003 s together against 0.004 s one after another.

Decode: 64.9 tokens/s over the run (62293 tokens in 959 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 3468 generated tokens, 63 s); explore 23 (0 without thinking, 8383 generated tokens, 137 s); edit 33 (0 without thinking, 17375 generated tokens, 292 s); debug 56 (0 without thinking, 22077 generated tokens, 344 s); answer 56 (0 without thinking, 10990 generated tokens, 200 s).

Where the agent's time went, per task: 39.8 s in model calls, 0.3 s in tools (with the checks after edits), 0.05 s in the harness itself; time to first token 246 ms (median). 1166 reasoning tokens per task. 19 prompt-cache invalidations (2 after the history was rewritten), 43733 prompt tokens recomputed; 5787 prompt tokens evaluated by the server per task, in 2.5 s. Cost of a passed task: 6761 processed tokens and 23 s; of a failed one: 14154 and 115 s. Peak memory: harness 39 MiB, model server 10530 MiB, GPU 5041 MiB.

Failures by cause: environment (skipped) 2, model 5. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 13/17 | 0 | 8.2 | 7.0 | 75525 | 52.8 | 0.4 | 54 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8712 | 3.6 | 0.0 | 4 |
| multi | 1/2 | 0 | 8.5 | 10.5 | 55216 | 26.7 | 0.4 | 27 |
| python | 3/3 | 0 | 6.3 | 5.0 | 41555 | 19.8 | 0.1 | 20 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9057 | 5.6 | 0.0 | 6 |
| typescript | 2/2 | 0 | 3.0 | 2.0 | 13967 | 7.0 | 0.1 | 7 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 0/1 | 8.0 | 7.0 | 77050 | 75% | 22532 | 369 |
| diagnose | 2/2 | 3.5 | 3.0 | 16363 | 90% | 786 | 13 |
| explain | 5/5 | 2.2 | 1.4 | 10164 | 88% | 471 | 7 |
| feature | 0/2 | 12.5 | 15.0 | 92208 | 97% | 3619 | 57 |
| small-fix | 3/4 | 5.2 | 4.5 | 26931 | 95% | 883 | 14 |
| large-change | 0/1 | 16.0 | 17.0 | 191456 | 96% | 4613 | 77 |
| large-locate | 1/1 | 2.0 | 1.0 | 13474 | 60% | 344 | 8 |
| locate | 3/3 | 3.0 | 2.7 | 13470 | 92% | 368 | 6 |
| refactor | 2/2 | 7.5 | 8.5 | 43096 | 94% | 1156 | 19 |
| session | 5/5 | 14.4 | 10.0 | 156092 | 89% | 3339 | 61 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | FAIL | 8 | 7 | 77050 | 58025 | 19827 | 22532 | 0 | 0 | 0 | 369 | … got 0 --- FAIL: TestBalanceSimple (0.00s)     balance_test.go:62: Expected balance 200, got 0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| diagnose-config | diagnose | chat | pass | 4 | 4 | 18705 | 17532 | 5076 | 450 | 0 | 0 | 0 | 8 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 2 | 14021 | 11828 | 5643 | 1121 | 0 | 0 | 0 | 17 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 2 | 15442 | 13255 | 6031 | 599 | 0 | 0 | 0 | 10 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8792 | 7806 | 4749 | 797 | 0 | 0 | 0 | 12 |  |
| feature-currency | feature | code | FAIL | 11 | 11 | 87091 | 84224 | 10527 | 4205 | 0 | 0 | 2 | 66 | …al/config	[no test files] ?   	example.com/payment-service/internal/events	[no test files] ?   	example.com/payment-service/internal/store	[no test files] FAIL  |
| fix-balance-query | small-fix | code | FAIL | 5 | 5 | 25634 | 24065 | 6048 | 868 | 0 | 0 | 0 | 14 | …r	0.002s --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 4 | 26802 | 24993 | 6264 | 873 | 0 | 0 | 0 | 14 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8712 | 7806 | 4673 | 222 | 0 | 0 | 0 | 4 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | FAIL | 16 | 17 | 191456 | 182985 | 16440 | 4613 | 0 | 0 | 1 | 77 | …: --- FAIL: TestHiddenLookup (0.00s)     hidden_lookup_test.go:16: Lookup must return a pointer into the archive FAIL FAIL	golang.org/x/tools/txtar	0.002s FAIL  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13474 | 8069 | 9045 | 344 | 0 | 0 | 0 | 8 |  |
| locate-topic-name | locate | chat | pass | 4 | 5 | 18559 | 17169 | 5407 | 435 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 14 | 19 | 97324 | 93739 | 9967 | 3033 | 0 | 0 | 0 | 48 | …-- FAIL: TestHiddenDiscount (0.00s)     hidden_test.go:12: {ID:o1 SubtotalCents:1000 TotalCents:700 DiscountCents:0} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 3 | 2 | 13108 | 12379 | 4670 | 374 | 0 | 0 | 0 | 6 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8818 | 7858 | 4766 | 383 | 0 | 0 | 0 | 6 |  |
| py-fix-inventory | small-fix | code | pass | 7 | 6 | 36096 | 34859 | 6045 | 1259 | 0 | 0 | 0 | 19 |  |
| refactor-validate | refactor | code | pass | 7 | 6 | 41672 | 38935 | 7658 | 1464 | 0 | 0 | 1 | 23 |  |
| rename-entries | refactor | code | pass | 8 | 11 | 44519 | 42245 | 6731 | 849 | 0 | 0 | 0 | 15 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9057 | 7852 | 4988 | 352 | 0 | 0 | 0 | 6 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 8 | 80341 | 73162 | 8349 | 3897 | 0 | 0 | 0 | 62 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 10 | 7 | 79752 | 68340 | 10330 | 1974 | 0 | 0 | 0 | 35 |  |
| session-resume-constraints | session | code, chat, code, code | pass | 15 | 11 | 105010 | 97598 | 9267 | 3326 | 0 | 0 | 0 | 54 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 14 | 9 | 99508 | 91978 | 9268 | 1693 | 0 | 0 | 0 | 30 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 21 | 15 | 415850 | 361710 | 34060 | 5803 | 0 | 0 | 0 | 123 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 3 | 19191 | 18042 | 5295 | 531 | 0 | 0 | 0 | 10 |  |
| ts-locate-discount | locate | chat | pass | 2 | 1 | 8743 | 7816 | 4693 | 296 | 0 | 0 | 0 | 5 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:read | tool:read_many | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 19827 | 150 | 200 | 76 | 35 | 537 | 172 | 25 | 0 | 0 | 0 | 1088 | 0 | 31 | 1455 | 2338 | 65 | 13655 |
| diagnose-config | 5076 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 2 | 663 | 0 | 0 | 34 | 2338 | 29 | 1055 |
| diagnose-failing-test | 5643 | 0 | 206 | 76 | 58 | 537 | 130 | 0 | 0 | 0 | 0 | 0 | 1178 | 0 | 62 | 2338 | 29 | 1029 |
| explain-event-flow | 6031 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2338 | 33 | 989 |
| explain-ledger-posting | 4749 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2338 | 22 | 904 |
| feature-currency | 10527 | 97 | 198 | 131 | 103 | 537 | 447 | 195 | 0 | 0 | 0 | 1565 | 0 | 0 | 2866 | 2338 | 55 | 1995 |
| fix-balance-query | 6048 | 87 | 202 | 76 | 73 | 537 | 208 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 198 | 2338 | 32 | 1396 |
| fix-ledger-balance | 6264 | 75 | 204 | 76 | 39 | 537 | 36 | 24 | 0 | 0 | 0 | 1306 | 0 | 0 | 221 | 2338 | 52 | 1356 |
| java-explain-orders | 4673 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2338 | 35 | 908 |
| large-change-txtar | 16440 | 278 | 200 | 286 | 153 | 537 | 2001 | 285 | 0 | 0 | 0 | 5085 | 0 | 0 | 3613 | 2338 | 79 | 1585 |
| large-locate-preorder | 9045 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 0 | 15 | 2338 | 42 | 903 |
| locate-topic-name | 5407 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 190 | 0 | 705 | 0 | 0 | 74 | 2338 | 25 | 1137 |
| multi-feature-discount | 9967 | 128 | 202 | 113 | 68 | 537 | 629 | 156 | 0 | 0 | 0 | 2071 | 0 | 0 | 1456 | 2338 | 117 | 2152 |
| multi-locate-mapping | 4670 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 5 | 0 | 259 | 0 | 0 | 118 | 2338 | 38 | 991 |
| py-explain-reorder | 4766 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2338 | 40 | 945 |
| py-fix-inventory | 6045 | 121 | 200 | 65 | 36 | 537 | 85 | 16 | 0 | 3 | 0 | 436 | 248 | 0 | 319 | 2338 | 43 | 1598 |
| refactor-validate | 7658 | 90 | 196 | 131 | 34 | 537 | 80 | 96 | 0 | 0 | 0 | 2028 | 0 | 0 | 635 | 2338 | 68 | 1425 |
| rename-entries | 6731 | 0 | 200 | 76 | 47 | 537 | 36 | 24 | 29 | 85 | 12 | 1599 | 0 | 0 | 172 | 2338 | 30 | 1546 |
| rust-explain-ledger | 4988 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 8 | 2338 | 41 | 927 |
| session-ledger | 8349 | 357 | 597 | 76 | 193 | 537 | 426 | 48 | 0 | 0 | 0 | 0 | 1181 | 31 | 1002 | 2338 | 92 | 1471 |
| session-noisy-tests | 10330 | 567 | 607 | 65 | 74 | 537 | 3834 | 16 | 0 | 0 | 0 | 809 | 0 | 0 | 274 | 2338 | 81 | 1128 |
| session-resume-constraints | 9267 | 828 | 810 | 76 | 216 | 537 | 262 | 48 | 0 | 0 | 0 | 1791 | 0 | 31 | 838 | 2338 | 126 | 1366 |
| session-stale-config | 9268 | 299 | 1022 | 131 | 258 | 537 | 155 | 22 | 0 | 0 | 0 | 2433 | 0 | 28 | 564 | 2338 | 118 | 1363 |
| session-x-tools | 34060 | 2247 | 1175 | 286 | 692 | 537 | 421 | 63 | 21 | 31 | 0 | 22999 | 0 | 0 | 1749 | 2338 | 209 | 1292 |
| ts-fix-cart-discount | 5295 | 37 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 0 | 551 | 0 | 148 | 2338 | 44 | 1146 |
| ts-locate-discount | 4693 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 551 | 0 | 15 | 2338 | 30 | 905 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.5-4B-Q4_K_M.gguf (sha256 00fe7986ff5f6b463e62455821146049db6f9313603938a70800d1fb69ef11a4); tasks bdf975b69c63e339.
