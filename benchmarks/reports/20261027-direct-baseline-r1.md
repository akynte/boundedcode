# Direct-mode suite: direct-20261027T115637Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T11:56:37Z, finished 2026-10-27T12:16:01Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**22/23 passed (96%)**, 6.3 model calls and 7.0 tool calls per task, 52645 prompt tokens per task with a 92% prompt-cache hit rate, 1350 generated tokens per task, 8 KB of tool results per task, 51 s per task, 37305 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 6 broken edits caught by the check, 1 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 50.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1827 ms (median). 524 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 614 prompt tokens recomputed. Cost of a passed task: 5657 processed tokens and 49 s; of a failed one: 6754 and 87 s. Peak memory: harness 37 MiB, model server 28007 MiB, GPU 7141 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/15 | 0 | 7.1 | 7.3 | 68405 | 63.1 | 0.2 | 64 |
| java | 1/1 | 1 | 2.0 | 1.0 | 7466 | 7.6 | 0.0 | 8 |
| multi | 1/2 | 0 | 7.5 | 11.0 | 41944 | 50.4 | 0.2 | 51 |
| python | 2/2 | 0 | 4.5 | 5.0 | 19225 | 22.6 | 0.0 | 23 |
| rust | 1/1 | 1 | 6.0 | 7.0 | 23243 | 21.8 | 0.0 | 22 |
| typescript | 2/2 | 0 | 4.0 | 5.0 | 15865 | 16.2 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 7.0 | 7.0 | 45170 | 83% | 2088 | 76 |
| diagnose | 2/2 | 3.0 | 4.0 | 12000 | 84% | 837 | 28 |
| explain | 5/5 | 3.0 | 3.0 | 11711 | 88% | 365 | 15 |
| feature | 1/2 | 14.0 | 18.5 | 103969 | 94% | 2708 | 99 |
| small-fix | 4/4 | 5.2 | 5.8 | 23310 | 91% | 619 | 26 |
| large-change | 1/1 | 6.0 | 7.0 | 43451 | 88% | 1215 | 47 |
| large-locate | 1/1 | 4.0 | 3.0 | 20049 | 72% | 467 | 24 |
| locate | 3/3 | 2.7 | 4.0 | 10166 | 87% | 331 | 13 |
| refactor | 2/2 | 6.5 | 7.5 | 32977 | 90% | 724 | 30 |
| session | 2/2 | 19.0 | 16.5 | 310995 | 94% | 6727 | 243 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 7 | 7 | 45170 | 37567 | 7579 | 2088 | 0 | 0 | 1 | 76 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 11473 | 10345 | 4209 | 436 | 0 | 0 | 0 | 17 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 12527 | 9763 | 5534 | 1238 | 0 | 0 | 0 | 39 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 12789 | 10330 | 5540 | 637 | 0 | 0 | 0 | 23 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 7610 | 6559 | 4136 | 249 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 16 | 21 | 135845 | 127282 | 11592 | 2943 | 0 | 0 | 2 | 111 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 17171 | 15541 | 4707 | 648 | 0 | 0 | 0 | 25 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 24393 | 21828 | 5638 | 590 | 0 | 0 | 0 | 25 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 7466 | 6525 | 4026 | 160 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 7 | 43451 | 38144 | 8376 | 1215 | 0 | 0 | 0 | 47 |  |
| large-locate-preorder | large-locate | chat | pass | 4 | 3 | 20049 | 14488 | 8638 | 467 | 0 | 0 | 0 | 24 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 7646 | 6530 | 4201 | 268 | 0 | 0 | 0 | 11 |  |
| multi-feature-discount | feature | code | FAIL | 12 | 16 | 72093 | 67812 | 7326 | 2473 | 0 | 0 | 0 | 87 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 3 | 6 | 11796 | 10054 | 4823 | 347 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 7449 | 6485 | 4049 | 255 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 7 | 8 | 31001 | 28671 | 5395 | 865 | 0 | 0 | 0 | 35 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 31881 | 28003 | 6947 | 719 | 0 | 0 | 0 | 30 |  |
| rename-entries | refactor | code | pass | 7 | 9 | 34073 | 31498 | 5640 | 728 | 0 | 0 | 0 | 29 |  |
| rust-explain-ledger | explain | chat | pass | 6 | 7 | 23243 | 21418 | 4894 | 526 | 0 | 0 | 0 | 22 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 60263 | 55466 | 7555 | 2119 | 0 | 0 | 0 | 76 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 28 | 24 | 561727 | 527403 | 37305 | 11335 | 0 | 0 | 3 | 410 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 6 | 20674 | 19022 | 4725 | 372 | 0 | 0 | 0 | 18 |  |
| ts-locate-discount | locate | chat | pass | 3 | 4 | 11056 | 9969 | 4168 | 378 | 0 | 0 | 0 | 14 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:multi_edit | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 7579 | 32 | 200 | 68 | 0 | 356 | 2272 | 0 | 0 | 0 | 0 | 0 | 712 | 0 | 92 | 805 | 1992 | 65 | 985 |
| diagnose-config | 4209 | 0 | 199 | 121 | 0 | 356 | 0 | 0 | 0 | 171 | 0 | 0 | 355 | 0 | 0 | 124 | 1992 | 29 | 862 |
| diagnose-failing-test | 5534 | 19 | 206 | 68 | 0 | 356 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 70 | 1992 | 29 | 891 |
| explain-event-flow | 5540 | 0 | 197 | 121 | 0 | 356 | 0 | 0 | 28 | 224 | 0 | 0 | 1672 | 0 | 0 | 42 | 1992 | 33 | 875 |
| explain-ledger-posting | 4136 | 0 | 203 | 68 | 78 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 1992 | 22 | 787 |
| feature-currency | 11592 | 165 | 198 | 121 | 0 | 356 | 875 | 175 | 32 | 0 | 0 | 0 | 4483 | 152 | 0 | 1552 | 1992 | 55 | 1436 |
| fix-balance-query | 4707 | 60 | 202 | 68 | 48 | 356 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 117 | 1992 | 32 | 893 |
| fix-ledger-balance | 5638 | 69 | 204 | 68 | 78 | 356 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 0 | 233 | 1992 | 52 | 927 |
| java-explain-orders | 4026 | 0 | 197 | 51 | 54 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 1992 | 35 | 787 |
| large-change-txtar | 8376 | 6 | 200 | 261 | 0 | 356 | 18 | 141 | 0 | 0 | 13 | 0 | 3623 | 0 | 0 | 693 | 1992 | 79 | 994 |
| large-locate-preorder | 8638 | 0 | 201 | 261 | 0 | 356 | 0 | 0 | 84 | 251 | 0 | 0 | 4526 | 0 | 0 | 51 | 1992 | 42 | 874 |
| locate-topic-name | 4201 | 0 | 197 | 121 | 0 | 356 | 0 | 0 | 0 | 551 | 0 | 0 | 0 | 0 | 0 | 115 | 1992 | 25 | 844 |
| multi-feature-discount | 7326 | 117 | 202 | 92 | 130 | 356 | 172 | 111 | 0 | 0 | 0 | 0 | 1316 | 0 | 0 | 1366 | 1992 | 117 | 1355 |
| multi-locate-mapping | 4823 | 0 | 203 | 92 | 0 | 356 | 0 | 0 | 26 | 0 | 0 | 0 | 1155 | 0 | 0 | 54 | 1992 | 38 | 907 |
| py-explain-reorder | 4049 | 0 | 201 | 57 | 0 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 1992 | 40 | 806 |
| py-fix-inventory | 5395 | 55 | 200 | 57 | 46 | 356 | 103 | 34 | 0 | 0 | 13 | 0 | 1229 | 0 | 0 | 229 | 1992 | 43 | 1038 |
| refactor-validate | 6947 | 54 | 196 | 121 | 73 | 356 | 81 | 0 | 12 | 0 | 0 | 24 | 2630 | 0 | 0 | 373 | 1992 | 68 | 967 |
| rename-entries | 5640 | 81 | 200 | 68 | 0 | 356 | 37 | 24 | 32 | 6 | 0 | 0 | 1599 | 0 | 0 | 160 | 1992 | 30 | 1055 |
| rust-explain-ledger | 4894 | 0 | 205 | 75 | 0 | 356 | 0 | 0 | 12 | 0 | 18 | 0 | 1119 | 0 | 0 | 102 | 1992 | 41 | 974 |
| session-ledger | 7555 | 593 | 798 | 68 | 0 | 356 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 0 | 31 | 477 | 1992 | 110 | 1127 |
| session-x-tools | 37305 | 1053 | 1175 | 261 | 0 | 356 | 42 | 359 | 157 | 0 | 13 | 33 | 27036 | 0 | 0 | 2970 | 1992 | 209 | 1649 |
| ts-fix-cart-discount | 4725 | 41 | 200 | 68 | 0 | 356 | 121 | 9 | 12 | 0 | 0 | 0 | 847 | 0 | 0 | 78 | 1992 | 44 | 957 |
| ts-locate-discount | 4168 | 0 | 199 | 68 | 0 | 356 | 0 | 0 | 7 | 0 | 0 | 0 | 620 | 0 | 0 | 30 | 1992 | 30 | 866 |

Environment: build dev 8e4caea75a51; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server 0.00.000.258 I srv  llama_server: initializing ... | version: 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
