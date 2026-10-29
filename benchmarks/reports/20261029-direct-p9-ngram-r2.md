# Direct-mode suite: direct-20261029T183145Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T18:31:45Z, finished 2026-10-29T18:49:37Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=ngram-mod, working_set_tokens=0.

**24/26 passed (92%)**, 6.3 model calls and 6.1 tool calls per task, 51293 prompt tokens per task with a 93% prompt-cache hit rate, 1114 generated tokens per task, 12 KB of tool results per task, 41 s per task, 28848 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 4 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 143 KB → 12 KB. Freshness notes: 1.

Calls run together: 27 replies, 63 calls, 0.005 s together against 0.009 s one after another.

Decode: 36.0 tokens/s over the run (28966 tokens in 804 s of the server's decode time). Speculative decoding: 8781 tokens drafted, 1910 accepted (22%).

Model calls by role: plan 18 (0 without thinking, 2432 generated tokens, 110 s); explore 37 (0 without thinking, 10692 generated tokens, 354 s); edit 20 (0 without thinking, 2921 generated tokens, 98 s); debug 38 (0 without thinking, 5227 generated tokens, 185 s); answer 50 (0 without thinking, 7694 generated tokens, 315 s).

Where the agent's time went, per task: 40.8 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1781 ms (median). 383 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3448 prompt tokens evaluated by the server per task, in 9.6 s. Cost of a passed task: 4558 processed tokens and 39 s; of a failed one: 4617 and 63 s. Peak memory: harness 40 MiB, model server 29226 MiB, GPU 7137 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/17 | 0 | 7.4 | 7.0 | 64650 | 49.2 | 0.3 | 50 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8701 | 8.7 | 0.0 | 9 |
| multi | 2/2 | 0 | 5.5 | 8.0 | 33166 | 41.7 | 0.3 | 42 |
| python | 3/3 | 0 | 5.3 | 4.7 | 38947 | 30.6 | 0.1 | 31 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9078 | 12.4 | 0.0 | 12 |
| typescript | 2/2 | 0 | 3.5 | 3.5 | 16812 | 14.8 | 0.1 | 15 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25405 | 81% | 926 | 35 |
| diagnose | 2/2 | 4.0 | 4.0 | 20465 | 91% | 629 | 23 |
| explain | 5/5 | 2.2 | 1.6 | 10203 | 86% | 361 | 14 |
| feature | 2/2 | 8.5 | 11.5 | 55478 | 95% | 1774 | 61 |
| small-fix | 3/4 | 5.0 | 5.5 | 25476 | 94% | 574 | 23 |
| large-change | 1/1 | 6.0 | 6.0 | 48821 | 91% | 1296 | 44 |
| large-locate | 1/1 | 2.0 | 1.0 | 13506 | 61% | 303 | 17 |
| locate | 3/3 | 2.0 | 2.0 | 9124 | 86% | 224 | 10 |
| refactor | 2/2 | 8.5 | 11.5 | 49504 | 94% | 1070 | 40 |
| session | 4/5 | 14.2 | 11.0 | 162941 | 95% | 2945 | 108 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25405 | 20468 | 5775 | 926 | 0 | 0 | 0 | 35 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8757 | 7928 | 4607 | 288 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 6 | 7 | 32173 | 29313 | 6548 | 970 | 0 | 0 | 0 | 35 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10166 | 7990 | 5988 | 664 | 0 | 0 | 0 | 22 |  |
| explain-ledger-posting | explain | chat | pass | 3 | 2 | 14281 | 12525 | 5497 | 360 | 0 | 0 | 0 | 16 |  |
| feature-currency | feature | code | pass | 8 | 10 | 53977 | 50790 | 7975 | 1432 | 0 | 0 | 1 | 50 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 20189 | 18729 | 5582 | 608 | 0 | 0 | 0 | 24 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 6 | 8 | 32897 | 30734 | 6544 | 749 | 0 | 0 | 0 | 29 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8701 | 7795 | 4662 | 201 | 0 | 0 | 0 | 9 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48821 | 44229 | 9382 | 1296 | 0 | 0 | 0 | 44 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13506 | 8227 | 9077 | 303 | 0 | 0 | 0 | 17 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 9041 | 7883 | 4912 | 124 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | pass | 9 | 13 | 56978 | 54311 | 8244 | 2116 | 0 | 0 | 0 | 73 |  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9353 | 7953 | 5228 | 258 | 0 | 0 | 0 | 12 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8790 | 7830 | 4738 | 253 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24174 | 22871 | 5332 | 498 | 0 | 0 | 0 | 20 |  |
| refactor-validate | refactor | code | pass | 8 | 9 | 48882 | 45340 | 8143 | 1316 | 0 | 0 | 1 | 48 |  |
| rename-entries | refactor | code | pass | 9 | 14 | 50127 | 47727 | 6834 | 823 | 0 | 0 | 0 | 31 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9078 | 7873 | 5009 | 325 | 0 | 0 | 0 | 12 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | FAIL | 11 | 9 | 79500 | 75363 | 9365 | 3029 | 0 | 0 | 0 | 103 | switched to code. turn 4: answer does not match /Balance\|ledger\.go/ |
| session-noisy-tests | session | chat, code, chat | pass | 9 | 7 | 83878 | 76297 | 12163 | 1487 | 0 | 0 | 0 | 63 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 15 | 13 | 124567 | 120246 | 11322 | 3588 | 0 | 0 | 0 | 115 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 13 | 8 | 92656 | 88386 | 9164 | 1371 | 0 | 0 | 1 | 57 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 23 | 18 | 434104 | 412172 | 28848 | 5249 | 0 | 0 | 1 | 201 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 5 | 24646 | 23256 | 5406 | 441 | 0 | 0 | 0 | 19 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8977 | 7735 | 4927 | 291 | 0 | 0 | 0 | 11 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:multi_edit | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5775 | 0 | 200 | 76 | 35 | 537 | 37 | 0 | 10 | 0 | 0 | 0 | 712 | 31 | 423 | 2338 | 65 | 1311 |
| diagnose-config | 4607 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2338 | 29 | 921 |
| diagnose-failing-test | 6548 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 0 | 1799 | 0 | 87 | 2338 | 29 | 1289 |
| explain-event-flow | 5988 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2338 | 33 | 946 |
| explain-ledger-posting | 5497 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 1307 | 0 | 22 | 2338 | 22 | 953 |
| feature-currency | 7975 | 8 | 198 | 131 | 103 | 537 | 80 | 111 | 5 | 0 | 0 | 0 | 2212 | 0 | 841 | 2338 | 55 | 1356 |
| fix-balance-query | 5582 | 42 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 114 | 2338 | 32 | 1230 |
| fix-ledger-balance | 6544 | 21 | 204 | 76 | 39 | 537 | 36 | 24 | 14 | 0 | 0 | 0 | 1622 | 0 | 257 | 2338 | 52 | 1324 |
| java-explain-orders | 4662 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2338 | 35 | 897 |
| large-change-txtar | 9382 | 58 | 200 | 286 | 153 | 537 | 17 | 141 | 0 | 0 | 0 | 0 | 3623 | 0 | 667 | 2338 | 79 | 1283 |
| large-locate-preorder | 9077 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 15 | 2338 | 42 | 935 |
| locate-topic-name | 4912 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2338 | 25 | 896 |
| multi-feature-discount | 8244 | 48 | 202 | 113 | 68 | 537 | 591 | 12 | 14 | 0 | 0 | 84 | 1316 | 0 | 1282 | 2338 | 117 | 1522 |
| multi-locate-mapping | 5228 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 947 | 0 | 27 | 2338 | 38 | 957 |
| py-explain-reorder | 4738 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2338 | 40 | 917 |
| py-fix-inventory | 5332 | 30 | 200 | 65 | 36 | 537 | 71 | 16 | 10 | 0 | 0 | 0 | 785 | 0 | 74 | 2338 | 43 | 1127 |
| refactor-validate | 8143 | 20 | 196 | 131 | 34 | 537 | 80 | 98 | 6 | 0 | 0 | 0 | 2642 | 0 | 393 | 2338 | 68 | 1600 |
| rename-entries | 6834 | 0 | 200 | 76 | 47 | 537 | 36 | 24 | 36 | 44 | 12 | 0 | 1735 | 0 | 212 | 2338 | 30 | 1507 |
| rust-explain-ledger | 5009 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 50 | 2338 | 41 | 906 |
| session-ledger | 9365 | 668 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 0 | 1791 | 31 | 478 | 2338 | 110 | 2145 |
| session-noisy-tests | 12163 | 367 | 607 | 65 | 74 | 537 | 5304 | 16 | 0 | 0 | 0 | 0 | 809 | 0 | 431 | 2338 | 81 | 1534 |
| session-resume-constraints | 11322 | 831 | 810 | 76 | 204 | 537 | 301 | 48 | 14 | 0 | 0 | 0 | 2479 | 31 | 644 | 2338 | 126 | 2883 |
| session-stale-config | 9164 | 327 | 1022 | 131 | 258 | 537 | 16 | 22 | 4 | 0 | 0 | 0 | 2380 | 72 | 365 | 2338 | 118 | 1574 |
| session-x-tools | 28848 | 1336 | 1175 | 286 | 692 | 537 | 20 | 191 | 95 | 31 | 0 | 0 | 17015 | 0 | 1493 | 2338 | 209 | 3430 |
| ts-fix-cart-discount | 5406 | 0 | 200 | 82 | 36 | 537 | 122 | 54 | 7 | 0 | 0 | 0 | 769 | 0 | 73 | 2338 | 44 | 1144 |
| ts-locate-discount | 4927 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2338 | 30 | 919 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
