# Direct-mode suite: direct-20261028T160827Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T16:08:27Z, finished 2026-10-28T16:22:10Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.5 model calls and 5.8 tool calls per task, 44843 prompt tokens per task with a 93% prompt-cache hit rate, 1032 generated tokens per task, 6 KB of tool results per task, 36 s per task, 30201 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 35.4 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1692 ms (median). 344 reasoning tokens per task. 2 prompt-cache invalidations (2 after the history was rewritten), 748 prompt tokens recomputed; 2941 prompt tokens evaluated by the server per task, in 7.9 s. Cost of a passed task: 3992 processed tokens and 34 s; of a failed one: 3771 and 56 s. Peak memory: harness 38 MiB, model server 28862 MiB, GPU 7133 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.5 | 6.3 | 58865 | 42.2 | 0.2 | 43 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8404 | 10.7 | 0.0 | 11 |
| multi | 1/2 | 0 | 5.5 | 11.0 | 33538 | 49.7 | 0.3 | 50 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15938 | 14.6 | 0.0 | 15 |
| rust | 1/1 | 1 | 3.0 | 3.0 | 13607 | 11.3 | 0.0 | 11 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 13728 | 14.8 | 0.1 | 15 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 7.0 | 32189 | 95% | 1330 | 41 |
| diagnose | 2/2 | 3.0 | 3.5 | 14067 | 87% | 794 | 26 |
| explain | 5/5 | 2.4 | 2.0 | 10828 | 87% | 335 | 13 |
| feature | 1/2 | 8.0 | 14.5 | 51659 | 95% | 2206 | 67 |
| small-fix | 3/4 | 5.0 | 5.2 | 24552 | 94% | 598 | 22 |
| large-change | 1/1 | 6.0 | 6.0 | 47633 | 90% | 1214 | 41 |
| large-locate | 1/1 | 2.0 | 1.0 | 13178 | 60% | 272 | 17 |
| locate | 3/3 | 2.0 | 1.7 | 8712 | 87% | 260 | 11 |
| refactor | 2/2 | 7.0 | 9.0 | 40167 | 93% | 812 | 30 |
| session | 2/2 | 19.0 | 15.0 | 274063 | 95% | 4220 | 147 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 7 | 32189 | 30635 | 6297 | 1330 | 0 | 0 | 0 | 41 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8434 | 7605 | 4442 | 281 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 6 | 19700 | 16882 | 6167 | 1308 | 0 | 0 | 0 | 42 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 9851 | 7675 | 5831 | 475 | 0 | 0 | 0 | 17 |  |
| explain-ledger-posting | explain | chat | pass | 3 | 2 | 13806 | 12119 | 5338 | 384 | 0 | 0 | 0 | 15 |  |
| feature-currency | feature | code | pass | 7 | 9 | 44986 | 42044 | 7660 | 1387 | 0 | 0 | 0 | 44 |  |
| fix-balance-query | small-fix | code | FAIL | 5 | 5 | 24108 | 22595 | 5544 | 646 | 0 | 0 | 0 | 23 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 31905 | 29739 | 6451 | 850 | 0 | 0 | 0 | 29 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8404 | 7498 | 4523 | 284 | 0 | 0 | 0 | 11 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 47633 | 43041 | 9174 | 1214 | 0 | 0 | 0 | 41 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13178 | 7899 | 8907 | 272 | 0 | 0 | 0 | 17 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8730 | 7572 | 4759 | 127 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 9 | 20 | 58332 | 55975 | 8684 | 3026 | 0 | 0 | 0 | 90 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.001s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 2 | 8743 | 7635 | 4776 | 244 | 0 | 0 | 0 | 10 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8474 | 7514 | 4580 | 247 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23401 | 22098 | 5211 | 527 | 0 | 0 | 0 | 20 |  |
| refactor-validate | refactor | code | pass | 7 | 9 | 41733 | 38346 | 7803 | 1039 | 0 | 0 | 1 | 37 |  |
| rename-entries | refactor | code | pass | 7 | 9 | 38601 | 36387 | 6256 | 586 | 0 | 0 | 0 | 23 |  |
| rust-explain-ledger | explain | chat | pass | 3 | 3 | 13607 | 12368 | 4880 | 287 | 0 | 0 | 0 | 11 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 82335 | 78486 | 9063 | 2573 | 0 | 0 | 0 | 83 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 26 | 20 | 465791 | 444702 | 30201 | 5866 | 0 | 0 | 1 | 212 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 18793 | 17426 | 5156 | 369 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8662 | 7517 | 4770 | 408 | 0 | 0 | 0 | 14 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:read | tool:read_many | tool:web_search | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6297 | 0 | 200 | 76 | 35 | 537 | 38 | 0 | 11 | 0 | 1004 | 0 | 0 | 31 | 552 | 2199 | 65 | 1549 |
| diagnose-config | 4442 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 0 | 9 | 2199 | 29 | 895 |
| diagnose-failing-test | 6167 | 15 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 1788 | 0 | 0 | 0 | 80 | 2199 | 29 | 1050 |
| explain-event-flow | 5831 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 0 | 20 | 2199 | 33 | 928 |
| explain-ledger-posting | 5338 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 1307 | 0 | 0 | 0 | 22 | 2199 | 22 | 933 |
| feature-currency | 7660 | 70 | 198 | 131 | 103 | 537 | 81 | 45 | 5 | 0 | 2212 | 0 | 0 | 0 | 728 | 2199 | 55 | 1296 |
| fix-balance-query | 5544 | 62 | 202 | 76 | 73 | 537 | 153 | 24 | 0 | 0 | 877 | 0 | 0 | 0 | 148 | 2199 | 32 | 1161 |
| fix-ledger-balance | 6451 | 43 | 204 | 76 | 39 | 537 | 37 | 24 | 21 | 0 | 1622 | 0 | 0 | 0 | 250 | 2199 | 52 | 1347 |
| java-explain-orders | 4523 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 0 | 11 | 2199 | 35 | 897 |
| large-change-txtar | 9174 | 44 | 200 | 286 | 153 | 537 | 18 | 141 | 0 | 0 | 3623 | 0 | 0 | 0 | 704 | 2199 | 79 | 1190 |
| large-locate-preorder | 8907 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 4526 | 0 | 0 | 0 | 15 | 2199 | 42 | 904 |
| locate-topic-name | 4759 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 0 | 10 | 2199 | 25 | 882 |
| multi-feature-discount | 8684 | 72 | 202 | 113 | 68 | 537 | 161 | 175 | 22 | 0 | 1316 | 0 | 0 | 0 | 1791 | 2199 | 117 | 1911 |
| multi-locate-mapping | 4776 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 5 | 0 | 0 | 647 | 0 | 0 | 31 | 2199 | 38 | 935 |
| py-explain-reorder | 4580 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 0 | 15 | 2199 | 40 | 898 |
| py-fix-inventory | 5211 | 25 | 200 | 65 | 36 | 537 | 72 | 16 | 10 | 0 | 785 | 0 | 0 | 0 | 116 | 2199 | 43 | 1107 |
| refactor-validate | 7803 | 20 | 196 | 131 | 34 | 537 | 81 | 96 | 35 | 0 | 2632 | 0 | 0 | 0 | 512 | 2199 | 68 | 1262 |
| rename-entries | 6256 | 0 | 200 | 76 | 47 | 537 | 37 | 24 | 21 | 44 | 1623 | 0 | 0 | 0 | 157 | 2199 | 30 | 1261 |
| rust-explain-ledger | 4880 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 4 | 0 | 821 | 0 | 0 | 0 | 22 | 2199 | 41 | 931 |
| session-ledger | 9063 | 555 | 798 | 76 | 183 | 537 | 188 | 24 | 14 | 0 | 1791 | 0 | 0 | 31 | 463 | 2199 | 110 | 2094 |
| session-x-tools | 30201 | 1469 | 1175 | 286 | 692 | 537 | 141 | 202 | 95 | 0 | 17684 | 0 | 64 | 0 | 1345 | 2199 | 209 | 4103 |
| ts-fix-cart-discount | 5156 | 27 | 200 | 82 | 36 | 537 | 123 | 54 | 0 | 0 | 769 | 0 | 0 | 0 | 63 | 2199 | 44 | 1022 |
| ts-locate-discount | 4770 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 0 | 17 | 2199 | 30 | 901 |

Environment: build dev f95dd3463ef6-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
