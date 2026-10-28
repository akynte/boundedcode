# Direct-mode suite: direct-20261028T093303Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T09:33:03Z, finished 2026-10-28T09:51:32Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=basic, seed_pack=true, working_set_tokens=0.

**23/23 passed (100%)**, 6.3 model calls and 7.4 tool calls per task, 54918 prompt tokens per task with a 93% prompt-cache hit rate, 1324 generated tokens per task, 7 KB of tool results per task, 48 s per task, 28874 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 6 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 47.8 s in model calls, 0.2 s in tools (with the checks after edits), 0.03 s in the harness itself; time to first token 1744 ms (median). 479 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 630 prompt tokens recomputed. Cost of a passed task: 5212 processed tokens and 48 s; of a failed one: 0 and 0 s. Peak memory: harness 37 MiB, model server 29422 MiB, GPU 7161 MiB.

Failures by cause: environment (skipped) 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/15 | 0 | 6.8 | 7.0 | 68012 | 53.2 | 0.3 | 54 |
| java | 1/1 | 1 | 2.0 | 2.0 | 8736 | 10.6 | 0.0 | 11 |
| multi | 2/2 | 0 | 10.5 | 20.0 | 71624 | 102.3 | 0.3 | 103 |
| python | 2/2 | 0 | 4.0 | 4.0 | 18340 | 18.9 | 0.0 | 19 |
| rust | 1/1 | 1 | 4.0 | 4.0 | 18666 | 17.0 | 0.0 | 17 |
| typescript | 2/2 | 0 | 4.0 | 5.5 | 17797 | 15.4 | 0.1 | 16 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 5.0 | 5.0 | 22976 | 92% | 902 | 31 |
| diagnose | 2/2 | 3.0 | 3.5 | 13147 | 86% | 769 | 25 |
| explain | 5/5 | 2.6 | 2.6 | 11595 | 88% | 354 | 14 |
| feature | 2/2 | 12.0 | 17.5 | 84793 | 93% | 3908 | 125 |
| small-fix | 4/4 | 5.0 | 5.5 | 24280 | 92% | 582 | 24 |
| large-change | 1/1 | 12.0 | 13.0 | 114385 | 89% | 2026 | 84 |
| large-locate | 1/1 | 3.0 | 3.0 | 17248 | 69% | 366 | 20 |
| locate | 3/3 | 3.3 | 7.0 | 14487 | 89% | 365 | 15 |
| refactor | 2/2 | 6.5 | 8.0 | 35783 | 92% | 777 | 31 |
| session | 2/2 | 19.5 | 17.5 | 321250 | 95% | 5534 | 201 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 5 | 5 | 22976 | 21245 | 5246 | 902 | 0 | 0 | 0 | 31 |  |
| diagnose-config | diagnose | chat | pass | 3 | 2 | 12389 | 11471 | 4441 | 342 | 0 | 0 | 0 | 13 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 13905 | 11111 | 5998 | 1196 | 0 | 0 | 0 | 37 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 13715 | 11461 | 5777 | 501 | 0 | 0 | 0 | 18 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 1 | 8510 | 7451 | 4586 | 297 | 0 | 0 | 0 | 11 |  |
| feature-currency | feature | code | pass | 8 | 10 | 49050 | 44904 | 7649 | 2016 | 0 | 0 | 1 | 67 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 18985 | 17340 | 5164 | 590 | 0 | 0 | 0 | 22 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 5 | 26632 | 24066 | 6081 | 654 | 0 | 0 | 0 | 26 |  |
| java-explain-orders | explain | chat | pass | 2 | 2 | 8736 | 7423 | 4840 | 269 | 0 | 0 | 0 | 11 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 12 | 13 | 114385 | 102042 | 15830 | 2026 | 0 | 0 | 1 | 84 |  |
| large-locate-preorder | large-locate | chat | pass | 3 | 3 | 17248 | 11897 | 8874 | 366 | 0 | 0 | 0 | 20 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 8325 | 7424 | 4428 | 259 | 0 | 0 | 0 | 10 |  |
| multi-feature-discount | feature | code | pass | 16 | 25 | 120536 | 113400 | 10607 | 5801 | 0 | 0 | 0 | 183 |  |
| multi-locate-mapping | locate | chat | pass | 5 | 15 | 22713 | 20131 | 6097 | 550 | 0 | 0 | 0 | 23 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 8349 | 7377 | 4499 | 283 | 0 | 0 | 0 | 11 |  |
| py-fix-inventory | small-fix | code | pass | 6 | 6 | 28331 | 26638 | 5204 | 692 | 0 | 0 | 0 | 27 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 40034 | 36977 | 6564 | 879 | 0 | 0 | 1 | 35 |  |
| rename-entries | refactor | code | pass | 6 | 9 | 31532 | 28897 | 6146 | 675 | 0 | 0 | 0 | 27 |  |
| rust-explain-ledger | explain | chat | pass | 4 | 4 | 18666 | 17085 | 5100 | 420 | 0 | 0 | 0 | 17 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 62039 | 57598 | 7633 | 2237 | 0 | 0 | 0 | 78 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 29 | 26 | 580462 | 555007 | 28874 | 8830 | 0 | 0 | 3 | 323 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 7 | 23170 | 21426 | 5259 | 390 | 0 | 0 | 0 | 19 |  |
| ts-locate-discount | locate | chat | pass | 3 | 4 | 12424 | 11323 | 4624 | 287 | 0 | 0 | 0 | 12 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:todo_write | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5246 | 0 | 200 | 76 | 0 | 537 | 40 | 0 | 0 | 6 | 0 | 0 | 712 | 0 | 31 | 418 | 2199 | 65 | 962 |
| diagnose-config | 4441 | 0 | 199 | 131 | 0 | 537 | 0 | 0 | 0 | 0 | 95 | 0 | 355 | 0 | 0 | 19 | 2199 | 29 | 877 |
| diagnose-failing-test | 5998 | 19 | 206 | 76 | 0 | 537 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 76 | 2199 | 29 | 953 |
| explain-event-flow | 5777 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 33 | 0 | 0 | 1695 | 0 | 0 | 35 | 2199 | 33 | 917 |
| explain-ledger-posting | 4586 | 0 | 203 | 76 | 78 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 619 | 0 | 0 | 11 | 2199 | 22 | 841 |
| feature-currency | 7649 | 79 | 198 | 131 | 0 | 537 | 291 | 98 | 0 | 33 | 0 | 0 | 1943 | 0 | 0 | 955 | 2199 | 55 | 1130 |
| fix-balance-query | 5164 | 67 | 202 | 76 | 48 | 537 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 117 | 2199 | 32 | 947 |
| fix-ledger-balance | 6081 | 65 | 204 | 76 | 78 | 537 | 37 | 24 | 0 | 0 | 0 | 0 | 1598 | 0 | 0 | 230 | 2199 | 52 | 981 |
| java-explain-orders | 4840 | 0 | 197 | 65 | 54 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 869 | 0 | 0 | 23 | 2199 | 35 | 861 |
| large-change-txtar | 15830 | 6 | 200 | 286 | 0 | 537 | 33 | 248 | 0 | 0 | 0 | 0 | 9907 | 0 | 0 | 1054 | 2199 | 79 | 1281 |
| large-locate-preorder | 8874 | 0 | 201 | 286 | 0 | 537 | 0 | 0 | 46 | 0 | 70 | 0 | 4526 | 0 | 0 | 52 | 2199 | 42 | 915 |
| locate-topic-name | 4428 | 0 | 197 | 131 | 0 | 537 | 0 | 0 | 0 | 28 | 303 | 0 | 0 | 0 | 0 | 116 | 2199 | 25 | 892 |
| multi-feature-discount | 10607 | 193 | 202 | 113 | 130 | 537 | 177 | 175 | 0 | 0 | 0 | 0 | 2183 | 291 | 52 | 2556 | 2199 | 117 | 1682 |
| multi-locate-mapping | 6097 | 0 | 203 | 113 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 31 | 1676 | 0 | 0 | 118 | 2199 | 38 | 1182 |
| py-explain-reorder | 4499 | 0 | 201 | 65 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 2199 | 40 | 860 |
| py-fix-inventory | 5204 | 0 | 200 | 65 | 46 | 537 | 112 | 16 | 0 | 10 | 0 | 0 | 785 | 0 | 0 | 157 | 2199 | 43 | 1034 |
| refactor-validate | 6564 | 52 | 196 | 131 | 73 | 537 | 81 | 96 | 0 | 6 | 0 | 0 | 1565 | 0 | 0 | 496 | 2199 | 68 | 1064 |
| rename-entries | 6146 | 97 | 200 | 76 | 0 | 537 | 37 | 24 | 0 | 32 | 3 | 0 | 1623 | 0 | 0 | 203 | 2199 | 30 | 1085 |
| rust-explain-ledger | 5100 | 0 | 205 | 89 | 0 | 537 | 0 | 0 | 0 | 4 | 0 | 0 | 1059 | 0 | 0 | 34 | 2199 | 41 | 932 |
| session-ledger | 7633 | 231 | 798 | 76 | 0 | 537 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 0 | 31 | 461 | 2199 | 110 | 1187 |
| session-x-tools | 28874 | 1618 | 1175 | 286 | 0 | 537 | 89 | 417 | 0 | 263 | 31 | 0 | 17915 | 0 | 0 | 2317 | 2199 | 209 | 1818 |
| ts-fix-cart-discount | 5259 | 38 | 200 | 82 | 0 | 537 | 123 | 54 | 0 | 12 | 0 | 0 | 847 | 0 | 0 | 88 | 2199 | 44 | 1035 |
| ts-locate-discount | 4624 | 0 | 199 | 82 | 0 | 537 | 0 | 0 | 0 | 7 | 0 | 0 | 620 | 0 | 0 | 30 | 2199 | 30 | 920 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
