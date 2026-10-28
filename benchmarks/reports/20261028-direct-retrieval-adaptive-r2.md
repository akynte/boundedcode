# Direct-mode suite: direct-20261028T102329Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T10:23:29Z, finished 2026-10-28T10:32:49Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**18/23 passed (78%)**, 3.9 model calls and 4.1 tool calls per task, 21001 prompt tokens per task with a 88% prompt-cache hit rate, 636 generated tokens per task, 4 KB of tool results per task, 24 s per task, 9963 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 1 broken edits caught by the check, 0 check notes, 0 history notes, 3 errors.

Where the agent's time went, per task: 24.1 s in model calls, 0.1 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1827 ms (median). 175 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 748 prompt tokens recomputed. Cost of a passed task: 2909 processed tokens and 24 s; of a failed one: 3739 and 26 s. Peak memory: harness 34 MiB, model server 27744 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2, environment 3, model 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 12/15 | 0 | 4.5 | 4.6 | 25689 | 29.8 | 0.2 | 30 |
| java | 1/1 | 1 | 2.0 | 1.0 | 8338 | 7.8 | 0.0 | 8 |
| multi | 1/2 | 0 | 1.5 | 3.5 | 6538 | 8.8 | 0.0 | 9 |
| python | 1/2 | 0 | 3.5 | 3.5 | 15696 | 16.7 | 0.0 | 17 |
| rust | 1/1 | 1 | 4.0 | 5.0 | 17586 | 19.1 | 0.0 | 19 |
| typescript | 2/2 | 0 | 3.0 | 3.0 | 13646 | 14.1 | 0.1 | 14 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 5.0 | 24422 | 93% | 1015 | 35 |
| diagnose | 2/2 | 3.5 | 3.5 | 16648 | 89% | 604 | 22 |
| explain | 5/5 | 2.4 | 2.2 | 10515 | 87% | 343 | 13 |
| feature | 1/2 | 3.0 | 4.0 | 16394 | 89% | 608 | 22 |
| small-fix | 2/4 | 4.5 | 4.5 | 21775 | 92% | 590 | 23 |
| large-change | 0/1 | 4.0 | 5.0 | 29137 | 83% | 1122 | 41 |
| large-locate | 1/1 | 3.0 | 2.0 | 17598 | 69% | 294 | 18 |
| locate | 3/3 | 2.0 | 2.3 | 8777 | 85% | 292 | 11 |
| refactor | 2/2 | 6.0 | 10.0 | 35090 | 90% | 848 | 33 |
| session | 1/2 | 8.0 | 6.0 | 54792 | 89% | 1566 | 60 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 5 | 24422 | 22594 | 5343 | 1015 | 0 | 0 | 0 | 35 |  |
| diagnose-config | diagnose | chat | pass | 2 | 1 | 8380 | 7519 | 4388 | 295 | 0 | 0 | 0 | 11 |  |
| diagnose-failing-test | diagnose | chat | pass | 5 | 6 | 24917 | 21957 | 6097 | 912 | 0 | 0 | 0 | 32 | switched to code. |
| explain-event-flow | explain | chat | pass | 2 | 2 | 9784 | 7547 | 5764 | 493 | 0 | 0 | 0 | 17 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8432 | 7412 | 4547 | 315 | 0 | 0 | 0 | 12 |  |
| feature-currency | feature | code | pass | 5 | 5 | 28732 | 25619 | 6628 | 1113 | 0 | 0 | 0 | 39 |  |
| fix-balance-query | small-fix | code | FAIL | 4 | 4 | 19073 | 17409 | 5183 | 626 | 0 | 0 | 0 | 23 | …(cached) --- FAIL: TestHiddenBalanceMulti (0.00s)     hidden_accept_test.go:10: x=30 y=-30 z=0 FAIL FAIL	example.com/ledger-service/internal/ledger	0.002s FAIL  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 26401 | 23892 | 6024 | 819 | 0 | 0 | 0 | 30 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 8338 | 7403 | 4462 | 171 | 0 | 0 | 0 | 8 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | FAIL | 4 | 5 | 29137 | 24071 | 8585 | 1122 | 0 | 0 | 0 | 41 | model request failed (http 502): Post "http://127.0.0.1:8765/v1/chat/completions": EOF |
| large-locate-preorder | large-locate | chat | pass | 3 | 2 | 17598 | 12171 | 8950 | 294 | 0 | 0 | 0 | 18 |  |
| locate-topic-name | locate | chat | pass | 2 | 1 | 8689 | 7498 | 4718 | 163 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | FAIL | 1 | 3 | 4057 | 3531 | 4057 | 103 | 0 | 0 | 0 | 5 | model request failed (http 502): Post "http://127.0.0.1:8765/v1/chat/completions": EOF |
| multi-locate-mapping | locate | chat | pass | 2 | 4 | 9019 | 7494 | 5052 | 356 | 0 | 0 | 0 | 13 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8437 | 7421 | 4543 | 253 | 0 | 0 | 0 | 10 |  |
| py-fix-inventory | small-fix | code | FAIL | 5 | 5 | 22956 | 21371 | 5100 | 593 | 0 | 0 | 0 | 23 | …^^^^^^^^^^^^^^^^^^^^^ AssertionError: 3 != 0  ---------------------------------------------------------------------- Ran 6 tests in 0.000s  FAILED (failures=1)  |
| refactor-validate | refactor | code | pass | 7 | 8 | 40270 | 37174 | 6603 | 1032 | 0 | 0 | 1 | 39 |  |
| rename-entries | refactor | code | pass | 5 | 12 | 29910 | 26240 | 7185 | 665 | 0 | 0 | 0 | 27 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 5 | 17586 | 15741 | 5364 | 482 | 0 | 0 | 0 | 19 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 12 | 10 | 77239 | 72122 | 8242 | 2168 | 0 | 0 | 0 | 80 | switched to code. |
| session-x-tools | session | chat, chat, chat | FAIL | 4 | 2 | 32345 | 25901 | 9963 | 965 | 0 | 0 | 0 | 40 | turn 3 of 6: model request failed (http 502): Post "http://127.0.0.1:8765/v1/chat/completions": EOF |
| ts-fix-cart-discount | small-fix | code | pass | 4 | 4 | 18670 | 17089 | 5100 | 324 | 0 | 0 | 0 | 16 |  |
| ts-locate-discount | locate | chat | pass | 2 | 2 | 8622 | 7419 | 4730 | 357 | 0 | 0 | 0 | 13 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5343 | 0 | 200 | 76 | 78 | 537 | 78 | 0 | 0 | 6 | 0 | 0 | 688 | 31 | 416 | 2199 | 65 | 969 |
| diagnose-config | 4388 | 0 | 199 | 131 | 88 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 355 | 0 | 9 | 2199 | 29 | 841 |
| diagnose-failing-test | 6097 | 0 | 206 | 76 | 58 | 537 | 115 | 0 | 0 | 14 | 0 | 0 | 1788 | 0 | 80 | 2199 | 29 | 995 |
| explain-event-flow | 5764 | 0 | 197 | 131 | 114 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 1672 | 0 | 20 | 2199 | 33 | 861 |
| explain-ledger-posting | 4547 | 0 | 203 | 76 | 39 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 6628 | 47 | 198 | 131 | 103 | 537 | 88 | 45 | 0 | 0 | 0 | 0 | 1565 | 0 | 694 | 2199 | 55 | 966 |
| fix-balance-query | 5183 | 56 | 202 | 76 | 73 | 537 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 123 | 2199 | 32 | 946 |
| fix-ledger-balance | 6024 | 47 | 204 | 76 | 39 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 230 | 2199 | 52 | 981 |
| java-explain-orders | 4462 | 0 | 197 | 65 | 34 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 11 | 2199 | 35 | 841 |
| large-change-txtar | 8585 | 0 | 200 | 286 | 153 | 537 | 0 | 119 | 0 | 0 | 0 | 0 | 3623 | 0 | 442 | 2199 | 79 | 947 |
| large-locate-preorder | 8950 | 0 | 201 | 286 | 197 | 537 | 0 | 0 | 46 | 0 | 0 | 0 | 4526 | 0 | 32 | 2199 | 42 | 884 |
| locate-topic-name | 4718 | 0 | 197 | 131 | 73 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 705 | 0 | 10 | 2199 | 25 | 841 |
| multi-feature-discount | 4057 | 0 | 202 | 113 | 80 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 2199 | 117 | 809 |
| multi-locate-mapping | 5052 | 0 | 203 | 113 | 68 | 537 | 0 | 0 | 0 | 5 | 0 | 0 | 947 | 0 | 36 | 2199 | 38 | 906 |
| py-explain-reorder | 4543 | 0 | 201 | 65 | 43 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 15 | 2199 | 40 | 861 |
| py-fix-inventory | 5100 | 43 | 200 | 65 | 32 | 537 | 20 | 16 | 0 | 10 | 0 | 0 | 785 | 0 | 162 | 2199 | 43 | 988 |
| refactor-validate | 6603 | 48 | 196 | 131 | 48 | 537 | 81 | 96 | 0 | 28 | 0 | 0 | 1588 | 0 | 499 | 2199 | 68 | 1084 |
| rename-entries | 7185 | 28 | 200 | 76 | 47 | 537 | 37 | 24 | 0 | 21 | 47 | 0 | 2608 | 0 | 185 | 2199 | 30 | 1146 |
| rust-explain-ledger | 5364 | 0 | 205 | 89 | 27 | 537 | 0 | 0 | 0 | 0 | 0 | 18 | 1122 | 0 | 171 | 2199 | 41 | 955 |
| session-ledger | 8242 | 605 | 798 | 76 | 183 | 537 | 188 | 24 | 0 | 14 | 0 | 0 | 1791 | 31 | 478 | 2199 | 110 | 1208 |
| session-x-tools | 9963 | 221 | 390 | 286 | 269 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 5080 | 0 | 29 | 2199 | 59 | 893 |
| ts-fix-cart-discount | 5100 | 38 | 200 | 82 | 36 | 537 | 123 | 54 | 0 | 0 | 0 | 0 | 769 | 0 | 63 | 2199 | 44 | 955 |
| ts-locate-discount | 4730 | 0 | 199 | 82 | 36 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 769 | 0 | 17 | 2199 | 30 | 861 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
