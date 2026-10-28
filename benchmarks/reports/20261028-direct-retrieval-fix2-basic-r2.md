# Direct-mode suite: direct-20261028T121714Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T12:17:14Z, finished 2026-10-28T12:31:57Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=basic, seed_pack=true, working_set_tokens=0.

**20/23 passed (87%)**, 5.7 model calls and 6.2 tool calls per task, 41213 prompt tokens per task with a 92% prompt-cache hit rate, 1030 generated tokens per task, 6 KB of tool results per task, 38 s per task, 22573 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 2 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 38.0 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1776 ms (median). 301 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 630 prompt tokens recomputed. Cost of a passed task: 4236 processed tokens and 33 s; of a failed one: 6453 and 73 s. Peak memory: harness 35 MiB, model server 28479 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, model 3. Stalls: empty_answer 1.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 13/15 | 0 | 6.0 | 6.3 | 47035 | 43.1 | 0.3 | 44 |
| java | 1/1 | 1 | 2.0 | 2.0 | 8736 | 9.0 | 0.0 | 9 |
| multi | 1/2 | 0 | 10.5 | 11.5 | 74790 | 70.9 | 0.3 | 71 |
| python | 2/2 | 0 | 3.5 | 3.5 | 15670 | 13.0 | 0.0 | 13 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 17268 | 16.9 | 0.0 | 17 |
| typescript | 2/2 | 0 | 4.0 | 5.0 | 17722 | 16.7 | 0.1 | 17 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 8.0 | 35907 | 85% | 1297 | 47 |
| diagnose | 2/2 | 3.0 | 4.0 | 13293 | 85% | 520 | 19 |
| explain | 5/5 | 2.6 | 2.8 | 11316 | 87% | 384 | 14 |
| feature | 1/2 | 13.5 | 16.5 | 97936 | 94% | 2807 | 97 |
| small-fix | 3/4 | 5.0 | 5.5 | 24100 | 92% | 608 | 24 |
| large-change | 0/1 | 7.0 | 7.0 | 46015 | 89% | 1192 | 45 |
| large-locate | 1/1 | 4.0 | 4.0 | 27332 | 78% | 680 | 29 |
| locate | 3/3 | 2.3 | 2.0 | 10066 | 87% | 237 | 10 |
| refactor | 2/2 | 7.0 | 8.0 | 38883 | 93% | 790 | 32 |
| session | 2/2 | 14.5 | 12.0 | 177621 | 93% | 3617 | 133 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 8 | 35907 | 30570 | 8852 | 1297 | 0 | 0 | 0 | 47 |  |
| diagnose-config | diagnose | chat | pass | 3 | 3 | 12709 | 11631 | 4601 | 364 | 0 | 0 | 0 | 15 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 13877 | 11097 | 5984 | 675 | 0 | 0 | 0 | 24 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 13715 | 11461 | 5777 | 555 | 0 | 0 | 0 | 20 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8510 | 7451 | 4586 | 549 | 0 | 0 | 0 | 18 |  |
| feature-currency | feature | code | pass | 8 | 11 | 55159 | 49772 | 8890 | 1706 | 0 | 0 | 0 | 60 |  |
| fix-balance-query | small-fix | code | FAIL | 6 | 6 | 29648 | 27757 | 5402 | 1105 | 0 | 0 | 0 | 39 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 4 | 4 | 20594 | 18085 | 6028 | 512 | 0 | 0 | 0 | 21 |  |
| java-explain-orders | explain | chat | pass | 2 | 2 | 8736 | 7423 | 4840 | 209 | 0 | 0 | 0 | 9 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | FAIL | 7 | 7 | 46015 | 40795 | 8727 | 1192 | 0 | 0 | 0 | 45 | …: --- FAIL: TestHiddenLookup (0.00s)     hidden_lookup_test.go:16: Lookup must return a pointer into the archive FAIL FAIL	golang.org/x/tools/txtar	0.002s FAIL  |
| large-locate-preorder | large-locate | chat | pass | 4 | 4 | 27332 | 21357 | 9494 | 680 | 0 | 0 | 0 | 29 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 9050 | 7424 | 5153 | 174 | 0 | 0 | 0 | 9 |  |
| multi-feature-discount | feature | code | FAIL | 19 | 22 | 140712 | 134669 | 9502 | 3908 | 0 | 0 | 0 | 134 | ….00s)     hidden_test.go:15: discount above the subtotal: {ID:o2 SubtotalCents:1000 TotalCents:0 DiscountCents:5000} FAIL FAIL	example.com/shop/api	0.002s FAIL  |
| multi-locate-mapping | locate | chat | pass | 2 | 1 | 8869 | 7425 | 4971 | 166 | 0 | 0 | 0 | 9 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8349 | 7377 | 4499 | 197 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 5 | 5 | 22991 | 21528 | 4978 | 391 | 0 | 0 | 0 | 18 |  |
| refactor-validate | refactor | code | pass | 8 | 8 | 46707 | 43510 | 6700 | 998 | 0 | 0 | 1 | 40 |  |
| rename-entries | refactor | code | pass | 6 | 8 | 31059 | 28584 | 5986 | 583 | 0 | 0 | 0 | 24 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 17268 | 15645 | 5142 | 412 | 0 | 0 | 0 | 17 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 65651 | 60724 | 8119 | 3408 | 0 | 0 | 0 | 110 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 19 | 15 | 289591 | 270477 | 22573 | 3826 | 0 | 0 | 1 | 156 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 7 | 23166 | 21427 | 5254 | 422 | 0 | 0 | 0 | 20 |  |
| ts-locate-discount | locate | chat | pass | 3 | 3 | 12279 | 11323 | 4479 | 370 | 0 | 0 | 0 | 14 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:read_many | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 8852 | 15 | 200 | 76 | 0 | 537 | 2232 | 0 | 0 | 0 | 0 | 0 | 1721 | 0 | 0 | 31 | 750 | 2199 | 65 | 1026 |
| diagnose-config | 4601 | 0 | 199 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 195 | 0 | 355 | 0 | 0 | 0 | 46 | 2199 | 29 | 910 |
| diagnose-failing-test | 5984 | 19 | 206 | 76 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 0 | 70 | 2199 | 29 | 945 |
| explain-event-flow | 5777 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 33 | 0 | 0 | 1695 | 0 | 0 | 0 | 35 | 2199 | 33 | 917 |
| explain-ledger-posting | 4586 | 0 | 203 | 76 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 8890 | 245 | 198 | 131 | 0 | 537 | 169 | 45 | 0 | 33 | 0 | 0 | 3220 | 0 | 0 | 0 | 935 | 2199 | 55 | 1123 |
| fix-balance-query | 5402 | 69 | 202 | 76 | 48 | 537 | 148 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 0 | 151 | 2199 | 32 | 1039 |
| fix-ledger-balance | 6028 | 108 | 204 | 76 | 78 | 537 | 0 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 0 | 0 | 215 | 2199 | 52 | 937 |
| java-explain-orders | 4840 | 0 | 197 | 65 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 869 | 0 | 0 | 0 | 23 | 2199 | 35 | 861 |
| large-change-txtar | 8727 | 6 | 200 | 286 | 0 | 537 | 18 | 120 | 0 | 0 | 0 | 13 | 3633 | 0 | 0 | 0 | 594 | 2199 | 79 | 1042 |
| large-locate-preorder | 9494 | 0 | 201 | 286 | 0 | 537 | 0 | 0 | 46 | 84 | 0 | 0 | 5080 | 0 | 0 | 0 | 59 | 2199 | 57 | 945 |
| locate-topic-name | 5153 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 1144 | 0 | 0 | 0 | 0 | 0 | 34 | 2199 | 25 | 886 |
| multi-feature-discount | 9502 | 63 | 202 | 113 | 130 | 537 | 122 | 175 | 0 | 0 | 0 | 0 | 1316 | 0 | 425 | 0 | 2451 | 2199 | 117 | 1652 |
| multi-locate-mapping | 4971 | 0 | 203 | 113 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1022 | 0 | 0 | 14 | 2199 | 38 | 845 |
| py-explain-reorder | 4499 | 0 | 201 | 65 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 0 | 15 | 2199 | 40 | 860 |
| py-fix-inventory | 4978 | 0 | 200 | 65 | 46 | 537 | 20 | 16 | 0 | 0 | 0 | 0 | 794 | 0 | 0 | 0 | 67 | 2199 | 43 | 991 |
| refactor-validate | 6700 | 97 | 196 | 131 | 73 | 537 | 144 | 96 | 0 | 6 | 0 | 0 | 1565 | 0 | 0 | 0 | 479 | 2199 | 68 | 1109 |
| rename-entries | 5986 | 28 | 200 | 76 | 0 | 537 | 37 | 24 | 0 | 32 | 3 | 0 | 1599 | 0 | 0 | 0 | 148 | 2199 | 30 | 1073 |
| rust-explain-ledger | 5142 | 0 | 205 | 89 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 20 | 928 | 0 | 0 | 0 | 165 | 2199 | 41 | 958 |
| session-ledger | 8119 | 693 | 798 | 76 | 0 | 537 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 0 | 0 | 31 | 496 | 2199 | 110 | 1176 |
| session-x-tools | 22573 | 1041 | 1175 | 286 | 0 | 537 | 21 | 152 | 0 | 179 | 863 | 0 | 13432 | 0 | 0 | 0 | 1100 | 2199 | 209 | 1379 |
| ts-fix-cart-discount | 5254 | 39 | 200 | 82 | 0 | 537 | 123 | 54 | 0 | 12 | 0 | 0 | 847 | 0 | 0 | 0 | 82 | 2199 | 44 | 1035 |
| ts-locate-discount | 4479 | 0 | 199 | 82 | 0 | 537 | 0 | 0 | 0 | 7 | 0 | 0 | 503 | 0 | 0 | 0 | 22 | 2199 | 30 | 900 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
