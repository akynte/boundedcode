# Direct-mode suite: direct-20261028T222758Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T22:27:58Z, finished 2026-10-28T22:45:19Z.

Settings: approval=auto, checks=true, compaction=legacy, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=false, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**24/26 passed (92%)**, 6.3 model calls and 6.3 tool calls per task, 57209 prompt tokens per task with a 93% prompt-cache hit rate, 1084 generated tokens per task, 13 KB of tool results per task, 40 s per task, 37000 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 0, 0 KB → 0 KB. Freshness notes: 1.

Where the agent's time went, per task: 39.6 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1774 ms (median). 353 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 4097 prompt tokens evaluated by the server per task, in 10.3 s. Cost of a passed task: 5044 processed tokens and 35 s; of a failed one: 6821 and 102 s. Peak memory: harness 40 MiB, model server 29303 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 6.9 | 6.5 | 64935 | 44.4 | 0.3 | 45 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8609 | 8.1 | 0.0 | 8 |
| multi | 1/2 | 0 | 9.5 | 13.5 | 67085 | 60.9 | 0.3 | 61 |
| python | 3/3 | 0 | 5.7 | 5.0 | 66377 | 34.2 | 0.2 | 34 |
| rust | 1/1 | 1 | 3.0 | 4.0 | 13504 | 13.7 | 0.0 | 14 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14067 | 14.6 | 0.1 | 15 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 26138 | 95% | 1077 | 34 |
| diagnose | 2/2 | 3.5 | 3.5 | 17232 | 89% | 794 | 26 |
| explain | 5/5 | 2.2 | 2.0 | 9916 | 87% | 307 | 12 |
| feature | 1/2 | 12.5 | 16.0 | 89636 | 96% | 2491 | 80 |
| small-fix | 4/4 | 4.5 | 4.8 | 22556 | 93% | 532 | 20 |
| large-change | 1/1 | 6.0 | 6.0 | 48183 | 90% | 1217 | 42 |
| large-locate | 1/1 | 3.0 | 2.0 | 18148 | 70% | 320 | 19 |
| locate | 3/3 | 2.0 | 2.7 | 9076 | 80% | 300 | 13 |
| refactor | 2/2 | 6.0 | 8.5 | 35687 | 93% | 844 | 30 |
| session | 4/5 | 14.4 | 11.2 | 188566 | 93% | 2550 | 99 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 26138 | 24855 | 5862 | 1077 | 0 | 0 | 0 | 34 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8670 | 7841 | 4565 | 306 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 25794 | 22961 | 6319 | 1282 | 0 | 0 | 0 | 42 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10072 | 7896 | 5939 | 503 | 0 | 0 | 0 | 17 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8695 | 7709 | 4697 | 253 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 8 | 10 | 54483 | 51315 | 8237 | 1633 | 0 | 0 | 0 | 53 |  |
| fix-balance-query | small-fix | code | pass | 5 | 5 | 25478 | 23965 | 5617 | 555 | 0 | 0 | 0 | 21 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 6 | 27206 | 25091 | 6351 | 676 | 0 | 0 | 0 | 25 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8609 | 7703 | 4615 | 181 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48183 | 43591 | 9238 | 1217 | 0 | 0 | 0 | 42 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 18148 | 12726 | 9178 | 320 | 0 | 0 | 0 | 19 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8960 | 7802 | 4876 | 172 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | FAIL | 17 | 22 | 124789 | 121482 | 10083 | 3349 | 0 | 0 | 0 | 106 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 5 | 9381 | 6259 | 5301 | 363 | 0 | 0 | 0 | 17 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8700 | 7740 | 4693 | 252 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | pass | 4 | 4 | 18294 | 17353 | 4967 | 512 | 0 | 0 | 0 | 18 |  |
| refactor-validate | refactor | code | pass | 6 | 7 | 37006 | 34179 | 7284 | 934 | 0 | 0 | 1 | 33 |  |
| rename-entries | refactor | code | pass | 6 | 10 | 34368 | 32079 | 6619 | 755 | 0 | 0 | 0 | 28 |  |
| rust-explain-ledger | explain | chat | pass | 3 | 4 | 13504 | 12153 | 5197 | 347 | 0 | 0 | 0 | 14 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | FAIL | 12 | 10 | 89043 | 85193 | 9919 | 3136 | 0 | 0 | 0 | 99 | switched to code. turn 4: answer does not match /Balance\|ledger\.go/ |
| session-noisy-tests | session | chat, code, chat | pass | 11 | 9 | 172138 | 154103 | 22668 | 1545 | 0 | 0 | 0 | 75 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 12 | 109204 | 105644 | 10318 | 3305 | 0 | 0 | 0 | 104 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 13 | 8 | 92301 | 86107 | 9372 | 1345 | 0 | 0 | 0 | 58 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 22 | 17 | 480144 | 449554 | 37000 | 3420 | 0 | 0 | 0 | 159 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19247 | 17880 | 5271 | 385 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8887 | 7742 | 4882 | 366 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5862 | 0 | 200 | 76 | 35 | 537 | 78 | 0 | 6 | 0 | 0 | 712 | 0 | 31 | 421 | 2297 | 65 | 1404 |
| diagnose-config | 4565 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2297 | 29 | 920 |
| diagnose-failing-test | 6319 | 19 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 0 | 80 | 2297 | 29 | 1100 |
| explain-event-flow | 5939 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2297 | 33 | 938 |
| explain-ledger-posting | 4697 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2297 | 22 | 893 |
| feature-currency | 8237 | 13 | 198 | 131 | 103 | 537 | 292 | 45 | 5 | 0 | 0 | 2212 | 0 | 0 | 967 | 2297 | 55 | 1382 |
| fix-balance-query | 5617 | 44 | 202 | 76 | 73 | 537 | 153 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 96 | 2297 | 32 | 1206 |
| fix-ledger-balance | 6351 | 34 | 204 | 76 | 39 | 537 | 37 | 24 | 7 | 0 | 0 | 1598 | 0 | 0 | 240 | 2297 | 52 | 1206 |
| java-explain-orders | 4615 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2297 | 35 | 891 |
| large-change-txtar | 9238 | 56 | 200 | 286 | 153 | 537 | 18 | 141 | 0 | 0 | 0 | 3623 | 0 | 0 | 633 | 2297 | 79 | 1215 |
| large-locate-preorder | 9178 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 70 | 0 | 4526 | 0 | 0 | 35 | 2297 | 42 | 987 |
| locate-topic-name | 4876 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 10 | 2297 | 25 | 901 |
| multi-feature-discount | 10083 | 0 | 202 | 113 | 68 | 537 | 619 | 42 | 16 | 0 | 78 | 1316 | 459 | 0 | 2086 | 2297 | 117 | 2133 |
| multi-locate-mapping | 5301 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 18 | 0 | 0 | 947 | 0 | 0 | 44 | 2297 | 38 | 1036 |
| py-explain-reorder | 4693 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2297 | 40 | 913 |
| py-fix-inventory | 4967 | 49 | 200 | 65 | 36 | 537 | 72 | 16 | 10 | 0 | 0 | 436 | 0 | 0 | 108 | 2297 | 43 | 1098 |
| refactor-validate | 7284 | 20 | 196 | 131 | 34 | 537 | 81 | 74 | 12 | 0 | 0 | 2137 | 0 | 0 | 509 | 2297 | 68 | 1188 |
| rename-entries | 6619 | 31 | 200 | 76 | 47 | 537 | 37 | 24 | 29 | 44 | 0 | 1697 | 0 | 0 | 179 | 2297 | 30 | 1391 |
| rust-explain-ledger | 5197 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 4 | 0 | 0 | 928 | 0 | 0 | 37 | 2297 | 41 | 1028 |
| session-ledger | 9919 | 575 | 798 | 76 | 183 | 537 | 188 | 24 | 14 | 0 | 0 | 1791 | 0 | 31 | 508 | 2297 | 110 | 2787 |
| session-noisy-tests | 22668 | 455 | 607 | 65 | 126 | 537 | 15709 | 16 | 0 | 0 | 0 | 809 | 0 | 0 | 331 | 2297 | 81 | 1635 |
| session-resume-constraints | 10318 | 802 | 810 | 76 | 183 | 537 | 262 | 49 | 14 | 0 | 0 | 1791 | 0 | 31 | 703 | 2297 | 126 | 2637 |
| session-stale-config | 9372 | 269 | 1022 | 131 | 258 | 537 | 17 | 0 | 4 | 0 | 0 | 2733 | 0 | 28 | 356 | 2297 | 118 | 1602 |
| session-x-tools | 37000 | 1277 | 1175 | 286 | 692 | 537 | 21 | 63 | 95 | 65 | 0 | 27192 | 0 | 0 | 622 | 2297 | 209 | 2469 |
| ts-fix-cart-discount | 5271 | 32 | 200 | 82 | 36 | 537 | 123 | 54 | 0 | 0 | 0 | 769 | 0 | 0 | 65 | 2297 | 44 | 1032 |
| ts-locate-discount | 4882 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2297 | 30 | 915 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
