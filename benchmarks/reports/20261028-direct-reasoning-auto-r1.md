# Direct-mode suite: direct-20261028T154155Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T15:41:55Z, finished 2026-10-28T15:54:48Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.4 model calls and 5.8 tool calls per task, 45287 prompt tokens per task with a 93% prompt-cache hit rate, 932 generated tokens per task, 7 KB of tool results per task, 34 s per task, 34972 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 33.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1739 ms (median). 297 reasoning tokens per task. 2 prompt-cache invalidations (2 after the history was rewritten), 748 prompt tokens recomputed; 3218 prompt tokens evaluated by the server per task, in 8.4 s. Cost of a passed task: 4152 processed tokens and 32 s; of a failed one: 4130 and 49 s. Peak memory: harness 39 MiB, model server 28572 MiB, GPU 7133 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.3 | 6.2 | 59403 | 39.9 | 0.2 | 40 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8383 | 9.7 | 0.0 | 10 |
| multi | 1/2 | 0 | 5.0 | 10.5 | 29893 | 40.2 | 0.3 | 41 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15956 | 14.1 | 0.0 | 14 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 17503 | 14.5 | 0.0 | 15 |
| typescript | 2/2 | 0 | 3.5 | 3.5 | 16479 | 16.2 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25244 | 95% | 949 | 30 |
| diagnose | 2/2 | 2.5 | 2.5 | 10950 | 86% | 556 | 19 |
| explain | 5/5 | 2.4 | 2.2 | 10534 | 88% | 364 | 13 |
| feature | 1/2 | 8.0 | 14.0 | 51746 | 94% | 1848 | 58 |
| small-fix | 3/4 | 5.8 | 6.5 | 29447 | 94% | 658 | 25 |
| large-change | 1/1 | 6.0 | 6.0 | 47557 | 90% | 1233 | 42 |
| large-locate | 1/1 | 3.0 | 2.0 | 17709 | 69% | 396 | 20 |
| locate | 3/3 | 2.0 | 2.0 | 8642 | 88% | 255 | 10 |
| refactor | 2/2 | 7.5 | 9.0 | 43476 | 94% | 843 | 31 |
| session | 2/2 | 17.0 | 13.0 | 271176 | 94% | 3576 | 134 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25244 | 23963 | 5641 | 949 | 0 | 0 | 0 | 30 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8434 | 7605 | 4442 | 271 | 0 | 0 | 0 | 10 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 4 | 13467 | 11300 | 5424 | 840 | 0 | 0 | 0 | 28 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 9848 | 7672 | 5828 | 696 | 0 | 0 | 0 | 22 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8469 | 7483 | 4584 | 270 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 8 | 10 | 52243 | 49232 | 7766 | 1411 | 0 | 0 | 1 | 45 |  |
| fix-balance-query | small-fix | code | FAIL | 6 | 7 | 32812 | 30373 | 6621 | 771 | 0 | 0 | 0 | 28 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 7 | 9 | 37235 | 35045 | 6473 | 885 | 0 | 0 | 0 | 31 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8383 | 7477 | 4502 | 243 | 0 | 0 | 0 | 10 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 47557 | 42965 | 9139 | 1233 | 0 | 0 | 0 | 42 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 17709 | 12305 | 9018 | 396 | 0 | 0 | 0 | 20 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8728 | 7570 | 4757 | 137 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 8 | 18 | 51248 | 48484 | 8401 | 2285 | 0 | 0 | 0 | 71 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 8538 | 7646 | 4571 | 263 | 0 | 0 | 0 | 11 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8466 | 7506 | 4572 | 237 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23445 | 22142 | 5224 | 495 | 0 | 0 | 0 | 19 |  |
| refactor-validate | refactor | code | pass | 8 | 9 | 48508 | 45096 | 7836 | 1097 | 0 | 0 | 1 | 39 |  |
| rename-entries | refactor | code | pass | 7 | 9 | 38445 | 36245 | 6219 | 589 | 0 | 0 | 0 | 23 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 17503 | 16113 | 5204 | 372 | 0 | 0 | 0 | 15 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 75842 | 72022 | 8884 | 2627 | 0 | 0 | 0 | 84 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 23 | 17 | 466510 | 439018 | 34972 | 4525 | 0 | 0 | 0 | 184 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 5 | 24297 | 22801 | 5412 | 483 | 0 | 0 | 0 | 20 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8661 | 7516 | 4769 | 364 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5641 | 0 | 200 | 76 | 35 | 537 | 78 | 0 | 0 | 4 | 0 | 0 | 712 | 31 | 424 | 2199 | 65 | 1280 |
| diagnose-config | 4442 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2199 | 29 | 895 |
| diagnose-failing-test | 5424 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1169 | 0 | 59 | 2199 | 29 | 976 |
| explain-event-flow | 5828 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2199 | 33 | 925 |
| explain-ledger-posting | 4584 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2199 | 22 | 878 |
| feature-currency | 7766 | 50 | 198 | 131 | 103 | 537 | 81 | 98 | 0 | 5 | 0 | 0 | 2212 | 0 | 736 | 2199 | 55 | 1361 |
| fix-balance-query | 6621 | 52 | 202 | 76 | 73 | 537 | 153 | 24 | 0 | 0 | 0 | 0 | 1788 | 0 | 173 | 2199 | 32 | 1312 |
| fix-ledger-balance | 6473 | 45 | 204 | 76 | 39 | 537 | 37 | 24 | 0 | 14 | 0 | 0 | 1633 | 0 | 267 | 2199 | 52 | 1346 |
| java-explain-orders | 4502 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2199 | 35 | 876 |
| large-change-txtar | 9139 | 62 | 200 | 286 | 153 | 537 | 18 | 141 | 0 | 0 | 0 | 0 | 3623 | 0 | 639 | 2199 | 79 | 1202 |
| large-locate-preorder | 9018 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 46 | 0 | 0 | 0 | 4526 | 0 | 32 | 2199 | 42 | 952 |
| locate-topic-name | 4757 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2199 | 25 | 880 |
| multi-feature-discount | 8401 | 20 | 202 | 113 | 68 | 537 | 631 | 144 | 0 | 11 | 0 | 0 | 1316 | 0 | 1006 | 2199 | 117 | 2037 |
| multi-locate-mapping | 4571 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 5 | 0 | 0 | 431 | 0 | 29 | 2199 | 38 | 948 |
| py-explain-reorder | 4572 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2199 | 40 | 890 |
| py-fix-inventory | 5224 | 38 | 200 | 65 | 36 | 537 | 72 | 16 | 0 | 10 | 0 | 0 | 785 | 0 | 116 | 2199 | 43 | 1107 |
| refactor-validate | 7836 | 0 | 196 | 131 | 34 | 537 | 81 | 96 | 0 | 28 | 0 | 0 | 2655 | 0 | 508 | 2199 | 68 | 1303 |
| rename-entries | 6219 | 0 | 200 | 76 | 47 | 537 | 37 | 24 | 0 | 29 | 44 | 0 | 1599 | 0 | 161 | 2199 | 30 | 1236 |
| rust-explain-ledger | 5204 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 18 | 937 | 0 | 83 | 2199 | 41 | 1064 |
| session-ledger | 8884 | 542 | 798 | 76 | 183 | 537 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 31 | 474 | 2199 | 110 | 1931 |
| session-x-tools | 34972 | 1532 | 1175 | 286 | 692 | 537 | 21 | 63 | 0 | 95 | 31 | 0 | 24172 | 0 | 1116 | 2199 | 209 | 2844 |
| ts-fix-cart-discount | 5412 | 33 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 0 | 886 | 0 | 75 | 2199 | 44 | 1144 |
| ts-locate-discount | 4769 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2199 | 30 | 900 |

Environment: build dev f95dd3463ef6-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
