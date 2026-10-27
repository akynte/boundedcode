# Direct-mode suite: direct-20261027T195149Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T19:51:49Z, finished 2026-10-27T20:09:19Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.6 model calls and 6.8 tool calls per task, 41065 prompt tokens per task with a 91% prompt-cache hit rate, 1260 generated tokens per task, 7 KB of tool results per task, 46 s per task, 26141 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 45.3 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1786 ms (median). 558 reasoning tokens per task. 3 prompt-cache invalidations (0 after the history was rewritten), 1113 prompt tokens recomputed. Cost of a passed task: 4855 processed tokens and 39 s; of a failed one: 7406 and 112 s. Peak memory: harness 35 MiB, model server 28031 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: empty_answer 1.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.4 | 6.5 | 53081 | 48.4 | 0.2 | 49 |
| java | 1/1 | 1 | 2.0 | 1.0 | 7466 | 8.2 | 0.0 | 8 |
| multi | 1/2 | 0 | 5.5 | 15.5 | 30871 | 110.0 | 0.3 | 110 |
| python | 2/2 | 0 | 3.0 | 3.5 | 11894 | 15.9 | 0.0 | 16 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 15365 | 17.0 | 0.0 | 17 |
| typescript | 2/2 | 0 | 5.0 | 7.5 | 19964 | 19.5 | 0.1 | 20 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 8.0 | 27129 | 80% | 1299 | 49 |
| diagnose | 2/2 | 3.0 | 4.0 | 11874 | 84% | 724 | 25 |
| explain | 5/5 | 2.6 | 2.6 | 10043 | 87% | 336 | 13 |
| feature | 1/2 | 7.5 | 15.0 | 58566 | 87% | 4466 | 138 |
| small-fix | 3/4 | 4.5 | 5.5 | 19765 | 91% | 592 | 24 |
| large-change | 1/1 | 5.0 | 6.0 | 34834 | 86% | 1191 | 44 |
| large-locate | 1/1 | 4.0 | 3.0 | 19667 | 73% | 365 | 21 |
| locate | 3/3 | 4.0 | 6.3 | 15455 | 91% | 335 | 15 |
| refactor | 2/2 | 6.5 | 7.5 | 32510 | 91% | 746 | 30 |
| session | 2/2 | 18.5 | 16.5 | 240668 | 94% | 4603 | 172 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 8 | 27129 | 21828 | 5281 | 1299 | 0 | 0 | 0 | 49 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 11195 | 10206 | 4070 | 373 | 0 | 0 | 0 | 15 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 12554 | 9777 | 5547 | 1076 | 0 | 0 | 0 | 35 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 12323 | 10097 | 5307 | 607 | 0 | 0 | 0 | 21 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 7610 | 6559 | 4136 | 258 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 9 | 10 | 75928 | 66496 | 12489 | 2016 | 0 | 0 | 0 | 76 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 17147 | 15529 | 4695 | 588 | 0 | 0 | 0 | 23 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 24362 | 21814 | 5621 | 685 | 0 | 0 | 0 | 27 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 7466 | 6525 | 4026 | 184 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 6 | 34834 | 29788 | 8119 | 1191 | 0 | 0 | 0 | 44 |  |
| large-locate-preorder | large-locate | chat | pass | 4 | 3 | 19667 | 14297 | 8447 | 365 | 0 | 0 | 0 | 21 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 7109 | 6530 | 3664 | 141 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 6 | 20 | 41205 | 35517 | 8757 | 6917 | 0 | 0 | 0 | 201 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 5 | 11 | 20537 | 18358 | 5252 | 466 | 0 | 0 | 0 | 20 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 7449 | 6485 | 4049 | 222 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 4 | 5 | 16339 | 14850 | 4566 | 606 | 0 | 0 | 0 | 23 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 31006 | 28060 | 6015 | 838 | 0 | 0 | 1 | 33 |  |
| rename-entries | refactor | code | pass | 7 | 9 | 34013 | 31426 | 5652 | 653 | 0 | 0 | 0 | 28 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 15365 | 13835 | 4607 | 408 | 0 | 0 | 0 | 17 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 11 | 78752 | 73135 | 8367 | 3590 | 0 | 0 | 0 | 121 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 25 | 22 | 402583 | 378940 | 26141 | 5616 | 0 | 0 | 2 | 223 | switched to code. |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 8 | 21211 | 19394 | 4890 | 489 | 0 | 0 | 0 | 22 |  |
| ts-locate-discount | locate | chat | pass | 5 | 7 | 18718 | 17274 | 4517 | 399 | 0 | 0 | 0 | 17 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:multi_edit | tool:read | tool:read_many | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5281 | 30 | 200 | 68 | 0 | 356 | 78 | 0 | 21 | 0 | 0 | 0 | 1015 | 0 | 31 | 429 | 1992 | 65 | 996 |
| diagnose-config | 4070 | 0 | 199 | 121 | 0 | 356 | 0 | 0 | 0 | 98 | 0 | 0 | 355 | 0 | 0 | 50 | 1992 | 29 | 870 |
| diagnose-failing-test | 5547 | 18 | 206 | 68 | 0 | 356 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 76 | 1992 | 29 | 899 |
| explain-event-flow | 5307 | 0 | 197 | 121 | 0 | 356 | 0 | 0 | 33 | 0 | 0 | 0 | 1672 | 0 | 0 | 36 | 1992 | 33 | 867 |
| explain-ledger-posting | 4136 | 0 | 203 | 68 | 78 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 1992 | 22 | 787 |
| feature-currency | 12489 | 106 | 198 | 121 | 0 | 356 | 3233 | 45 | 0 | 0 | 0 | 0 | 2032 | 2258 | 0 | 999 | 1992 | 55 | 1094 |
| fix-balance-query | 4695 | 43 | 202 | 68 | 48 | 356 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 123 | 1992 | 32 | 892 |
| fix-ledger-balance | 5621 | 55 | 204 | 68 | 78 | 356 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 0 | 230 | 1992 | 52 | 927 |
| java-explain-orders | 4026 | 0 | 197 | 51 | 54 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 1992 | 35 | 787 |
| large-change-txtar | 8119 | 12 | 200 | 261 | 0 | 356 | 18 | 43 | 21 | 0 | 0 | 0 | 3623 | 0 | 0 | 562 | 1992 | 79 | 952 |
| large-locate-preorder | 8447 | 0 | 201 | 261 | 0 | 356 | 0 | 0 | 84 | 70 | 0 | 0 | 4526 | 0 | 0 | 48 | 1992 | 42 | 867 |
| locate-topic-name | 3664 | 0 | 197 | 121 | 0 | 356 | 0 | 0 | 0 | 157 | 0 | 0 | 0 | 0 | 0 | 17 | 1992 | 25 | 799 |
| multi-feature-discount | 8757 | 0 | 202 | 92 | 130 | 356 | 336 | 19 | 0 | 0 | 0 | 67 | 2904 | 0 | 0 | 1172 | 1992 | 132 | 1355 |
| multi-locate-mapping | 5252 | 0 | 203 | 92 | 0 | 356 | 0 | 0 | 0 | 0 | 28 | 0 | 1417 | 0 | 0 | 85 | 1992 | 38 | 1041 |
| py-explain-reorder | 4049 | 0 | 201 | 57 | 0 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 1992 | 40 | 806 |
| py-fix-inventory | 4566 | 0 | 200 | 57 | 46 | 356 | 20 | 17 | 10 | 0 | 0 | 0 | 785 | 0 | 0 | 113 | 1992 | 43 | 927 |
| refactor-validate | 6015 | 28 | 196 | 121 | 73 | 356 | 81 | 74 | 6 | 0 | 0 | 0 | 1565 | 0 | 0 | 499 | 1992 | 68 | 956 |
| rename-entries | 5652 | 59 | 200 | 68 | 0 | 356 | 37 | 24 | 32 | 44 | 0 | 0 | 1599 | 0 | 0 | 155 | 1992 | 30 | 1056 |
| rust-explain-ledger | 4607 | 0 | 205 | 75 | 0 | 356 | 0 | 0 | 4 | 0 | 18 | 0 | 928 | 0 | 0 | 83 | 1992 | 41 | 905 |
| session-ledger | 8367 | 929 | 798 | 68 | 0 | 356 | 270 | 49 | 0 | 0 | 0 | 0 | 1791 | 0 | 31 | 758 | 1992 | 110 | 1215 |
| session-x-tools | 26141 | 1016 | 1175 | 261 | 0 | 356 | 129 | 293 | 157 | 31 | 13 | 0 | 17756 | 0 | 0 | 1148 | 1992 | 209 | 1605 |
| ts-fix-cart-discount | 4890 | 27 | 200 | 68 | 0 | 356 | 123 | 9 | 12 | 0 | 0 | 0 | 964 | 0 | 0 | 94 | 1992 | 44 | 1001 |
| ts-locate-discount | 4517 | 0 | 199 | 68 | 0 | 356 | 0 | 0 | 16 | 0 | 15 | 0 | 821 | 0 | 0 | 57 | 1992 | 30 | 963 |

Environment: build dev 63b72d496420; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
