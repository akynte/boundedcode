# Direct-mode suite: direct-20261029T195551Z

Model: qwen3.6-35b-a3b-mtp. Started 2026-10-29T19:55:51Z, finished 2026-10-29T20:13:08Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=off, working_set_tokens=0.

**23/26 passed (88%)**, 6.5 model calls and 6.1 tool calls per task, 49645 prompt tokens per task with a 93% prompt-cache hit rate, 1051 generated tokens per task, 13 KB of tool results per task, 40 s per task, 27210 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 3, 185 KB → 19 KB. Freshness notes: 1.

Calls run together: 24 replies, 56 calls, 0.005 s together against 0.007 s one after another.

Decode: 36.5 tokens/s over the run (27328 tokens in 748 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 2336 generated tokens, 104 s); explore 33 (0 without thinking, 8810 generated tokens, 312 s); edit 31 (0 without thinking, 4454 generated tokens, 148 s); debug 38 (0 without thinking, 5131 generated tokens, 171 s); answer 49 (0 without thinking, 6597 generated tokens, 290 s).

Where the agent's time went, per task: 39.4 s in model calls, 0.3 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1888 ms (median). 289 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3534 prompt tokens evaluated by the server per task, in 10.3 s. Cost of a passed task: 4348 processed tokens and 37 s; of a failed one: 6405 and 65 s. Peak memory: harness 38 MiB, model server 30481 MiB, GPU 5763 MiB.

