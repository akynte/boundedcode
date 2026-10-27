# Direct-mode suite: direct-20261027T222126Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T22:21:26Z, finished 2026-10-27T22:35:01Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**22/23 passed (96%)**, 5.5 model calls and 6.0 tool calls per task, 40578 prompt tokens per task with a 91% prompt-cache hit rate, 917 generated tokens per task, 6 KB of tool results per task, 35 s per task, 28177 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 35.1 s in model calls, 0.2 s in tools (with the checks after edits), 0.02 s in the harness itself; time to first token 1768 ms (median). 224 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 614 prompt tokens recomputed. Cost of a passed task: 4469 processed tokens and 36 s; of a failed one: 2330 and 24 s. Peak memory: harness 35 MiB, model server 26509 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.1 | 6.2 | 50390 | 39.3 | 0.2 | 40 |
| java | 1/1 | 1 | 2.0 | 1.0 | 7828 | 7.8 | 0.0 | 8 |
| multi | 2/2 | 0 | 7.0 | 11.0 | 45335 | 63.2 | 0.3 | 64 |
| python | 2/2 | 0 | 3.5 | 3.5 | 14802 | 17.8 | 0.0 | 18 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 16175 | 15.4 | 0.0 | 15 |
| typescript | 2/2 | 0 | 4.0 | 5.0 | 16585 | 16.1 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 5.0 | 21606 | 77% | 804 | 35 |
| diagnose | 2/2 | 3.0 | 4.0 | 12558 | 84% | 530 | 19 |
| explain | 5/5 | 2.6 | 2.6 | 10626 | 87% | 370 | 14 |
| feature | 2/2 | 9.5 | 14.0 | 64386 | 91% | 2540 | 85 |
| small-fix | 3/4 | 4.8 | 5.0 | 21634 | 91% | 582 | 23 |
| large-change | 1/1 | 6.0 | 6.0 | 44009 | 88% | 1056 | 41 |
| large-locate | 1/1 | 2.0 | 2.0 | 7827 | 90% | 236 | 10 |
| locate | 3/3 | 2.7 | 3.3 | 10498 | 89% | 332 | 13 |
| refactor | 2/2 | 6.5 | 7.5 | 33794 | 92% | 738 | 30 |
| session | 2/2 | 17.5 | 15.5 | 233610 | 94% | 3102 | 129 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 5 | 21606 | 16628 | 4962 | 804 | 0 | 0 | 0 | 35 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 12047 | 10884 | 4425 | 446 | 0 | 0 | 0 | 17 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 13070 | 10306 | 5715 | 615 | 0 | 0 | 0 | 22 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 13344 | 10879 | 5727 | 754 | 0 | 0 | 0 | 25 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 7972 | 6921 | 4317 | 280 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 8 | 10 | 49975 | 45130 | 8087 | 1570 | 0 | 0 | 1 | 56 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 18014 | 16325 | 4947 | 641 | 0 | 0 | 0 | 24 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 25156 | 22664 | 5746 | 689 | 0 | 0 | 0 | 27 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 7828 | 6887 | 4207 | 167 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 44009 | 38810 | 8449 | 1056 | 0 | 0 | 0 | 41 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 2 | 7827 | 7053 | 4040 | 236 | 0 | 0 | 0 | 10 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 8024 | 6892 | 4398 | 288 | 0 | 0 | 0 | 11 |  |
| multi-feature-discount | feature | code | pass | 11 | 18 | 78798 | 72424 | 9604 | 3510 | 0 | 0 | 0 | 114 |  |
| multi-locate-mapping | locate | chat | pass | 3 | 4 | 11872 | 10736 | 4398 | 322 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 7811 | 6847 | 4230 | 299 | 0 | 0 | 0 | 11 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 21794 | 20240 | 4808 | 638 | 0 | 0 | 0 | 25 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 38018 | 35013 | 6251 | 874 | 0 | 0 | 1 | 35 |  |
| rename-entries | refactor | code | pass | 6 | 8 | 29569 | 27064 | 5755 | 602 | 0 | 0 | 0 | 25 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 16175 | 14561 | 4872 | 349 | 0 | 0 | 0 | 15 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 12 | 72662 | 67483 | 8110 | 2200 | 0 | 0 | 0 | 81 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 23 | 19 | 394559 | 369564 | 28177 | 4005 | 0 | 0 | 1 | 176 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 6 | 21571 | 19923 | 4902 | 358 | 0 | 0 | 0 | 18 |  |
| ts-locate-discount | locate | chat | pass | 3 | 4 | 11599 | 10512 | 4349 | 385 | 0 | 0 | 0 | 15 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:multi_edit | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 4962 | 0 | 200 | 68 | 0 | 537 | 40 | 0 | 6 | 0 | 0 | 0 | 712 | 31 | 407 | 1992 | 65 | 904 |
| diagnose-config | 4425 | 0 | 199 | 121 | 0 | 537 | 0 | 0 | 0 | 171 | 0 | 0 | 355 | 0 | 159 | 1992 | 29 | 862 |
| diagnose-failing-test | 5715 | 19 | 206 | 68 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 70 | 1992 | 29 | 891 |
| explain-event-flow | 5727 | 0 | 197 | 121 | 0 | 537 | 0 | 0 | 28 | 224 | 0 | 0 | 1672 | 0 | 47 | 1992 | 33 | 876 |
| explain-ledger-posting | 4317 | 0 | 203 | 68 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 1992 | 22 | 787 |
| feature-currency | 8087 | 93 | 198 | 121 | 0 | 537 | 81 | 98 | 33 | 0 | 0 | 0 | 2865 | 0 | 956 | 1992 | 55 | 1058 |
| fix-balance-query | 4947 | 78 | 202 | 68 | 48 | 537 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 165 | 1992 | 32 | 886 |
| fix-ledger-balance | 5746 | 0 | 204 | 68 | 78 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 229 | 1992 | 52 | 927 |
| java-explain-orders | 4207 | 0 | 197 | 51 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 1992 | 35 | 787 |
| large-change-txtar | 8449 | 39 | 200 | 261 | 0 | 537 | 18 | 141 | 0 | 0 | 0 | 0 | 3623 | 0 | 586 | 1992 | 79 | 973 |
| large-locate-preorder | 4040 | 0 | 201 | 261 | 0 | 537 | 0 | 0 | 84 | 70 | 0 | 0 | 0 | 0 | 31 | 1992 | 42 | 822 |
| locate-topic-name | 4398 | 0 | 197 | 121 | 0 | 537 | 0 | 0 | 0 | 551 | 0 | 0 | 0 | 0 | 123 | 1992 | 25 | 852 |
| multi-feature-discount | 9604 | 260 | 202 | 92 | 130 | 537 | 161 | 79 | 0 | 0 | 0 | 33 | 2602 | 0 | 2043 | 1992 | 117 | 1356 |
| multi-locate-mapping | 4398 | 0 | 203 | 92 | 0 | 537 | 0 | 0 | 0 | 126 | 8 | 0 | 467 | 0 | 54 | 1992 | 38 | 881 |
| py-explain-reorder | 4230 | 0 | 201 | 57 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 1992 | 40 | 806 |
| py-fix-inventory | 4808 | 0 | 200 | 57 | 46 | 537 | 72 | 17 | 10 | 0 | 0 | 0 | 785 | 0 | 114 | 1992 | 43 | 935 |
| refactor-validate | 6251 | 56 | 196 | 121 | 73 | 537 | 81 | 96 | 6 | 0 | 0 | 0 | 1565 | 0 | 449 | 1992 | 68 | 1011 |
| rename-entries | 5755 | 52 | 200 | 68 | 0 | 537 | 51 | 24 | 21 | 3 | 0 | 0 | 1599 | 0 | 152 | 1992 | 30 | 1026 |
| rust-explain-ledger | 4872 | 0 | 205 | 75 | 0 | 537 | 0 | 0 | 4 | 0 | 18 | 0 | 928 | 0 | 167 | 1992 | 41 | 905 |
| session-ledger | 8110 | 767 | 798 | 68 | 0 | 537 | 188 | 24 | 29 | 0 | 0 | 0 | 1813 | 31 | 532 | 1992 | 110 | 1221 |
| session-x-tools | 28177 | 881 | 1175 | 261 | 0 | 537 | 21 | 186 | 179 | 910 | 0 | 0 | 19473 | 0 | 795 | 1992 | 209 | 1558 |
| ts-fix-cart-discount | 4902 | 37 | 200 | 68 | 0 | 537 | 123 | 9 | 12 | 0 | 0 | 0 | 847 | 0 | 76 | 1992 | 44 | 957 |
| ts-locate-discount | 4349 | 0 | 199 | 68 | 0 | 537 | 0 | 0 | 7 | 0 | 0 | 0 | 620 | 0 | 30 | 1992 | 30 | 866 |

Environment: build dev 79ce13b927e3-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
