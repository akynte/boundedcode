# Direct-mode suite: direct-20261028T152805Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T15:28:05Z, finished 2026-10-28T15:41:55Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, reasoning_history=drop, reasoning_round_trip=false, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.5 model calls and 5.4 tool calls per task, 44914 prompt tokens per task with a 92% prompt-cache hit rate, 961 generated tokens per task, 6 KB of tool results per task, 36 s per task, 27657 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 35.7 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1835 ms (median). 324 reasoning tokens per task. 2 prompt-cache invalidations (2 after the history was rewritten), 748 prompt tokens recomputed; 3565 prompt tokens evaluated by the server per task, in 10.2 s. Cost of a passed task: 4481 processed tokens and 34 s; of a failed one: 4997 and 55 s. Peak memory: harness 38 MiB, model server 27353 MiB, GPU 7133 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: empty_answer 1.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.3 | 6.1 | 58355 | 42.5 | 0.2 | 43 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8348 | 8.2 | 0.0 | 8 |
| multi | 1/2 | 0 | 6.5 | 9.0 | 38540 | 42.6 | 0.2 | 43 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15741 | 21.7 | 0.0 | 22 |
| rust | 1/1 | 1 | 3.0 | 1.0 | 13467 | 16.2 | 0.0 | 16 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 13652 | 14.6 | 0.1 | 15 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 7.0 | 25761 | 77% | 916 | 37 |
| diagnose | 2/2 | 3.5 | 3.5 | 16648 | 89% | 872 | 28 |
| explain | 5/5 | 2.2 | 1.4 | 9694 | 87% | 338 | 12 |
| feature | 1/2 | 8.5 | 10.5 | 52150 | 92% | 1796 | 62 |
| small-fix | 3/4 | 5.0 | 5.5 | 24664 | 92% | 822 | 30 |
| large-change | 1/1 | 6.0 | 6.0 | 46617 | 88% | 1250 | 46 |
| large-locate | 1/1 | 2.0 | 1.0 | 13115 | 59% | 212 | 15 |
| locate | 3/3 | 2.0 | 2.0 | 8764 | 85% | 253 | 10 |
| refactor | 2/2 | 6.5 | 8.0 | 36018 | 92% | 804 | 31 |
| session | 2/2 | 19.5 | 15.5 | 282236 | 95% | 3515 | 139 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 7 | 25761 | 19714 | 6031 | 916 | 0 | 0 | 0 | 37 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8380 | 7519 | 4388 | 237 | 0 | 0 | 0 | 9 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 24917 | 21957 | 6097 | 1507 | 0 | 0 | 0 | 47 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 9784 | 7547 | 5764 | 537 | 0 | 0 | 0 | 18 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8432 | 7412 | 4547 | 284 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 6 | 6 | 36199 | 32661 | 7049 | 1487 | 0 | 0 | 1 | 50 |  |
| fix-balance-query | small-fix | code | FAIL | 6 | 7 | 30702 | 28388 | 5825 | 1014 | 0 | 0 | 0 | 36 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 6 | 26229 | 23687 | 6057 | 830 | 0 | 0 | 0 | 30 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8348 | 7408 | 4467 | 190 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 46617 | 41252 | 8876 | 1250 | 0 | 0 | 0 | 46 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13115 | 7798 | 8844 | 212 | 0 | 0 | 0 | 15 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8689 | 7498 | 4718 | 133 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 11 | 15 | 68101 | 63541 | 8051 | 2106 | 0 | 0 | 0 | 74 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 8980 | 7494 | 5013 | 304 | 0 | 0 | 0 | 12 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8437 | 7421 | 4543 | 207 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23045 | 21500 | 5060 | 1072 | 0 | 0 | 0 | 35 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 39258 | 36345 | 6420 | 947 | 0 | 0 | 1 | 36 |  |
| rename-entries | refactor | code | pass | 6 | 9 | 32777 | 30093 | 6195 | 662 | 0 | 0 | 0 | 26 |  |
| rust-explain-ledger | explain | chat | pass | 3 | 1 | 13467 | 12197 | 4793 | 471 | 0 | 0 | 0 | 16 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 13 | 11 | 84633 | 79454 | 8300 | 2149 | 0 | 0 | 0 | 78 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 26 | 20 | 479839 | 455613 | 27657 | 4881 | 0 | 0 | 1 | 199 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 18682 | 17093 | 5108 | 373 | 0 | 0 | 0 | 18 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8622 | 7419 | 4730 | 323 | 0 | 0 | 0 | 12 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6031 | 0 | 200 | 76 | 35 | 537 | 40 | 0 | 11 | 0 | 0 | 1400 | 31 | 432 | 2199 | 65 | 1005 |
| diagnose-config | 4388 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2199 | 29 | 841 |
| diagnose-failing-test | 6097 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 80 | 2199 | 29 | 995 |
| explain-event-flow | 5764 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2199 | 33 | 861 |
| explain-ledger-posting | 4547 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 7049 | 29 | 198 | 131 | 103 | 537 | 115 | 111 | 0 | 0 | 0 | 1565 | 0 | 1018 | 2199 | 55 | 988 |
| fix-balance-query | 5825 | 136 | 202 | 76 | 73 | 537 | 153 | 24 | 14 | 0 | 0 | 1169 | 0 | 162 | 2199 | 32 | 1048 |
| fix-ledger-balance | 6057 | 37 | 204 | 76 | 39 | 537 | 37 | 24 | 7 | 0 | 0 | 1598 | 0 | 241 | 2199 | 52 | 1006 |
| java-explain-orders | 4467 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 8876 | 35 | 200 | 286 | 153 | 537 | 18 | 141 | 0 | 0 | 0 | 3623 | 0 | 587 | 2199 | 79 | 1018 |
| large-locate-preorder | 8844 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 15 | 2199 | 42 | 841 |
| locate-topic-name | 4718 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2199 | 25 | 841 |
| multi-feature-discount | 8051 | 119 | 202 | 113 | 68 | 537 | 140 | 30 | 14 | 0 | 78 | 1780 | 0 | 1347 | 2199 | 117 | 1307 |
| multi-locate-mapping | 5013 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 947 | 0 | 27 | 2199 | 38 | 881 |
| py-explain-reorder | 4543 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2199 | 40 | 861 |
| py-fix-inventory | 5060 | 46 | 200 | 65 | 36 | 537 | 20 | 16 | 10 | 0 | 0 | 785 | 0 | 110 | 2199 | 43 | 993 |
| refactor-validate | 6420 | 16 | 196 | 131 | 34 | 537 | 81 | 96 | 12 | 0 | 0 | 1565 | 0 | 413 | 2199 | 68 | 1072 |
| rename-entries | 6195 | 32 | 200 | 76 | 47 | 537 | 37 | 24 | 21 | 44 | 0 | 1697 | 0 | 159 | 2199 | 30 | 1092 |
| rust-explain-ledger | 4793 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 8 | 2199 | 56 | 856 |
| session-ledger | 8300 | 540 | 798 | 76 | 183 | 537 | 188 | 59 | 14 | 0 | 0 | 1791 | 31 | 521 | 2199 | 110 | 1253 |
| session-x-tools | 27657 | 1011 | 1175 | 286 | 692 | 537 | 21 | 171 | 95 | 31 | 0 | 18404 | 0 | 1160 | 2199 | 209 | 1666 |
| ts-fix-cart-discount | 5108 | 42 | 200 | 82 | 36 | 537 | 123 | 54 | 0 | 0 | 0 | 769 | 0 | 67 | 2199 | 44 | 955 |
| ts-locate-discount | 4730 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2199 | 30 | 861 |

Environment: build dev f95dd3463ef6-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
