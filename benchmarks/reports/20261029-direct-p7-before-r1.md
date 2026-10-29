# Direct-mode suite: direct-20261028T215424Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T21:54:24Z, finished 2026-10-28T22:11:47Z.

Settings: approval=auto, checks=true, compaction=legacy, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=false, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**23/26 passed (88%)**, 6.0 model calls and 6.2 tool calls per task, 51380 prompt tokens per task with a 93% prompt-cache hit rate, 1107 generated tokens per task, 11 KB of tool results per task, 40 s per task, 34081 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 0, 0 KB → 0 KB. Freshness notes: 1.

Where the agent's time went, per task: 39.7 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1780 ms (median). 374 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3734 prompt tokens evaluated by the server per task, in 9.7 s. Cost of a passed task: 4454 processed tokens and 37 s; of a failed one: 7803 and 63 s. Peak memory: harness 40 MiB, model server 29327 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 3. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 6.8 | 6.5 | 59214 | 45.3 | 0.3 | 46 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8619 | 10.8 | 0.0 | 11 |
| multi | 1/2 | 0 | 6.0 | 12.5 | 38578 | 50.4 | 0.3 | 51 |
| python | 2/3 | 0 | 6.0 | 5.3 | 67277 | 36.2 | 0.2 | 36 |
| rust | 1/1 | 1 | 3.0 | 4.0 | 13499 | 13.4 | 0.0 | 13 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14070 | 13.7 | 0.1 | 14 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25638 | 95% | 888 | 28 |
| diagnose | 2/2 | 3.5 | 3.5 | 17348 | 89% | 614 | 22 |
| explain | 5/5 | 2.2 | 2.0 | 9921 | 87% | 341 | 13 |
| feature | 1/2 | 8.5 | 14.5 | 55906 | 95% | 2156 | 68 |
| small-fix | 3/4 | 4.5 | 5.0 | 22668 | 93% | 532 | 20 |
| large-change | 1/1 | 5.0 | 5.0 | 38045 | 88% | 740 | 28 |
| large-locate | 1/1 | 3.0 | 4.0 | 18829 | 68% | 459 | 23 |
| locate | 3/3 | 2.3 | 2.3 | 10467 | 88% | 306 | 12 |
| refactor | 2/2 | 7.0 | 8.5 | 42213 | 93% | 872 | 32 |
| session | 4/5 | 14.0 | 11.4 | 170151 | 94% | 2930 | 108 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25638 | 24355 | 5694 | 888 | 0 | 0 | 0 | 28 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8648 | 7819 | 4543 | 218 | 0 | 0 | 0 | 9 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 26049 | 23218 | 6458 | 1011 | 0 | 0 | 0 | 35 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10083 | 7907 | 5950 | 615 | 0 | 0 | 0 | 20 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8696 | 7710 | 4698 | 248 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 7 | 7 | 43922 | 41395 | 7392 | 1377 | 0 | 0 | 1 | 45 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 19906 | 18524 | 5485 | 542 | 0 | 0 | 0 | 19 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 7 | 27403 | 25248 | 6389 | 667 | 0 | 0 | 0 | 24 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8619 | 7713 | 4625 | 280 | 0 | 0 | 0 | 11 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 5 | 38045 | 33567 | 8687 | 740 | 0 | 0 | 0 | 28 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 4 | 18829 | 12773 | 9825 | 459 | 0 | 0 | 0 | 23 |  |
| locate-topic-name | locate | chat | pass | 3 | 2 | 13245 | 12024 | 4857 | 341 | 0 | 0 | 0 | 14 |  |
| multi-feature-discount | feature | code | FAIL | 10 | 22 | 67889 | 65052 | 9182 | 2936 | 0 | 0 | 0 | 91 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9268 | 7868 | 5188 | 260 | 0 | 0 | 0 | 11 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8710 | 7750 | 4703 | 227 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24112 | 22809 | 5343 | 558 | 0 | 0 | 0 | 21 |  |
| refactor-validate | refactor | code | pass | 8 | 8 | 50442 | 47082 | 7957 | 1133 | 0 | 0 | 1 | 40 |  |
| rename-entries | refactor | code | pass | 6 | 9 | 33984 | 31707 | 6457 | 612 | 0 | 0 | 0 | 24 |  |
| rust-explain-ledger | explain | chat | pass | 3 | 4 | 13499 | 12148 | 5194 | 337 | 0 | 0 | 0 | 13 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 14 | 12 | 109480 | 104772 | 10403 | 2995 | 0 | 0 | 0 | 98 | switched to code. |
| session-noisy-tests | session | chat, code, chat | FAIL | 11 | 9 | 169008 | 155156 | 18762 | 1859 | 0 | 0 | 0 | 79 | switched to code. turn 1: answer does not match /test_total_value\|total_value/ |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 12 | 110526 | 106965 | 10499 | 3571 | 0 | 0 | 0 | 111 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 82374 | 76488 | 8978 | 1238 | 0 | 0 | 0 | 54 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 17 | 379368 | 353125 | 34081 | 4986 | 0 | 0 | 0 | 195 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19253 | 17887 | 5275 | 363 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8887 | 7742 | 4882 | 317 | 0 | 0 | 0 | 12 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5694 | 0 | 200 | 76 | 35 | 537 | 78 | 0 | 0 | 6 | 0 | 712 | 31 | 330 | 2297 | 65 | 1327 |
| diagnose-config | 4543 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2297 | 29 | 898 |
| diagnose-failing-test | 6458 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 14 | 0 | 1788 | 0 | 80 | 2297 | 29 | 1258 |
| explain-event-flow | 5950 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2297 | 33 | 949 |
| explain-ledger-posting | 4698 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2297 | 22 | 894 |
| feature-currency | 7392 | 68 | 198 | 131 | 103 | 537 | 271 | 97 | 0 | 0 | 0 | 1565 | 0 | 823 | 2297 | 55 | 1247 |
| fix-balance-query | 5485 | 50 | 202 | 76 | 73 | 537 | 38 | 24 | 0 | 0 | 0 | 877 | 0 | 94 | 2297 | 32 | 1185 |
| fix-ledger-balance | 6389 | 49 | 204 | 76 | 39 | 537 | 37 | 24 | 0 | 21 | 0 | 1622 | 0 | 245 | 2297 | 52 | 1186 |
| java-explain-orders | 4625 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2297 | 35 | 901 |
| large-change-txtar | 8687 | 0 | 200 | 286 | 153 | 537 | 18 | 43 | 0 | 0 | 0 | 3623 | 0 | 385 | 2297 | 79 | 1066 |
| large-locate-preorder | 9825 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 86 | 0 | 0 | 5080 | 0 | 62 | 2297 | 42 | 1037 |
| locate-topic-name | 4857 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 517 | 0 | 0 | 110 | 2297 | 25 | 970 |
| multi-feature-discount | 9182 | 85 | 202 | 113 | 68 | 537 | 631 | 167 | 0 | 18 | 0 | 1316 | 0 | 1396 | 2297 | 117 | 2235 |
| multi-locate-mapping | 5188 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 947 | 0 | 27 | 2297 | 38 | 958 |
| py-explain-reorder | 4703 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2297 | 40 | 923 |
| py-fix-inventory | 5343 | 49 | 200 | 65 | 36 | 537 | 72 | 16 | 0 | 10 | 0 | 785 | 0 | 71 | 2297 | 43 | 1162 |
| refactor-validate | 7957 | 32 | 196 | 131 | 34 | 537 | 95 | 74 | 0 | 12 | 0 | 2630 | 0 | 532 | 2297 | 68 | 1319 |
| rename-entries | 6457 | 0 | 200 | 76 | 47 | 537 | 37 | 24 | 0 | 21 | 44 | 1697 | 0 | 168 | 2297 | 30 | 1279 |
| rust-explain-ledger | 5194 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 4 | 0 | 928 | 0 | 37 | 2297 | 41 | 1025 |
| session-ledger | 10403 | 618 | 798 | 76 | 204 | 537 | 303 | 48 | 0 | 0 | 0 | 2479 | 31 | 536 | 2297 | 110 | 2366 |
| session-noisy-tests | 18762 | 572 | 607 | 65 | 74 | 537 | 11544 | 16 | 0 | 0 | 0 | 809 | 0 | 480 | 2297 | 81 | 1680 |
| session-resume-constraints | 10499 | 758 | 810 | 76 | 183 | 537 | 263 | 49 | 0 | 14 | 0 | 1791 | 31 | 671 | 2297 | 126 | 2893 |
| session-stale-config | 8978 | 285 | 1022 | 131 | 258 | 537 | 75 | 0 | 0 | 4 | 0 | 2380 | 28 | 312 | 2297 | 118 | 1531 |
| session-x-tools | 34081 | 1522 | 1175 | 286 | 692 | 537 | 21 | 63 | 0 | 95 | 0 | 22788 | 0 | 1252 | 2297 | 209 | 3144 |
| ts-fix-cart-discount | 5275 | 34 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 67 | 2297 | 44 | 1033 |
| ts-locate-discount | 4882 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2297 | 30 | 915 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
