# Direct-mode suite: direct-20261029T193333Z

Model: qwen3-coder-next. Started 2026-10-29T19:33:33Z, finished 2026-10-29T19:53:47Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=false, retrieval=adaptive, routing=off, seed_pack=true, speculative=off, working_set_tokens=0.

**24/26 passed (92%)**, 6.7 model calls and 5.8 tool calls per task, 53031 prompt tokens per task with a 94% prompt-cache hit rate, 778 generated tokens per task, 12 KB of tool results per task, 47 s per task, 23845 tokens in the largest single prompt; 0 results set aside, 1 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 143 KB → 12 KB. Freshness notes: 1.

Calls run together: 17 replies, 36 calls, 0.007 s together against 0.016 s one after another.

Decode: 25.1 tokens/s over the run (20231 tokens in 807 s of the server's decode time).

Model calls by role: plan 18 (0 without thinking, 2412 generated tokens, 163 s); explore 28 (0 without thinking, 5020 generated tokens, 276 s); edit 35 (0 without thinking, 4088 generated tokens, 212 s); debug 38 (0 without thinking, 4669 generated tokens, 239 s); answer 56 (0 without thinking, 4042 generated tokens, 312 s).

Where the agent's time went, per task: 46.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 2405 ms (median). 0 reasoning tokens per task. 2 prompt-cache invalidations (2 after the history was rewritten), 4348 prompt tokens recomputed; 3435 prompt tokens evaluated by the server per task, in 14.7 s. Cost of a passed task: 4044 processed tokens and 41 s; of a failed one: 6244 and 119 s. Peak memory: harness 38 MiB, model server 34509 MiB, GPU 7561 MiB.

Failures by cause: environment (skipped) 2, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 16/17 | 0 | 7.2 | 6.2 | 59917 | 50.3 | 0.3 | 51 |
| java | 1/1 | 1 | 2.0 | 1.0 | 9397 | 7.7 | 0.0 | 8 |
| multi | 1/2 | 0 | 12.0 | 11.0 | 88133 | 89.6 | 0.4 | 90 |
| python | 3/3 | 0 | 6.0 | 5.0 | 44785 | 39.8 | 0.1 | 40 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 9706 | 7.4 | 0.0 | 7 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 15250 | 15.9 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 5.0 | 27241 | 80% | 555 | 56 |
| diagnose | 2/2 | 4.5 | 6.0 | 24080 | 93% | 506 | 29 |
| explain | 5/5 | 2.0 | 1.4 | 9791 | 88% | 173 | 12 |
| feature | 1/2 | 17.0 | 16.0 | 131006 | 97% | 2864 | 141 |
| small-fix | 4/4 | 5.0 | 5.0 | 27072 | 94% | 563 | 32 |
| large-change | 0/1 | 7.0 | 7.0 | 60588 | 92% | 1299 | 69 |
| large-locate | 1/1 | 3.0 | 2.0 | 24560 | 74% | 163 | 24 |
| locate | 3/3 | 2.0 | 1.3 | 9747 | 83% | 91 | 10 |
| refactor | 2/2 | 6.5 | 6.5 | 37760 | 95% | 700 | 39 |
| session | 5/5 | 13.6 | 9.8 | 138851 | 94% | 1337 | 86 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 5 | 27241 | 21737 | 6048 | 555 | 0 | 0 | 0 | 56 |  |
| diagnose-config | diagnose | chat | pass | 2 | 2 | 9570 | 8698 | 5034 | 269 | 0 | 0 | 0 | 15 |  |
| diagnose-failing-test | diagnose | chat | pass | 7 | 10 | 38590 | 35987 | 7180 | 743 | 0 | 0 | 0 | 42 |  |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10791 | 8721 | 6227 | 327 | 0 | 0 | 0 | 19 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 9485 | 8546 | 5053 | 176 | 0 | 0 | 0 | 12 |  |
| feature-currency | feature | code | pass | 12 | 11 | 95598 | 91492 | 10323 | 2268 | 0 | 0 | 2 | 113 |  |
| fix-balance-query | small-fix | code | pass | 5 | 5 | 26814 | 25316 | 6107 | 563 | 0 | 0 | 0 | 33 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 6 | 29440 | 27434 | 6807 | 796 | 0 | 0 | 0 | 42 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 9397 | 8543 | 4968 | 88 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | FAIL | 7 | 7 | 60588 | 55739 | 10194 | 1299 | 0 | 0 | 1 | 69 | …: --- FAIL: TestHiddenLookup (0.00s)     hidden_lookup_test.go:16: Lookup must return a pointer into the archive FAIL FAIL	golang.org/x/tools/txtar	0.002s FAIL  |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 24560 | 18130 | 10575 | 163 | 0 | 0 | 0 | 24 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 9732 | 8628 | 5217 | 58 | 0 | 0 | 0 | 7 |  |
| multi-feature-discount | feature | code | FAIL | 22 | 21 | 166414 | 163534 | 10256 | 3461 | 0 | 0 | 0 | 168 | …-- FAIL: TestHiddenDiscount (0.00s)     hidden_test.go:12: {ID:o1 SubtotalCents:1000 TotalCents:700 DiscountCents:0} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 1 | 9852 | 6981 | 5340 | 66 | 0 | 0 | 0 | 12 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 9574 | 8655 | 5132 | 210 | 0 | 0 | 0 | 13 |  |
| py-fix-inventory | small-fix | code | pass | 6 | 5 | 31192 | 29995 | 5800 | 579 | 0 | 0 | 0 | 31 |  |
| refactor-validate | refactor | code | pass | 6 | 6 | 35982 | 33631 | 6799 | 673 | 0 | 0 | 0 | 38 |  |
| rename-entries | refactor | code | pass | 7 | 7 | 39539 | 37984 | 6318 | 726 | 0 | 0 | 0 | 39 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 9706 | 8569 | 5248 | 65 | 0 | 0 | 0 | 7 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 87781 | 83566 | 9174 | 1536 | 0 | 0 | 0 | 85 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 10 | 8 | 93590 | 81061 | 13535 | 1017 | 0 | 1 | 0 | 76 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 11 | 7 | 74014 | 71314 | 8112 | 1371 | 0 | 0 | 0 | 74 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 11 | 6 | 75351 | 71432 | 8772 | 779 | 0 | 0 | 0 | 53 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 24 | 18 | 363517 | 345678 | 23845 | 1982 | 0 | 0 | 0 | 144 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 20843 | 19555 | 5658 | 313 | 0 | 0 | 0 | 21 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 9657 | 8573 | 5218 | 148 | 0 | 0 | 0 | 11 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:read | tool:read_many | tool:recall | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 6048 | 0 | 197 | 72 | 34 | 521 | 18 | 0 | 7 | 0 | 0 | 923 | 0 | 0 | 0 | 30 | 507 | 2339 | 65 | 1335 |
| diagnose-config | 5034 | 24 | 196 | 124 | 87 | 521 | 0 | 0 | 0 | 0 | 0 | 409 | 0 | 0 | 0 | 0 | 21 | 2339 | 29 | 1284 |
| diagnose-failing-test | 7180 | 11 | 202 | 72 | 57 | 521 | 113 | 0 | 29 | 0 | 10 | 1985 | 0 | 0 | 0 | 0 | 327 | 2339 | 29 | 1485 |
| explain-event-flow | 6227 | 20 | 194 | 124 | 113 | 521 | 0 | 0 | 0 | 0 | 0 | 1579 | 0 | 0 | 0 | 0 | 20 | 2339 | 33 | 1284 |
| explain-ledger-posting | 5053 | 0 | 200 | 72 | 38 | 521 | 0 | 0 | 0 | 0 | 0 | 584 | 0 | 0 | 0 | 0 | 11 | 2339 | 22 | 1266 |
| feature-currency | 10323 | 224 | 195 | 124 | 102 | 521 | 366 | 166 | 0 | 0 | 0 | 1974 | 969 | 0 | 0 | 0 | 1869 | 2339 | 55 | 1419 |
| fix-balance-query | 6107 | 136 | 199 | 72 | 72 | 521 | 204 | 23 | 0 | 0 | 0 | 828 | 0 | 0 | 0 | 0 | 324 | 2339 | 32 | 1357 |
| fix-ledger-balance | 6807 | 125 | 201 | 72 | 38 | 521 | 36 | 23 | 7 | 0 | 0 | 1506 | 0 | 0 | 0 | 0 | 495 | 2339 | 52 | 1392 |
| java-explain-orders | 4968 | 0 | 194 | 62 | 38 | 521 | 0 | 0 | 0 | 0 | 0 | 502 | 0 | 0 | 0 | 0 | 11 | 2339 | 35 | 1266 |
| large-change-txtar | 10194 | 137 | 197 | 271 | 152 | 521 | 17 | 161 | 0 | 0 | 0 | 3879 | 0 | 0 | 0 | 0 | 1164 | 2339 | 79 | 1277 |
| large-locate-preorder | 10575 | 0 | 198 | 271 | 196 | 521 | 0 | 0 | 0 | 0 | 0 | 5687 | 0 | 0 | 0 | 0 | 29 | 2339 | 42 | 1292 |
| locate-topic-name | 5217 | 0 | 194 | 124 | 72 | 521 | 0 | 0 | 0 | 0 | 0 | 666 | 0 | 0 | 0 | 0 | 10 | 2339 | 25 | 1266 |
| multi-feature-discount | 10256 | 0 | 199 | 107 | 66 | 521 | 609 | 112 | 0 | 0 | 0 | 197 | 570 | 0 | 615 | 0 | 3028 | 2339 | 117 | 1776 |
| multi-locate-mapping | 5340 | 0 | 200 | 107 | 67 | 521 | 0 | 0 | 0 | 0 | 0 | 0 | 766 | 0 | 0 | 0 | 34 | 2339 | 38 | 1268 |
| py-explain-reorder | 5132 | 0 | 198 | 62 | 42 | 521 | 0 | 0 | 0 | 0 | 0 | 550 | 0 | 0 | 0 | 0 | 96 | 2339 | 40 | 1284 |
| py-fix-inventory | 5800 | 65 | 197 | 62 | 35 | 521 | 17 | 15 | 10 | 0 | 0 | 741 | 0 | 0 | 0 | 0 | 381 | 2339 | 43 | 1374 |
| refactor-validate | 6799 | 0 | 193 | 124 | 33 | 521 | 77 | 42 | 6 | 0 | 0 | 1476 | 0 | 0 | 0 | 0 | 572 | 2339 | 68 | 1348 |
| rename-entries | 6318 | 166 | 197 | 72 | 46 | 521 | 39 | 23 | 21 | 3 | 0 | 0 | 1031 | 0 | 0 | 0 | 392 | 2339 | 30 | 1438 |
| rust-explain-ledger | 5248 | 0 | 202 | 85 | 30 | 521 | 0 | 0 | 0 | 0 | 0 | 756 | 0 | 0 | 0 | 0 | 8 | 2339 | 41 | 1266 |
| session-ledger | 9174 | 293 | 784 | 72 | 180 | 521 | 183 | 23 | 0 | 0 | 0 | 2273 | 0 | 0 | 0 | 30 | 862 | 2339 | 110 | 1504 |
| session-noisy-tests | 13535 | 460 | 596 | 62 | 72 | 521 | 4966 | 15 | 0 | 0 | 0 | 769 | 0 | 1784 | 0 | 0 | 361 | 2339 | 81 | 1509 |
| session-resume-constraints | 8112 | 463 | 797 | 72 | 180 | 521 | 255 | 23 | 0 | 0 | 0 | 0 | 1075 | 0 | 0 | 30 | 776 | 2339 | 126 | 1455 |
| session-stale-config | 8772 | 277 | 1005 | 124 | 253 | 521 | 16 | 0 | 0 | 0 | 0 | 2241 | 0 | 0 | 0 | 27 | 462 | 2339 | 118 | 1389 |
| session-x-tools | 23845 | 672 | 1156 | 271 | 686 | 521 | 20 | 61 | 73 | 31 | 13 | 15013 | 0 | 0 | 0 | 0 | 990 | 2339 | 209 | 1790 |
| ts-fix-cart-discount | 5658 | 38 | 197 | 78 | 35 | 521 | 110 | 53 | 0 | 0 | 0 | 718 | 0 | 0 | 0 | 0 | 187 | 2339 | 44 | 1338 |
| ts-locate-discount | 5218 | 0 | 196 | 78 | 35 | 521 | 0 | 0 | 0 | 0 | 0 | 718 | 0 | 0 | 0 | 0 | 17 | 2339 | 30 | 1284 |

Environment: build dev 656874fb1b19-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3-Coder-Next-UD-IQ3_XXS.gguf (sha256 00a9bf4f9dcb6ef4fb78ef810e6b5dbd13cc604ceb0fa10f56c9ae2529c0ade4); tasks bdf975b69c63e339.
