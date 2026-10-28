# Direct-mode suite: direct-20261027T234522Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T23:45:22Z, finished 2026-10-28T00:01:18Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**22/23 passed (96%)**, 6.0 model calls and 6.8 tool calls per task, 49378 prompt tokens per task with a 92% prompt-cache hit rate, 1097 generated tokens per task, 7 KB of tool results per task, 42 s per task, 35246 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 41.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1738 ms (median). 315 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 630 prompt tokens recomputed. Cost of a passed task: 4856 processed tokens and 38 s; of a failed one: 10082 and 123 s. Peak memory: harness 37 MiB, model server 29035 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/15 | 0 | 6.3 | 7.0 | 59411 | 46.9 | 0.3 | 47 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8378 | 7.8 | 0.0 | 8 |
| multi | 2/2 | 0 | 11.0 | 14.0 | 75780 | 77.0 | 0.4 | 78 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15753 | 15.0 | 0.0 | 15 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 17188 | 17.5 | 0.0 | 18 |
| typescript | 2/2 | 0 | 4.0 | 5.5 | 17945 | 17.2 | 0.1 | 17 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 5.0 | 22920 | 93% | 652 | 25 |
| diagnose | 2/2 | 3.5 | 5.0 | 15747 | 87% | 639 | 23 |
| explain | 5/5 | 2.6 | 2.6 | 11228 | 88% | 393 | 15 |
| feature | 2/2 | 13.0 | 19.0 | 96202 | 93% | 3042 | 105 |
| small-fix | 4/4 | 4.8 | 5.5 | 23075 | 92% | 557 | 23 |
| large-change | 1/1 | 6.0 | 8.0 | 49876 | 82% | 1214 | 51 |
| large-locate | 1/1 | 3.0 | 3.0 | 13585 | 88% | 357 | 15 |
| locate | 3/3 | 2.7 | 3.0 | 10993 | 91% | 284 | 12 |
| refactor | 2/2 | 6.5 | 9.0 | 35969 | 92% | 939 | 36 |
| session | 1/2 | 18.5 | 15.5 | 286024 | 93% | 4358 | 170 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 5 | 22920 | 21230 | 5205 | 652 | 0 | 0 | 0 | 25 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 12547 | 11550 | 4520 | 369 | 0 | 0 | 0 | 14 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 7 | 18947 | 15885 | 6262 | 909 | 0 | 0 | 0 | 32 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 13715 | 11461 | 5777 | 778 | 0 | 0 | 0 | 26 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8510 | 7451 | 4586 | 330 | 0 | 0 | 0 | 12 |  |
| feature-currency | feature | code | pass | 7 | 14 | 53276 | 46530 | 10253 | 1871 | 0 | 0 | 0 | 65 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 18935 | 17315 | 5139 | 686 | 0 | 0 | 0 | 25 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 26598 | 24049 | 6064 | 587 | 0 | 0 | 0 | 24 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8378 | 7423 | 4482 | 170 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 8 | 49876 | 40685 | 12702 | 1214 | 0 | 0 | 0 | 51 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 3 | 13585 | 11890 | 5218 | 357 | 0 | 0 | 0 | 15 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 8267 | 7424 | 4370 | 284 | 0 | 0 | 0 | 11 |  |
| multi-feature-discount | feature | code | pass | 19 | 24 | 139127 | 132073 | 10513 | 4214 | 0 | 0 | 0 | 144 |  |
| multi-locate-mapping | locate | chat | pass | 3 | 4 | 12434 | 11419 | 4538 | 229 | 0 | 0 | 0 | 11 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8349 | 7377 | 4499 | 243 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 23157 | 21585 | 5087 | 484 | 0 | 0 | 0 | 21 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 39989 | 36917 | 6579 | 1052 | 0 | 0 | 1 | 40 |  |
| rename-entries | refactor | code | pass | 6 | 11 | 31949 | 29196 | 6264 | 826 | 0 | 0 | 0 | 31 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 17188 | 15645 | 5062 | 442 | 0 | 0 | 0 | 18 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | FAIL | 13 | 12 | 96789 | 90400 | 9569 | 3693 | 0 | 0 | 0 | 123 | switched to code. turn 4: answer does not match /Balance\|ledger\.go/ |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 24 | 19 | 475259 | 443452 | 35246 | 5022 | 0 | 0 | 1 | 217 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 8 | 23611 | 21720 | 5406 | 471 | 0 | 0 | 0 | 22 |  |
| ts-locate-discount | locate | chat | pass | 3 | 3 | 12279 | 11323 | 4479 | 338 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5205 | 12 | 200 | 76 | 0 | 537 | 19 | 0 | 0 | 6 | 0 | 0 | 712 | 0 | 31 | 385 | 2199 | 65 | 963 |
| diagnose-config | 4520 | 0 | 199 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 118 | 0 | 355 | 0 | 0 | 36 | 2199 | 29 | 916 |
| diagnose-failing-test | 6262 | 19 | 206 | 76 | 0 | 537 | 115 | 0 | 0 | 29 | 0 | 0 | 1788 | 0 | 0 | 258 | 2199 | 29 | 1006 |
| explain-event-flow | 5777 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 33 | 0 | 0 | 1695 | 0 | 0 | 35 | 2199 | 33 | 917 |
| explain-ledger-posting | 4586 | 0 | 203 | 76 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 10253 | 56 | 198 | 131 | 0 | 537 | 1266 | 45 | 0 | 59 | 0 | 0 | 3474 | 0 | 0 | 1057 | 2199 | 55 | 1176 |
| fix-balance-query | 5139 | 42 | 202 | 76 | 48 | 537 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 117 | 2199 | 32 | 947 |
| fix-ledger-balance | 6064 | 48 | 204 | 76 | 78 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 0 | 230 | 2199 | 52 | 981 |
| java-explain-orders | 4482 | 0 | 197 | 65 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 12702 | 24 | 200 | 286 | 0 | 537 | 18 | 43 | 0 | 21 | 0 | 0 | 7690 | 0 | 0 | 543 | 2199 | 79 | 1062 |
| large-locate-preorder | 5218 | 0 | 201 | 286 | 0 | 537 | 0 | 0 | 889 | 84 | 0 | 0 | 0 | 0 | 0 | 53 | 2199 | 42 | 927 |
| locate-topic-name | 4370 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 270 | 0 | 0 | 0 | 0 | 113 | 2199 | 25 | 898 |
| multi-feature-discount | 10513 | 129 | 202 | 113 | 130 | 537 | 647 | 162 | 0 | 0 | 0 | 0 | 2356 | 267 | 0 | 1969 | 2199 | 117 | 1685 |
| multi-locate-mapping | 4538 | 0 | 203 | 113 | 0 | 537 | 0 | 0 | 0 | 16 | 0 | 0 | 476 | 0 | 0 | 36 | 2199 | 38 | 920 |
| py-explain-reorder | 4499 | 0 | 201 | 65 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2199 | 40 | 860 |
| py-fix-inventory | 5087 | 45 | 200 | 65 | 46 | 537 | 72 | 16 | 0 | 10 | 0 | 0 | 785 | 0 | 0 | 76 | 2199 | 43 | 993 |
| refactor-validate | 6579 | 16 | 196 | 131 | 73 | 537 | 81 | 96 | 0 | 12 | 0 | 0 | 1565 | 0 | 0 | 540 | 2199 | 68 | 1065 |
| rename-entries | 6264 | 77 | 200 | 76 | 0 | 537 | 40 | 24 | 0 | 32 | 3 | 0 | 1721 | 0 | 0 | 179 | 2199 | 30 | 1146 |
| rust-explain-ledger | 5062 | 0 | 205 | 89 | 0 | 537 | 0 | 0 | 0 | 4 | 0 | 18 | 928 | 0 | 0 | 82 | 2199 | 41 | 959 |
| session-ledger | 9569 | 1063 | 798 | 76 | 0 | 537 | 303 | 48 | 0 | 0 | 0 | 0 | 2487 | 0 | 31 | 602 | 2199 | 110 | 1315 |
| session-x-tools | 35246 | 1007 | 1175 | 286 | 0 | 537 | 60 | 191 | 0 | 179 | 0 | 0 | 26149 | 0 | 0 | 1790 | 2199 | 209 | 1464 |
| ts-fix-cart-discount | 5406 | 42 | 200 | 82 | 0 | 537 | 123 | 54 | 0 | 12 | 0 | 0 | 964 | 0 | 0 | 94 | 2199 | 44 | 1055 |
| ts-locate-discount | 4479 | 0 | 199 | 82 | 0 | 537 | 0 | 0 | 0 | 7 | 0 | 0 | 503 | 0 | 0 | 22 | 2199 | 30 | 900 |

Environment: build dev f32ea3d8d2cb-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
