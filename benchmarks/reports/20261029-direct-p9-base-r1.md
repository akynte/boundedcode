# Direct-mode suite: direct-20261029T170844Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T17:08:44Z, finished 2026-10-29T17:25:20Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=off, working_set_tokens=0.

**25/26 passed (96%)**, 6.2 model calls and 6.0 tool calls per task, 49900 prompt tokens per task with a 93% prompt-cache hit rate, 1045 generated tokens per task, 12 KB of tool results per task, 38 s per task, 33961 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 143 KB → 12 KB. Freshness notes: 1.

Calls run together: 26 replies, 62 calls, 0.007 s together against 0.011 s one after another.

Decode: 37.5 tokens/s over the run (27179 tokens in 724 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 2511 generated tokens, 104 s); explore 28 (0 without thinking, 7913 generated tokens, 272 s); edit 28 (0 without thinking, 3404 generated tokens, 112 s); debug 35 (0 without thinking, 5970 generated tokens, 188 s); answer 51 (0 without thinking, 7381 generated tokens, 308 s).

Where the agent's time went, per task: 37.8 s in model calls, 0.3 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1847 ms (median). 320 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3610 prompt tokens evaluated by the server per task, in 9.6 s. Cost of a passed task: 4559 processed tokens and 36 s; of a failed one: 7061 and 108 s. Peak memory: harness 40 MiB, model server 28889 MiB, GPU 7141 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 6.6 | 6.1 | 58537 | 43.0 | 0.3 | 44 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8713 | 9.2 | 0.0 | 9 |
| multi | 1/2 | 0 | 11.5 | 15.5 | 84028 | 61.9 | 0.6 | 63 |
| python | 3/3 | 0 | 4.7 | 4.3 | 29344 | 27.5 | 0.1 | 28 |
| rust | 1/1 | 1 | 2.0 | 2.0 | 9063 | 9.3 | 0.0 | 9 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14198 | 14.3 | 0.1 | 15 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 6.0 | 32973 | 85% | 1438 | 49 |
| diagnose | 2/2 | 3.5 | 3.5 | 17554 | 90% | 598 | 22 |
| explain | 5/5 | 2.0 | 1.6 | 9104 | 86% | 278 | 11 |
| feature | 1/2 | 14.5 | 17.5 | 106210 | 97% | 2606 | 84 |
| small-fix | 4/4 | 4.8 | 5.0 | 24134 | 94% | 536 | 21 |
| large-change | 1/1 | 5.0 | 5.0 | 38806 | 88% | 891 | 32 |
| large-locate | 1/1 | 3.0 | 2.0 | 18187 | 70% | 369 | 20 |
| locate | 3/3 | 2.3 | 3.0 | 10969 | 82% | 374 | 15 |
| refactor | 2/2 | 5.5 | 8.5 | 32744 | 92% | 769 | 28 |
| session | 5/5 | 12.6 | 9.6 | 143890 | 94% | 2376 | 89 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 6 | 32973 | 27974 | 6321 | 1438 | 0 | 0 | 1 | 49 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8758 | 7929 | 4608 | 283 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 26350 | 23521 | 6534 | 912 | 0 | 0 | 0 | 32 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10165 | 7989 | 5987 | 423 | 0 | 0 | 0 | 15 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8784 | 7798 | 4741 | 256 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 8 | 8 | 53765 | 51180 | 8009 | 1891 | 0 | 0 | 0 | 59 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 19989 | 18607 | 5474 | 509 | 0 | 0 | 0 | 19 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 33272 | 31106 | 6425 | 673 | 0 | 0 | 0 | 26 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8713 | 7807 | 4674 | 220 | 0 | 0 | 0 | 9 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 5 | 38806 | 34328 | 8941 | 891 | 0 | 0 | 0 | 32 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 18187 | 12783 | 9178 | 369 | 0 | 0 | 0 | 20 |  |
| locate-topic-name | locate | chat | pass | 3 | 3 | 14530 | 12676 | 5580 | 366 | 0 | 0 | 0 | 15 |  |
| multi-feature-discount | feature | code | FAIL | 21 | 27 | 158655 | 154914 | 10536 | 3320 | 0 | 0 | 0 | 108 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 4 | 9401 | 6333 | 5276 | 389 | 0 | 0 | 0 | 17 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8793 | 7833 | 4741 | 276 | 0 | 0 | 0 | 11 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23854 | 22554 | 5417 | 602 | 0 | 0 | 0 | 22 |  |
| refactor-validate | refactor | code | pass | 6 | 8 | 37565 | 34697 | 7378 | 956 | 0 | 0 | 1 | 34 |  |
| rename-entries | refactor | code | pass | 5 | 9 | 27922 | 25690 | 6428 | 582 | 0 | 0 | 0 | 23 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 2 | 9063 | 7844 | 4994 | 216 | 0 | 0 | 0 | 9 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 80339 | 76525 | 9418 | 2611 | 0 | 0 | 0 | 85 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 7 | 6 | 55386 | 48549 | 11198 | 1260 | 0 | 0 | 0 | 50 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 12 | 10 | 88751 | 85322 | 9848 | 2854 | 0 | 0 | 0 | 91 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 83547 | 79353 | 9104 | 1335 | 0 | 0 | 0 | 54 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 21 | 16 | 411429 | 384330 | 33961 | 3822 | 0 | 0 | 0 | 165 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19420 | 18053 | 5313 | 359 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8976 | 7831 | 4926 | 366 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6321 | 17 | 200 | 76 | 35 | 537 | 52 | 0 | 0 | 4 | 0 | 688 | 0 | 93 | 719 | 2338 | 65 | 1497 |
| diagnose-config | 4608 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2338 | 29 | 922 |
| diagnose-failing-test | 6534 | 16 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 14 | 0 | 1788 | 0 | 0 | 80 | 2338 | 29 | 1277 |
| explain-event-flow | 5987 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2338 | 33 | 945 |
| explain-ledger-posting | 4741 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2338 | 22 | 896 |
| feature-currency | 8009 | 87 | 198 | 131 | 103 | 537 | 340 | 68 | 0 | 0 | 0 | 1565 | 0 | 0 | 1289 | 2338 | 55 | 1298 |
| fix-balance-query | 5474 | 69 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 107 | 2338 | 32 | 1102 |
| fix-ledger-balance | 6425 | 0 | 204 | 76 | 39 | 537 | 36 | 24 | 0 | 21 | 0 | 1622 | 0 | 0 | 245 | 2338 | 52 | 1231 |
| java-explain-orders | 4674 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2338 | 35 | 909 |
| large-change-txtar | 8941 | 20 | 200 | 286 | 153 | 537 | 17 | 43 | 0 | 0 | 0 | 3623 | 0 | 0 | 530 | 2338 | 79 | 1115 |
| large-locate-preorder | 9178 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 46 | 0 | 0 | 4526 | 0 | 0 | 32 | 2338 | 42 | 973 |
| locate-topic-name | 5580 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 460 | 705 | 0 | 0 | 127 | 2338 | 25 | 987 |
| multi-feature-discount | 10536 | 60 | 202 | 113 | 68 | 537 | 1085 | 155 | 0 | 14 | 0 | 1316 | 324 | 0 | 1759 | 2338 | 117 | 2448 |
| multi-locate-mapping | 5276 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 5 | 0 | 947 | 0 | 0 | 36 | 2338 | 38 | 991 |
| py-explain-reorder | 4741 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2338 | 40 | 920 |
| py-fix-inventory | 5417 | 38 | 200 | 65 | 36 | 537 | 68 | 16 | 0 | 10 | 0 | 785 | 0 | 0 | 120 | 2338 | 43 | 1161 |
| refactor-validate | 7378 | 20 | 196 | 131 | 34 | 537 | 80 | 74 | 0 | 28 | 0 | 2160 | 0 | 0 | 512 | 2338 | 68 | 1200 |
| rename-entries | 6428 | 28 | 200 | 76 | 47 | 537 | 36 | 24 | 0 | 29 | 3 | 1697 | 0 | 0 | 162 | 2338 | 30 | 1221 |
| rust-explain-ledger | 4994 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 821 | 0 | 0 | 15 | 2338 | 41 | 917 |
| session-ledger | 9418 | 559 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 447 | 2338 | 110 | 2338 |
| session-noisy-tests | 11198 | 320 | 607 | 65 | 74 | 537 | 4584 | 16 | 0 | 0 | 0 | 809 | 0 | 0 | 330 | 2338 | 81 | 1437 |
| session-resume-constraints | 9848 | 756 | 810 | 76 | 183 | 537 | 186 | 24 | 0 | 14 | 0 | 1791 | 0 | 31 | 491 | 2338 | 126 | 2485 |
| session-stale-config | 9104 | 349 | 1022 | 131 | 258 | 537 | 76 | 0 | 0 | 4 | 0 | 2380 | 0 | 28 | 329 | 2338 | 118 | 1534 |
| session-x-tools | 33961 | 1426 | 1175 | 286 | 692 | 537 | 20 | 63 | 0 | 95 | 0 | 23757 | 0 | 0 | 570 | 2338 | 209 | 2793 |
| ts-fix-cart-discount | 5313 | 25 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 0 | 65 | 2338 | 44 | 1041 |
| ts-locate-discount | 4926 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2338 | 30 | 918 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
