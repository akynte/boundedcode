# Direct-mode suite: direct-20261028T120016Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T12:00:16Z, finished 2026-10-28T12:17:14Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**21/23 passed (91%)**, 5.2 model calls and 5.3 tool calls per task, 34369 prompt tokens per task with a 90% prompt-cache hit rate, 1244 generated tokens per task, 6 KB of tool results per task, 44 s per task, 23919 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 43.9 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1838 ms (median). 616 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 748 prompt tokens recomputed. Cost of a passed task: 3027 processed tokens and 26 s; of a failed one: 20576 and 238 s. Peak memory: harness 36 MiB, model server 29028 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 5.5 | 5.2 | 38638 | 53.1 | 0.2 | 54 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8348 | 8.0 | 0.0 | 8 |
| multi | 1/2 | 0 | 9.5 | 13.0 | 65352 | 63.5 | 0.5 | 64 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15800 | 18.1 | 0.0 | 18 |
| rust | 1/1 | 1 | 3.0 | 4.0 | 13010 | 12.6 | 0.0 | 13 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 13634 | 14.2 | 0.1 | 14 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 5.0 | 24473 | 92% | 965 | 34 |
| diagnose | 2/2 | 3.0 | 3.5 | 14226 | 85% | 845 | 28 |
| explain | 5/5 | 2.2 | 2.0 | 9602 | 86% | 304 | 12 |
| feature | 1/2 | 11.0 | 14.0 | 75186 | 94% | 2196 | 77 |
| small-fix | 4/4 | 5.2 | 5.8 | 25976 | 92% | 779 | 29 |
| large-change | 1/1 | 5.0 | 5.0 | 37275 | 87% | 772 | 32 |
| large-locate | 1/1 | 3.0 | 2.0 | 17598 | 69% | 268 | 17 |
| locate | 3/3 | 2.0 | 2.0 | 8764 | 85% | 236 | 10 |
| refactor | 2/2 | 7.0 | 8.0 | 38268 | 93% | 778 | 32 |
| session | 1/2 | 13.5 | 10.0 | 138790 | 91% | 6818 | 227 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 5 | 24473 | 22528 | 5460 | 965 | 0 | 0 | 1 | 34 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8380 | 7519 | 4388 | 254 | 0 | 0 | 0 | 10 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 6 | 20071 | 16526 | 6686 | 1436 | 0 | 0 | 0 | 46 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 9784 | 7547 | 5764 | 531 | 0 | 0 | 0 | 18 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8432 | 7412 | 4547 | 284 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 5 | 5 | 28650 | 25566 | 6599 | 1080 | 0 | 0 | 0 | 38 |  |
| fix-balance-query | small-fix | code | pass | 5 | 5 | 24570 | 22661 | 5424 | 1075 | 0 | 0 | 0 | 36 |  |
| fix-ledger-balance | small-fix | code | pass | 7 | 9 | 37526 | 34721 | 6312 | 947 | 0 | 0 | 0 | 36 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8348 | 7408 | 4467 | 179 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 5 | 37275 | 32297 | 8493 | 772 | 0 | 0 | 0 | 32 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 17598 | 12171 | 8950 | 268 | 0 | 0 | 0 | 17 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8689 | 7498 | 4718 | 123 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 17 | 23 | 121723 | 115233 | 9957 | 3312 | 0 | 0 | 0 | 117 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.001s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 8980 | 7494 | 5013 | 276 | 0 | 0 | 0 | 11 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8437 | 7421 | 4543 | 233 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23162 | 21582 | 5095 | 726 | 0 | 0 | 0 | 27 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 39802 | 36755 | 6554 | 913 | 0 | 0 | 1 | 36 |  |
| rename-entries | refactor | code | pass | 7 | 9 | 36735 | 34068 | 6174 | 643 | 0 | 0 | 0 | 28 |  |
| rust-explain-ledger | explain | chat | pass | 3 | 4 | 13010 | 11539 | 4994 | 292 | 0 | 0 | 0 | 13 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 78027 | 72840 | 8312 | 2730 | 0 | 0 | 0 | 94 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | FAIL | 15 | 10 | 199552 | 179108 | 23919 | 10906 | 0 | 0 | 0 | 359 | …est.go:24: Len() = 65, want 33 FAIL FAIL	golang.org/x/tools/go/ast/inspector	0.127s FAIL ; sh -c grep -l 'Len()' go/ast/inspector/*_test.go \| grep -qv hidden_:  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 18645 | 17077 | 5087 | 367 | 0 | 0 | 0 | 17 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8622 | 7419 | 4730 | 309 | 0 | 0 | 0 | 12 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5460 | 0 | 200 | 76 | 35 | 537 | 40 | 0 | 0 | 6 | 0 | 688 | 93 | 560 | 2199 | 65 | 961 |
| diagnose-config | 4388 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2199 | 29 | 841 |
| diagnose-failing-test | 6686 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 0 | 2407 | 0 | 81 | 2199 | 29 | 978 |
| explain-event-flow | 5764 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2199 | 33 | 861 |
| explain-ledger-posting | 4547 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 6599 | 24 | 198 | 131 | 103 | 537 | 88 | 45 | 0 | 0 | 0 | 1565 | 0 | 694 | 2199 | 55 | 960 |
| fix-balance-query | 5424 | 79 | 202 | 76 | 73 | 537 | 182 | 24 | 0 | 0 | 0 | 877 | 0 | 151 | 2199 | 32 | 992 |
| fix-ledger-balance | 6312 | 44 | 204 | 76 | 39 | 537 | 111 | 24 | 0 | 14 | 0 | 1622 | 0 | 282 | 2199 | 52 | 1108 |
| java-explain-orders | 4467 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 8493 | 6 | 200 | 286 | 153 | 537 | 18 | 43 | 0 | 0 | 0 | 3623 | 0 | 368 | 2199 | 79 | 981 |
| large-locate-preorder | 8950 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 46 | 0 | 0 | 4526 | 0 | 32 | 2199 | 42 | 884 |
| locate-topic-name | 4718 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2199 | 25 | 841 |
| multi-feature-discount | 9957 | 609 | 202 | 113 | 68 | 537 | 1329 | 156 | 0 | 14 | 0 | 1316 | 0 | 1620 | 2199 | 117 | 1677 |
| multi-locate-mapping | 5013 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 947 | 0 | 27 | 2199 | 38 | 881 |
| py-explain-reorder | 4543 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2199 | 40 | 861 |
| py-fix-inventory | 5095 | 46 | 200 | 65 | 36 | 537 | 20 | 16 | 0 | 10 | 0 | 785 | 0 | 149 | 2199 | 43 | 989 |
| refactor-validate | 6554 | 39 | 196 | 131 | 34 | 537 | 81 | 96 | 0 | 12 | 0 | 1565 | 0 | 531 | 2199 | 68 | 1065 |
| rename-entries | 6174 | 74 | 200 | 76 | 47 | 537 | 37 | 24 | 0 | 25 | 3 | 1657 | 0 | 156 | 2199 | 30 | 1109 |
| rust-explain-ledger | 4994 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 4 | 0 | 928 | 0 | 36 | 2199 | 41 | 924 |
| session-ledger | 8312 | 659 | 798 | 76 | 183 | 537 | 188 | 24 | 0 | 14 | 0 | 1791 | 31 | 501 | 2199 | 110 | 1201 |
| session-x-tools | 23919 | 1170 | 1175 | 286 | 692 | 537 | 0 | 31 | 0 | 95 | 31 | 15919 | 0 | 325 | 2199 | 209 | 1250 |
| ts-fix-cart-discount | 5087 | 26 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 63 | 2199 | 44 | 955 |
| ts-locate-discount | 4730 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2199 | 30 | 861 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
