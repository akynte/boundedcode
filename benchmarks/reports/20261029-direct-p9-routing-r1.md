# Direct-mode suite: direct-20261029T172520Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T17:25:20Z, finished 2026-10-29T17:42:34Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=adaptive, seed_pack=true, speculative=off, working_set_tokens=0.

**24/26 passed (92%)**, 6.7 model calls and 6.5 tool calls per task, 59083 prompt tokens per task with a 93% prompt-cache hit rate, 1050 generated tokens per task, 15 KB of tool results per task, 40 s per task, 30645 tokens in the largest single prompt; 0 results set aside, 4 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 3, 183 KB → 20 KB. Freshness notes: 1.

Calls run together: 26 replies, 63 calls, 0.004 s together against 0.007 s one after another.

Decode: 37.0 tokens/s over the run (27291 tokens in 738 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 2366 generated tokens, 96 s); explore 39 (39 without thinking, 5971 generated tokens, 255 s); edit 30 (0 without thinking, 5274 generated tokens, 167 s); debug 41 (0 without thinking, 9735 generated tokens, 298 s); answer 46 (46 without thinking, 3945 generated tokens, 206 s).

Where the agent's time went, per task: 39.3 s in model calls, 0.2 s in tools (with the checks after edits), 0.05 s in the harness itself; time to first token 1824 ms (median). 221 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1108 prompt tokens recomputed; 4100 prompt tokens evaluated by the server per task, in 10.6 s. Cost of a passed task: 5104 processed tokens and 36 s; of a failed one: 5698 and 82 s. Peak memory: harness 38 MiB, model server 28284 MiB, GPU 7141 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 7.0 | 6.5 | 56855 | 42.2 | 0.3 | 43 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8668 | 5.7 | 0.0 | 6 |
| multi | 1/2 | 0 | 12.5 | 16.0 | 99560 | 76.1 | 0.4 | 77 |
| python | 3/3 | 0 | 6.7 | 6.0 | 108273 | 39.0 | 0.1 | 39 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 8994 | 7.3 | 0.0 | 7 |
| typescript | 2/2 | 0 | 3.0 | 2.5 | 14018 | 11.5 | 0.1 | 12 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 8.0 | 9.0 | 46227 | 96% | 1399 | 45 |
| diagnose | 2/2 | 3.0 | 4.0 | 14584 | 87% | 447 | 17 |
| explain | 5/5 | 2.0 | 1.4 | 9059 | 86% | 187 | 9 |
| feature | 1/2 | 16.0 | 19.5 | 126826 | 97% | 2993 | 96 |
| small-fix | 3/4 | 5.2 | 5.8 | 26616 | 94% | 547 | 21 |
| large-change | 1/1 | 6.0 | 6.0 | 48319 | 90% | 1195 | 42 |
| large-locate | 1/1 | 2.0 | 1.0 | 13475 | 61% | 154 | 14 |
| locate | 3/3 | 2.0 | 1.7 | 8989 | 81% | 126 | 8 |
| refactor | 2/2 | 6.0 | 7.5 | 34454 | 93% | 850 | 30 |
| session | 5/5 | 14.2 | 11.0 | 179540 | 93% | 2492 | 99 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 8 | 9 | 46227 | 44380 | 6723 | 1399 | 0 | 0 | 2 | 45 |  |
| diagnose-config | diagnose | chat | pass | 2 | 2 | 8813 | 7897 | 4661 | 195 | 0 | 0 | 0 | 9 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 6 | 20354 | 17517 | 6321 | 699 | 0 | 0 | 0 | 26 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10124 | 7944 | 5944 | 323 | 0 | 0 | 0 | 13 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8752 | 7762 | 4707 | 203 | 0 | 0 | 0 | 9 |  |
| feature-currency | feature | code | pass | 9 | 10 | 63852 | 60108 | 8705 | 1461 | 0 | 0 | 0 | 50 |  |
| fix-balance-query | small-fix | code | FAIL | 5 | 5 | 25494 | 23979 | 5625 | 613 | 0 | 0 | 0 | 22 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 8 | 10 | 43253 | 40891 | 6651 | 771 | 0 | 0 | 0 | 30 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8668 | 7758 | 4627 | 90 | 0 | 0 | 0 | 6 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48319 | 43725 | 9235 | 1195 | 0 | 0 | 0 | 42 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13475 | 8192 | 9044 | 154 | 0 | 0 | 0 | 14 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 9009 | 7847 | 4878 | 73 | 0 | 0 | 0 | 6 |  |
| multi-feature-discount | feature | code | FAIL | 23 | 29 | 189799 | 185055 | 12715 | 4525 | 0 | 0 | 0 | 142 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9320 | 6258 | 5193 | 164 | 0 | 0 | 0 | 12 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8757 | 7793 | 4703 | 179 | 0 | 0 | 0 | 8 |  |
| py-fix-inventory | small-fix | code | pass | 4 | 4 | 18317 | 17429 | 4915 | 428 | 0 | 0 | 0 | 15 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 34939 | 32711 | 6830 | 1063 | 0 | 0 | 0 | 36 |  |
| rename-entries | refactor | code | pass | 6 | 9 | 33969 | 31706 | 6428 | 638 | 0 | 0 | 0 | 24 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 8994 | 7785 | 4923 | 141 | 0 | 0 | 0 | 7 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 81862 | 77990 | 8910 | 2109 | 0 | 0 | 0 | 72 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 14 | 12 | 297744 | 272089 | 30458 | 1618 | 0 | 4 | 0 | 94 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 12 | 100095 | 96526 | 9445 | 2267 | 0 | 0 | 0 | 77 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 80580 | 74714 | 8617 | 822 | 0 | 0 | 0 | 43 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 14 | 337417 | 315721 | 30645 | 5644 | 0 | 0 | 0 | 209 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19399 | 18030 | 5317 | 377 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 1 | 8638 | 7766 | 4586 | 140 | 0 | 0 | 0 | 7 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:recall | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6723 | 21 | 200 | 76 | 35 | 537 | 76 | 80 | 4 | 0 | 0 | 1121 | 0 | 0 | 61 | 770 | 2338 | 65 | 1339 |
| diagnose-config | 4661 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 436 | 0 | 0 | 0 | 21 | 2338 | 29 | 882 |
| diagnose-failing-test | 6321 | 13 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 0 | 0 | 125 | 2338 | 29 | 1022 |
| explain-event-flow | 5944 | 20 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 0 | 20 | 2338 | 33 | 882 |
| explain-ledger-posting | 4707 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 0 | 11 | 2338 | 22 | 862 |
| feature-currency | 8705 | 89 | 198 | 131 | 103 | 537 | 167 | 45 | 5 | 0 | 0 | 2894 | 0 | 0 | 0 | 936 | 2338 | 55 | 1207 |
| fix-balance-query | 5625 | 55 | 202 | 76 | 73 | 537 | 152 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 0 | 130 | 2338 | 32 | 1129 |
| fix-ledger-balance | 6651 | 60 | 204 | 76 | 39 | 537 | 86 | 24 | 7 | 0 | 0 | 1738 | 0 | 0 | 0 | 285 | 2338 | 52 | 1205 |
| java-explain-orders | 4627 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 0 | 11 | 2338 | 35 | 862 |
| large-change-txtar | 9235 | 63 | 200 | 286 | 153 | 537 | 17 | 141 | 0 | 0 | 0 | 3623 | 0 | 0 | 0 | 668 | 2338 | 79 | 1130 |
| large-locate-preorder | 9044 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 0 | 0 | 55 | 2338 | 42 | 862 |
| locate-topic-name | 4878 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 0 | 10 | 2338 | 25 | 862 |
| multi-feature-discount | 12715 | 140 | 202 | 113 | 68 | 537 | 640 | 78 | 11 | 0 | 58 | 2579 | 0 | 503 | 0 | 2252 | 2338 | 117 | 3079 |
| multi-locate-mapping | 5193 | 20 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 947 | 0 | 0 | 0 | 27 | 2338 | 38 | 902 |
| py-explain-reorder | 4703 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 0 | 15 | 2338 | 40 | 882 |
| py-fix-inventory | 4915 | 48 | 200 | 65 | 36 | 537 | 19 | 16 | 0 | 0 | 0 | 445 | 0 | 0 | 0 | 108 | 2338 | 43 | 1060 |
| refactor-validate | 6830 | 39 | 196 | 131 | 34 | 537 | 80 | 0 | 12 | 0 | 45 | 1565 | 0 | 0 | 0 | 695 | 2338 | 68 | 1090 |
| rename-entries | 6428 | 79 | 200 | 76 | 47 | 537 | 36 | 24 | 21 | 3 | 0 | 1721 | 0 | 0 | 0 | 164 | 2338 | 30 | 1152 |
| rust-explain-ledger | 4923 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 0 | 8 | 2338 | 41 | 862 |
| session-ledger | 8910 | 730 | 798 | 76 | 183 | 537 | 186 | 24 | 14 | 0 | 0 | 1791 | 0 | 0 | 31 | 594 | 2338 | 110 | 1498 |
| session-noisy-tests | 30458 | 487 | 607 | 65 | 74 | 537 | 7192 | 16 | 0 | 3 | 0 | 809 | 16095 | 0 | 0 | 636 | 2338 | 81 | 1518 |
| session-resume-constraints | 9445 | 608 | 810 | 76 | 183 | 537 | 261 | 49 | 14 | 0 | 0 | 1791 | 0 | 0 | 31 | 792 | 2338 | 126 | 1829 |
| session-stale-config | 8617 | 249 | 1022 | 131 | 258 | 537 | 72 | 0 | 4 | 0 | 0 | 2380 | 0 | 0 | 28 | 328 | 2338 | 118 | 1152 |
| session-x-tools | 30645 | 1176 | 1175 | 286 | 692 | 537 | 20 | 126 | 95 | 0 | 0 | 18479 | 0 | 0 | 0 | 1952 | 2338 | 209 | 3560 |
| ts-fix-cart-discount | 5317 | 42 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 0 | 0 | 65 | 2338 | 44 | 1028 |
| ts-locate-discount | 4586 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 494 | 0 | 0 | 0 | 8 | 2338 | 30 | 862 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
