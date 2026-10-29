# Direct-mode suite: direct-20261029T100557Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T10:05:57Z, finished 2026-10-29T10:26:59Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**24/26 passed (92%)**, 6.2 model calls and 6.0 tool calls per task, 56604 prompt tokens per task with a 92% prompt-cache hit rate, 1358 generated tokens per task, 10 KB of tool results per task, 49 s per task, 35895 tokens in the largest single prompt; 4 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 1, 102 KB → 5 KB. Freshness notes: 1.

Where the agent's time went, per task: 48.1 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1773 ms (median). 634 reasoning tokens per task. 4 prompt-cache invalidations (4 after the history was rewritten), 27509 prompt tokens recomputed; 4548 prompt tokens evaluated by the server per task, in 10.8 s. Cost of a passed task: 6075 processed tokens and 48 s; of a failed one: 3872 and 54 s. Peak memory: harness 40 MiB, model server 29041 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 7.0 | 6.3 | 71105 | 59.0 | 0.3 | 60 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8605 | 7.9 | 0.0 | 8 |
| multi | 1/2 | 0 | 7.0 | 12.0 | 47530 | 51.8 | 0.4 | 52 |
| python | 3/3 | 0 | 5.3 | 4.7 | 37691 | 30.3 | 0.1 | 31 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 18067 | 16.8 | 0.0 | 17 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14059 | 14.0 | 0.1 | 14 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 6.0 | 33737 | 85% | 1729 | 56 |
| diagnose | 2/2 | 3.0 | 3.0 | 14414 | 87% | 624 | 22 |
| explain | 5/5 | 2.4 | 2.2 | 10829 | 88% | 344 | 13 |
| feature | 1/2 | 9.0 | 13.0 | 61756 | 96% | 2223 | 70 |
| small-fix | 3/4 | 4.5 | 5.0 | 22546 | 93% | 494 | 19 |
| large-change | 1/1 | 5.0 | 5.0 | 38748 | 88% | 971 | 34 |
| large-locate | 1/1 | 2.0 | 1.0 | 13414 | 61% | 305 | 17 |
| locate | 3/3 | 2.0 | 2.3 | 9060 | 86% | 271 | 11 |
| refactor | 2/2 | 7.5 | 9.5 | 44539 | 94% | 981 | 35 |
| session | 5/5 | 14.6 | 11.2 | 194576 | 93% | 4025 | 146 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 6 | 33737 | 28759 | 6588 | 1729 | 0 | 0 | 1 | 56 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8669 | 7840 | 4564 | 279 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 5 | 20159 | 17360 | 6353 | 970 | 0 | 0 | 0 | 33 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10079 | 7903 | 5946 | 603 | 0 | 0 | 0 | 20 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8695 | 7709 | 4697 | 238 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 6 | 6 | 37794 | 35410 | 7363 | 1535 | 0 | 0 | 0 | 47 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 19592 | 18210 | 5319 | 417 | 0 | 0 | 0 | 16 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 7 | 27354 | 25213 | 6373 | 643 | 0 | 0 | 0 | 24 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8605 | 7699 | 4611 | 174 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 5 | 38748 | 34268 | 8936 | 971 | 0 | 0 | 0 | 34 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13414 | 8135 | 9030 | 305 | 0 | 0 | 0 | 17 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8951 | 7793 | 4867 | 129 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 12 | 20 | 85717 | 82682 | 9378 | 2911 | 0 | 0 | 0 | 92 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 4 | 9343 | 7933 | 5263 | 336 | 0 | 0 | 0 | 13 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8700 | 7740 | 4693 | 250 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24008 | 22757 | 5286 | 545 | 0 | 0 | 0 | 20 |  |
| refactor-validate | refactor | code | pass | 9 | 9 | 54922 | 51460 | 8019 | 1157 | 0 | 0 | 1 | 41 |  |
| rename-entries | refactor | code | pass | 6 | 10 | 34156 | 31905 | 6619 | 805 | 0 | 0 | 0 | 29 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 18067 | 16684 | 5360 | 455 | 0 | 0 | 0 | 17 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 77676 | 73836 | 9042 | 2447 | 0 | 0 | 0 | 80 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 9 | 7 | 80364 | 73500 | 11510 | 1610 | 0 | 0 | 0 | 62 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 12 | 10 | 86800 | 83371 | 9457 | 2632 | 0 | 0 | 0 | 84 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 82711 | 78521 | 9045 | 1307 | 0 | 0 | 0 | 53 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 29 | 23 | 645330 | 591171 | 35895 | 12130 | 4 | 0 | 0 | 450 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19231 | 17864 | 5263 | 370 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8887 | 7742 | 4882 | 349 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6588 | 0 | 200 | 76 | 35 | 537 | 76 | 0 | 4 | 0 | 0 | 688 | 93 | 732 | 2297 | 65 | 1785 |
| diagnose-config | 4564 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2297 | 29 | 919 |
| diagnose-failing-test | 6353 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 0 | 1788 | 0 | 70 | 2297 | 29 | 1177 |
| explain-event-flow | 5946 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2297 | 33 | 945 |
| explain-ledger-posting | 4697 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2297 | 22 | 893 |
| feature-currency | 7363 | 46 | 198 | 131 | 103 | 537 | 194 | 45 | 0 | 0 | 0 | 1565 | 0 | 906 | 2297 | 55 | 1286 |
| fix-balance-query | 5319 | 0 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 113 | 2297 | 32 | 1051 |
| fix-ledger-balance | 6373 | 37 | 204 | 76 | 39 | 537 | 36 | 24 | 29 | 0 | 0 | 1598 | 0 | 249 | 2297 | 52 | 1195 |
| java-explain-orders | 4611 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2297 | 35 | 887 |
| large-change-txtar | 8936 | 24 | 200 | 286 | 153 | 537 | 19 | 43 | 0 | 0 | 0 | 3623 | 0 | 554 | 2297 | 79 | 1121 |
| large-locate-preorder | 9030 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 15 | 2297 | 42 | 929 |
| locate-topic-name | 4867 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2297 | 25 | 892 |
| multi-feature-discount | 9378 | 45 | 202 | 113 | 68 | 537 | 602 | 146 | 14 | 0 | 0 | 1558 | 0 | 1670 | 2297 | 117 | 2009 |
| multi-locate-mapping | 5263 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 5 | 0 | 0 | 947 | 0 | 36 | 2297 | 38 | 1019 |
| py-explain-reorder | 4693 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2297 | 40 | 913 |
| py-fix-inventory | 5286 | 27 | 200 | 65 | 36 | 537 | 19 | 16 | 10 | 0 | 0 | 785 | 0 | 104 | 2297 | 43 | 1147 |
| refactor-validate | 8019 | 35 | 196 | 131 | 34 | 537 | 80 | 96 | 6 | 0 | 0 | 2653 | 0 | 508 | 2297 | 68 | 1378 |
| rename-entries | 6619 | 29 | 200 | 76 | 47 | 537 | 36 | 24 | 29 | 6 | 0 | 1697 | 0 | 176 | 2297 | 30 | 1435 |
| rust-explain-ledger | 5360 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 20 | 928 | 0 | 81 | 2297 | 41 | 1131 |
| session-ledger | 9042 | 505 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 31 | 500 | 2297 | 110 | 2004 |
| session-noisy-tests | 11510 | 453 | 607 | 65 | 74 | 537 | 4581 | 16 | 0 | 0 | 0 | 809 | 0 | 421 | 2297 | 81 | 1569 |
| session-resume-constraints | 9457 | 779 | 810 | 76 | 183 | 537 | 186 | 24 | 14 | 0 | 0 | 1791 | 31 | 400 | 2297 | 126 | 2203 |
| session-stale-config | 9045 | 269 | 1022 | 131 | 258 | 537 | 72 | 0 | 4 | 0 | 0 | 2380 | 28 | 305 | 2297 | 118 | 1624 |
| session-x-tools | 35895 | 877 | 976 | 286 | 492 | 537 | 68 | 63 | 179 | 26 | 0 | 22021 | 0 | 953 | 2297 | 179 | 6941 |
| ts-fix-cart-discount | 5263 | 21 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 63 | 2297 | 44 | 1038 |
| ts-locate-discount | 4882 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2297 | 30 | 915 |

Environment: build dev 74e6f0c1c132; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
