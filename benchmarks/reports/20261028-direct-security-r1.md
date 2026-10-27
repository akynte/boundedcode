# Direct-mode suite: direct-20261027T211808Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T21:18:08Z, finished 2026-10-27T21:33:33Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 6.0 model calls and 6.7 tool calls per task, 49780 prompt tokens per task with a 92% prompt-cache hit rate, 1033 generated tokens per task, 7 KB of tool results per task, 40 s per task, 34152 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 39.7 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1768 ms (median). 278 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 614 prompt tokens recomputed. Cost of a passed task: 5058 processed tokens and 38 s; of a failed one: 5374 and 64 s. Peak memory: harness 35 MiB, model server 29336 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.4 | 6.5 | 61985 | 46.5 | 0.2 | 47 |
| java | 1/1 | 1 | 2.0 | 1.0 | 7828 | 6.7 | 0.0 | 7 |
| multi | 1/2 | 0 | 9.0 | 12.5 | 55875 | 56.8 | 0.2 | 57 |
| python | 2/2 | 0 | 3.5 | 3.5 | 14837 | 16.7 | 0.0 | 17 |
| rust | 1/1 | 1 | 6.0 | 10.0 | 24592 | 23.1 | 0.0 | 23 |
| typescript | 2/2 | 0 | 5.0 | 6.5 | 20656 | 20.0 | 0.1 | 20 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 6.0 | 26164 | 80% | 1093 | 44 |
| diagnose | 2/2 | 3.0 | 4.0 | 12562 | 84% | 794 | 26 |
| explain | 5/5 | 3.0 | 3.8 | 12295 | 88% | 373 | 15 |
| feature | 1/2 | 11.0 | 13.0 | 74378 | 93% | 2122 | 76 |
| small-fix | 3/4 | 5.0 | 5.2 | 22952 | 92% | 582 | 24 |
| large-change | 1/1 | 6.0 | 6.0 | 43917 | 88% | 1163 | 47 |
| large-locate | 1/1 | 5.0 | 5.0 | 28562 | 74% | 741 | 34 |
| locate | 3/3 | 3.3 | 5.0 | 13360 | 89% | 393 | 16 |
| refactor | 2/2 | 6.5 | 8.0 | 33688 | 92% | 938 | 35 |
| session | 2/2 | 18.0 | 15.5 | 305837 | 94% | 3842 | 155 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 6 | 26164 | 20983 | 5161 | 1093 | 0 | 0 | 0 | 44 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 12050 | 10905 | 4407 | 412 | 0 | 0 | 0 | 16 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 13075 | 10318 | 5708 | 1176 | 0 | 0 | 0 | 37 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 12866 | 10640 | 5488 | 675 | 0 | 0 | 0 | 23 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 2 | 8296 | 6921 | 4641 | 237 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 7 | 7 | 49345 | 43811 | 8780 | 1429 | 0 | 0 | 1 | 53 |  |
| fix-balance-query | small-fix | code | FAIL | 5 | 5 | 23232 | 21312 | 5174 | 773 | 0 | 0 | 0 | 29 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 25215 | 22686 | 5783 | 566 | 0 | 0 | 0 | 24 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 7828 | 6887 | 4207 | 125 | 0 | 0 | 0 | 7 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 43917 | 38736 | 8431 | 1163 | 0 | 0 | 0 | 47 |  |
| large-locate-preorder | large-locate | chat | pass | 5 | 5 | 28562 | 21260 | 10556 | 741 | 0 | 0 | 0 | 34 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 8008 | 6892 | 4382 | 268 | 0 | 0 | 0 | 11 |  |
| multi-feature-discount | feature | code | FAIL | 15 | 19 | 99411 | 94171 | 8454 | 2815 | 0 | 0 | 0 | 99 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 3 | 6 | 12339 | 10597 | 5004 | 371 | 0 | 0 | 0 | 15 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 7892 | 6847 | 4311 | 259 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 21782 | 20240 | 4796 | 594 | 0 | 0 | 0 | 23 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 32098 | 29146 | 6202 | 883 | 0 | 0 | 1 | 34 |  |
| rename-entries | refactor | code | pass | 7 | 10 | 35278 | 32679 | 5845 | 992 | 0 | 0 | 0 | 36 |  |
| rust-explain-ledger | explain | chat | pass | 6 | 10 | 24592 | 22660 | 5182 | 567 | 0 | 0 | 0 | 23 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 62340 | 57513 | 7766 | 2498 | 0 | 0 | 0 | 87 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 26 | 22 | 549334 | 518352 | 34152 | 5186 | 0 | 0 | 1 | 223 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 6 | 21580 | 19925 | 4909 | 397 | 0 | 0 | 0 | 19 |  |
| ts-locate-discount | locate | chat | pass | 5 | 7 | 19733 | 18252 | 4735 | 541 | 0 | 0 | 0 | 21 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:read | tool:read_many | tool:todo_write | tool:web_search | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5161 | 48 | 200 | 68 | 0 | 537 | 40 | 0 | 0 | 0 | 7 | 712 | 0 | 0 | 0 | 31 | 524 | 1992 | 65 | 937 |
| diagnose-config | 4407 | 0 | 199 | 121 | 0 | 537 | 0 | 0 | 0 | 188 | 0 | 355 | 0 | 0 | 0 | 0 | 124 | 1992 | 29 | 862 |
| diagnose-failing-test | 5708 | 0 | 206 | 68 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 0 | 0 | 74 | 1992 | 29 | 899 |
| explain-event-flow | 5488 | 0 | 197 | 121 | 0 | 537 | 0 | 0 | 33 | 0 | 0 | 1672 | 0 | 0 | 0 | 0 | 36 | 1992 | 33 | 867 |
| explain-ledger-posting | 4641 | 0 | 203 | 68 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 911 | 0 | 0 | 0 | 0 | 23 | 1992 | 22 | 807 |
| feature-currency | 8780 | 72 | 198 | 121 | 0 | 537 | 1265 | 98 | 0 | 0 | 0 | 23 | 2513 | 0 | 0 | 0 | 901 | 1992 | 55 | 1005 |
| fix-balance-query | 5174 | 82 | 202 | 68 | 48 | 537 | 153 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 0 | 0 | 222 | 1992 | 32 | 937 |
| fix-ledger-balance | 5783 | 44 | 204 | 68 | 78 | 537 | 46 | 24 | 0 | 0 | 0 | 1598 | 0 | 0 | 0 | 0 | 210 | 1992 | 52 | 930 |
| java-explain-orders | 4207 | 0 | 197 | 51 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 0 | 0 | 11 | 1992 | 35 | 787 |
| large-change-txtar | 8431 | 34 | 200 | 261 | 0 | 537 | 18 | 141 | 0 | 0 | 0 | 3623 | 0 | 0 | 0 | 0 | 575 | 1992 | 79 | 971 |
| large-locate-preorder | 10556 | 0 | 201 | 261 | 0 | 537 | 0 | 0 | 84 | 1638 | 0 | 4526 | 0 | 0 | 0 | 0 | 308 | 1992 | 42 | 967 |
| locate-topic-name | 4382 | 0 | 197 | 121 | 0 | 537 | 0 | 0 | 0 | 551 | 0 | 0 | 0 | 0 | 0 | 0 | 115 | 1992 | 25 | 844 |
| multi-feature-discount | 8454 | 133 | 202 | 92 | 130 | 537 | 138 | 111 | 0 | 0 | 0 | 1316 | 0 | 323 | 0 | 0 | 1895 | 1992 | 117 | 1468 |
| multi-locate-mapping | 5004 | 0 | 203 | 92 | 0 | 537 | 0 | 0 | 26 | 0 | 0 | 1155 | 0 | 0 | 0 | 0 | 54 | 1992 | 38 | 907 |
| py-explain-reorder | 4311 | 0 | 201 | 57 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 0 | 0 | 96 | 1992 | 40 | 806 |
| py-fix-inventory | 4796 | 0 | 200 | 57 | 46 | 537 | 20 | 17 | 10 | 0 | 0 | 785 | 0 | 0 | 0 | 0 | 154 | 1992 | 43 | 935 |
| refactor-validate | 6202 | 34 | 196 | 121 | 73 | 537 | 81 | 74 | 6 | 0 | 0 | 1565 | 0 | 0 | 0 | 0 | 499 | 1992 | 68 | 956 |
| rename-entries | 5845 | 47 | 200 | 68 | 0 | 537 | 40 | 24 | 21 | 6 | 0 | 1623 | 0 | 0 | 0 | 0 | 174 | 1992 | 30 | 1083 |
| rust-explain-ledger | 5182 | 0 | 205 | 75 | 0 | 537 | 0 | 0 | 0 | 0 | 21 | 1193 | 0 | 0 | 0 | 0 | 79 | 1992 | 41 | 1039 |
| session-ledger | 7766 | 668 | 798 | 68 | 0 | 537 | 188 | 24 | 0 | 0 | 0 | 1791 | 0 | 0 | 0 | 31 | 457 | 1992 | 110 | 1102 |
| session-x-tools | 34152 | 1502 | 1175 | 261 | 0 | 537 | 21 | 181 | 95 | 913 | 0 | 24397 | 0 | 0 | 64 | 0 | 1158 | 1992 | 209 | 1647 |
| ts-fix-cart-discount | 4909 | 39 | 200 | 68 | 0 | 537 | 123 | 9 | 12 | 0 | 0 | 847 | 0 | 0 | 0 | 0 | 81 | 1992 | 44 | 957 |
| ts-locate-discount | 4735 | 0 | 199 | 68 | 0 | 537 | 0 | 0 | 16 | 0 | 15 | 821 | 0 | 0 | 0 | 0 | 94 | 1992 | 30 | 963 |

Environment: build dev a4311aeaf04e-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