Failures by cause: environment (skipped) 2, model 3. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 7.2 | 6.5 | 57096 | 44.4 | 0.3 | 45 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8707 | 11.4 | 0.0 | 11 |
| multi | 1/2 | 0 | 11.0 | 13.5 | 79954 | 63.2 | 0.4 | 64 |
| python | 2/3 | 0 | 5.0 | 4.3 | 38029 | 30.2 | 0.1 | 30 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9016 | 11.5 | 0.0 | 12 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14212 | 14.8 | 0.1 | 15 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 26131 | 81% | 995 | 38 |
| diagnose | 2/2 | 3.5 | 3.5 | 17526 | 90% | 693 | 25 |
| explain | 5/5 | 2.0 | 1.4 | 9096 | 86% | 329 | 13 |
| feature | 1/2 | 16.5 | 19.5 | 125157 | 97% | 2781 | 93 |
| small-fix | 3/4 | 4.5 | 4.8 | 22622 | 94% | 492 | 20 |
| large-change | 1/1 | 6.0 | 6.0 | 48433 | 91% | 1129 | 41 |
| large-locate | 1/1 | 3.0 | 2.0 | 18429 | 70% | 431 | 22 |
| locate | 3/3 | 2.0 | 2.0 | 9125 | 80% | 253 | 12 |
| refactor | 2/2 | 7.0 | 8.0 | 39730 | 94% | 839 | 32 |
| session | 4/5 | 13.4 | 10.2 | 133924 | 94% | 2356 | 92 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 26131 | 21161 | 5834 | 995 | 0 | 0 | 0 | 38 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8748 | 7919 | 4598 | 316 | 0 | 0 | 0 | 12 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 26305 | 23477 | 6519 | 1070 | 0 | 0 | 0 | 38 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10174 | 7998 | 5996 | 606 | 0 | 0 | 0 | 21 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8786 | 7800 | 4743 | 264 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 13 | 15 | 99755 | 94976 | 10394 | 2109 | 0 | 0 | 1 | 72 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 19810 | 18428 | 5382 | 417 | 0 | 0 | 0 | 17 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 32833 | 30696 | 6472 | 722 | 0 | 0 | 0 | 28 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8707 | 7801 | 4668 | 291 | 0 | 0 | 0 | 11 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48433 | 43841 | 9245 | 1129 | 0 | 0 | 0 | 41 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 18429 | 12961 | 9319 | 431 | 0 | 0 | 0 | 22 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 9048 | 7890 | 4919 | 163 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | FAIL | 20 | 24 | 150559 | 147252 | 10213 | 3453 | 0 | 0 | 0 | 114 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9349 | 6291 | 5224 | 243 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8795 | 7835 | 4743 | 199 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 4 | 4 | 18398 | 17458 | 4985 | 462 | 0 | 0 | 0 | 17 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 34869 | 32616 | 6719 | 905 | 0 | 0 | 1 | 33 |  |
| rename-entries | refactor | code | pass | 8 | 10 | 44591 | 42300 | 6675 | 773 | 0 | 0 | 0 | 30 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9016 | 7811 | 4947 | 284 | 0 | 0 | 0 | 12 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 78278 | 74458 | 9128 | 2323 | 0 | 0 | 0 | 80 | switched to code. |
| session-noisy-tests | session | chat, code, chat | FAIL | 9 | 7 | 86893 | 77741 | 13782 | 1504 | 0 | 0 | 0 | 65 | switched to code. turn 3: answer does not match /OK\|no failures\|all (?:tests )?pass\|none failed\|0 fail/ |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 12 | 106710 | 103149 | 9949 | 2840 | 0 | 0 | 0 | 95 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 14 | 9 | 101273 | 96933 | 9534 | 1622 | 0 | 0 | 0 | 65 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 14 | 296466 | 274187 | 27210 | 3490 | 0 | 0 | 0 | 152 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19445 | 18078 | 5326 | 365 | 0 | 0 | 0 | 17 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8978 | 7833 | 4928 | 352 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5834 | 0 | 200 | 76 | 35 | 537 | 76 | 0 | 4 | 0 | 0 | 712 | 0 | 31 | 413 | 2338 | 65 | 1347 |
| diagnose-config | 4598 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2338 | 29 | 912 |
| diagnose-failing-test | 6519 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 0 | 80 | 2338 | 29 | 1278 |
| explain-event-flow | 5996 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2338 | 33 | 954 |
| explain-ledger-posting | 4743 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2338 | 22 | 898 |
| feature-currency | 10394 | 82 | 198 | 131 | 103 | 537 | 301 | 134 | 0 | 0 | 0 | 3458 | 192 | 0 | 1261 | 2338 | 55 | 1604 |
| fix-balance-query | 5382 | 46 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 85 | 2338 | 32 | 1055 |
| fix-ledger-balance | 6472 | 49 | 204 | 76 | 39 | 537 | 36 | 24 | 14 | 0 | 0 | 1598 | 0 | 0 | 250 | 2338 | 52 | 1255 |
| java-explain-orders | 4668 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2338 | 35 | 903 |
| large-change-txtar | 9245 | 49 | 200 | 286 | 153 | 537 | 17 | 141 | 0 | 0 | 0 | 3623 | 0 | 0 | 640 | 2338 | 79 | 1182 |
| large-locate-preorder | 9319 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 70 | 0 | 4526 | 0 | 0 | 118 | 2338 | 42 | 1004 |
| locate-topic-name | 4919 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 10 | 2338 | 25 | 903 |
| multi-feature-discount | 10213 | 28 | 202 | 113 | 68 | 537 | 613 | 90 | 14 | 0 | 58 | 1316 | 395 | 0 | 1945 | 2338 | 117 | 2379 |
| multi-locate-mapping | 5224 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 947 | 0 | 0 | 27 | 2338 | 38 | 953 |
| py-explain-reorder | 4743 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2338 | 40 | 922 |
| py-fix-inventory | 4985 | 49 | 200 | 65 | 36 | 537 | 71 | 16 | 0 | 0 | 0 | 445 | 0 | 0 | 107 | 2338 | 43 | 1078 |
| refactor-validate | 6719 | 20 | 196 | 131 | 34 | 537 | 80 | 74 | 12 | 0 | 0 | 1565 | 0 | 0 | 497 | 2338 | 68 | 1167 |
| rename-entries | 6675 | 49 | 200 | 76 | 47 | 537 | 36 | 24 | 29 | 3 | 0 | 1721 | 0 | 0 | 221 | 2338 | 30 | 1364 |
| rust-explain-ledger | 4947 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 8 | 2338 | 41 | 886 |
| session-ledger | 9128 | 604 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 475 | 2338 | 110 | 1975 |
| session-noisy-tests | 13782 | 520 | 607 | 65 | 74 | 537 | 6914 | 16 | 0 | 0 | 0 | 809 | 0 | 0 | 248 | 2338 | 81 | 1573 |
| session-resume-constraints | 9949 | 776 | 810 | 76 | 183 | 537 | 261 | 49 | 14 | 0 | 0 | 1791 | 0 | 31 | 606 | 2338 | 126 | 2351 |
| session-stale-config | 9534 | 301 | 1022 | 131 | 258 | 537 | 168 | 22 | 4 | 0 | 0 | 2380 | 0 | 28 | 651 | 2338 | 118 | 1576 |
| session-x-tools | 27210 | 1287 | 1175 | 286 | 692 | 537 | 20 | 63 | 95 | 0 | 0 | 17372 | 0 | 0 | 999 | 2338 | 209 | 2137 |
| ts-fix-cart-discount | 5326 | 32 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 0 | 67 | 2338 | 44 | 1045 |
| ts-locate-discount | 4928 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2338 | 30 | 920 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model mtp/Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 0b21525e972670ed59e1812e170b27c26355381f0656ecc4e25617ece7dac58b); tasks bdf975b69c63e339.
