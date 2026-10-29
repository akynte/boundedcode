# Direct-mode suite: direct-20261029T201311Z

Model: qwen3.6-35b-a3b-mtp. Started 2026-10-29T20:13:11Z, finished 2026-10-29T20:28:44Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=draft-mtp, working_set_tokens=0.

**23/26 passed (88%)**, 6.5 model calls and 6.2 tool calls per task, 59528 prompt tokens per task with a 92% prompt-cache hit rate, 1147 generated tokens per task, 12 KB of tool results per task, 36 s per task, 38459 tokens in the largest single prompt; 3 results set aside, 0 recalled, 0 summaries, 5 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 1, 102 KB → 5 KB. Freshness notes: 1.

Calls run together: 25 replies, 59 calls, 0.035 s together against 0.038 s one after another.

Decode: 51.4 tokens/s over the run (29814 tokens in 580 s of the server's decode time). Speculative decoding: 26412 tokens drafted, 21041 accepted (80%).

Model calls by role: plan 18 (0 without thinking, 2544 generated tokens, 93 s); explore 34 (0 without thinking, 10906 generated tokens, 322 s); edit 24 (0 without thinking, 2991 generated tokens, 79 s); debug 38 (0 without thinking, 5812 generated tokens, 137 s); answer 54 (0 without thinking, 7561 generated tokens, 290 s).

Where the agent's time went, per task: 35.4 s in model calls, 0.3 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1946 ms (median). 365 reasoning tokens per task. 4 prompt-cache invalidations (4 after the history was rewritten), 29652 prompt tokens recomputed; 4994 prompt tokens evaluated by the server per task, in 12.7 s. Cost of a passed task: 6347 processed tokens and 35 s; of a failed one: 4559 and 41 s. Peak memory: harness 40 MiB, model server 32468 MiB, GPU 7433 MiB.

Failures by cause: environment (skipped) 2, model 3. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 14/17 | 0 | 7.5 | 6.9 | 76272 | 41.2 | 0.3 | 42 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8712 | 9.3 | 0.0 | 9 |
| multi | 2/2 | 0 | 6.0 | 8.5 | 43156 | 47.0 | 0.4 | 48 |
| python | 3/3 | 0 | 6.0 | 5.3 | 37621 | 25.6 | 0.1 | 26 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9096 | 8.7 | 0.0 | 9 |
| typescript | 2/2 | 0 | 3.5 | 3.5 | 17063 | 16.0 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25787 | 81% | 863 | 27 |
| diagnose | 1/2 | 3.5 | 3.5 | 17362 | 89% | 716 | 20 |
| explain | 5/5 | 2.0 | 1.4 | 9111 | 86% | 326 | 10 |
| feature | 2/2 | 9.5 | 11.5 | 69620 | 96% | 2756 | 63 |
| small-fix | 3/4 | 6.0 | 6.5 | 31544 | 93% | 668 | 22 |
| large-change | 1/1 | 7.0 | 7.0 | 57884 | 92% | 1369 | 36 |
| large-locate | 1/1 | 3.0 | 2.0 | 18391 | 70% | 462 | 20 |
| locate | 3/3 | 2.0 | 2.0 | 9125 | 86% | 238 | 9 |
| refactor | 2/2 | 7.5 | 9.0 | 45170 | 94% | 946 | 27 |
| session | 4/5 | 14.4 | 11.6 | 196451 | 92% | 2654 | 93 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25787 | 20820 | 5716 | 863 | 0 | 0 | 0 | 27 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8758 | 7931 | 4608 | 298 | 0 | 0 | 0 | 10 |  |
| diagnose-failing-test | diagnose | chat | FAIL | 5 | 6 | 25966 | 23139 | 6351 | 1134 | 0 | 0 | 0 | 31 | switched to code. answer does not match /TestBalance/ |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10167 | 7992 | 5989 | 660 | 0 | 0 | 0 | 18 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8785 | 7801 | 4742 | 199 | 0 | 0 | 0 | 8 |  |
| feature-currency | feature | code | pass | 9 | 9 | 62278 | 59195 | 8192 | 1582 | 0 | 0 | 2 | 40 |  |
| fix-balance-query | small-fix | code | FAIL | 6 | 7 | 33699 | 31262 | 6759 | 767 | 0 | 0 | 0 | 24 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.001s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 33209 | 31063 | 6439 | 673 | 0 | 0 | 0 | 22 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8712 | 7806 | 4673 | 296 | 0 | 0 | 0 | 9 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 7 | 7 | 57884 | 53260 | 9476 | 1369 | 0 | 0 | 0 | 36 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 18391 | 12929 | 9297 | 462 | 0 | 0 | 0 | 20 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 9043 | 7887 | 4914 | 119 | 0 | 0 | 0 | 6 |  |
| multi-feature-discount | feature | code | pass | 10 | 14 | 76962 | 74270 | 10026 | 3931 | 0 | 0 | 2 | 86 |  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9350 | 7950 | 5225 | 230 | 0 | 0 | 0 | 9 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8795 | 7836 | 4743 | 217 | 0 | 0 | 0 | 8 |  |
| py-fix-inventory | small-fix | code | pass | 7 | 7 | 34126 | 32786 | 5491 | 570 | 0 | 0 | 0 | 19 |  |
| refactor-validate | refactor | code | pass | 9 | 10 | 56605 | 53228 | 8036 | 1241 | 0 | 0 | 1 | 34 |  |
| rename-entries | refactor | code | pass | 6 | 8 | 33736 | 31588 | 6428 | 651 | 0 | 0 | 0 | 20 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9096 | 7893 | 5027 | 258 | 0 | 0 | 0 | 9 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | FAIL | 12 | 10 | 84130 | 80285 | 9161 | 2668 | 0 | 0 | 0 | 69 | switched to code. turn 4: answer does not match /Balance\|ledger\.go/ |
| session-noisy-tests | session | chat, code, chat | pass | 9 | 7 | 69942 | 63027 | 10766 | 1466 | 0 | 0 | 0 | 51 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 14 | 12 | 113841 | 110303 | 10912 | 3833 | 0 | 0 | 0 | 93 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 82886 | 78597 | 8927 | 1147 | 0 | 0 | 0 | 42 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 25 | 22 | 631455 | 569121 | 38459 | 4155 | 3 | 0 | 0 | 210 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 5 | 25143 | 22073 | 5619 | 661 | 0 | 0 | 0 | 22 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8983 | 7838 | 4933 | 364 | 0 | 0 | 0 | 11 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:multi_edit | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5716 | 0 | 200 | 76 | 35 | 537 | 76 | 0 | 6 | 0 | 0 | 712 | 31 | 421 | 2338 | 65 | 1219 |
| diagnose-config | 4608 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2338 | 29 | 922 |
| diagnose-failing-test | 6351 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 0 | 1788 | 0 | 80 | 2338 | 29 | 1110 |
| explain-event-flow | 5989 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2338 | 33 | 947 |
| explain-ledger-posting | 4742 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2338 | 22 | 897 |
| feature-currency | 8192 | 19 | 198 | 131 | 103 | 537 | 167 | 164 | 0 | 0 | 0 | 2137 | 0 | 943 | 2338 | 55 | 1400 |
| fix-balance-query | 6759 | 53 | 202 | 76 | 73 | 537 | 152 | 24 | 0 | 0 | 0 | 1788 | 0 | 145 | 2338 | 32 | 1339 |
| fix-ledger-balance | 6439 | 40 | 204 | 76 | 39 | 537 | 36 | 24 | 7 | 0 | 0 | 1622 | 0 | 247 | 2338 | 52 | 1217 |
| java-explain-orders | 4673 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2338 | 35 | 908 |
| large-change-txtar | 9476 | 63 | 200 | 286 | 153 | 537 | 17 | 163 | 0 | 0 | 0 | 3623 | 0 | 726 | 2338 | 79 | 1291 |
| large-locate-preorder | 9297 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 70 | 0 | 4526 | 0 | 109 | 2338 | 42 | 991 |
| locate-topic-name | 4914 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2338 | 25 | 898 |
| multi-feature-discount | 10026 | 33 | 202 | 113 | 68 | 537 | 159 | 78 | 11 | 0 | 143 | 1643 | 0 | 2221 | 2338 | 117 | 2363 |
| multi-locate-mapping | 5225 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 0 | 947 | 0 | 27 | 2338 | 38 | 954 |
| py-explain-reorder | 4743 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2338 | 40 | 922 |
| py-fix-inventory | 5491 | 40 | 200 | 65 | 36 | 537 | 71 | 16 | 10 | 3 | 0 | 794 | 0 | 96 | 2338 | 43 | 1242 |
| refactor-validate | 8036 | 0 | 196 | 131 | 34 | 537 | 65 | 96 | 19 | 0 | 0 | 2631 | 0 | 533 | 2338 | 68 | 1388 |
| rename-entries | 6428 | 15 | 200 | 76 | 47 | 537 | 36 | 24 | 21 | 3 | 0 | 1623 | 0 | 152 | 2338 | 30 | 1326 |
| rust-explain-ledger | 5027 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 0 | 812 | 0 | 50 | 2338 | 41 | 924 |
| session-ledger | 9161 | 557 | 798 | 76 | 183 | 537 | 186 | 24 | 14 | 0 | 0 | 1791 | 31 | 468 | 2338 | 110 | 2048 |
| session-noisy-tests | 10766 | 405 | 607 | 65 | 74 | 537 | 3928 | 16 | 0 | 0 | 0 | 809 | 0 | 317 | 2338 | 81 | 1589 |
| session-resume-constraints | 10912 | 762 | 810 | 76 | 183 | 537 | 257 | 49 | 14 | 0 | 0 | 1791 | 31 | 1146 | 2338 | 126 | 2792 |
| session-stale-config | 8927 | 332 | 1022 | 131 | 258 | 537 | 74 | 0 | 4 | 0 | 0 | 2380 | 28 | 309 | 2338 | 118 | 1396 |
| session-x-tools | 38459 | 1182 | 976 | 286 | 492 | 537 | 0 | 0 | 179 | 76 | 0 | 29754 | 0 | 290 | 2338 | 179 | 2170 |
| ts-fix-cart-discount | 5619 | 44 | 200 | 82 | 36 | 537 | 143 | 64 | 0 | 0 | 0 | 769 | 0 | 257 | 2338 | 44 | 1105 |
| ts-locate-discount | 4933 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2338 | 30 | 925 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model mtp/Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 0b21525e972670ed59e1812e170b27c26355381f0656ecc4e25617ece7dac58b); tasks bdf975b69c63e339.
