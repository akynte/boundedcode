# Direct-mode suite: direct-20261028T221147Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T22:11:47Z, finished 2026-10-28T22:27:58Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, observation_bytes=0, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**25/26 passed (96%)**, 6.3 model calls and 6.0 tool calls per task, 52424 prompt tokens per task with a 93% prompt-cache hit rate, 1014 generated tokens per task, 12 KB of tool results per task, 37 s per task, 34025 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 3 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Summaries: 0, 0.0 s writing them, 0 prompt tokens evaluated for them and 0 by the first call after each. Command outputs pruned: 2, 143 KB → 12 KB. Freshness notes: 1.

Where the agent's time went, per task: 36.9 s in model calls, 0.2 s in tools (with the checks after edits), 0.04 s in the harness itself; time to first token 1771 ms (median). 312 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 1102 prompt tokens recomputed; 3555 prompt tokens evaluated by the server per task, in 9.6 s. Cost of a passed task: 4533 processed tokens and 36 s; of a failed one: 5462 and 82 s. Peak memory: harness 40 MiB, model server 28824 MiB, GPU 7155 MiB.

Failures by cause: environment (skipped) 2, model 1. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 17/17 | 0 | 7.2 | 6.8 | 65654 | 43.7 | 0.3 | 44 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8622 | 8.3 | 0.0 | 8 |
| multi | 1/2 | 0 | 8.5 | 11.0 | 54945 | 46.2 | 0.4 | 47 |
| python | 3/3 | 0 | 4.7 | 4.0 | 30533 | 27.3 | 0.1 | 28 |
| rust | 1/1 | 1 | 2.0 | 1.0 | 8963 | 8.1 | 0.0 | 8 |
| typescript | 2/2 | 0 | 3.0 | 2.5 | 13922 | 13.0 | 0.1 | 13 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 6.0 | 25464 | 95% | 835 | 27 |
| diagnose | 2/2 | 3.5 | 3.5 | 17201 | 89% | 686 | 24 |
| explain | 5/5 | 2.0 | 1.4 | 9010 | 86% | 284 | 11 |
| feature | 1/2 | 11.0 | 14.5 | 73704 | 96% | 1990 | 64 |
| small-fix | 4/4 | 4.5 | 4.8 | 22686 | 94% | 534 | 20 |
| large-change | 1/1 | 6.0 | 6.0 | 48411 | 91% | 1260 | 43 |
| large-locate | 1/1 | 4.0 | 3.0 | 25985 | 73% | 504 | 25 |
| locate | 3/3 | 2.0 | 1.7 | 8935 | 87% | 242 | 10 |
| refactor | 2/2 | 6.0 | 8.0 | 34196 | 92% | 739 | 27 |
| session | 5/5 | 14.8 | 11.6 | 170074 | 94% | 2528 | 96 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 6 | 25464 | 24183 | 5639 | 835 | 0 | 0 | 0 | 27 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8670 | 7841 | 4565 | 322 | 0 | 0 | 0 | 12 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 25732 | 22903 | 6299 | 1050 | 0 | 0 | 0 | 36 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 10073 | 7897 | 5940 | 580 | 0 | 0 | 0 | 19 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8697 | 7711 | 4699 | 279 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 7 | 10 | 46789 | 43732 | 7956 | 1415 | 0 | 0 | 0 | 47 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 19794 | 18412 | 5426 | 515 | 0 | 0 | 0 | 19 |  |
| fix-ledger-balance | small-fix | code | pass | 6 | 7 | 33316 | 31150 | 6509 | 775 | 0 | 0 | 0 | 29 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8622 | 7716 | 4628 | 188 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 6 | 6 | 48411 | 43819 | 9311 | 1260 | 0 | 0 | 0 | 43 |  |
| large-locate-preorder | large-locate | chat | pass | 4 | 3 | 25985 | 18954 | 10847 | 504 | 0 | 0 | 0 | 25 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8951 | 7793 | 4867 | 158 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | FAIL | 15 | 19 | 100619 | 97721 | 8894 | 2564 | 0 | 0 | 0 | 82 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 3 | 9271 | 7871 | 5191 | 268 | 0 | 0 | 0 | 12 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8696 | 7736 | 4689 | 202 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 4 | 4 | 18375 | 17437 | 5009 | 511 | 0 | 0 | 0 | 18 |  |
| refactor-validate | refactor | code | pass | 7 | 8 | 41677 | 38320 | 7757 | 906 | 0 | 0 | 1 | 34 |  |
| rename-entries | refactor | code | pass | 5 | 8 | 26715 | 24545 | 6319 | 572 | 0 | 0 | 0 | 21 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 1 | 8963 | 7758 | 4939 | 170 | 0 | 0 | 0 | 8 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 13 | 11 | 95230 | 91102 | 9518 | 2527 | 0 | 0 | 0 | 84 | switched to code. |
| session-noisy-tests | session | chat, code, chat | pass | 8 | 6 | 64529 | 57215 | 11783 | 1408 | 0 | 0 | 0 | 56 | switched to code. |
| session-resume-constraints | session | code, chat, code, code | pass | 18 | 16 | 151827 | 147279 | 11392 | 3464 | 0 | 0 | 1 | 113 |  |
| session-stale-config | session | chat, chat, chat, chat, code | pass | 12 | 7 | 82317 | 76489 | 8920 | 1240 | 0 | 0 | 0 | 54 |  |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 23 | 18 | 456468 | 429415 | 34025 | 4002 | 0 | 0 | 1 | 171 |  |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 19261 | 17894 | 5278 | 337 | 0 | 0 | 0 | 15 |  |
| ts-locate-discount | locate | chat | pass | 2 | 1 | 8584 | 7716 | 4579 | 299 | 0 | 0 | 0 | 11 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5639 | 0 | 200 | 76 | 35 | 537 | 76 | 0 | 4 | 0 | 712 | 31 | 413 | 2297 | 65 | 1193 |
| diagnose-config | 4565 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2297 | 29 | 920 |
| diagnose-failing-test | 6299 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 14 | 0 | 1788 | 0 | 80 | 2297 | 29 | 1099 |
| explain-event-flow | 5940 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2297 | 33 | 939 |
| explain-ledger-posting | 4699 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2297 | 22 | 895 |
| feature-currency | 7956 | 11 | 198 | 131 | 103 | 537 | 167 | 45 | 28 | 0 | 2212 | 0 | 804 | 2297 | 55 | 1368 |
| fix-balance-query | 5426 | 50 | 202 | 76 | 73 | 537 | 37 | 24 | 0 | 0 | 877 | 0 | 108 | 2297 | 32 | 1113 |
| fix-ledger-balance | 6509 | 39 | 204 | 76 | 39 | 537 | 36 | 24 | 21 | 0 | 1622 | 0 | 249 | 2297 | 52 | 1313 |
| java-explain-orders | 4628 | 0 | 197 | 65 | 39 | 537 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2297 | 35 | 904 |
| large-change-txtar | 9311 | 55 | 200 | 286 | 153 | 537 | 17 | 141 | 0 | 0 | 3623 | 0 | 726 | 2297 | 79 | 1197 |
| large-locate-preorder | 10847 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 0 | 1635 | 4526 | 0 | 56 | 2297 | 42 | 1070 |
| locate-topic-name | 4867 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2297 | 25 | 892 |
| multi-feature-discount | 8894 | 26 | 202 | 113 | 68 | 537 | 618 | 156 | 6 | 0 | 1372 | 0 | 1409 | 2297 | 117 | 1973 |
| multi-locate-mapping | 5191 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 0 | 947 | 0 | 27 | 2297 | 38 | 961 |
| py-explain-reorder | 4689 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2297 | 40 | 909 |
| py-fix-inventory | 5009 | 38 | 200 | 65 | 36 | 537 | 68 | 16 | 10 | 0 | 436 | 0 | 151 | 2297 | 43 | 1112 |
| refactor-validate | 7757 | 0 | 196 | 131 | 34 | 537 | 80 | 96 | 12 | 0 | 2629 | 0 | 411 | 2297 | 68 | 1266 |
| rename-entries | 6319 | 28 | 200 | 76 | 47 | 537 | 36 | 24 | 29 | 41 | 1599 | 0 | 150 | 2297 | 30 | 1225 |
| rust-explain-ledger | 4939 | 0 | 205 | 89 | 31 | 537 | 0 | 0 | 0 | 0 | 812 | 0 | 8 | 2297 | 41 | 919 |
| session-ledger | 9518 | 594 | 798 | 76 | 250 | 537 | 301 | 48 | 0 | 0 | 1860 | 31 | 528 | 2297 | 110 | 2088 |
| session-noisy-tests | 11783 | 361 | 607 | 65 | 74 | 537 | 5051 | 16 | 0 | 0 | 809 | 0 | 317 | 2297 | 81 | 1568 |
| session-resume-constraints | 11392 | 776 | 810 | 76 | 204 | 537 | 380 | 147 | 14 | 0 | 2479 | 31 | 941 | 2297 | 126 | 2574 |
| session-stale-config | 8920 | 298 | 1022 | 131 | 258 | 537 | 16 | 0 | 4 | 0 | 2380 | 28 | 350 | 2297 | 118 | 1481 |
| session-x-tools | 34025 | 1189 | 1175 | 286 | 692 | 537 | 20 | 153 | 95 | 31 | 23525 | 0 | 953 | 2297 | 209 | 2863 |
| ts-fix-cart-discount | 5278 | 33 | 200 | 82 | 36 | 537 | 122 | 54 | 0 | 0 | 769 | 0 | 65 | 2297 | 44 | 1039 |
| ts-locate-discount | 4579 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 494 | 0 | 8 | 2297 | 30 | 896 |

Environment: build dev 1d8506b2e766-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks bdf975b69c63e339.
