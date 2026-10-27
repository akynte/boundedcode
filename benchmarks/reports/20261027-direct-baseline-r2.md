# Direct-mode suite: direct-20261027T121601Z

Model: qwen3.6-35b-a3b. Started 2026-10-27T12:16:01Z, finished 2026-10-27T12:29:24Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, seed_pack=true, working_set_tokens=0.

**23/23 passed (100%)**, 5.1 model calls and 6.0 tool calls per task, 33210 prompt tokens per task with a 90% prompt-cache hit rate, 923 generated tokens per task, 6 KB of tool results per task, 35 s per task, 25037 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 4 broken edits caught by the check, 1 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 34.6 s in model calls, 0.1 s in tools (with the checks after edits), 0.02 s in the harness itself; time to first token 1769 ms (median). 245 reasoning tokens per task. 2 prompt-cache invalidations (0 after the history was rewritten), 614 prompt tokens recomputed. Cost of a passed task: 4328 processed tokens and 35 s; of a failed one: 0 and 0 s. Peak memory: harness 32 MiB, model server 29372 MiB, GPU 7141 MiB.

Failures by cause: environment (skipped) 2. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| go | 15/15 | 0 | 5.9 | 6.3 | 42162 | 41.0 | 0.2 | 41 |
| java | 1/1 | 1 | 2.0 | 1.0 | 7466 | 6.9 | 0.0 | 7 |
| multi | 2/2 | 0 | 4.5 | 7.5 | 25835 | 45.5 | 0.3 | 46 |
| python | 2/2 | 0 | 3.0 | 3.5 | 11946 | 14.3 | 0.0 | 14 |
| rust | 1/1 | 1 | 2.0 | 2.0 | 7788 | 8.0 | 0.0 | 8 |
| typescript | 2/2 | 0 | 5.0 | 8.5 | 20300 | 23.0 | 0.1 | 23 |

