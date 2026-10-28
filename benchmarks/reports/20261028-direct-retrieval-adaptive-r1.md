# Direct-mode suite: direct-20261028T095132Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T09:51:32Z, finished 2026-10-28T10:08:35Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**20/23 passed (87%)**, 5.2 model calls and 5.3 tool calls per task, 42762 prompt tokens per task with a 92% prompt-cache hit rate, 1260 generated tokens per task, 6 KB of tool results per task, 44 s per task, 35920 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 44.1 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1883 ms (median). 660 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 748 prompt tokens recomputed. Cost of a passed task: 4542 processed tokens and 33 s; of a failed one: 6641 and 119 s. Peak memory: harness 39 MiB, model server 28797 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 3. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 13/15 | 0 | 5.7 | 5.7 | 53950 | 55.7 | 0.2 | 56 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8338 | 8.3 | 0.0 | 8 |
| multi | 1/2 | 0 | 8.0 | 10.5 | 49149 | 48.2 | 0.3 | 49 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15734 | 15.7 | 0.0 | 16 |
| rust | 1/1 | 1 | 2.0 | 2.0 | 8886 | 11.5 | 0.0 | 12 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 13640 | 15.9 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 0/1 | 3.0 | 4.0 | 13992 | 88% | 8847 | 243 |
| diagnose | 2/2 | 3.0 | 3.5 | 13950 | 86% | 728 | 25 |
| explain | 5/5 | 2.0 | 1.6 | 8775 | 85% | 300 | 12 |
| feature | 1/2 | 10.0 | 12.0 | 62746 | 94% | 1818 | 65 |
| small-fix | 3/4 | 4.5 | 4.8 | 21754 | 92% | 635 | 24 |
| large-change | 1/1 | 5.0 | 5.0 | 37317 | 87% | 790 | 33 |
| large-locate | 1/1 | 2.0 | 1.0 | 9465 | 82% | 508 | 18 |
| locate | 3/3 | 2.0 | 2.0 | 8544 | 87% | 249 | 10 |
| refactor | 2/2 | 7.0 | 10.0 | 40464 | 91% | 1294 | 47 |
| session | 2/2 | 17.5 | 14.5 | 265948 | 93% | 3182 | 136 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | FAIL | 3 | 4 | 13992 | 12313 | 5202 | 8847 | 0 | 0 | 1 | 243 | …:11:3: expected operand, found '{' FAIL	example.com/ledger-service/internal/ledger [setup failed] ok  	example.com/ledger-service/internal/consumer	0.002s FAIL  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8380 | 7519 | 4388 | 267 | 0 | 0 | 0 | 10 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 6 | 19519 | 16575 | 6085 | 1188 | 0 | 0 | 0 | 39 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 9784 | 7547 | 5764 | 499 | 0 | 0 | 0 | 17 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8432 | 7412 | 4547 | 292 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 6 | 6 | 35515 | 32253 | 6773 | 1231 | 0 | 0 | 0 | 43 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 19028 | 17382 | 5165 | 844 | 0 | 0 | 0 | 29 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 6 | 26299 | 23721 | 6093 | 652 | 0 | 0 | 0 | 26 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8338 | 7403 | 4462 | 192 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 5 | 37317 | 32295 | 8537 | 790 | 0 | 0 | 0 | 33 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 9465 | 7798 | 5194 | 508 | 0 | 0 | 0 | 18 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8689 | 7498 | 4718 | 154 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | FAIL | 14 | 18 | 89978 | 85475 | 7982 | 2404 | 0 | 0 | 0 | 87 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 8320 | 7494 | 4353 | 266 | 0 | 0 | 0 | 11 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8437 | 7421 | 4543 | 215 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23032 | 21484 | 5063 | 570 | 0 | 0 | 0 | 23 |  |
| refactor-validate | refactor | code | pass | 7 | 8 | 41648 | 37504 | 7651 | 1187 | 0 | 0 | 1 | 44 |  |
| rename-entries | refactor | code | pass | 7 | 12 | 39280 | 36215 | 6572 | 1401 | 0 | 0 | 0 | 49 | switched to code. |
| rust-explain-ledger | explain | chat | pass | 2 | 2 | 8886 | 7434 | 4979 | 304 | 0 | 0 | 0 | 12 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 70888 | 65947 | 8070 | 2453 | 0 | 0 | 0 | 87 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 24 | 20 | 461009 | 428528 | 35920 | 3911 | 0 | 0 | 1 | 185 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 18658 | 17084 | 5093 | 475 | 0 | 0 | 0 | 20 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8622 | 7419 | 4730 | 326 | 0 | 0 | 0 | 12 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:exit_plan_mode | tool:find_symbol | tool:glob | tool:grep | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5202 | 0 | 200 | 76 | 78 | 537 | 0 | 0 | 0 | 0 | 6 | 0 | 744 | 0 | 61 | 324 | 2199 | 65 | 912 |
| diagnose-config | 4388 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2199 | 29 | 841 |
| diagnose-failing-test | 6085 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 14 | 0 | 1788 | 0 | 0 | 80 | 2199 | 29 | 983 |
| explain-event-flow | 5764 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2199 | 33 | 861 |
| explain-ledger-posting | 4547 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 6773 | 51 | 198 | 131 | 103 | 537 | 169 | 45 | 0 | 0 | 0 | 0 | 1565 | 0 | 0 | 713 | 2199 | 55 | 1007 |
| fix-balance-query | 5165 | 61 | 202 | 76 | 73 | 537 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 96 | 2199 | 32 | 950 |
| fix-ledger-balance | 6093 | 74 | 204 | 76 | 39 | 537 | 37 | 24 | 0 | 0 | 7 | 0 | 1598 | 0 | 0 | 240 | 2199 | 52 | 1006 |
| java-explain-orders | 4462 | 0 | 197 | 65 | 34 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 8537 | 6 | 200 | 286 | 153 | 537 | 18 | 43 | 0 | 0 | 0 | 0 | 3623 | 0 | 0 | 412 | 2199 | 79 | 981 |
| large-locate-preorder | 5194 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 849 | 0 | 0 | 0 | 0 | 0 | 23 | 2199 | 42 | 860 |
| locate-topic-name | 4718 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 10 | 2199 | 25 | 841 |
| multi-feature-discount | 7982 | 170 | 202 | 113 | 80 | 537 | 161 | 156 | 0 | 0 | 11 | 0 | 1316 | 0 | 0 | 1435 | 2199 | 117 | 1485 |
| multi-locate-mapping | 4353 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 18 | 0 | 259 | 0 | 0 | 27 | 2199 | 38 | 891 |
| py-explain-reorder | 4543 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2199 | 40 | 861 |
| py-fix-inventory | 5063 | 54 | 200 | 65 | 32 | 537 | 20 | 16 | 0 | 0 | 10 | 0 | 785 | 0 | 0 | 109 | 2199 | 43 | 993 |
| refactor-validate | 7651 | 71 | 196 | 131 | 48 | 537 | 81 | 74 | 0 | 0 | 35 | 0 | 2629 | 0 | 0 | 516 | 2199 | 68 | 1066 |
| rename-entries | 6572 | 93 | 200 | 76 | 47 | 537 | 40 | 24 | 8 | 0 | 29 | 47 | 1599 | 0 | 0 | 432 | 2199 | 30 | 1211 |
| rust-explain-ledger | 4979 | 0 | 205 | 89 | 27 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 1003 | 0 | 0 | 17 | 2199 | 41 | 861 |
| session-ledger | 8070 | 506 | 798 | 76 | 183 | 537 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 0 | 31 | 458 | 2199 | 110 | 1169 |
| session-x-tools | 35920 | 867 | 1175 | 286 | 985 | 537 | 60 | 150 | 0 | 0 | 95 | 31 | 26746 | 32 | 0 | 932 | 2199 | 209 | 1616 |
| ts-fix-cart-discount | 5093 | 33 | 200 | 82 | 36 | 537 | 121 | 54 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 63 | 2199 | 44 | 955 |
| ts-locate-discount | 4730 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2199 | 30 | 861 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
