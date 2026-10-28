# Direct-mode suite: direct-20261028T044229Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T04:42:29Z, finished 2026-10-28T05:56:17Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=basic, seed_pack=true, working_set_tokens=0.

**9/16 passed (56%)**, 9.9 model calls and 10.8 tool calls per task, 123488 prompt tokens per task with a 89% prompt-cache hit rate, 8845 generated tokens per task, 27 KB of tool results per task, 277 s per task, 34577 tokens in the largest single prompt; 4 results set aside, 0 recalled, 0 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 274.0 s in model calls, 0.1 s in tools (with the checks after edits), 0.45 s in the harness itself; time to first token 2185 ms (median). 7688 reasoning tokens per task. 1 prompt-cache invalidations (1 after the history was rewritten), 23437 prompt tokens recomputed. Cost of a passed task: 11149 processed tokens and 143 s; of a failed one: 36173 and 449 s. Peak memory: harness 68 MiB, model server 28951 MiB, GPU 7161 MiB.

Failures by cause: model 7. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| python | 2/2 | 0 | 8.5 | 7.5 | 44895 | 63.6 | 0.1 | 67 |
| javascript | 0/1 | 0 | 3.0 | 2.0 | 16685 | 65.0 | 0.0 | 66 |
| rust | 0/2 | 0 | 24.5 | 31.0 | 489470 | 717.4 | 0.5 | 720 |
| go | 2/2 | 0 | 3.0 | 2.0 | 14892 | 77.6 | 0.0 | 79 |
| ruby | 1/2 | 0 | 4.5 | 4.5 | 21910 | 133.6 | 0.0 | 139 |
| java | 1/2 | 0 | 6.0 | 6.0 | 45012 | 317.4 | 0.1 | 320 |
| typescript | 0/1 | 0 | 21.0 | 23.0 | 275416 | 270.6 | 0.0 | 272 |
| c | 1/2 | 0 | 11.5 | 13.0 | 146547 | 501.1 | 0.0 | 503 |
| php | 2/2 | 0 | 9.5 | 9.5 | 79130 | 213.2 | 0.2 | 217 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | locate-heldout | code | pass | 6 | 5 | 25222 | 21445 | 5914 | 718 | 0 | 0 | 0 | 34 |  |
| locate-axios__axios-5085 | locate-heldout | code | FAIL | 3 | 2 | 16685 | 11791 | 7043 | 2134 | 0 | 0 | 0 | 66 | answer does not match /(?:index\.d\.ts\|lib/core/AxiosHeaders\.js)/ |
| locate-burntsushi__ripgrep-2209 | locate-heldout | code | FAIL | 16 | 20 | 291486 | 266396 | 27187 | 26084 | 0 | 0 | 0 | 803 | answer does not match /(?:crates/printer/src/util\.rs)/ |
| locate-caddyserver__caddy-5404 | locate-heldout | code | pass | 2 | 1 | 13346 | 6518 | 9027 | 1053 | 0 | 0 | 0 | 39 |  |
| locate-caddyserver__caddy-6370 | locate-heldout | code | pass | 4 | 3 | 16437 | 13035 | 5547 | 4089 | 0 | 0 | 0 | 118 |  |
| locate-faker-ruby__faker-2705 | locate-heldout | code | pass | 5 | 5 | 19630 | 16698 | 5073 | 601 | 0 | 0 | 0 | 27 |  |
| locate-fastlane__fastlane-20958 | locate-heldout | code | FAIL | 4 | 4 | 24190 | 18153 | 8182 | 8553 | 0 | 0 | 0 | 250 | answer does not match /(?:fastlane/lib/fastlane/fast_file\.rb)/ |
| locate-google__gson-2479 | locate-heldout | code | pass | 4 | 3 | 15624 | 12179 | 5590 | 721 | 0 | 0 | 0 | 28 |  |
| locate-immutable-js__immutable-js-2006 | locate-heldout | code | FAIL | 21 | 23 | 275416 | 253321 | 24172 | 8081 | 0 | 0 | 0 | 272 | answer does not match /(?:src/Operations\.js)/ |
| locate-jqlang__jq-2839 | locate-heldout | code | pass | 13 | 14 | 153218 | 136948 | 18379 | 17023 | 0 | 0 | 0 | 505 |  |
| locate-laravel__framework-48636 | locate-heldout | code | pass | 3 | 3 | 12865 | 9990 | 5024 | 571 | 0 | 0 | 0 | 25 |  |
| locate-laravel__framework-52451 | locate-heldout | code | pass | 16 | 16 | 145394 | 132915 | 14576 | 13706 | 0 | 0 | 0 | 409 |  |
| locate-nushell__nushell-12950 | locate-heldout | code | FAIL | 33 | 42 | 687453 | 624866 | 34577 | 17767 | 4 | 0 | 0 | 637 | answer does not match /(?:crates/nu\-command/src/system/run_external\.rs)/ |
| locate-projectlombok__lombok-3371 | locate-heldout | code | FAIL | 8 | 9 | 74401 | 62977 | 13553 | 21332 | 0 | 0 | 0 | 611 | answer does not match /(?:src/core/lombok/eclipse/handlers/HandleNonNull\.java\|src/core/lombok/javac/handlers/HandleNonNull\.java)/ |
| locate-redis__redis-12472 | locate-heldout | code | FAIL | 10 | 12 | 139876 | 118907 | 23090 | 16167 | 0 | 0 | 0 | 501 | answer does not match /(?:src/acl\.c)/ |
| locate-sphinx-doc__sphinx-10325 | locate-heldout | code | pass | 11 | 10 | 64568 | 57640 | 9045 | 2925 | 0 | 0 | 0 | 100 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:find_symbol | tool:glob | tool:grep | tool:read | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | 5914 | 0 | 220 | 394 | 0 | 0 | 537 | 0 | 0 | 70 | 2224 | 124 | 1132 | 518 | 695 |
| locate-axios__axios-5085 | 7043 | 0 | 212 | 284 | 173 | 0 | 537 | 0 | 0 | 0 | 3664 | 23 | 1132 | 452 | 566 |
| locate-burntsushi__ripgrep-2209 | 27187 | 77 | 224 | 238 | 289 | 0 | 537 | 0 | 0 | 2018 | 19173 | 744 | 1132 | 1419 | 1336 |
| locate-caddyserver__caddy-5404 | 9027 | 0 | 216 | 283 | 131 | 0 | 537 | 0 | 0 | 0 | 4662 | 14 | 1132 | 1518 | 534 |
| locate-caddyserver__caddy-6370 | 5547 | 416 | 216 | 284 | 79 | 0 | 537 | 0 | 0 | 0 | 1925 | 46 | 1132 | 280 | 632 |
| locate-faker-ruby__faker-2705 | 5073 | 0 | 216 | 187 | 52 | 0 | 537 | 0 | 0 | 274 | 1571 | 81 | 1132 | 315 | 708 |
| locate-fastlane__fastlane-20958 | 8182 | 0 | 216 | 740 | 152 | 0 | 537 | 0 | 0 | 0 | 3976 | 66 | 1132 | 725 | 638 |
| locate-google__gson-2479 | 5590 | 14 | 212 | 259 | 74 | 0 | 537 | 24 | 0 | 0 | 2300 | 57 | 1132 | 362 | 619 |
| locate-immutable-js__immutable-js-2006 | 24172 | 2752 | 218 | 258 | 0 | 0 | 537 | 0 | 506 | 627 | 16138 | 375 | 1132 | 240 | 1389 |
| locate-jqlang__jq-2839 | 18379 | 751 | 214 | 280 | 91 | 0 | 537 | 0 | 0 | 4155 | 7578 | 296 | 1132 | 2166 | 1179 |
| locate-laravel__framework-48636 | 5024 | 17 | 212 | 441 | 394 | 0 | 537 | 24 | 0 | 0 | 1212 | 65 | 1132 | 377 | 613 |
| locate-laravel__framework-52451 | 14576 | 480 | 212 | 474 | 154 | 0 | 537 | 0 | 180 | 272 | 8604 | 423 | 1132 | 853 | 1255 |
| locate-nushell__nushell-12950 | 34577 | 981 | 222 | 279 | 0 | 197 | 537 | 0 | 90 | 3051 | 24414 | 881 | 1132 | 526 | 2267 |
| locate-projectlombok__lombok-3371 | 13553 | 379 | 216 | 267 | 0 | 0 | 537 | 95 | 0 | 1131 | 8325 | 210 | 1132 | 359 | 902 |
| locate-redis__redis-12472 | 23090 | 419 | 214 | 298 | 98 | 0 | 537 | 0 | 4 | 4326 | 14410 | 224 | 1132 | 404 | 1024 |
| locate-sphinx-doc__sphinx-10325 | 9045 | 102 | 218 | 405 | 0 | 0 | 537 | 25 | 0 | 919 | 4190 | 289 | 1132 | 260 | 968 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 24af01913d7f4237.
