# Direct-mode suite: direct-20261027T122924Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T12:29:24Z, finished 2026-10-27T12:42:38Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.4 model calls and 6.1 tool calls per task, 40081 prompt tokens per task with a 91% prompt-cache hit rate, 878 generated tokens per task, 7 KB of tool results per task, 35 s per task, 32719 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 4 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 34.2 s in model calls, 0.1 s in tools (with the checks after edits), 0.02 s in the harness itself; time to first token 1737 ms (median). 229 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 614 prompt tokens recomputed. Cost of a passed task: 4573 processed tokens and 33 s; of a failed one: 4322 and 50 s. Peak memory: harness 35 MiB, model server 29676 MiB, GPU 7141 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/15 | 0 | 6.1 | 6.7 | 51390 | 39.8 | 0.2 | 40 |
| java | 1/1 | 1 | 2.0 | 1.0 | 7466 | 7.9 | 0.0 | 8 |
| multi | 1/2 | 0 | 6.0 | 7.5 | 32876 | 47.9 | 0.2 | 48 |
| python | 2/2 | 0 | 3.5 | 4.0 | 14561 | 18.5 | 0.0 | 19 |
| rust | 0/1 | 1 | 4.0 | 4.0 | 16848 | 13.3 | 0.0 | 13 |
| typescript | 2/2 | 0 | 4.0 | 5.5 | 15918 | 17.4 | 0.1 | 18 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 7.0 | 8.0 | 32391 | 93% | 1241 | 44 |
| diagnose | 2/2 | 3.0 | 4.0 | 11935 | 84% | 639 | 22 |
| explain | 4/5 | 2.6 | 2.6 | 10447 | 87% | 294 | 12 |
| feature | 1/2 | 8.5 | 13.0 | 50772 | 91% | 2149 | 73 |
| small-fix | 4/4 | 5.8 | 6.8 | 26452 | 92% | 854 | 32 |
| large-change | 1/1 | 5.0 | 6.0 | 34941 | 86% | 879 | 35 |
| large-locate | 1/1 | 5.0 | 5.0 | 23843 | 77% | 584 | 28 |
| locate | 3/3 | 2.3 | 2.0 | 8745 | 88% | 272 | 11 |
| refactor | 2/2 | 5.5 | 7.0 | 27317 | 90% | 740 | 29 |
| session | 2/2 | 15.5 | 13.5 | 233182 | 93% | 2366 | 109 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 7 | 8 | 32391 | 29971 | 5485 | 1241 | 0 | 0 | 2 | 44 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 11343 | 10280 | 4144 | 362 | 0 | 0 | 0 | 14 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 12527 | 9763 | 5534 | 916 | 0 | 0 | 0 | 30 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 5 | 12863 | 10367 | 5577 | 632 | 0 | 0 | 0 | 22 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 7610 | 6559 | 4136 | 209 | 0 | 0 | 0 | 9 |  |
| feature-currency | feature | code | pass | 7 | 12 | 43736 | 38854 | 7947 | 1663 | 0 | 0 | 1 | 58 |  |
| fix-balance-query | small-fix | code | pass | 7 | 8 | 33271 | 30939 | 5397 | 1433 | 0 | 0 | 0 | 49 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 6 | 30083 | 27429 | 5723 | 771 | 0 | 0 | 0 | 30 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 7466 | 6525 | 4026 | 175 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 6 | 34941 | 29897 | 8117 | 879 | 0 | 0 | 0 | 35 |  |
| large-locate-preorder | large-locate | chat | pass | 5 | 5 | 23843 | 18343 | 8573 | 584 | 0 | 0 | 0 | 28 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 7237 | 6530 | 3792 | 171 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | FAIL | 10 | 14 | 57808 | 53649 | 7212 | 2635 | 0 | 0 | 0 | 87 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 DiscountCents:5000 TotalCents:0} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 1 | 7943 | 6520 | 4508 | 211 | 0 | 0 | 0 | 10 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 7449 | 6485 | 4049 | 174 | 0 | 0 | 0 | 8 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 6 | 21673 | 19910 | 4836 | 804 | 0 | 0 | 0 | 29 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 31053 | 28092 | 6030 | 755 | 0 | 0 | 1 | 30 |  |
| rename-entries | refactor | code | pass | 5 | 8 | 23581 | 21022 | 5632 | 724 | 0 | 0 | 0 | 27 |  |
| rust-explain-ledger | explain | chat | FAIL | 4 | 4 | 16848 | 15279 | 4646 | 280 | 0 | 0 | 0 | 13 | answer does not match /NonPositiveAmount/; answer does not match /(\b0\b\|zero)/ |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 60020 | 55249 | 7529 | 1925 | 0 | 0 | 0 | 71 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 21 | 18 | 406344 | 376634 | 32719 | 2808 | 0 | 0 | 0 | 146 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 7 | 20781 | 19110 | 4744 | 408 | 0 | 0 | 0 | 20 |  |
| ts-locate-discount | locate | chat | pass | 3 | 4 | 11056 | 9969 | 4168 | 433 | 0 | 0 | 0 | 16 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:read | tool:read_many | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5485 | 15 | 200 | 68 | 0 | 356 | 46 | 79 | 21 | 0 | 0 | 712 | 0 | 61 | 839 | 1992 | 65 | 1031 |
| diagnose-config | 4144 | 0 | 199 | 121 | 0 | 356 | 0 | 0 | 0 | 171 | 0 | 355 | 0 | 0 | 51 | 1992 | 29 | 870 |
| diagnose-failing-test | 5534 | 19 | 206 | 68 | 0 | 356 | 115 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 70 | 1992 | 29 | 891 |
| explain-event-flow | 5577 | 0 | 197 | 121 | 0 | 356 | 0 | 0 | 33 | 224 | 0 | 1672 | 0 | 0 | 50 | 1992 | 33 | 899 |
| explain-ledger-posting | 4136 | 0 | 203 | 68 | 78 | 356 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 1992 | 22 | 787 |
| feature-currency | 7947 | 52 | 198 | 121 | 0 | 356 | 115 | 111 | 33 | 0 | 0 | 2769 | 0 | 0 | 1071 | 1992 | 55 | 1074 |
| fix-balance-query | 5397 | 128 | 202 | 68 | 48 | 356 | 153 | 24 | 14 | 0 | 0 | 1180 | 0 | 0 | 170 | 1992 | 32 | 1030 |
| fix-ledger-balance | 5723 | 47 | 204 | 68 | 78 | 356 | 83 | 24 | 0 | 0 | 0 | 1598 | 0 | 0 | 250 | 1992 | 52 | 971 |
| java-explain-orders | 4026 | 0 | 197 | 51 | 54 | 356 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 1992 | 35 | 787 |
| large-change-txtar | 8117 | 0 | 200 | 261 | 0 | 356 | 18 | 43 | 0 | 0 | 0 | 3677 | 0 | 0 | 538 | 1992 | 79 | 953 |
| large-locate-preorder | 8573 | 0 | 201 | 261 | 0 | 356 | 0 | 0 | 84 | 76 | 0 | 4526 | 0 | 0 | 95 | 1992 | 42 | 940 |
| locate-topic-name | 3792 | 0 | 197 | 121 | 0 | 356 | 0 | 0 | 0 | 248 | 0 | 0 | 0 | 0 | 55 | 1992 | 25 | 798 |
| multi-feature-discount | 7212 | 93 | 202 | 92 | 130 | 356 | 143 | 99 | 0 | 0 | 0 | 1316 | 0 | 0 | 1418 | 1992 | 117 | 1254 |
| multi-locate-mapping | 4508 | 0 | 203 | 92 | 0 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 1022 | 0 | 14 | 1992 | 38 | 791 |
| py-explain-reorder | 4049 | 0 | 201 | 57 | 0 | 356 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 1992 | 40 | 806 |
| py-fix-inventory | 4836 | 50 | 200 | 57 | 46 | 356 | 20 | 17 | 0 | 0 | 13 | 932 | 0 | 0 | 156 | 1992 | 43 | 954 |
| refactor-validate | 6030 | 42 | 196 | 121 | 73 | 356 | 81 | 74 | 6 | 0 | 0 | 1565 | 0 | 0 | 500 | 1992 | 68 | 956 |
| rename-entries | 5632 | 37 | 200 | 68 | 0 | 356 | 62 | 24 | 0 | 25 | 0 | 1599 | 0 | 0 | 208 | 1992 | 30 | 1031 |
| rust-explain-ledger | 4646 | 0 | 205 | 75 | 0 | 356 | 0 | 0 | 4 | 0 | 0 | 1060 | 0 | 0 | 35 | 1992 | 41 | 878 |
| session-ledger | 7529 | 572 | 798 | 68 | 0 | 356 | 188 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 475 | 1992 | 110 | 1124 |
| session-x-tools | 32719 | 943 | 1175 | 261 | 0 | 356 | 21 | 63 | 241 | 0 | 13 | 25365 | 0 | 0 | 618 | 1992 | 209 | 1462 |
| ts-fix-cart-discount | 4744 | 33 | 200 | 68 | 0 | 356 | 120 | 9 | 12 | 0 | 0 | 847 | 0 | 0 | 82 | 1992 | 44 | 981 |
| ts-locate-discount | 4168 | 0 | 199 | 68 | 0 | 356 | 0 | 0 | 7 | 0 | 0 | 620 | 0 | 0 | 30 | 1992 | 30 | 866 |

Environment: build dev 8e4caea75a51; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server 0.00.000.167 I srv  llama_server: initializing ... | version: 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
