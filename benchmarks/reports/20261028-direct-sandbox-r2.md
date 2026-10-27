# Direct-mode suite: direct-20261027T223501Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T22:35:01Z, finished 2026-10-27T22:49:34Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.7 model calls and 6.0 tool calls per task, 38409 prompt tokens per task with a 91% prompt-cache hit rate, 1005 generated tokens per task, 6 KB of tool results per task, 38 s per task, 26675 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 37.5 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1762 ms (median). 296 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 614 prompt tokens recomputed. Cost of a passed task: 4419 processed tokens and 37 s; of a failed one: 4210 and 48 s. Peak memory: harness 34 MiB, model server 28859 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.3 | 6.2 | 47373 | 45.1 | 0.2 | 46 |
| java | 1/1 | 1 | 2.0 | 2.0 | 8186 | 8.3 | 0.0 | 8 |
| multi | 1/2 | 0 | 6.5 | 8.0 | 37432 | 41.9 | 0.2 | 42 |
| python | 2/2 | 0 | 4.0 | 4.5 | 17828 | 23.2 | 0.0 | 23 |
| rust | 1/1 | 1 | 6.0 | 10.0 | 24885 | 24.1 | 0.0 | 24 |
| typescript | 2/2 | 0 | 3.5 | 3.5 | 14616 | 12.2 | 0.1 | 12 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 6.0 | 26767 | 93% | 1270 | 42 |
| diagnose | 2/2 | 3.0 | 4.0 | 12478 | 85% | 591 | 21 |
| explain | 5/5 | 3.0 | 4.0 | 12364 | 88% | 375 | 15 |
| feature | 1/2 | 9.0 | 12.0 | 52749 | 93% | 1900 | 66 |
| small-fix | 3/4 | 5.0 | 5.8 | 23181 | 92% | 606 | 25 |
| large-change | 1/1 | 7.0 | 7.0 | 48616 | 89% | 1444 | 53 |
| large-locate | 1/1 | 4.0 | 3.0 | 20415 | 74% | 374 | 21 |
| locate | 3/3 | 2.0 | 1.3 | 7896 | 87% | 194 | 9 |
| refactor | 2/2 | 7.0 | 8.5 | 39282 | 91% | 978 | 38 |
| session | 2/2 | 17.0 | 12.5 | 200182 | 93% | 4098 | 154 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 6 | 26767 | 24907 | 5110 | 1270 | 0 | 0 | 1 | 42 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 11890 | 10825 | 4327 | 313 | 0 | 0 | 0 | 13 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 13067 | 10306 | 5712 | 869 | 0 | 0 | 0 | 29 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 5 | 12966 | 10690 | 5538 | 584 | 0 | 0 | 0 | 20 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 7972 | 6921 | 4317 | 254 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 7 | 9 | 38939 | 35299 | 6886 | 1616 | 0 | 0 | 0 | 55 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 5 | 18048 | 16361 | 4945 | 440 | 0 | 0 | 0 | 19 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 25259 | 22715 | 5798 | 661 | 0 | 0 | 0 | 26 |  |
| java-explain-orders | explain | chat | pass | 2 | 2 | 8186 | 6887 | 4565 | 180 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 7 | 7 | 48616 | 43202 | 8660 | 1444 | 0 | 0 | 0 | 53 |  |
| large-locate-preorder | large-locate | chat | pass | 4 | 3 | 20415 | 15033 | 8640 | 374 | 0 | 0 | 0 | 21 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 7725 | 6892 | 4099 | 277 | 0 | 0 | 0 | 11 |  |
| multi-feature-discount | feature | code | FAIL | 11 | 15 | 66559 | 62450 | 7339 | 2184 | 0 | 0 | 0 | 77 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 1 | 8305 | 6882 | 4689 | 150 | 0 | 0 | 0 | 8 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 7811 | 6847 | 4230 | 276 | 0 | 0 | 0 | 11 |  |
| py-fix-inventory | small-fix | code | pass | 6 | 7 | 27844 | 25848 | 5246 | 1002 | 0 | 0 | 0 | 36 |  |
| refactor-validate | refactor | code | pass | 8 | 8 | 48836 | 44534 | 7544 | 1049 | 0 | 0 | 1 | 42 |  |
| rename-entries | refactor | code | pass | 6 | 9 | 29728 | 27122 | 5856 | 907 | 0 | 0 | 0 | 33 |  |
| rust-explain-ledger | explain | chat | pass | 6 | 10 | 24885 | 22817 | 5318 | 579 | 0 | 0 | 0 | 24 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 13 | 9 | 76809 | 71922 | 7814 | 2904 | 0 | 0 | 0 | 102 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 21 | 16 | 323555 | 300070 | 26675 | 5293 | 0 | 0 | 1 | 207 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 6 | 21573 | 19923 | 4904 | 323 | 0 | 0 | 0 | 17 |  |
| ts-locate-discount | locate | chat | pass | 2 | 1 | 7659 | 6846 | 4079 | 156 | 0 | 0 | 0 | 7 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:multi_edit | tool:read | tool:read_many | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5110 | 0 | 200 | 68 | 0 | 537 | 40 | 25 | 6 | 0 | 0 | 0 | 712 | 0 | 53 | 459 | 1992 | 65 | 953 |
| diagnose-config | 4327 | 0 | 199 | 121 | 0 | 537 | 0 | 0 | 0 | 171 | 0 | 0 | 355 | 0 | 0 | 53 | 1992 | 29 | 870 |
| diagnose-failing-test | 5712 | 16 | 206 | 68 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 70 | 1992 | 29 | 891 |
| explain-event-flow | 5538 | 0 | 197 | 121 | 0 | 537 | 0 | 0 | 33 | 0 | 0 | 0 | 1695 | 0 | 0 | 43 | 1992 | 33 | 887 |
| explain-ledger-posting | 4317 | 0 | 203 | 68 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 1992 | 22 | 787 |
| feature-currency | 6886 | 85 | 198 | 121 | 0 | 537 | 196 | 23 | 33 | 0 | 0 | 24 | 1588 | 0 | 0 | 1023 | 1992 | 55 | 1011 |
| fix-balance-query | 4945 | 81 | 202 | 68 | 48 | 537 | 38 | 24 | 0 | 0 | 5 | 0 | 877 | 0 | 0 | 125 | 1992 | 32 | 916 |
| fix-ledger-balance | 5798 | 51 | 204 | 68 | 78 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 0 | 230 | 1992 | 52 | 927 |
| java-explain-orders | 4565 | 0 | 197 | 51 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 869 | 0 | 0 | 23 | 1992 | 35 | 807 |
| large-change-txtar | 8660 | 6 | 200 | 261 | 0 | 537 | 18 | 141 | 0 | 0 | 13 | 0 | 3623 | 0 | 0 | 798 | 1992 | 79 | 992 |
| large-locate-preorder | 8640 | 0 | 201 | 261 | 0 | 537 | 0 | 0 | 84 | 70 | 0 | 0 | 4526 | 0 | 0 | 53 | 1992 | 42 | 874 |
| locate-topic-name | 4099 | 0 | 197 | 121 | 0 | 537 | 0 | 0 | 0 | 270 | 0 | 0 | 0 | 0 | 0 | 113 | 1992 | 25 | 844 |
| multi-feature-discount | 7339 | 73 | 202 | 92 | 130 | 537 | 122 | 111 | 0 | 0 | 0 | 0 | 1316 | 0 | 0 | 1333 | 1992 | 117 | 1314 |
| multi-locate-mapping | 4689 | 0 | 203 | 92 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1022 | 0 | 21 | 1992 | 38 | 784 |
| py-explain-reorder | 4230 | 0 | 201 | 57 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 1992 | 40 | 806 |
| py-fix-inventory | 5246 | 68 | 200 | 57 | 46 | 537 | 106 | 17 | 10 | 0 | 0 | 0 | 932 | 0 | 0 | 241 | 1992 | 43 | 997 |
| refactor-validate | 7544 | 121 | 196 | 121 | 73 | 537 | 81 | 74 | 12 | 0 | 0 | 24 | 2637 | 0 | 0 | 559 | 1992 | 68 | 1049 |
| rename-entries | 5856 | 76 | 200 | 68 | 0 | 537 | 40 | 24 | 32 | 3 | 0 | 0 | 1599 | 0 | 0 | 211 | 1992 | 30 | 1044 |
| rust-explain-ledger | 5318 | 0 | 205 | 75 | 0 | 537 | 0 | 0 | 8 | 0 | 21 | 0 | 1179 | 0 | 0 | 214 | 1992 | 41 | 1046 |
| session-ledger | 7814 | 599 | 798 | 68 | 0 | 537 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 0 | 31 | 517 | 1992 | 110 | 1159 |
| session-x-tools | 26675 | 1113 | 1175 | 261 | 0 | 537 | 21 | 184 | 263 | 0 | 0 | 0 | 18083 | 0 | 0 | 1491 | 1992 | 209 | 1346 |
| ts-fix-cart-discount | 4904 | 37 | 200 | 68 | 0 | 537 | 123 | 9 | 12 | 0 | 0 | 0 | 847 | 0 | 0 | 78 | 1992 | 44 | 957 |
| ts-locate-discount | 4079 | 0 | 199 | 68 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 453 | 0 | 9 | 1992 | 30 | 791 |

Environment: build dev 79ce13b927e3-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
