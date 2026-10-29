# Direct-mode suite: direct-20261029T181550Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T18:15:50Z, finished 2026-10-29T18:31:42Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=adaptive, seed_pack=true, speculative=off, working_set_tokens=0.

**24/26 passed (92%)**, 6.8 model calls and 6.5 tool calls per task, 57202 prompt tokens per task with a 93% prompt-cache hit rate, 962 generated tokens per task, 12 KB of tool results per task, 37 s per task, 38260 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 4 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 143 KB → 12 KB. Freshness notes: 1.

Calls run together: 26 replies, 60 calls, 0.042 s together against 0.083 s one after another.

Decode: 37.5 tokens/s over the run (25011 tokens in 667 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 2857 generated tokens, 110 s); explore 31 (31 without thinking, 5460 generated tokens, 207 s); edit 38 (0 without thinking, 5538 generated tokens, 177 s); debug 39 (0 without thinking, 6667 generated tokens, 207 s); answer 51 (51 without thinking, 4489 generated tokens, 239 s).

Where the agent's time went, per task: 36.2 s in model calls, 0.3 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1831 ms (median). 153 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1108 prompt tokens recomputed; 3719 prompt tokens evaluated by the server per task, in 10.1 s. Cost of a passed task: 4668 processed tokens and 34 s; of a failed one: 4835 and 72 s. Peak memory: harness 41 MiB, model server 29163 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 7.3 | 6.9 | 66606 | 40.8 | 0.3 | 41 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8668 | 5.9 | 0.0 | 6 |
| multi | 1/2 | 0 | 13.5 | 16.0 | 104242 | 68.0 | 0.6 | 69 |
| python | 3/3 | 0 | 5.0 | 4.3 | 31869 | 25.2 | 0.1 | 25 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 8994 | 5.7 | 0.0 | 6 |
| typescript | 2/2 | 0 | 3.5 | 3.0 | 16602 | 11.7 | 0.1 | 12 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 11.0 | 12.0 | 61202 | 97% | 2025 | 62 |
| diagnose | 2/2 | 3.5 | 3.5 | 17353 | 89% | 454 | 18 |
| explain | 5/5 | 2.0 | 1.4 | 9064 | 86% | 191 | 9 |
| feature | 1/2 | 16.5 | 19.0 | 125874 | 97% | 2796 | 89 |
| small-fix | 3/4 | 5.0 | 5.2 | 25333 | 94% | 479 | 19 |
| large-change | 1/1 | 6.0 | 6.0 | 48376 | 91% | 1243 | 43 |
| large-locate | 1/1 | 2.0 | 1.0 | 13435 | 61% | 131 | 13 |
| locate | 3/3 | 2.0 | 1.7 | 8676 | 84% | 166 | 9 |
| refactor | 2/2 | 7.5 | 9.0 | 44406 | 94% | 914 | 33 |
| session | 5/5 | 13.4 | 11.0 | 163258 | 94% | 1983 | 81 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 11 | 12 | 61202 | 59587 | 7203 | 2025 | 0 | 0 | 1 | 62 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8700 | 7867 | 4548 | 153 | 0 | 0 | 0 | 7 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 26006 | 23156 | 6339 | 755 | 0 | 0 | 0 | 28 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10134 | 7954 | 5954 | 372 | 0 | 0 | 0 | 14 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8752 | 7762 | 4707 | 234 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 8 | 8 | 51887 | 49350 | 7631 | 1623 | 0 | 0 | 2 | 51 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 19788 | 18404 | 5379 | 414 | 0 | 0 | 0 | 16 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 32913 | 30755 | 6319 | 585 | 0 | 0 | 0 | 24 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8668 | 7758 | 4627 | 97 | 0 | 0 | 0 | 6 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48376 | 43782 | 9292 | 1243 | 0 | 0 | 0 | 43 |  |
| large-locate-preorder | large-locate | chat | pass | 2 | 1 | 13435 | 8152 | 9004 | 131 | 0 | 0 | 0 | 13 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 8767 | 7816 | 4636 | 221 | 0 | 0 | 0 | 9 |  |
| multi-feature-discount | feature | code | FAIL | 25 | 30 | 199860 | 195957 | 11279 | 3969 | 0 | 0 | 0 | 127 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 2 | 8624 | 6237 | 4497 | 141 | 0 | 0 | 0 | 10 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8774 | 7810 | 4720 | 172 | 0 | 0 | 0 | 8 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24066 | 22811 | 5339 | 526 | 0 | 0 | 0 | 20 |  |
| refactor-validate | refactor | code | pass | 8 | 8 | 47870 | 44504 | 7804 | 1020 | 0 | 0 | 1 | 37 |  |
| rename-entries | refactor | code | pass | 7 | 10 | 40942 | 38661 | 6664 | 808 | 0 | 0 | 0 | 29 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 8994 | 7785 | 4923 | 80 | 0 | 0 | 0 | 6 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 74702 | 70848 | 8623 | 1779 | 0 | 0 | 0 | 62 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 8 | 6 | 62767 | 55710 | 11395 | 1153 | 0 | 0 | 0 | 49 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 11 | 10 | 76755 | 73331 | 8925 | 2019 | 0 | 0 | 0 | 69 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 15 | 10 | 108712 | 104257 | 9542 | 1609 | 0 | 0 | 0 | 64 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 22 | 20 | 493356 | 459362 | 38260 | 3354 | 0 | 0 | 0 | 164 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 5 | 24565 | 23177 | 5334 | 391 | 0 | 0 | 0 | 17 |  |
| ts-locate-discount | locate | chat | pass | 2 | 1 | 8638 | 7766 | 4586 | 137 | 0 | 0 | 0 | 7 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 7203 | 43 | 200 | 76 | 35 | 537 | 98 | 0 | 4 | 0 | 55 | 770 | 0 | 127 | 1326 | 2338 | 65 | 1529 |
| diagnose-config | 4548 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2338 | 29 | 862 |
| diagnose-failing-test | 6339 | 19 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 0 | 125 | 2338 | 29 | 1034 |
| explain-event-flow | 5954 | 30 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2338 | 33 | 882 |
| explain-ledger-posting | 4707 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2338 | 22 | 862 |
| feature-currency | 7631 | 55 | 198 | 131 | 103 | 537 | 194 | 164 | 0 | 0 | 0 | 1565 | 0 | 0 | 1068 | 2338 | 55 | 1223 |
| fix-balance-query | 5379 | 71 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 123 | 2338 | 32 | 989 |
| fix-ledger-balance | 6319 | 46 | 204 | 76 | 39 | 537 | 36 | 24 | 7 | 0 | 0 | 1622 | 0 | 0 | 247 | 2338 | 52 | 1091 |
| java-explain-orders | 4627 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2338 | 35 | 862 |
| large-change-txtar | 9292 | 64 | 200 | 286 | 153 | 537 | 17 | 141 | 0 | 0 | 0 | 3623 | 0 | 0 | 723 | 2338 | 79 | 1131 |
| large-locate-preorder | 9004 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 0 | 0 | 4526 | 0 | 0 | 15 | 2338 | 42 | 862 |
| locate-topic-name | 4636 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 306 | 0 | 0 | 0 | 0 | 115 | 2338 | 25 | 914 |
| multi-feature-discount | 11279 | 51 | 202 | 113 | 68 | 537 | 1062 | 145 | 11 | 0 | 0 | 1316 | 461 | 0 | 2336 | 2338 | 117 | 2522 |
| multi-locate-mapping | 4497 | 24 | 203 | 113 | 68 | 537 | 0 | 0 | 12 | 0 | 0 | 259 | 0 | 0 | 18 | 2338 | 38 | 887 |
| py-explain-reorder | 4720 | 17 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2338 | 40 | 882 |
| py-fix-inventory | 5339 | 58 | 200 | 65 | 36 | 537 | 19 | 16 | 10 | 0 | 0 | 785 | 0 | 0 | 104 | 2338 | 43 | 1128 |
| refactor-validate | 7804 | 28 | 196 | 131 | 34 | 537 | 80 | 74 | 6 | 0 | 0 | 2652 | 0 | 0 | 516 | 2338 | 68 | 1144 |
| rename-entries | 6664 | 82 | 200 | 76 | 47 | 537 | 36 | 24 | 25 | 3 | 0 | 1721 | 0 | 0 | 234 | 2338 | 30 | 1311 |
| rust-explain-ledger | 4923 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 8 | 2338 | 41 | 862 |
| session-ledger | 8623 | 559 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 640 | 2338 | 110 | 1350 |
| session-noisy-tests | 11395 | 334 | 607 | 65 | 74 | 537 | 4820 | 16 | 0 | 0 | 0 | 809 | 0 | 0 | 319 | 2338 | 81 | 1395 |
| session-resume-constraints | 8925 | 785 | 810 | 76 | 183 | 537 | 186 | 24 | 14 | 0 | 0 | 1791 | 0 | 31 | 395 | 2338 | 126 | 1629 |
| session-stale-config | 9542 | 317 | 1022 | 131 | 258 | 537 | 249 | 22 | 4 | 0 | 0 | 2380 | 0 | 28 | 711 | 2338 | 118 | 1427 |
| session-x-tools | 38260 | 1211 | 1175 | 286 | 692 | 537 | 20 | 63 | 99 | 31 | 0 | 28356 | 0 | 0 | 1003 | 2338 | 209 | 2240 |
| ts-fix-cart-discount | 5334 | 31 | 200 | 82 | 36 | 537 | 125 | 54 | 0 | 0 | 0 | 769 | 0 | 0 | 66 | 2338 | 44 | 1052 |
| ts-locate-discount | 4586 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 494 | 0 | 0 | 8 | 2338 | 30 | 862 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
