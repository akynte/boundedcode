# Direct-mode suite: direct-20261029T204505Z

Model: qwen3.6-35b-a3b-mtp. Started 2026-10-29T20:45:05Z, finished 2026-10-29T20:58:22Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=draft-mtp, working_set_tokens=0.

**26/26 passed (100%)**, 6.3 model calls and 6.0 tool calls per task, 49355 prompt tokens per task with a 93% prompt-cache hit rate, 1032 generated tokens per task, 16 KB of tool results per task, 31 s per task, 27239 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 3, 259 KB → 16 KB. Freshness notes: 1.

Calls run together: 25 replies, 60 calls, 0.004 s together against 0.007 s one after another.

Decode: 52.4 tokens/s over the run (26834 tokens in 512 s of the server's decode time). Speculative decoding: 23475 tokens drafted, 19065 accepted (81%).

Model calls by role: plan 18 (0 without thinking, 2214 generated tokens, 84 s); explore 29 (0 without thinking, 7680 generated tokens, 207 s); edit 33 (0 without thinking, 4952 generated tokens, 122 s); debug 36 (0 without thinking, 4971 generated tokens, 118 s); answer 48 (0 without thinking, 7017 generated tokens, 255 s).

Where the agent's time went, per task: 30.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1945 ms (median). 295 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3450 prompt tokens evaluated by the server per task, in 10.2 s. Cost of a passed task: 4482 processed tokens and 31 s; of a failed one: 0 and 0 s. Peak memory: harness 38 MiB, model server 33347 MiB, GPU 7423 MiB.

Failures by cause: environment (skipped) 2. Stalls: empty_answer 1.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 6.8 | 6.1 | 56047 | 33.8 | 0.3 | 34 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8681 | 6.2 | 0.0 | 6 |
| multi | 2/2 | 0 | 11.0 | 14.0 | 82259 | 49.7 | 0.3 | 50 |
| python | 3/3 | 0 | 5.7 | 5.0 | 39910 | 24.5 | 0.1 | 25 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9053 | 7.5 | 0.0 | 8 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14226 | 12.3 | 0.1 | 13 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 6.0 | 31595 | 84% | 1058 | 31 |
| diagnose | 2/2 | 3.0 | 3.5 | 14546 | 87% | 638 | 19 |
| explain | 5/5 | 2.0 | 1.4 | 9096 | 86% | 279 | 9 |
| feature | 2/2 | 14.5 | 16.5 | 108176 | 97% | 2952 | 67 |
| small-fix | 4/4 | 4.5 | 4.8 | 22755 | 93% | 509 | 16 |
| large-change | 1/1 | 7.0 | 7.0 | 57900 | 92% | 1342 | 37 |
| large-locate | 1/1 | 3.0 | 2.0 | 18452 | 70% | 501 | 20 |
| locate | 3/3 | 2.0 | 2.7 | 9159 | 80% | 287 | 11 |
| refactor | 2/2 | 8.0 | 9.5 | 48048 | 94% | 878 | 27 |
| session | 5/5 | 12.6 | 9.4 | 133953 | 94% | 2142 | 68 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 6 | 31595 | 26596 | 5857 | 1058 | 0 | 0 | 0 | 31 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8761 | 7934 | 4611 | 243 | 0 | 0 | 0 | 9 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 6 | 20331 | 17515 | 6324 | 1034 | 0 | 0 | 0 | 29 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10163 | 7988 | 5985 | 439 | 0 | 0 | 0 | 13 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8786 | 7802 | 4743 | 358 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 9 | 10 | 61287 | 58533 | 8195 | 1971 | 0 | 0 | 0 | 48 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 19936 | 18558 | 5452 | 495 | 0 | 0 | 0 | 15 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 6 | 27450 | 25340 | 6405 | 683 | 0 | 0 | 0 | 20 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8681 | 7777 | 4642 | 137 | 0 | 0 | 0 | 6 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 7 | 7 | 57900 | 53277 | 9460 | 1342 | 0 | 0 | 0 | 37 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 18452 | 12977 | 9335 | 501 | 0 | 0 | 0 | 20 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 9045 | 7887 | 4916 | 154 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | pass | 20 | 23 | 155064 | 152171 | 10287 | 3932 | 0 | 0 | 0 | 87 |  |
| multi-locate-mapping | locate | chat | pass | 2 | 5 | 9454 | 6368 | 5329 | 325 | 0 | 0 | 0 | 14 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8795 | 7836 | 4743 | 262 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 24159 | 22914 | 5298 | 477 | 0 | 0 | 0 | 16 |  |
| refactor-validate | refactor | code | pass | 8 | 8 | 50632 | 47229 | 7985 | 1035 | 0 | 0 | 1 | 30 |  |
| rename-entries | refactor | code | pass | 8 | 11 | 45465 | 43137 | 6667 | 721 | 0 | 0 | 0 | 23 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9053 | 7849 | 4984 | 197 | 0 | 0 | 0 | 8 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 75720 | 70975 | 8799 | 2005 | 0 | 0 | 0 | 58 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 10 | 8 | 86775 | 78027 | 13252 | 1418 | 0 | 0 | 0 | 49 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 11 | 9 | 78075 | 74689 | 9025 | 2166 | 0 | 0 | 0 | 59 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 83761 | 79172 | 9101 | 1318 | 0 | 0 | 0 | 47 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 14 | 345433 | 325042 | 27239 | 3801 | 0 | 0 | 1 | 127 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19474 | 18110 | 5340 | 380 | 0 | 0 | 0 | 14 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8978 | 7834 | 4928 | 382 | 0 | 0 | 0 | 11 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5857 | 0 | 200 | 76 | 35 | 537 | 76 | 0 | 6 | 0 | 0 | 712 | 0 | 31 | 426 | 2338 | 80 | 1340 |
| diagnose-config | 4611 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2338 | 29 | 925 |
| diagnose-failing-test | 6324 | 17 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 0 | 80 | 2338 | 29 | 1066 |
| explain-event-flow | 5985 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2338 | 33 | 943 |
| explain-ledger-posting | 4743 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2338 | 22 | 898 |
| feature-currency | 8195 | 102 | 198 | 131 | 103 | 537 | 448 | 69 | 0 | 0 | 24 | 1588 | 0 | 0 | 1044 | 2338 | 55 | 1558 |
| fix-balance-query | 5452 | 43 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 116 | 2338 | 32 | 1097 |
| fix-ledger-balance | 6405 | 40 | 204 | 76 | 39 | 537 | 36 | 24 | 7 | 0 | 0 | 1598 | 0 | 0 | 240 | 2338 | 52 | 1214 |
| java-explain-orders | 4642 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2338 | 35 | 877 |
| large-change-txtar | 9460 | 68 | 200 | 286 | 153 | 537 | 17 | 163 | 0 | 0 | 0 | 3623 | 0 | 0 | 590 | 2338 | 79 | 1406 |
| large-locate-preorder | 9335 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 70 | 0 | 4526 | 0 | 0 | 112 | 2338 | 42 | 1026 |
| locate-topic-name | 4916 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 0 | 10 | 2338 | 25 | 900 |
| multi-feature-discount | 10287 | 0 | 202 | 113 | 68 | 537 | 124 | 141 | 11 | 0 | 0 | 1316 | 510 | 0 | 2712 | 2338 | 117 | 2098 |
| multi-locate-mapping | 5329 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 18 | 0 | 0 | 947 | 0 | 0 | 44 | 2338 | 38 | 1023 |
| py-explain-reorder | 4743 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2338 | 40 | 922 |
| py-fix-inventory | 5298 | 37 | 200 | 65 | 36 | 537 | 19 | 16 | 10 | 0 | 0 | 785 | 0 | 0 | 72 | 2338 | 43 | 1140 |
| refactor-validate | 7985 | 0 | 196 | 131 | 34 | 537 | 142 | 74 | 12 | 0 | 0 | 2630 | 0 | 0 | 527 | 2338 | 68 | 1296 |
| rename-entries | 6667 | 0 | 200 | 76 | 47 | 537 | 36 | 24 | 29 | 44 | 0 | 1721 | 0 | 0 | 172 | 2338 | 30 | 1413 |
| rust-explain-ledger | 4984 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 8 | 2338 | 41 | 923 |
| session-ledger | 8799 | 408 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 462 | 2338 | 110 | 1855 |
| session-noisy-tests | 13252 | 366 | 607 | 65 | 74 | 537 | 6465 | 16 | 0 | 0 | 0 | 809 | 0 | 0 | 337 | 2338 | 81 | 1557 |
| session-resume-constraints | 9025 | 710 | 810 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 364 | 2338 | 126 | 1849 |
| session-stale-config | 9101 | 332 | 1022 | 131 | 258 | 537 | 76 | 0 | 4 | 0 | 0 | 2380 | 0 | 28 | 355 | 2338 | 118 | 1522 |
| session-x-tools | 27239 | 1399 | 1175 | 286 | 692 | 537 | 20 | 153 | 95 | 0 | 0 | 16969 | 0 | 0 | 776 | 2338 | 209 | 2590 |
| ts-fix-cart-discount | 5340 | 32 | 200 | 82 | 36 | 537 | 121 | 54 | 0 | 0 | 0 | 769 | 0 | 0 | 67 | 2338 | 44 | 1060 |
| ts-locate-discount | 4928 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2338 | 30 | 920 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model mtp/Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 0b21525e972670ed59e1812e170b27c26355381f0656ecc4e25617ece7dac58b); tasks bdf975b69c63e339.
