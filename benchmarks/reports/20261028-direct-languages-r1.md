# Direct-mode suite: direct-20261027T232914Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T23:29:14Z, finished 2026-10-27T23:45:22Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**23/23 passed (100%)**, 6.2 model calls and 7.1 tool calls per task, 45006 prompt tokens per task with a 91% prompt-cache hit rate, 1110 generated tokens per task, 7 KB of tool results per task, 42 s per task, 27228 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 6 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 41.6 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1781 ms (median). 307 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 630 prompt tokens recomputed. Cost of a passed task: 5060 processed tokens and 42 s; of a failed one: 0 and 0 s. Peak memory: harness 35 MiB, model server 27638 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/15 | 0 | 6.3 | 6.5 | 51533 | 45.7 | 0.2 | 46 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8378 | 7.8 | 0.0 | 8 |
| multi | 2/2 | 0 | 10.5 | 15.5 | 71596 | 75.6 | 0.4 | 76 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15758 | 14.6 | 0.0 | 15 |
| rust | 1/1 | 1 | 4.0 | 6.0 | 17396 | 16.3 | 0.0 | 16 |
| typescript | 2/2 | 0 | 6.5 | 10.0 | 30828 | 34.1 | 0.2 | 34 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 10.0 | 10.0 | 63331 | 87% | 2352 | 86 |
| diagnose | 2/2 | 3.0 | 4.0 | 13216 | 86% | 750 | 25 |
| explain | 5/5 | 2.6 | 2.8 | 11270 | 88% | 356 | 14 |
| feature | 2/2 | 12.0 | 16.5 | 86388 | 93% | 2734 | 94 |
| small-fix | 4/4 | 5.2 | 6.2 | 26221 | 92% | 704 | 28 |
| large-change | 1/1 | 7.0 | 9.0 | 56062 | 90% | 1399 | 53 |
| large-locate | 1/1 | 3.0 | 3.0 | 17272 | 69% | 340 | 19 |
| locate | 3/3 | 4.0 | 6.7 | 17859 | 90% | 452 | 19 |
| refactor | 2/2 | 7.5 | 8.5 | 42788 | 92% | 805 | 34 |
| session | 2/2 | 15.5 | 12.0 | 199438 | 93% | 3456 | 134 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 10 | 10 | 63331 | 55219 | 8076 | 2352 | 0 | 0 | 4 | 86 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 12547 | 11550 | 4520 | 367 | 0 | 0 | 0 | 15 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 13886 | 11111 | 5979 | 1132 | 0 | 0 | 0 | 36 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 13715 | 11461 | 5777 | 697 | 0 | 0 | 0 | 23 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8510 | 7451 | 4586 | 331 | 0 | 0 | 0 | 12 |  |
| feature-currency | feature | code | pass | 7 | 11 | 48124 | 42831 | 8800 | 1501 | 0 | 0 | 0 | 54 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 19005 | 17350 | 5174 | 628 | 0 | 0 | 0 | 23 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 26630 | 24056 | 6089 | 627 | 0 | 0 | 0 | 26 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8378 | 7423 | 4482 | 172 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 7 | 9 | 56062 | 50314 | 9255 | 1399 | 0 | 0 | 1 | 53 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 3 | 17272 | 11909 | 8886 | 340 | 0 | 0 | 0 | 19 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 9465 | 7424 | 5568 | 193 | 0 | 0 | 0 | 9 |  |
| multi-feature-discount | feature | code | pass | 17 | 22 | 124653 | 118430 | 9690 | 3968 | 0 | 0 | 0 | 134 |  |
| multi-locate-mapping | locate | chat | pass | 4 | 9 | 18539 | 16549 | 5509 | 413 | 0 | 0 | 0 | 18 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8349 | 7377 | 4499 | 185 | 0 | 0 | 0 | 8 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23167 | 21590 | 5092 | 508 | 0 | 0 | 0 | 21 |  |
| refactor-validate | refactor | code | pass | 9 | 9 | 54360 | 50131 | 7728 | 1028 | 0 | 0 | 1 | 43 |  |
| rename-entries | refactor | code | pass | 6 | 8 | 31217 | 28718 | 6010 | 582 | 0 | 0 | 0 | 25 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 6 | 17396 | 15677 | 5238 | 393 | 0 | 0 | 0 | 16 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 70614 | 65707 | 8095 | 2276 | 0 | 0 | 0 | 82 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 20 | 15 | 328261 | 304488 | 27228 | 4637 | 0 | 0 | 0 | 187 |  |
| ts-fix-cart-discount | small-fix | code | pass | 7 | 11 | 36082 | 33525 | 6064 | 1055 | 0 | 0 | 0 | 40 |  |
| ts-locate-discount | locate | chat | pass | 6 | 9 | 25573 | 23989 | 5095 | 750 | 0 | 0 | 0 | 28 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 8076 | 45 | 200 | 76 | 0 | 537 | 71 | 190 | 0 | 6 | 0 | 0 | 1823 | 62 | 1675 | 2199 | 65 | 1127 |
| diagnose-config | 4520 | 0 | 199 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 118 | 0 | 355 | 0 | 36 | 2199 | 29 | 916 |
| diagnose-failing-test | 5979 | 0 | 206 | 76 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 76 | 2199 | 29 | 953 |
| explain-event-flow | 5777 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 33 | 0 | 0 | 1695 | 0 | 35 | 2199 | 33 | 917 |
| explain-ledger-posting | 4586 | 0 | 203 | 76 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 8800 | 95 | 198 | 131 | 0 | 537 | 81 | 45 | 0 | 33 | 0 | 0 | 3474 | 0 | 846 | 2199 | 55 | 1106 |
| fix-balance-query | 5174 | 53 | 202 | 76 | 48 | 537 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 143 | 2199 | 32 | 945 |
| fix-ledger-balance | 6089 | 55 | 204 | 76 | 78 | 537 | 46 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 238 | 2199 | 52 | 982 |
| java-explain-orders | 4482 | 0 | 197 | 65 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 9255 | 6 | 200 | 286 | 0 | 537 | 18 | 203 | 0 | 0 | 0 | 0 | 3731 | 0 | 907 | 2199 | 79 | 1089 |
| large-locate-preorder | 8886 | 0 | 201 | 286 | 0 | 537 | 0 | 0 | 46 | 0 | 70 | 0 | 4526 | 0 | 57 | 2199 | 42 | 922 |
| locate-topic-name | 5568 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 1544 | 0 | 0 | 0 | 41 | 2199 | 25 | 894 |
| multi-feature-discount | 9690 | 327 | 202 | 113 | 130 | 537 | 658 | 192 | 0 | 0 | 0 | 0 | 2040 | 0 | 1530 | 2199 | 117 | 1645 |
| multi-locate-mapping | 5509 | 0 | 203 | 113 | 0 | 537 | 0 | 0 | 0 | 33 | 0 | 0 | 1263 | 0 | 78 | 2199 | 38 | 1045 |
| py-explain-reorder | 4499 | 0 | 201 | 65 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2199 | 40 | 860 |
| py-fix-inventory | 5092 | 50 | 200 | 65 | 46 | 537 | 72 | 16 | 0 | 10 | 0 | 0 | 785 | 0 | 76 | 2199 | 43 | 993 |
| refactor-validate | 7728 | 63 | 196 | 131 | 73 | 537 | 66 | 96 | 0 | 6 | 0 | 0 | 2630 | 0 | 521 | 2199 | 68 | 1142 |
| rename-entries | 6010 | 44 | 200 | 76 | 0 | 537 | 37 | 24 | 0 | 21 | 3 | 0 | 1623 | 0 | 148 | 2199 | 30 | 1068 |
| rust-explain-ledger | 5238 | 0 | 205 | 89 | 0 | 537 | 0 | 0 | 0 | 12 | 0 | 0 | 1119 | 0 | 56 | 2199 | 41 | 980 |
| session-ledger | 8095 | 640 | 798 | 76 | 0 | 537 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 31 | 515 | 2199 | 110 | 1186 |
| session-x-tools | 27228 | 1317 | 1175 | 286 | 0 | 537 | 21 | 63 | 0 | 179 | 2944 | 0 | 15919 | 0 | 959 | 2199 | 209 | 1420 |
| ts-fix-cart-discount | 6064 | 119 | 200 | 82 | 0 | 537 | 265 | 64 | 0 | 12 | 0 | 0 | 1070 | 0 | 314 | 2199 | 44 | 1158 |
| ts-locate-discount | 5095 | 0 | 199 | 82 | 0 | 537 | 0 | 0 | 0 | 15 | 0 | 20 | 672 | 0 | 264 | 2199 | 30 | 1077 |

Environment: build dev f32ea3d8d2cb-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
