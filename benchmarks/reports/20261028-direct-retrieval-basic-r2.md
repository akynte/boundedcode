# Direct-mode suite: direct-20261028T100835Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T10:08:35Z, finished 2026-10-28T10:23:29Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=basic, seed_pack=true, working_set_tokens=0.

**23/23 passed (100%)**, 5.3 model calls and 6.4 tool calls per task, 40927 prompt tokens per task with a 91% prompt-cache hit rate, 1044 generated tokens per task, 7 KB of tool results per task, 39 s per task, 29872 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 38.4 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1781 ms (median). 280 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 630 prompt tokens recomputed. Cost of a passed task: 4730 processed tokens and 39 s; of a failed one: 0 and 0 s. Peak memory: harness 38 MiB, model server 29380 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/15 | 0 | 5.9 | 6.2 | 50823 | 42.3 | 0.2 | 43 |
| java | 1/1 | 1 | 2.0 | 2.0 | 8736 | 11.9 | 0.0 | 12 |
| multi | 2/2 | 0 | 6.0 | 13.5 | 40542 | 72.7 | 0.5 | 73 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15757 | 17.3 | 0.0 | 17 |
| rust | 1/1 | 1 | 3.0 | 4.0 | 12913 | 13.6 | 0.0 | 14 |
| typescript | 2/2 | 0 | 5.0 | 7.5 | 22365 | 22.2 | 0.1 | 22 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 5.0 | 22936 | 93% | 573 | 23 |
| diagnose | 2/2 | 3.0 | 4.0 | 13258 | 86% | 488 | 18 |
| explain | 5/5 | 2.4 | 2.6 | 10568 | 86% | 398 | 15 |
| feature | 2/2 | 7.5 | 15.5 | 54304 | 90% | 2906 | 92 |
| small-fix | 4/4 | 5.0 | 5.8 | 24225 | 92% | 599 | 24 |
| large-change | 1/1 | 6.0 | 6.0 | 45990 | 89% | 1231 | 46 |
| large-locate | 1/1 | 3.0 | 3.0 | 17178 | 69% | 294 | 18 |
| locate | 3/3 | 3.3 | 5.3 | 14670 | 87% | 386 | 16 |
| refactor | 2/2 | 6.5 | 7.5 | 35589 | 92% | 750 | 30 |
| session | 2/2 | 16.0 | 14.0 | 227583 | 93% | 4044 | 154 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 5 | 22936 | 21222 | 5229 | 573 | 0 | 0 | 0 | 23 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 12639 | 11596 | 4566 | 308 | 0 | 0 | 0 | 13 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 13876 | 11097 | 5983 | 668 | 0 | 0 | 0 | 23 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 14333 | 11770 | 6086 | 848 | 0 | 0 | 0 | 28 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8510 | 7451 | 4586 | 280 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 6 | 11 | 40804 | 35670 | 8645 | 1464 | 0 | 0 | 0 | 52 |  |
| fix-balance-query | small-fix | code | pass | 5 | 5 | 23386 | 21670 | 5231 | 507 | 0 | 0 | 0 | 22 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 26616 | 24058 | 6073 | 633 | 0 | 0 | 0 | 25 |  |
| java-explain-orders | explain | chat | pass | 2 | 2 | 8736 | 7423 | 4840 | 319 | 0 | 0 | 0 | 12 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 45990 | 40724 | 8777 | 1231 | 0 | 0 | 0 | 46 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 3 | 17178 | 11862 | 8839 | 294 | 0 | 0 | 0 | 18 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 9733 | 7424 | 5836 | 292 | 0 | 0 | 0 | 12 |  |
| multi-feature-discount | feature | code | pass | 9 | 20 | 67805 | 61781 | 9523 | 4347 | 0 | 0 | 0 | 133 |  |
| multi-locate-mapping | locate | chat | pass | 3 | 7 | 13279 | 11469 | 5333 | 339 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8349 | 7377 | 4499 | 213 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23165 | 21576 | 5104 | 686 | 0 | 0 | 0 | 26 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 39970 | 36914 | 6563 | 896 | 0 | 0 | 1 | 36 |  |
| rename-entries | refactor | code | pass | 6 | 8 | 31208 | 28705 | 6014 | 603 | 0 | 0 | 0 | 25 |  |
| rust-explain-ledger | explain | chat | pass | 3 | 4 | 12913 | 11475 | 4961 | 332 | 0 | 0 | 0 | 14 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 65014 | 60172 | 8034 | 2683 | 0 | 0 | 0 | 91 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 22 | 19 | 390152 | 363727 | 29872 | 5405 | 0 | 0 | 1 | 217 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 8 | 23732 | 21840 | 5407 | 571 | 0 | 0 | 0 | 24 |  |
| ts-locate-discount | locate | chat | pass | 5 | 7 | 20998 | 19540 | 4973 | 527 | 0 | 0 | 0 | 21 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5229 | 13 | 200 | 76 | 0 | 537 | 44 | 0 | 0 | 6 | 0 | 0 | 712 | 31 | 383 | 2199 | 65 | 963 |
| diagnose-config | 4566 | 0 | 199 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 171 | 0 | 355 | 0 | 43 | 2199 | 29 | 902 |
| diagnose-failing-test | 5983 | 18 | 206 | 76 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 70 | 2199 | 29 | 945 |
| explain-event-flow | 6086 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 33 | 224 | 0 | 1672 | 0 | 124 | 2199 | 33 | 936 |
| explain-ledger-posting | 4586 | 0 | 203 | 76 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 8645 | 62 | 198 | 131 | 0 | 537 | 81 | 45 | 0 | 33 | 0 | 0 | 3474 | 0 | 732 | 2199 | 55 | 1098 |
| fix-balance-query | 5231 | 60 | 202 | 76 | 48 | 537 | 38 | 24 | 0 | 29 | 0 | 0 | 877 | 0 | 125 | 2199 | 32 | 984 |
| fix-ledger-balance | 6073 | 57 | 204 | 76 | 78 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 230 | 2199 | 52 | 981 |
| java-explain-orders | 4840 | 0 | 197 | 65 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 869 | 0 | 23 | 2199 | 35 | 861 |
| large-change-txtar | 8777 | 6 | 200 | 286 | 0 | 537 | 18 | 141 | 0 | 0 | 0 | 0 | 3623 | 0 | 660 | 2199 | 79 | 1028 |
| large-locate-preorder | 8839 | 0 | 201 | 286 | 0 | 537 | 0 | 0 | 86 | 0 | 0 | 0 | 4526 | 0 | 48 | 2199 | 42 | 914 |
| locate-topic-name | 5836 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 1733 | 0 | 0 | 0 | 114 | 2199 | 25 | 900 |
| multi-feature-discount | 9523 | 132 | 202 | 113 | 130 | 537 | 340 | 200 | 0 | 0 | 0 | 0 | 1316 | 0 | 2771 | 2199 | 117 | 1466 |
| multi-locate-mapping | 5333 | 0 | 203 | 113 | 0 | 537 | 0 | 0 | 0 | 33 | 0 | 0 | 1164 | 0 | 61 | 2199 | 38 | 985 |
| py-explain-reorder | 4499 | 0 | 201 | 65 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2199 | 40 | 860 |
| py-fix-inventory | 5104 | 44 | 200 | 65 | 46 | 537 | 20 | 16 | 0 | 10 | 0 | 0 | 785 | 0 | 150 | 2199 | 43 | 989 |
| refactor-validate | 6563 | 49 | 196 | 131 | 73 | 537 | 81 | 96 | 0 | 6 | 0 | 0 | 1565 | 0 | 497 | 2199 | 68 | 1065 |
| rename-entries | 6014 | 55 | 200 | 76 | 0 | 537 | 37 | 24 | 0 | 32 | 3 | 0 | 1599 | 0 | 149 | 2199 | 30 | 1073 |
| rust-explain-ledger | 4961 | 0 | 205 | 89 | 0 | 537 | 0 | 0 | 0 | 4 | 0 | 0 | 928 | 0 | 35 | 2199 | 41 | 923 |
| session-ledger | 8034 | 603 | 798 | 76 | 0 | 537 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 31 | 519 | 2199 | 110 | 1158 |
| session-x-tools | 29872 | 1047 | 1175 | 286 | 0 | 537 | 28 | 171 | 0 | 73 | 894 | 41 | 19919 | 0 | 1724 | 2199 | 209 | 1569 |
| ts-fix-cart-discount | 5407 | 50 | 200 | 82 | 0 | 537 | 123 | 54 | 0 | 12 | 0 | 0 | 953 | 0 | 98 | 2199 | 44 | 1055 |
| ts-locate-discount | 4973 | 0 | 199 | 82 | 0 | 537 | 0 | 0 | 0 | 16 | 0 | 15 | 821 | 0 | 57 | 2199 | 30 | 1017 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
