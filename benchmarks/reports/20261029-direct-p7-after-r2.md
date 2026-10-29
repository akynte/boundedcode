# Direct-mode suite: direct-20261028T224519Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T22:45:19Z, finished 2026-10-28T23:01:04Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**25/26 passed (96%)**, 6.2 model calls and 5.8 tool calls per task, 46739 prompt tokens per task with a 93% prompt-cache hit rate, 999 generated tokens per task, 10 KB of tool results per task, 36 s per task, 23776 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 4, 114 KB → 14 KB. Freshness notes: 1.

Where the agent's time went, per task: 35.9 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1766 ms (median). 306 reasoning tokens per task. 4 prompt-cache invalidations (4 after the history was rewritten), 1340 prompt tokens recomputed; 3251 prompt tokens evaluated by the server per task, in 9.1 s. Cost of a passed task: 4177 processed tokens and 34 s; of a failed one: 6063 and 87 s. Peak memory: harness 38 MiB, model server 28806 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 6.8 | 6.1 | 52684 | 40.6 | 0.2 | 41 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8612 | 8.3 | 0.0 | 8 |
| multi | 1/2 | 0 | 9.0 | 12.0 | 61557 | 49.6 | 0.4 | 50 |
| python | 3/3 | 0 | 6.3 | 5.7 | 50351 | 34.7 | 0.2 | 35 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 8921 | 7.3 | 0.0 | 7 |
| typescript | 2/2 | 0 | 3.0 | 2.5 | 13945 | 12.3 | 0.1 | 13 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25824 | 95% | 948 | 30 |
| diagnose | 2/2 | 3.5 | 3.5 | 17230 | 89% | 732 | 25 |
| explain | 5/5 | 2.2 | 1.6 | 10112 | 86% | 307 | 12 |
| feature | 1/2 | 11.0 | 13.5 | 75688 | 96% | 2110 | 67 |
| small-fix | 4/4 | 5.0 | 5.8 | 27383 | 92% | 606 | 23 |
| large-change | 1/1 | 6.0 | 6.0 | 48652 | 91% | 1394 | 47 |
| large-locate | 1/1 | 4.0 | 3.0 | 26216 | 73% | 589 | 28 |
| locate | 3/3 | 2.0 | 1.7 | 8931 | 81% | 191 | 10 |
| refactor | 2/2 | 7.0 | 8.5 | 39839 | 94% | 842 | 31 |
| session | 5/5 | 13.4 | 10.0 | 132426 | 94% | 2226 | 82 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25824 | 24543 | 5758 | 948 | 0 | 0 | 0 | 30 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8680 | 7851 | 4575 | 268 | 0 | 0 | 0 | 10 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 25780 | 22951 | 6313 | 1197 | 0 | 0 | 0 | 40 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10081 | 7905 | 5948 | 435 | 0 | 0 | 0 | 16 |  |
| explain-ledger-posting | explain | chat | pass | 3 | 2 | 14244 | 12557 | 5550 | 431 | 0 | 0 | 0 | 17 |  |
| feature-currency | feature | code | pass | 6 | 6 | 37516 | 35134 | 7320 | 1490 | 0 | 0 | 0 | 47 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 19750 | 18368 | 5407 | 510 | 0 | 0 | 0 | 18 |  |
| fix-ledger-balance | small-fix | code | pass | 7 | 10 | 46378 | 42133 | 8730 | 1003 | 0 | 0 | 0 | 37 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8612 | 7706 | 4618 | 178 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48652 | 44060 | 9382 | 1394 | 0 | 0 | 0 | 47 |  |
| large-locate-preorder | large-locate | chat | pass | 4 | 3 | 26216 | 19145 | 10964 | 589 | 0 | 0 | 0 | 28 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8960 | 7802 | 4876 | 156 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | FAIL | 16 | 21 | 113859 | 110526 | 9500 | 2730 | 0 | 0 | 0 | 87 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9255 | 6161 | 5175 | 236 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8700 | 7740 | 4693 | 348 | 0 | 0 | 0 | 13 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24094 | 22843 | 5326 | 514 | 0 | 0 | 0 | 19 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 34631 | 32378 | 6690 | 952 | 0 | 0 | 1 | 33 |  |
| rename-entries | refactor | code | pass | 8 | 11 | 45047 | 42710 | 6609 | 732 | 0 | 0 | 0 | 28 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 8921 | 7716 | 4897 | 143 | 0 | 0 | 0 | 7 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 84619 | 80769 | 9253 | 2589 | 0 | 0 | 0 | 84 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 12 | 10 | 118259 | 110452 | 12362 | 1848 | 0 | 0 | 0 | 73 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 12 | 10 | 85945 | 82516 | 9395 | 2459 | 0 | 0 | 0 | 80 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 82705 | 78571 | 8895 | 1233 | 0 | 0 | 0 | 50 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 13 | 290601 | 272501 | 23776 | 3000 | 0 | 0 | 0 | 124 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19311 | 17944 | 5319 | 397 | 0 | 0 | 0 | 17 |  |
| ts-locate-discount | locate | chat | pass | 2 | 1 | 8579 | 7711 | 4574 | 182 | 0 | 0 | 0 | 8 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5758 | 0 | 200 | 76 | 35 | 537 | 76 | 0 | 4 | 0 | 712 | 31 | 407 | 2297 | 65 | 1318 |
| diagnose-config | 4575 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2297 | 29 | 930 |
| diagnose-failing-test | 6313 | 19 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 1788 | 0 | 80 | 2297 | 29 | 1094 |
| explain-event-flow | 5948 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2297 | 33 | 947 |
| explain-ledger-posting | 5550 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 1307 | 0 | 22 | 2297 | 22 | 1047 |
| feature-currency | 7320 | 68 | 198 | 131 | 103 | 537 | 192 | 45 | 0 | 0 | 1565 | 0 | 936 | 2297 | 55 | 1193 |
| fix-balance-query | 5407 | 36 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 877 | 0 | 117 | 2297 | 32 | 1099 |
| fix-ledger-balance | 8730 | 42 | 204 | 76 | 39 | 537 | 1996 | 24 | 0 | 0 | 1738 | 0 | 377 | 2297 | 52 | 1348 |
| java-explain-orders | 4618 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2297 | 35 | 894 |
| large-change-txtar | 9382 | 69 | 200 | 286 | 153 | 537 | 17 | 141 | 0 | 0 | 3623 | 0 | 733 | 2297 | 79 | 1247 |
| large-locate-preorder | 10964 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 1635 | 4526 | 0 | 170 | 2297 | 42 | 1073 |
| locate-topic-name | 4876 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2297 | 25 | 901 |
| multi-feature-discount | 9500 | 65 | 202 | 113 | 68 | 537 | 1074 | 167 | 11 | 0 | 1316 | 0 | 1396 | 2297 | 117 | 2137 |
| multi-locate-mapping | 5175 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 947 | 0 | 27 | 2297 | 38 | 945 |
| py-explain-reorder | 4693 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2297 | 40 | 913 |
| py-fix-inventory | 5326 | 40 | 200 | 65 | 36 | 537 | 19 | 16 | 10 | 0 | 785 | 0 | 104 | 2297 | 43 | 1174 |
| refactor-validate | 6690 | 20 | 196 | 131 | 34 | 537 | 80 | 74 | 12 | 0 | 1565 | 0 | 497 | 2297 | 68 | 1179 |
| rename-entries | 6609 | 0 | 200 | 76 | 47 | 537 | 36 | 24 | 29 | 44 | 1721 | 0 | 181 | 2297 | 30 | 1387 |
| rust-explain-ledger | 4897 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 812 | 0 | 8 | 2297 | 41 | 877 |
| session-ledger | 9253 | 531 | 798 | 76 | 183 | 537 | 186 | 24 | 14 | 0 | 1791 | 31 | 464 | 2297 | 110 | 2211 |
| session-noisy-tests | 12362 | 388 | 608 | 65 | 74 | 537 | 5106 | 16 | 0 | 0 | 809 | 0 | 683 | 2297 | 81 | 1698 |
| session-resume-constraints | 9395 | 705 | 810 | 76 | 183 | 537 | 186 | 24 | 14 | 0 | 1791 | 31 | 420 | 2297 | 126 | 2195 |
| session-stale-config | 8895 | 339 | 1022 | 131 | 258 | 537 | 16 | 0 | 4 | 0 | 2380 | 28 | 320 | 2297 | 118 | 1445 |
| session-x-tools | 23776 | 1136 | 1175 | 286 | 692 | 537 | 20 | 63 | 95 | 0 | 14461 | 0 | 542 | 2297 | 209 | 2263 |
| ts-fix-cart-discount | 5319 | 25 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 769 | 0 | 67 | 2297 | 44 | 1086 |
| ts-locate-discount | 4574 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 494 | 0 | 8 | 2297 | 30 | 891 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
