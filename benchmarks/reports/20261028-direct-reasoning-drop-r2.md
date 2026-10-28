# Direct-mode suite: direct-20261028T155448Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T15:54:48Z, finished 2026-10-28T16:08:27Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, reasoning_history=drop, reasoning_round_trip=false, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.2 model calls and 5.5 tool calls per task, 36048 prompt tokens per task with a 91% prompt-cache hit rate, 984 generated tokens per task, 5 KB of tool results per task, 36 s per task, 21617 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 35.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1866 ms (median). 311 reasoning tokens per task. 2 prompt-cache invalidations (2 after the history was rewritten), 748 prompt tokens recomputed; 3208 prompt tokens evaluated by the server per task, in 9.3 s. Cost of a passed task: 4031 processed tokens and 32 s; of a failed one: 5886 and 70 s. Peak memory: harness 38 MiB, model server 28556 MiB, GPU 7133 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 5.7 | 5.5 | 43110 | 40.6 | 0.2 | 41 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8348 | 7.5 | 0.0 | 8 |
| multi | 1/2 | 0 | 7.5 | 13.5 | 48790 | 60.7 | 0.3 | 61 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15773 | 15.1 | 0.0 | 15 |
| rust | 1/1 | 1 | 3.0 | 2.0 | 12795 | 11.5 | 0.0 | 12 |
| typescript | 2/2 | 0 | 3.5 | 3.5 | 16094 | 14.9 | 0.1 | 15 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 24164 | 92% | 985 | 33 |
| diagnose | 2/2 | 2.5 | 2.5 | 10882 | 86% | 522 | 18 |
| explain | 5/5 | 2.2 | 1.6 | 9559 | 86% | 302 | 12 |
| feature | 1/2 | 10.0 | 15.0 | 65970 | 93% | 2484 | 81 |
| small-fix | 3/4 | 5.0 | 5.2 | 24374 | 92% | 655 | 25 |
| large-change | 1/1 | 6.0 | 6.0 | 46792 | 88% | 1367 | 49 |
| large-locate | 1/1 | 3.0 | 2.0 | 17656 | 69% | 294 | 18 |
| locate | 3/3 | 2.0 | 2.7 | 8913 | 84% | 299 | 12 |
| refactor | 2/2 | 6.0 | 9.0 | 35373 | 90% | 908 | 34 |
| session | 2/2 | 15.5 | 11.5 | 172008 | 93% | 3567 | 130 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 24164 | 22246 | 5433 | 985 | 0 | 0 | 0 | 33 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8380 | 7519 | 4388 | 245 | 0 | 0 | 0 | 10 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 4 | 13385 | 11156 | 5374 | 799 | 0 | 0 | 0 | 27 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 9784 | 7547 | 5764 | 608 | 0 | 0 | 0 | 20 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8432 | 7412 | 4547 | 290 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 7 | 7 | 43378 | 39718 | 7167 | 1659 | 0 | 0 | 1 | 55 |  |
| fix-balance-query | small-fix | code | FAIL | 5 | 5 | 24563 | 22682 | 5396 | 937 | 0 | 0 | 0 | 33 | …r	0.002s --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 6 | 26261 | 23703 | 6073 | 620 | 0 | 0 | 0 | 25 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8348 | 7408 | 4467 | 159 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 46792 | 41387 | 8916 | 1367 | 0 | 0 | 0 | 49 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 17656 | 12200 | 8979 | 294 | 0 | 0 | 0 | 18 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 9099 | 7498 | 5128 | 219 | 0 | 0 | 0 | 10 |  |
| multi-feature-discount | feature | code | FAIL | 13 | 23 | 88562 | 82917 | 9128 | 3310 | 0 | 0 | 0 | 107 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 4 | 9019 | 7494 | 5052 | 430 | 0 | 0 | 0 | 15 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8437 | 7421 | 4543 | 186 | 0 | 0 | 0 | 8 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23109 | 21547 | 5077 | 580 | 0 | 0 | 0 | 22 |  |
| refactor-validate | refactor | code | pass | 6 | 7 | 33989 | 30915 | 6585 | 973 | 0 | 0 | 1 | 36 |  |
| rename-entries | refactor | code | pass | 6 | 11 | 36757 | 33071 | 7197 | 844 | 0 | 0 | 0 | 31 |  |
| rust-explain-ledger | explain | chat | pass | 3 | 2 | 12795 | 11450 | 4868 | 269 | 0 | 0 | 0 | 12 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 79662 | 74486 | 8301 | 2983 | 0 | 0 | 0 | 99 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 13 | 264353 | 246195 | 21617 | 4151 | 0 | 0 | 0 | 160 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 5 | 23565 | 21929 | 5151 | 482 | 0 | 0 | 0 | 20 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8622 | 7419 | 4730 | 247 | 0 | 0 | 0 | 10 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5433 | 9 | 200 | 76 | 35 | 537 | 40 | 0 | 10 | 0 | 712 | 0 | 32 | 530 | 2199 | 65 | 988 |
| diagnose-config | 4388 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2199 | 29 | 841 |
| diagnose-failing-test | 5374 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 1169 | 0 | 0 | 59 | 2199 | 29 | 926 |
| explain-event-flow | 5764 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2199 | 33 | 861 |
| explain-ledger-posting | 4547 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 7167 | 49 | 198 | 131 | 103 | 537 | 196 | 111 | 0 | 0 | 1565 | 0 | 0 | 988 | 2199 | 55 | 1035 |
| fix-balance-query | 5396 | 112 | 202 | 76 | 73 | 537 | 110 | 24 | 0 | 0 | 877 | 0 | 0 | 163 | 2199 | 32 | 991 |
| fix-ledger-balance | 6073 | 53 | 204 | 76 | 39 | 537 | 37 | 24 | 7 | 0 | 1598 | 0 | 0 | 241 | 2199 | 52 | 1006 |
| java-explain-orders | 4467 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 8916 | 31 | 200 | 286 | 153 | 537 | 18 | 141 | 0 | 0 | 3623 | 0 | 0 | 631 | 2199 | 79 | 1018 |
| large-locate-preorder | 8979 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 70 | 4526 | 0 | 0 | 38 | 2199 | 42 | 883 |
| locate-topic-name | 5128 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 303 | 705 | 0 | 0 | 77 | 2199 | 25 | 881 |
| multi-feature-discount | 9128 | 95 | 202 | 113 | 68 | 537 | 631 | 156 | 20 | 0 | 1316 | 248 | 0 | 1813 | 2199 | 117 | 1613 |
| multi-locate-mapping | 5052 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 5 | 0 | 947 | 0 | 0 | 36 | 2199 | 38 | 906 |
| py-explain-reorder | 4543 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2199 | 40 | 861 |
| py-fix-inventory | 5077 | 47 | 200 | 65 | 36 | 537 | 72 | 16 | 10 | 0 | 785 | 0 | 0 | 74 | 2199 | 43 | 993 |
| refactor-validate | 6585 | 79 | 196 | 131 | 34 | 537 | 81 | 74 | 35 | 0 | 1565 | 0 | 0 | 552 | 2199 | 68 | 1034 |
| rename-entries | 7197 | 28 | 200 | 76 | 47 | 537 | 37 | 24 | 21 | 44 | 2608 | 0 | 0 | 199 | 2199 | 30 | 1147 |
| rust-explain-ledger | 4868 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 871 | 0 | 0 | 19 | 2199 | 41 | 876 |
| session-ledger | 8301 | 624 | 798 | 76 | 183 | 537 | 226 | 24 | 0 | 0 | 1791 | 0 | 31 | 487 | 2199 | 110 | 1215 |
| session-x-tools | 21617 | 1103 | 1175 | 286 | 692 | 537 | 21 | 63 | 95 | 0 | 13103 | 0 | 0 | 805 | 2199 | 209 | 1329 |
| ts-fix-cart-discount | 5151 | 33 | 200 | 82 | 36 | 537 | 123 | 54 | 7 | 0 | 769 | 0 | 0 | 75 | 2199 | 44 | 992 |
| ts-locate-discount | 4730 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2199 | 30 | 861 |

Environment: build dev f95dd3463ef6-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
