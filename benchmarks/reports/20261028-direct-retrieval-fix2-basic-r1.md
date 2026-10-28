# Direct-mode suite: direct-20261028T114450Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T11:44:50Z, finished 2026-10-28T12:00:16Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=basic, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.6 model calls and 6.3 tool calls per task, 41080 prompt tokens per task with a 91% prompt-cache hit rate, 1086 generated tokens per task, 6 KB of tool results per task, 40 s per task, 27305 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 39.8 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1765 ms (median). 282 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 630 prompt tokens recomputed. Cost of a passed task: 4758 processed tokens and 37 s; of a failed one: 5870 and 74 s. Peak memory: harness 36 MiB, model server 28689 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.1 | 6.0 | 50297 | 46.0 | 0.3 | 46 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8378 | 9.8 | 0.0 | 10 |
| multi | 1/2 | 0 | 7.5 | 13.5 | 44501 | 62.8 | 0.3 | 63 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15702 | 15.3 | 0.0 | 15 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 17268 | 19.7 | 0.0 | 20 |
| typescript | 2/2 | 0 | 5.0 | 7.0 | 22159 | 20.6 | 0.1 | 21 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 8.0 | 8.0 | 42476 | 85% | 2023 | 71 |
| diagnose | 2/2 | 3.0 | 4.0 | 13212 | 86% | 496 | 18 |
| explain | 5/5 | 2.6 | 2.6 | 11368 | 87% | 460 | 16 |
| feature | 1/2 | 8.0 | 13.0 | 60541 | 90% | 2457 | 82 |
| small-fix | 3/4 | 4.8 | 5.2 | 23021 | 92% | 686 | 26 |
| large-change | 1/1 | 9.0 | 9.0 | 75057 | 92% | 1766 | 66 |
| large-locate | 1/1 | 3.0 | 3.0 | 17248 | 69% | 516 | 24 |
| locate | 3/3 | 4.3 | 5.3 | 18032 | 94% | 329 | 15 |
| refactor | 2/2 | 6.5 | 8.0 | 37675 | 91% | 823 | 33 |
| session | 2/2 | 14.5 | 12.0 | 192088 | 93% | 3540 | 135 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 8 | 8 | 42476 | 35955 | 6493 | 2023 | 0 | 0 | 1 | 71 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 12547 | 11550 | 4520 | 395 | 0 | 0 | 0 | 15 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 13877 | 11097 | 5984 | 597 | 0 | 0 | 0 | 21 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 14333 | 11770 | 6086 | 805 | 0 | 0 | 0 | 27 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8510 | 7451 | 4586 | 493 | 0 | 0 | 0 | 16 |  |
| feature-currency | feature | code | pass | 7 | 6 | 57082 | 50692 | 9897 | 1343 | 0 | 0 | 0 | 52 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 18927 | 17311 | 5135 | 1140 | 0 | 0 | 0 | 37 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 26624 | 24062 | 6077 | 603 | 0 | 0 | 0 | 25 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8378 | 7423 | 4482 | 246 | 0 | 0 | 0 | 10 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 9 | 9 | 75057 | 68839 | 9717 | 1766 | 0 | 0 | 0 | 66 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 3 | 17248 | 11897 | 8874 | 516 | 0 | 0 | 0 | 24 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 8252 | 7424 | 4355 | 183 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | FAIL | 9 | 20 | 64000 | 58586 | 8913 | 3571 | 0 | 0 | 0 | 111 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 DiscountCents:5000 TotalCents:0} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 6 | 7 | 25002 | 23978 | 4535 | 304 | 0 | 0 | 0 | 16 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8349 | 7377 | 4499 | 238 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23055 | 21540 | 5030 | 520 | 0 | 0 | 0 | 21 |  |
| refactor-validate | refactor | code | pass | 7 | 8 | 40438 | 37320 | 6625 | 904 | 0 | 0 | 1 | 36 |  |
| rename-entries | refactor | code | pass | 6 | 8 | 34912 | 31595 | 6828 | 742 | 0 | 0 | 0 | 30 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 17268 | 15645 | 5142 | 519 | 0 | 0 | 0 | 20 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 65141 | 60281 | 8052 | 2203 | 0 | 0 | 0 | 78 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 15 | 319035 | 295189 | 27305 | 4877 | 0 | 0 | 1 | 191 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 7 | 23477 | 21620 | 5372 | 481 | 0 | 0 | 0 | 22 |  |
| ts-locate-discount | locate | chat | pass | 5 | 7 | 20841 | 19541 | 4815 | 501 | 0 | 0 | 0 | 20 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:read_many | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6493 | 72 | 200 | 76 | 0 | 537 | 80 | 0 | 0 | 0 | 0 | 2 | 712 | 0 | 0 | 128 | 1418 | 2199 | 65 | 1004 |
| diagnose-config | 4520 | 0 | 199 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 118 | 0 | 355 | 0 | 0 | 0 | 36 | 2199 | 29 | 916 |
| diagnose-failing-test | 5984 | 19 | 206 | 76 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 0 | 70 | 2199 | 29 | 945 |
| explain-event-flow | 6086 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 33 | 224 | 0 | 1672 | 0 | 0 | 0 | 124 | 2199 | 33 | 936 |
| explain-ledger-posting | 4586 | 0 | 203 | 76 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 9897 | 92 | 198 | 131 | 0 | 537 | 2480 | 45 | 0 | 0 | 0 | 0 | 0 | 2258 | 0 | 0 | 872 | 2199 | 55 | 1030 |
| fix-balance-query | 5135 | 57 | 202 | 76 | 48 | 537 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 0 | 95 | 2199 | 32 | 950 |
| fix-ledger-balance | 6077 | 61 | 204 | 76 | 78 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 0 | 0 | 230 | 2199 | 52 | 981 |
| java-explain-orders | 4482 | 0 | 197 | 65 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 9717 | 0 | 200 | 286 | 0 | 537 | 470 | 184 | 0 | 0 | 0 | 0 | 3623 | 0 | 0 | 0 | 1018 | 2199 | 79 | 1121 |
| large-locate-preorder | 8874 | 0 | 201 | 286 | 0 | 537 | 0 | 0 | 46 | 84 | 0 | 0 | 4526 | 0 | 0 | 0 | 45 | 2199 | 42 | 908 |
| locate-topic-name | 4355 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 338 | 0 | 0 | 0 | 0 | 0 | 34 | 2199 | 25 | 894 |
| multi-feature-discount | 8913 | 25 | 202 | 113 | 130 | 537 | 138 | 175 | 0 | 0 | 0 | 0 | 1316 | 0 | 287 | 0 | 2198 | 2199 | 117 | 1476 |
| multi-locate-mapping | 4535 | 0 | 203 | 113 | 0 | 537 | 0 | 0 | 0 | 12 | 0 | 19 | 332 | 0 | 0 | 0 | 51 | 2199 | 38 | 1031 |
| py-explain-reorder | 4499 | 0 | 201 | 65 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 0 | 15 | 2199 | 40 | 860 |
| py-fix-inventory | 5030 | 6 | 200 | 65 | 46 | 537 | 20 | 16 | 0 | 10 | 0 | 0 | 785 | 0 | 0 | 0 | 110 | 2199 | 43 | 993 |
| refactor-validate | 6625 | 46 | 196 | 131 | 73 | 537 | 81 | 96 | 0 | 28 | 0 | 0 | 1588 | 0 | 0 | 0 | 497 | 2199 | 68 | 1085 |
| rename-entries | 6828 | 130 | 200 | 76 | 0 | 537 | 717 | 24 | 0 | 0 | 3 | 0 | 1623 | 0 | 0 | 0 | 214 | 2199 | 30 | 1075 |
| rust-explain-ledger | 5142 | 0 | 205 | 89 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 20 | 928 | 0 | 0 | 0 | 165 | 2199 | 41 | 958 |
| session-ledger | 8052 | 613 | 798 | 76 | 0 | 537 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 0 | 0 | 31 | 505 | 2199 | 110 | 1180 |
| session-x-tools | 27305 | 1535 | 1175 | 286 | 0 | 537 | 21 | 150 | 0 | 21 | 1716 | 0 | 16597 | 0 | 0 | 0 | 1432 | 2199 | 209 | 1427 |
| ts-fix-cart-discount | 5372 | 38 | 200 | 82 | 0 | 537 | 121 | 54 | 0 | 12 | 0 | 0 | 964 | 0 | 0 | 0 | 90 | 2199 | 44 | 1031 |
| ts-locate-discount | 4815 | 0 | 199 | 82 | 0 | 537 | 0 | 0 | 0 | 11 | 0 | 15 | 672 | 0 | 0 | 0 | 54 | 2199 | 30 | 1016 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