| Category | Passed | Calls | Tools | Prompt tok | Cache | Gen tok | s/task |
|---|---|---|---|---|---|---|---|
| add-test | 1/1 | 6.0 | 7.0 | 26732 | 92% | 1314 | 45 |
| diagnose | 2/2 | 3.0 | 4.5 | 12146 | 83% | 512 | 19 |
| explain | 5/5 | 2.2 | 2.2 | 8731 | 84% | 307 | 12 |
| feature | 2/2 | 8.0 | 13.0 | 51576 | 90% | 2202 | 73 |
| small-fix | 4/4 | 4.5 | 5.8 | 19956 | 90% | 594 | 23 |
| large-change | 1/1 | 7.0 | 7.0 | 47030 | 89% | 1249 | 47 |
| large-locate | 1/1 | 4.0 | 3.0 | 19669 | 73% | 433 | 23 |
| locate | 3/3 | 3.0 | 4.0 | 11454 | 89% | 312 | 13 |
| refactor | 2/2 | 6.5 | 7.5 | 32335 | 91% | 866 | 33 |
| session | 2/2 | 14.0 | 12.0 | 160226 | 92% | 3118 | 122 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | add-test | code | pass | 6 | 7 | 26732 | 24511 | 5290 | 1314 | 0 | 0 | 1 | 45 |  |
| diagnose-config | diagnose | chat | pass | 3 | 4 | 11765 | 10299 | 4547 | 393 | 0 | 0 | 0 | 15 |  |
| diagnose-failing-test | diagnose | chat | pass | 3 | 5 | 12527 | 9763 | 5534 | 630 | 0 | 0 | 0 | 23 | switched to code. |
| explain-event-flow | explain | chat | pass | 3 | 4 | 13019 | 10445 | 5655 | 689 | 0 | 0 | 0 | 24 |  |
| explain-ledger-posting | explain | chat | pass | 2 | 2 | 7934 | 6559 | 4460 | 339 | 0 | 0 | 0 | 13 |  |
| feature-currency | feature | code | pass | 9 | 12 | 59424 | 53975 | 8506 | 1763 | 0 | 0 | 2 | 63 |  |
| fix-balance-query | small-fix | code | pass | 4 | 4 | 17153 | 15534 | 4696 | 683 | 0 | 0 | 0 | 25 |  |
| fix-ledger-balance | small-fix | code | pass | 5 | 6 | 24578 | 21973 | 5678 | 608 | 0 | 0 | 0 | 25 |  |
| java-explain-orders | explain | chat | pass | 2 | 1 | 7466 | 6525 | 4026 | 133 | 0 | 0 | 0 | 7 |  |
| java-fix-paid-total | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | javac, java, make not on PATH |
| large-change-txtar | large-change | code | pass | 7 | 7 | 47030 | 41774 | 8321 | 1249 | 0 | 0 | 0 | 47 |  |
| large-locate-preorder | large-locate | chat | pass | 4 | 3 | 19669 | 14298 | 8448 | 433 | 0 | 0 | 0 | 23 |  |
| locate-topic-name | locate | chat | pass | 2 | 2 | 7470 | 6530 | 4025 | 181 | 0 | 0 | 0 | 8 |  |
| multi-feature-discount | feature | code | pass | 7 | 14 | 43727 | 39267 | 7525 | 2641 | 0 | 0 | 0 | 83 |  |
| multi-locate-mapping | locate | chat | pass | 2 | 1 | 7943 | 6520 | 4508 | 177 | 0 | 0 | 0 | 9 |  |
| py-explain-reorder | explain | chat | pass | 2 | 2 | 7449 | 6485 | 4049 | 207 | 0 | 0 | 0 | 9 |  |
| py-fix-inventory | small-fix | code | pass | 4 | 5 | 16444 | 14872 | 4649 | 502 | 0 | 0 | 0 | 20 |  |
| refactor-validate | refactor | code | pass | 7 | 7 | 35887 | 32887 | 6065 | 957 | 0 | 0 | 1 | 37 |  |
| rename-entries | refactor | code | pass | 6 | 8 | 28783 | 26215 | 5637 | 776 | 0 | 0 | 0 | 30 |  |
| rust-explain-ledger | explain | chat | pass | 2 | 2 | 7788 | 6508 | 4365 | 168 | 0 | 0 | 0 | 8 |  |
| rust-fix-transfer | small-fix |  | skipped | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | cargo not on PATH |
| session-ledger | session | chat, code, code, chat | pass | 10 | 9 | 61444 | 56460 | 7742 | 2235 | 0 | 0 | 0 | 79 | switched to code. |
| session-x-tools | session | chat, chat, chat, chat, code, chat | pass | 18 | 15 | 259008 | 236992 | 25037 | 4000 | 0 | 0 | 0 | 165 |  |
| ts-fix-cart-discount | small-fix | code | pass | 5 | 8 | 21650 | 19716 | 5007 | 584 | 0 | 0 | 0 | 24 |  |
| ts-locate-discount | locate | chat | pass | 5 | 9 | 18950 | 17429 | 4594 | 577 | 0 | 0 | 0 | 22 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:edit | tool:glob | tool:grep | tool:ls | tool:multi_edit | tool:read | tool:read_many | tool:write | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| add-balance-test | 5290 | 0 | 200 | 68 | 0 | 356 | 48 | 0 | 21 | 0 | 0 | 0 | 712 | 0 | 92 | 763 | 1992 | 65 | 973 |
| diagnose-config | 4547 | 0 | 199 | 121 | 0 | 356 | 0 | 0 | 0 | 195 | 0 | 0 | 710 | 0 | 0 | 63 | 1992 | 29 | 882 |
| diagnose-failing-test | 5534 | 19 | 206 | 68 | 0 | 356 | 115 | 0 | 0 | 0 | 0 | 0 | 1788 | 0 | 0 | 70 | 1992 | 29 | 891 |
| explain-event-flow | 5655 | 0 | 197 | 121 | 0 | 356 | 0 | 0 | 28 | 325 | 0 | 0 | 1672 | 0 | 0 | 55 | 1992 | 33 | 876 |
| explain-ledger-posting | 4460 | 0 | 203 | 68 | 78 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 911 | 0 | 0 | 23 | 1992 | 22 | 807 |
| feature-currency | 8506 | 100 | 198 | 121 | 0 | 356 | 81 | 164 | 33 | 0 | 0 | 0 | 3220 | 0 | 0 | 1066 | 1992 | 55 | 1120 |
| fix-balance-query | 4696 | 53 | 202 | 68 | 48 | 356 | 38 | 24 | 0 | 0 | 0 | 0 | 877 | 0 | 0 | 113 | 1992 | 32 | 893 |
| fix-ledger-balance | 5678 | 61 | 204 | 68 | 78 | 356 | 37 | 24 | 0 | 0 | 0 | 0 | 1622 | 0 | 0 | 237 | 1992 | 52 | 947 |
| java-explain-orders | 4026 | 0 | 197 | 51 | 54 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 543 | 0 | 0 | 11 | 1992 | 35 | 787 |
| large-change-txtar | 8321 | 43 | 200 | 261 | 0 | 356 | 18 | 141 | 0 | 0 | 13 | 0 | 3623 | 0 | 0 | 589 | 1992 | 79 | 1006 |
| large-locate-preorder | 8448 | 0 | 201 | 261 | 0 | 356 | 0 | 0 | 84 | 70 | 0 | 0 | 4526 | 0 | 0 | 51 | 1992 | 42 | 865 |
| locate-topic-name | 4025 | 0 | 197 | 121 | 0 | 356 | 0 | 0 | 0 | 460 | 0 | 0 | 0 | 0 | 0 | 36 | 1992 | 25 | 838 |
| multi-feature-discount | 7525 | 76 | 202 | 92 | 130 | 356 | 632 | 24 | 0 | 0 | 0 | 33 | 1316 | 0 | 26 | 1306 | 1992 | 117 | 1223 |
| multi-locate-mapping | 4508 | 0 | 203 | 92 | 0 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1022 | 0 | 21 | 1992 | 38 | 784 |
| py-explain-reorder | 4049 | 0 | 201 | 57 | 0 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 582 | 0 | 0 | 15 | 1992 | 40 | 806 |
| py-fix-inventory | 4649 | 0 | 200 | 57 | 46 | 356 | 72 | 17 | 10 | 0 | 0 | 0 | 785 | 0 | 0 | 143 | 1992 | 43 | 928 |
| refactor-validate | 6065 | 39 | 196 | 121 | 73 | 356 | 63 | 74 | 6 | 0 | 0 | 0 | 1575 | 0 | 0 | 511 | 1992 | 68 | 991 |
| rename-entries | 5637 | 61 | 200 | 68 | 0 | 356 | 51 | 24 | 21 | 3 | 0 | 0 | 1599 | 0 | 0 | 198 | 1992 | 30 | 1034 |
| rust-explain-ledger | 4365 | 0 | 205 | 75 | 0 | 356 | 0 | 0 | 0 | 0 | 0 | 0 | 869 | 0 | 0 | 18 | 1992 | 41 | 809 |
| session-ledger | 7742 | 769 | 798 | 68 | 0 | 356 | 188 | 24 | 0 | 0 | 0 | 0 | 1791 | 0 | 31 | 486 | 1992 | 110 | 1129 |
| session-x-tools | 25037 | 1120 | 1175 | 261 | 0 | 356 | 21 | 63 | 263 | 31 | 0 | 0 | 17359 | 0 | 0 | 856 | 1992 | 209 | 1331 |
| ts-fix-cart-discount | 5007 | 43 | 200 | 68 | 0 | 356 | 122 | 9 | 12 | 0 | 0 | 0 | 1070 | 0 | 0 | 94 | 1992 | 44 | 997 |
| ts-locate-discount | 4594 | 0 | 199 | 68 | 0 | 356 | 0 | 0 | 20 | 0 | 15 | 0 | 830 | 0 | 0 | 73 | 1992 | 30 | 1011 |

Environment: build dev 8e4caea75a51; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server 0.00.000.169 I srv  llama_server: initializing ... | version: 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 547dd200a937ade8.
