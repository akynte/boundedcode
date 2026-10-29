# Direct-mode suite: direct-20261029T180016Z

Model: qwen3.6-35b-a3b. Started 2026-10-29T18:00:16Z, finished 2026-10-29T18:15:50Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=off, working_set_tokens=0.

**26/26 passed (100%)**, 6.2 model calls and 5.8 tool calls per task, 45876 prompt tokens per task with a 92% prompt-cache hit rate, 967 generated tokens per task, 13 KB of tool results per task, 36 s per task, 27246 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 3, 185 KB → 19 KB. Freshness notes: 1.

Calls run together: 24 replies, 57 calls, 0.006 s together against 0.010 s one after another.

Decode: 37.8 tokens/s over the run (25133 tokens in 665 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 2410 generated tokens, 101 s); explore 29 (0 without thinking, 7681 generated tokens, 271 s); edit 29 (0 without thinking, 3652 generated tokens, 119 s); debug 34 (0 without thinking, 4367 generated tokens, 141 s); answer 50 (0 without thinking, 7023 generated tokens, 290 s).

Where the agent's time went, per task: 35.5 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1840 ms (median). 262 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3527 prompt tokens evaluated by the server per task, in 9.6 s. Cost of a passed task: 4494 processed tokens and 36 s; of a failed one: 0 and 0 s. Peak memory: harness 38 MiB, model server 28791 MiB, GPU 7141 MiB.

