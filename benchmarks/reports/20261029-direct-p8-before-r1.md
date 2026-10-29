# Direct-mode suite: direct-20261029T091929Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T09:19:29Z, finished 2026-10-29T09:49:15Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**24/26 passed (92%)**, 12.0 model calls and 11.8 tool calls per task, 231570 prompt tokens per task with a 94% prompt-cache hit rate, 1339 generated tokens per task, 20 KB of tool results per task, 69 s per task, 37405 tokens in the largest single prompt; 162 results set aside, 0 recalled, 0 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 3, 184 KB → 20 KB. Freshness notes: 1.

Where the agent's time went, per task: 68.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.05 s in the harness itself; time to first token 1834 ms (median). 418 reasoning tokens per task. 13 prompt-cache invalidations (13 after the history was rewritten), 203842 prompt tokens recomputed; 13840 prompt tokens evaluated by the server per task, in 31.0 s. Cost of a passed task: 3713 processed tokens and 30 s; of a failed one: 152776 and 532 s. Peak memory: harness 68 MiB, model server 29375 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: max_turns 2.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 15.5 | 15.1 | 334838 | 88.9 | 0.3 | 89 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8623 | 10.4 | 0.0 | 10 |
| multi | 1/2 | 0 | 11.0 | 13.0 | 79862 | 62.7 | 0.2 | 63 |
| python | 3/3 | 0 | 5.7 | 5.0 | 41023 | 29.1 | 0.1 | 29 |
| rust | 1/1 | 1 | 2.0 | 2.0 | 9001 | 9.2 | 0.0 | 9 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14077 | 15.0 | 0.1 | 15 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25841 | 81% | 989 | 36 |
| diagnose | 2/2 | 3.0 | 3.0 | 14512 | 87% | 648 | 22 |
| explain | 5/5 | 2.0 | 1.6 | 9019 | 86% | 284 | 11 |
| feature | 1/2 | 12.5 | 14.5 | 90642 | 97% | 2417 | 75 |
| small-fix | 4/4 | 5.0 | 5.2 | 25431 | 92% | 570 | 22 |
| large-change | 1/1 | 6.0 | 6.0 | 47921 | 90% | 1110 | 39 |
| large-locate | 1/1 | 3.0 | 3.0 | 18730 | 68% | 464 | 23 |
| locate | 3/3 | 2.0 | 2.3 | 9081 | 80% | 275 | 12 |
| refactor | 2/2 | 6.0 | 7.5 | 34888 | 92% | 780 | 29 |
| session | 4/5 | 44.0 | 41.0 | 1094835 | 94% | 4009 | 252 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25841 | 20940 | 5751 | 989 | 0 | 0 | 0 | 36 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8668 | 7839 | 4563 | 304 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 5 | 20356 | 17555 | 6526 | 992 | 0 | 0 | 0 | 34 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10073 | 7897 | 5940 | 440 | 0 | 0 | 0 | 16 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8694 | 7708 | 4696 | 257 | 0 | 0 | 0 | 10 |  |
| feature-currency | feature | code | pass | 5 | 6 | 30590 | 28244 | 7046 | 1205 | 0 | 0 | 0 | 38 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 19784 | 18402 | 5425 | 495 | 0 | 0 | 0 | 18 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 32941 | 30789 | 6391 | 688 | 0 | 0 | 0 | 26 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8623 | 7717 | 4629 | 274 | 0 | 0 | 0 | 10 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 47921 | 43329 | 9167 | 1110 | 0 | 0 | 0 | 39 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 3 | 18730 | 12708 | 9781 | 464 | 0 | 0 | 0 | 23 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 9325 | 7724 | 5241 | 209 | 0 | 0 | 0 | 10 |  |
| multi-feature-discount | feature | code | FAIL | 20 | 23 | 150693 | 147734 | 9986 | 3629 | 0 | 0 | 0 | 112 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9031 | 6220 | 4951 | 282 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8705 | 7745 | 4698 | 232 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 6 | 6 | 29731 | 28424 | 5522 | 721 | 0 | 0 | 0 | 25 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 42298 | 38968 | 7777 | 938 | 0 | 0 | 1 | 34 |  |
| rename-entries | refactor | code | pass | 5 | 8 | 27477 | 25311 | 6354 | 623 | 0 | 0 | 0 | 23 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 2 | 9001 | 7782 | 4977 | 217 | 0 | 0 | 0 | 9 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 75661 | 71841 | 8740 | 2133 | 0 | 0 | 0 | 71 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 9 | 7 | 84633 | 75380 | 13516 | 1181 | 0 | 0 | 0 | 54 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 12 | 107723 | 104162 | 10405 | 3425 | 0 | 0 | 0 | 107 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 14 | 9 | 102609 | 96610 | 9803 | 1935 | 0 | 0 | 0 | 74 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | FAIL | 172 | 168 | 5103550 | 4815956 | 37405 | 11370 | 162 | 0 | 0 | 952 | …ling\|FirstChild\|LastChild\|Children\|Enclosing\|FindNode\|Inspect/; turn 3: answer does not match /rewrite\.go/; turn 3: answer does not match /replac\|delet\|insert/ |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19267 | 16242 | 5282 | 378 | 0 | 0 | 0 | 18 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8887 | 7742 | 4882 | 333 | 0 | 0 | 0 | 12 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5751 | 0 | 200 | 76 | 35 | 537 | 52 | 0 | 0 | 4 | 0 | 712 | 0 | 31 | 413 | 2297 | 65 | 1329 |
| diagnose-config | 4563 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2297 | 29 | 918 |
| diagnose-failing-test | 6526 | 17 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 70 | 2297 | 29 | 1333 |
| explain-event-flow | 5940 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2297 | 33 | 939 |
| explain-ledger-posting | 4696 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2297 | 22 | 892 |
| feature-currency | 7046 | 11 | 198 | 131 | 103 | 537 | 167 | 45 | 0 | 0 | 0 | 1565 | 0 | 0 | 714 | 2297 | 55 | 1223 |
| fix-balance-query | 5425 | 49 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 116 | 2297 | 32 | 1105 |
| fix-ledger-balance | 6391 | 43 | 204 | 76 | 39 | 537 | 36 | 24 | 0 | 7 | 0 | 1622 | 0 | 0 | 247 | 2297 | 52 | 1207 |
| java-explain-orders | 4629 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2297 | 35 | 905 |
| large-change-txtar | 9167 | 53 | 200 | 286 | 153 | 537 | 17 | 141 | 0 | 0 | 0 | 3623 | 0 | 0 | 590 | 2297 | 79 | 1191 |
| large-locate-preorder | 9781 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 46 | 0 | 0 | 5080 | 0 | 0 | 46 | 2297 | 42 | 1049 |
| locate-topic-name | 5241 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 303 | 705 | 0 | 0 | 39 | 2297 | 25 | 934 |
| multi-feature-discount | 9986 | 9 | 202 | 113 | 68 | 537 | 180 | 116 | 0 | 22 | 0 | 1316 | 510 | 0 | 2439 | 2297 | 117 | 2060 |
| multi-locate-mapping | 4951 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 5 | 0 | 692 | 0 | 0 | 27 | 2297 | 38 | 971 |
| py-explain-reorder | 4698 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2297 | 40 | 918 |
| py-fix-inventory | 5522 | 39 | 200 | 65 | 36 | 537 | 59 | 16 | 0 | 10 | 0 | 785 | 0 | 0 | 155 | 2297 | 43 | 1280 |
| refactor-validate | 7777 | 0 | 196 | 131 | 34 | 537 | 80 | 74 | 0 | 12 | 0 | 2629 | 0 | 0 | 507 | 2297 | 68 | 1212 |
| rename-entries | 6354 | 20 | 200 | 76 | 47 | 537 | 36 | 24 | 0 | 21 | 44 | 1599 | 0 | 0 | 151 | 2297 | 30 | 1272 |
| rust-explain-ledger | 4977 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 821 | 0 | 0 | 15 | 2297 | 41 | 941 |
| session-ledger | 8740 | 452 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 465 | 2297 | 110 | 1790 |
| session-noisy-tests | 13516 | 328 | 607 | 65 | 74 | 537 | 7011 | 16 | 0 | 0 | 0 | 809 | 0 | 0 | 255 | 2297 | 81 | 1436 |
| session-resume-constraints | 10405 | 706 | 810 | 76 | 183 | 537 | 261 | 49 | 0 | 14 | 0 | 1791 | 0 | 31 | 1043 | 2297 | 126 | 2481 |
| session-stale-config | 9803 | 338 | 1022 | 131 | 258 | 537 | 168 | 22 | 0 | 4 | 0 | 2380 | 0 | 29 | 884 | 2297 | 118 | 1615 |
| session-x-tools | 37405 | 315 | 390 | 286 | 269 | 537 | 0 | 0 | 0 | 0 | 0 | 28902 | 0 | 0 | 631 | 2297 | 59 | 3719 |
| ts-fix-cart-discount | 5282 | 26 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 0 | 67 | 2297 | 44 | 1048 |
| ts-locate-discount | 4882 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2297 | 30 | 915 |

Environment: build dev 74e6f0c1c132; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
