# Direct-mode suite: direct-20261029T202847Z

Model: qwen3.6-35b-a3b-mtp. Started 2026-10-29T20:28:47Z, finished 2026-10-29T20:45:01Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=off, working_set_tokens=0.

**25/26 passed (96%)**, 5.9 model calls and 5.4 tool calls per task, 44677 prompt tokens per task with a 92% prompt-cache hit rate, 990 generated tokens per task, 11 KB of tool results per task, 37 s per task, 28218 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 143 KB → 12 KB. Freshness notes: 1.

Calls run together: 23 replies, 53 calls, 0.006 s together against 0.008 s one after another.

Decode: 36.6 tokens/s over the run (25730 tokens in 703 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 2386 generated tokens, 105 s); explore 30 (0 without thinking, 7949 generated tokens, 283 s); edit 23 (0 without thinking, 3005 generated tokens, 103 s); debug 34 (0 without thinking, 5747 generated tokens, 183 s); answer 48 (0 without thinking, 6643 generated tokens, 289 s).

Where the agent's time went, per task: 37.0 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1956 ms (median). 303 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3366 prompt tokens evaluated by the server per task, in 9.7 s. Cost of a passed task: 4317 processed tokens and 36 s; of a failed one: 5327 and 80 s. Peak memory: harness 38 MiB, model server 30130 MiB, GPU 5759 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 6.7 | 5.9 | 54608 | 43.4 | 0.3 | 44 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8714 | 11.5 | 0.0 | 12 |
| multi | 1/2 | 0 | 7.0 | 9.5 | 45708 | 45.4 | 0.5 | 46 |
| python | 3/3 | 0 | 5.0 | 4.3 | 31887 | 28.2 | 0.1 | 28 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9053 | 9.5 | 0.0 | 10 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14210 | 14.3 | 0.1 | 14 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25700 | 81% | 850 | 33 |
| diagnose | 2/2 | 3.5 | 3.5 | 17373 | 89% | 698 | 25 |
| explain | 5/5 | 2.2 | 1.6 | 10207 | 86% | 327 | 13 |
| feature | 1/2 | 9.0 | 11.0 | 59905 | 96% | 2028 | 66 |
| small-fix | 4/4 | 4.8 | 5.0 | 24217 | 94% | 503 | 20 |
| large-change | 1/1 | 6.0 | 6.0 | 49130 | 90% | 1316 | 48 |
| large-locate | 1/1 | 2.0 | 1.0 | 13502 | 61% | 337 | 19 |
| locate | 3/3 | 2.0 | 2.3 | 9104 | 87% | 289 | 12 |
| refactor | 2/2 | 7.0 | 8.0 | 41202 | 93% | 762 | 30 |
| session | 5/5 | 13.0 | 9.6 | 132219 | 94% | 2348 | 90 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25700 | 20754 | 5679 | 850 | 0 | 0 | 0 | 33 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8732 | 7903 | 4582 | 260 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 26014 | 23181 | 6361 | 1136 | 0 | 0 | 0 | 39 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10163 | 7987 | 5985 | 445 | 0 | 0 | 0 | 16 |  |
| explain-ledger-posting | explain | chat | pass | 3 | 2 | 14308 | 12621 | 5522 | 422 | 0 | 0 | 0 | 17 |  |
| feature-currency | feature | code | pass | 6 | 6 | 37783 | 35399 | 7339 | 1610 | 0 | 0 | 0 | 52 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 20041 | 18659 | 5506 | 547 | 0 | 0 | 0 | 20 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 33248 | 31096 | 6407 | 648 | 0 | 0 | 0 | 26 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8714 | 7808 | 4675 | 296 | 0 | 0 | 0 | 12 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 49130 | 44119 | 9788 | 1316 | 0 | 0 | 0 | 48 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13502 | 8223 | 9073 | 337 | 0 | 0 | 0 | 19 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 8949 | 7814 | 4820 | 270 | 0 | 0 | 0 | 11 |  |
| multi-feature-discount | feature | code | FAIL | 12 | 16 | 82027 | 79146 | 8807 | 2446 | 0 | 0 | 0 | 80 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9389 | 7989 | 5264 | 271 | 0 | 0 | 0 | 12 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8797 | 7837 | 4745 | 259 | 0 | 0 | 0 | 11 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24131 | 22880 | 5288 | 458 | 0 | 0 | 0 | 18 |  |
| refactor-validate | refactor | code | pass | 8 | 9 | 49167 | 45774 | 7888 | 983 | 0 | 0 | 1 | 38 |  |
| rename-entries | refactor | code | pass | 6 | 7 | 33236 | 31105 | 6288 | 540 | 0 | 0 | 0 | 23 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9053 | 7848 | 4984 | 213 | 0 | 0 | 0 | 10 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 8 | 66221 | 63010 | 8330 | 2204 | 0 | 0 | 0 | 74 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 8 | 6 | 62734 | 55839 | 11334 | 1336 | 0 | 0 | 0 | 56 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 13 | 11 | 101544 | 98012 | 10084 | 3085 | 0 | 0 | 0 | 101 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 15 | 10 | 111779 | 107401 | 9637 | 1701 | 0 | 0 | 0 | 68 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 13 | 318818 | 295773 | 28218 | 3414 | 0 | 0 | 0 | 150 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19447 | 18081 | 5326 | 358 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8973 | 7828 | 4923 | 325 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5679 | 0 | 200 | 76 | 35 | 537 | 52 | 0 | 4 | 0 | 0 | 712 | 31 | 413 | 2338 | 65 | 1216 |
| diagnose-config | 4582 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2338 | 29 | 896 |
| diagnose-failing-test | 6361 | 15 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 80 | 2338 | 29 | 1105 |
| explain-event-flow | 5985 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2338 | 33 | 943 |
| explain-ledger-posting | 5522 | 19 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 1307 | 0 | 22 | 2338 | 22 | 959 |
| feature-currency | 7339 | 37 | 198 | 131 | 103 | 537 | 194 | 45 | 0 | 0 | 0 | 1565 | 0 | 898 | 2338 | 55 | 1238 |
| fix-balance-query | 5506 | 44 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 117 | 2338 | 32 | 1149 |
| fix-ledger-balance | 6407 | 37 | 204 | 76 | 39 | 537 | 36 | 24 | 7 | 0 | 0 | 1622 | 0 | 153 | 2338 | 52 | 1282 |
| java-explain-orders | 4675 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2338 | 35 | 910 |
| large-change-txtar | 9788 | 70 | 200 | 286 | 153 | 537 | 436 | 141 | 0 | 0 | 0 | 3623 | 0 | 683 | 2338 | 79 | 1242 |
| large-locate-preorder | 9073 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 15 | 2338 | 42 | 931 |
| locate-topic-name | 4820 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 460 | 0 | 0 | 0 | 118 | 2338 | 25 | 941 |
| multi-feature-discount | 8807 | 14 | 202 | 113 | 68 | 537 | 726 | 60 | 11 | 0 | 71 | 1316 | 0 | 1487 | 2338 | 117 | 1747 |
| multi-locate-mapping | 5264 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 947 | 0 | 27 | 2338 | 38 | 993 |
| py-explain-reorder | 4745 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2338 | 40 | 924 |
| py-fix-inventory | 5288 | 38 | 200 | 65 | 36 | 537 | 19 | 16 | 10 | 0 | 0 | 785 | 0 | 71 | 2338 | 43 | 1130 |
| refactor-validate | 7888 | 36 | 196 | 131 | 34 | 537 | 80 | 96 | 12 | 0 | 0 | 2652 | 0 | 387 | 2338 | 68 | 1321 |
| rename-entries | 6288 | 0 | 200 | 76 | 47 | 537 | 36 | 24 | 21 | 3 | 0 | 1599 | 0 | 139 | 2338 | 30 | 1238 |
| rust-explain-ledger | 4984 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 8 | 2338 | 41 | 923 |
| session-ledger | 8330 | 622 | 798 | 76 | 206 | 537 | 186 | 24 | 0 | 0 | 0 | 1172 | 31 | 431 | 2338 | 110 | 1799 |
| session-noisy-tests | 11334 | 371 | 607 | 65 | 74 | 537 | 4633 | 16 | 0 | 0 | 0 | 809 | 0 | 283 | 2338 | 81 | 1520 |
| session-resume-constraints | 10084 | 773 | 810 | 76 | 183 | 537 | 262 | 49 | 0 | 0 | 0 | 1791 | 31 | 656 | 2338 | 126 | 2452 |
| session-stale-config | 9637 | 345 | 1022 | 131 | 258 | 537 | 170 | 22 | 4 | 0 | 0 | 2403 | 28 | 500 | 2338 | 118 | 1761 |
| session-x-tools | 28218 | 958 | 1175 | 286 | 692 | 537 | 20 | 63 | 95 | 0 | 0 | 18433 | 0 | 973 | 2338 | 209 | 2439 |
| ts-fix-cart-discount | 5326 | 40 | 200 | 82 | 36 | 537 | 121 | 54 | 0 | 0 | 0 | 769 | 0 | 63 | 2338 | 44 | 1042 |
| ts-locate-discount | 4923 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2338 | 30 | 915 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model mtp/Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 0b21525e972670ed59e1812e170b27c26355381f0656ecc4e25617ece7dac58b); tasks bdf975b69c63e339.