Failures by cause: environment (skipped) 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 6.6 | 5.9 | 51126 | 39.0 | 0.3 | 40 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8701 | 9.7 | 0.0 | 10 |
| multi | 2/2 | 0 | 10.5 | 13.0 | 75549 | 57.0 | 0.3 | 58 |
| python | 3/3 | 0 | 5.7 | 5.0 | 42105 | 33.3 | 0.1 | 34 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9096 | 10.2 | 0.0 | 10 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 14216 | 12.7 | 0.1 | 13 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25734 | 81% | 853 | 33 |
| diagnose | 2/2 | 3.0 | 3.0 | 14550 | 88% | 474 | 18 |
| explain | 5/5 | 2.0 | 1.4 | 9106 | 86% | 361 | 13 |
| feature | 2/2 | 13.5 | 15.0 | 96770 | 97% | 2322 | 74 |
| small-fix | 4/4 | 5.0 | 5.2 | 25667 | 94% | 560 | 21 |
| large-change | 1/1 | 5.0 | 5.0 | 38765 | 88% | 915 | 33 |
| large-locate | 1/1 | 3.0 | 2.0 | 18325 | 70% | 401 | 21 |
| locate | 3/3 | 2.3 | 3.0 | 10717 | 83% | 297 | 13 |
| refactor | 2/2 | 6.5 | 8.0 | 39296 | 93% | 846 | 31 |
| session | 5/5 | 12.8 | 9.6 | 125676 | 93% | 2149 | 82 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25734 | 20764 | 5698 | 853 | 0 | 0 | 0 | 33 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8748 | 7919 | 4598 | 276 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 4 | 5 | 20351 | 17552 | 6391 | 671 | 0 | 0 | 0 | 25 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10162 | 7986 | 5984 | 523 | 0 | 0 | 0 | 18 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8787 | 7801 | 4744 | 461 | 0 | 0 | 0 | 16 |  |
| feature-currency | feature | code | pass | 8 | 8 | 51853 | 49346 | 7511 | 1480 | 0 | 0 | 2 | 47 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 20036 | 18654 | 5497 | 531 | 0 | 0 | 0 | 19 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 33262 | 31110 | 6468 | 705 | 0 | 0 | 0 | 27 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8701 | 7795 | 4662 | 242 | 0 | 0 | 0 | 10 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 5 | 5 | 38765 | 34287 | 8913 | 915 | 0 | 0 | 0 | 33 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 18325 | 12903 | 9244 | 401 | 0 | 0 | 0 | 21 |  |
| locate-topic-name | locate | chat | pass | 3 | 3 | 13766 | 12480 | 5198 | 342 | 0 | 0 | 0 | 14 |  |
| multi-feature-discount | feature | code | pass | 19 | 22 | 141687 | 138429 | 9850 | 3163 | 0 | 0 | 0 | 100 |  |
| multi-locate-mapping | locate | chat | pass | 2 | 4 | 9411 | 6343 | 5286 | 304 | 0 | 0 | 0 | 15 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8782 | 7822 | 4730 | 323 | 0 | 0 | 0 | 12 |  |
| py-fix-inventory | small-fix | code | pass | 6 | 6 | 29912 | 28553 | 5590 | 646 | 0 | 0 | 0 | 24 |  |
| refactor-validate | refactor | code | pass | 8 | 8 | 50688 | 47280 | 7976 | 1082 | 0 | 0 | 1 | 39 |  |
| rename-entries | refactor | code | pass | 5 | 8 | 27905 | 25667 | 6466 | 611 | 0 | 0 | 0 | 23 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9096 | 7891 | 5027 | 255 | 0 | 0 | 0 | 10 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 11 | 9 | 78648 | 74834 | 9121 | 2766 | 0 | 0 | 0 | 89 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 9 | 7 | 87621 | 78398 | 13876 | 1560 | 0 | 0 | 0 | 65 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 12 | 101766 | 98205 | 9538 | 2420 | 0 | 0 | 0 | 81 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 83329 | 77481 | 9059 | 1275 | 0 | 0 | 0 | 55 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 18 | 13 | 277014 | 255651 | 27246 | 2726 | 0 | 0 | 0 | 122 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19459 | 18092 | 5332 | 357 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8973 | 7828 | 4923 | 245 | 0 | 0 | 0 | 10 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5698 | 0 | 200 | 76 | 35 | 537 | 76 | 0 | 4 | 0 | 0 | 712 | 0 | 31 | 441 | 2338 | 65 | 1183 |
| diagnose-config | 4598 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 0 | 9 | 2338 | 29 | 912 |
| diagnose-failing-test | 6391 | 19 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 68 | 2338 | 29 | 1157 |
| explain-event-flow | 5984 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 0 | 20 | 2338 | 33 | 942 |
| explain-ledger-posting | 4744 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2338 | 22 | 899 |
| feature-currency | 7511 | 31 | 198 | 131 | 103 | 537 | 167 | 163 | 0 | 0 | 0 | 1565 | 0 | 0 | 840 | 2338 | 55 | 1383 |
| fix-balance-query | 5497 | 38 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 0 | 877 | 0 | 0 | 107 | 2338 | 32 | 1156 |
| fix-ledger-balance | 6468 | 36 | 204 | 76 | 39 | 537 | 36 | 24 | 7 | 0 | 0 | 1622 | 0 | 0 | 252 | 2338 | 52 | 1245 |
| java-explain-orders | 4662 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 2338 | 35 | 897 |
| large-change-txtar | 8913 | 24 | 200 | 286 | 153 | 537 | 17 | 43 | 0 | 0 | 0 | 3623 | 0 | 0 | 505 | 2338 | 79 | 1108 |
| large-locate-preorder | 9244 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 70 | 0 | 4526 | 0 | 0 | 40 | 2338 | 42 | 1007 |
| locate-topic-name | 5198 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 107 | 0 | 705 | 0 | 0 | 106 | 2338 | 25 | 979 |
| multi-feature-discount | 9850 | 47 | 202 | 113 | 68 | 537 | 647 | 42 | 11 | 0 | 78 | 1316 | 365 | 0 | 1930 | 2338 | 117 | 2039 |
| multi-locate-mapping | 5286 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 5 | 0 | 0 | 947 | 0 | 0 | 36 | 2338 | 38 | 1001 |
| py-explain-reorder | 4730 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2338 | 40 | 909 |
| py-fix-inventory | 5590 | 44 | 200 | 65 | 36 | 537 | 111 | 16 | 10 | 0 | 0 | 785 | 0 | 0 | 161 | 2338 | 43 | 1244 |
| refactor-validate | 7976 | 0 | 196 | 131 | 34 | 537 | 142 | 74 | 12 | 0 | 0 | 2629 | 0 | 0 | 514 | 2338 | 68 | 1301 |
| rename-entries | 6466 | 0 | 200 | 76 | 47 | 537 | 36 | 24 | 21 | 0 | 0 | 1721 | 0 | 0 | 152 | 2338 | 30 | 1284 |
| rust-explain-ledger | 5027 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 0 | 50 | 2338 | 41 | 924 |
| session-ledger | 9121 | 596 | 798 | 76 | 183 | 537 | 186 | 24 | 0 | 0 | 0 | 1791 | 0 | 31 | 454 | 2338 | 110 | 1997 |
| session-noisy-tests | 13876 | 315 | 607 | 65 | 74 | 537 | 6942 | 16 | 0 | 0 | 0 | 809 | 0 | 0 | 546 | 2338 | 81 | 1546 |
| session-resume-constraints | 9538 | 748 | 810 | 76 | 183 | 537 | 261 | 49 | 14 | 0 | 0 | 1791 | 0 | 31 | 888 | 2338 | 126 | 1686 |
| session-stale-config | 9059 | 291 | 1022 | 131 | 258 | 537 | 72 | 0 | 4 | 0 | 0 | 2380 | 0 | 28 | 359 | 2338 | 118 | 1521 |
| session-x-tools | 27246 | 1058 | 1175 | 286 | 692 | 537 | 20 | 0 | 95 | 0 | 60 | 18187 | 0 | 0 | 515 | 2338 | 209 | 2074 |
| ts-fix-cart-discount | 5332 | 37 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 0 | 769 | 0 | 0 | 65 | 2338 | 44 | 1048 |
| ts-locate-discount | 4923 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 0 | 17 | 2338 | 30 | 915 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
