# Direct-mode suite: direct-20261027T213333Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T21:33:33Z, finished 2026-10-27T21:48:08Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**22/23 passed (96%)**, 5.9 model calls and 6.8 tool calls per task, 43003 prompt tokens per task with a 92% prompt-cache hit rate, 986 generated tokens per task, 7 KB of tool results per task, 38 s per task, 26263 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 37.7 s in model calls, 0.2 s in tools (with the checks after edits), 0.02 s in the harness itself; time to first token 1770 ms (median). 278 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 614 prompt tokens recomputed. Cost of a passed task: 4530 processed tokens and 36 s; of a failed one: 6651 and 81 s. Peak memory: harness 34 MiB, model server 28822 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/15 | 0 | 6.7 | 7.2 | 54558 | 44.8 | 0.2 | 45 |
| java | 1/1 | 1 | 2.0 | 2.0 | 8186 | 8.0 | 0.0 | 8 |
| multi | 1/2 | 0 | 6.0 | 9.5 | 33577 | 47.4 | 0.2 | 48 |
| python | 2/2 | 0 | 5.0 | 5.0 | 22772 | 23.1 | 0.0 | 23 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 16089 | 14.7 | 0.0 | 15 |
| typescript | 2/2 | 0 | 4.0 | 6.0 | 16866 | 15.6 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 8.0 | 30584 | 91% | 1355 | 45 |
| diagnose | 2/2 | 3.0 | 2.0 | 12036 | 87% | 415 | 16 |
| explain | 5/5 | 2.8 | 3.2 | 11684 | 87% | 366 | 14 |
| feature | 1/2 | 8.5 | 13.0 | 53647 | 92% | 2068 | 71 |
| small-fix | 4/4 | 5.5 | 6.2 | 25684 | 92% | 624 | 26 |
| large-change | 1/1 | 10.0 | 11.0 | 86963 | 91% | 1879 | 73 |
| large-locate | 1/1 | 4.0 | 3.0 | 20417 | 74% | 459 | 24 |
| locate | 3/3 | 3.3 | 5.0 | 13448 | 89% | 334 | 14 |
| refactor | 2/2 | 7.5 | 10.0 | 40840 | 92% | 950 | 37 |
| session | 2/2 | 16.0 | 14.0 | 218287 | 94% | 3398 | 133 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 8 | 30584 | 27800 | 6034 | 1355 | 0 | 0 | 0 | 45 |  |
| diagnose-config | diagnose | chat | pass | 3 | 2 | 11674 | 10717 | 4219 | 373 | 0 | 0 | 0 | 14 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 2 | 12397 | 10306 | 5042 | 457 | 0 | 0 | 0 | 18 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 13342 | 10878 | 5726 | 653 | 0 | 0 | 0 | 23 |  |
| explain-ledger-posting | explain | chat | pass | 3 | 3 | 12990 | 11558 | 4694 | 461 | 0 | 0 | 0 | 17 |  |
| feature-currency | feature | code | pass | 8 | 13 | 52479 | 47667 | 8054 | 1674 | 0 | 0 | 1 | 60 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 17799 | 16217 | 4840 | 691 | 0 | 0 | 0 | 25 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 25071 | 22621 | 5704 | 497 | 0 | 0 | 0 | 22 |  |
| java-explain-orders | explain | chat | pass | 2 | 2 | 8186 | 6887 | 4565 | 167 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 10 | 11 | 86963 | 78804 | 11393 | 1879 | 0 | 0 | 1 | 73 |  |
| large-locate-preorder | large-locate | chat | pass | 4 | 3 | 20417 | 15034 | 8641 | 459 | 0 | 0 | 0 | 24 |  |
| locate-topic-name | locate | chat | pass | 4 | 5 | 16405 | 14786 | 4877 | 367 | 0 | 0 | 0 | 16 |  |
| multi-feature-discount | feature | code | FAIL | 9 | 13 | 54815 | 50625 | 7428 | 2461 | 0 | 0 | 0 | 81 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 3 | 6 | 12339 | 10597 | 5004 | 341 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 7811 | 6847 | 4230 | 225 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 8 | 8 | 37733 | 35250 | 5725 | 919 | 0 | 0 | 0 | 37 |  |
| refactor-validate | refactor | code | pass | 8 | 9 | 45338 | 41238 | 7342 | 1003 | 0 | 0 | 1 | 41 |  |
| rename-entries | refactor | code | pass | 7 | 11 | 36341 | 33574 | 6013 | 897 | 0 | 0 | 0 | 34 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 16089 | 14559 | 4788 | 326 | 0 | 0 | 0 | 15 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 62377 | 57535 | 7781 | 2081 | 0 | 0 | 0 | 76 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 22 | 19 | 374197 | 351120 | 26263 | 4714 | 0 | 0 | 0 | 191 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 8 | 22132 | 20309 | 5077 | 391 | 0 | 0 | 0 | 19 |  |
| ts-locate-discount | locate | chat | pass | 3 | 4 | 11599 | 10512 | 4349 | 294 | 0 | 0 | 0 | 12 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:read | tool:read_many | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6034 | 0 | 200 | 68 | 0 | 537 | 78 | 0 | 21 | 0 | 0 | 1623 | 0 | 31 | 430 | 1992 | 65 | 989 |
| diagnose-config | 4219 | 0 | 199 | 121 | 0 | 537 | 0 | 0 | 0 | 95 | 0 | 355 | 0 | 0 | 61 | 1992 | 29 | 830 |
| diagnose-failing-test | 5042 | 19 | 206 | 68 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1178 | 0 | 62 | 1992 | 29 | 836 |
| explain-event-flow | 5726 | 0 | 197 | 121 | 0 | 537 | 0 | 0 | 28 | 224 | 0 | 1672 | 0 | 0 | 47 | 1992 | 33 | 875 |
| explain-ledger-posting | 4694 | 0 | 203 | 68 | 78 | 537 | 0 | 0 | 6 | 0 | 0 | 911 | 0 | 0 | 33 | 1992 | 22 | 844 |
| feature-currency | 8054 | 46 | 198 | 121 | 0 | 537 | 169 | 111 | 33 | 0 | 0 | 2769 | 0 | 0 | 887 | 1992 | 55 | 1136 |
| fix-balance-query | 4840 | 36 | 202 | 68 | 48 | 537 | 38 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 89 | 1992 | 32 | 897 |
| fix-ledger-balance | 5704 | 47 | 204 | 68 | 78 | 537 | 37 | 24 | 0 | 0 | 0 | 1598 | 0 | 0 | 135 | 1992 | 52 | 932 |
| java-explain-orders | 4565 | 0 | 197 | 51 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 869 | 0 | 0 | 23 | 1992 | 35 | 807 |
| large-change-txtar | 11393 | 55 | 200 | 261 | 0 | 537 | 33 | 304 | 21 | 0 | 0 | 5866 | 0 | 0 | 883 | 1992 | 79 | 1162 |
| large-locate-preorder | 8641 | 0 | 201 | 261 | 0 | 537 | 0 | 0 | 84 | 70 | 0 | 4526 | 0 | 0 | 56 | 1992 | 42 | 872 |
| locate-topic-name | 4877 | 0 | 197 | 121 | 0 | 537 | 234 | 0 | 0 | 6 | 0 | 714 | 0 | 0 | 114 | 1992 | 25 | 937 |
| multi-feature-discount | 7428 | 56 | 202 | 92 | 130 | 537 | 177 | 77 | 0 | 0 | 0 | 1316 | 0 | 0 | 1544 | 1992 | 117 | 1188 |
| multi-locate-mapping | 5004 | 0 | 203 | 92 | 0 | 537 | 0 | 0 | 26 | 0 | 0 | 1155 | 0 | 0 | 54 | 1992 | 38 | 907 |
| py-explain-reorder | 4230 | 0 | 201 | 57 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 1992 | 40 | 806 |
| py-fix-inventory | 5725 | 101 | 200 | 57 | 46 | 537 | 86 | 34 | 0 | 0 | 0 | 1299 | 0 | 0 | 292 | 1992 | 43 | 1038 |
| refactor-validate | 7342 | 51 | 196 | 121 | 73 | 537 | 81 | 96 | 12 | 0 | 0 | 2655 | 0 | 0 | 389 | 1992 | 68 | 1071 |
| rename-entries | 6013 | 88 | 200 | 68 | 0 | 537 | 37 | 24 | 32 | 6 | 0 | 1721 | 0 | 0 | 183 | 1992 | 30 | 1095 |
| rust-explain-ledger | 4788 | 0 | 205 | 75 | 0 | 537 | 0 | 0 | 4 | 0 | 18 | 928 | 0 | 0 | 83 | 1992 | 41 | 905 |
| session-ledger | 7781 | 633 | 798 | 68 | 0 | 537 | 188 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 485 | 1992 | 110 | 1124 |
| session-x-tools | 26263 | 753 | 1175 | 261 | 0 | 537 | 21 | 94 | 179 | 941 | 0 | 17184 | 0 | 0 | 1467 | 1992 | 209 | 1450 |
| ts-fix-cart-discount | 5077 | 37 | 200 | 68 | 0 | 537 | 123 | 9 | 12 | 0 | 0 | 964 | 0 | 0 | 90 | 1992 | 44 | 1001 |
| ts-locate-discount | 4349 | 0 | 199 | 68 | 0 | 537 | 0 | 0 | 7 | 0 | 0 | 620 | 0 | 0 | 30 | 1992 | 30 | 866 |

Environment: build dev a4311aeaf04e-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
