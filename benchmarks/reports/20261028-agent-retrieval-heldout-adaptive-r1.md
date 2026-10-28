# Direct-mode suite: direct-20261028T055617Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T05:56:17Z, finished 2026-10-28T07:09:16Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**10/16 passed (62%)**, 11.2 model calls and 13.2 tool calls per task, 142844 prompt tokens per task with a 91% prompt-cache hit rate, 8781 generated tokens per task, 29 KB of tool results per task, 274 s per task, 34568 tokens in the largest single prompt; 11 results set aside, 0 recalled, 0 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 1 errors.

Where the agent's time went, per task: 273.1 s in model calls, 0.2 s in tools (with the checks after edits), 0.09 s in the harness itself; time to first token 2149 ms (median). 7661 reasoning tokens per task. 1 prompt-cache invalidations (1 after the history was rewritten), 25621 prompt tokens recomputed. Cost of a passed task: 13638 processed tokens and 149 s; of a failed one: 36348 and 481 s. Peak memory: harness 40 MiB, model server 29300 MiB, GPU 7161 MiB.

Failures by cause: environment 1, model 5. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| python | 2/2 | 0 | 8.0 | 10.0 | 54392 | 80.2 | 0.0 | 81 |
| javascript | 0/1 | 0 | 4.0 | 2.0 | 16391 | 239.9 | 0.0 | 240 |
| rust | 1/2 | 0 | 36.0 | 42.5 | 705386 | 591.6 | 0.4 | 592 |
| go | 2/2 | 0 | 4.0 | 5.5 | 20304 | 110.9 | 0.1 | 111 |
| ruby | 2/2 | 0 | 3.5 | 2.5 | 16392 | 81.7 | 0.0 | 82 |
| java | 1/2 | 0 | 8.5 | 12.0 | 85168 | 213.0 | 0.2 | 213 |
| typescript | 0/1 | 0 | 12.0 | 13.0 | 89675 | 426.3 | 0.0 | 427 |
| c | 1/2 | 0 | 11.0 | 14.0 | 114758 | 459.9 | 0.4 | 461 |
| php | 1/2 | 0 | 11.0 | 12.0 | 93322 | 314.2 | 0.1 | 315 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | locate-heldout | code | pass | 9 | 8 | 46980 | 41659 | 7446 | 1677 | 0 | 0 | 0 | 62 |  |
| locate-axios__axios-5085 | locate-heldout | code | FAIL | 4 | 2 | 16391 | 11584 | 6956 | 8510 | 0 | 0 | 0 | 240 | …ption.parse_error.101] parse error at line 1, column 28: syntax error while parsing value - invalid string: missing closing quote; last read: '"\\.normalize\\(' |
| locate-burntsushi__ripgrep-2209 | locate-heldout | code | FAIL | 41 | 57 | 766729 | 702948 | 34568 | 23726 | 11 | 0 | 0 | 826 | answer does not match /(?:crates/printer/src/util\.rs)/ |
| locate-caddyserver__caddy-5404 | locate-heldout | code | pass | 2 | 1 | 13224 | 6411 | 8966 | 665 | 0 | 0 | 0 | 29 |  |
| locate-caddyserver__caddy-6370 | locate-heldout | code | pass | 6 | 10 | 27383 | 23360 | 6160 | 6836 | 0 | 0 | 0 | 194 |  |
| locate-faker-ruby__faker-2705 | locate-heldout | code | pass | 3 | 2 | 11133 | 8793 | 4489 | 518 | 0 | 0 | 0 | 20 |  |
| locate-fastlane__fastlane-20958 | locate-heldout | code | pass | 4 | 3 | 21651 | 16974 | 6822 | 5057 | 0 | 0 | 0 | 145 |  |
| locate-google__gson-2479 | locate-heldout | code | pass | 5 | 4 | 23297 | 18569 | 6869 | 816 | 0 | 0 | 0 | 32 |  |
| locate-immutable-js__immutable-js-2006 | locate-heldout | code | FAIL | 12 | 13 | 89675 | 78923 | 12865 | 14590 | 0 | 0 | 0 | 427 | answer does not match /(?:src/Operations\.js)/ |
| locate-jqlang__jq-2839 | locate-heldout | code | pass | 9 | 13 | 102456 | 89196 | 15385 | 16919 | 0 | 0 | 0 | 492 |  |
| locate-laravel__framework-48636 | locate-heldout | code | pass | 7 | 9 | 50882 | 43856 | 9159 | 1558 | 0 | 0 | 0 | 58 |  |
| locate-laravel__framework-52451 | locate-heldout | code | FAIL | 15 | 15 | 135762 | 124198 | 13665 | 19772 | 0 | 0 | 0 | 572 | answer does not match /(?:src/Illuminate/Validation/Concerns/FormatsMessages\.php)/ |
| locate-nushell__nushell-12950 | locate-heldout | code | pass | 31 | 28 | 644042 | 614575 | 31504 | 9914 | 0 | 0 | 0 | 358 |  |
| locate-projectlombok__lombok-3371 | locate-heldout | code | FAIL | 12 | 20 | 147038 | 131028 | 18123 | 13023 | 0 | 0 | 0 | 395 | answer does not match /(?:src/core/lombok/eclipse/handlers/HandleNonNull\.java\|src/core/lombok/javac/handlers/HandleNonNull\.java)/ |
| locate-redis__redis-12472 | locate-heldout | code | FAIL | 13 | 15 | 127060 | 109498 | 19671 | 13989 | 0 | 0 | 0 | 429 | answer does not match /(?:src/acl\.c)/ |
| locate-sphinx-doc__sphinx-10325 | locate-heldout | code | pass | 7 | 12 | 61805 | 49967 | 13971 | 2932 | 0 | 0 | 0 | 100 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:bash | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:web_fetch | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | 7446 | 9 | 220 | 394 | 285 | 0 | 537 | 0 | 0 | 0 | 287 | 0 | 3130 | 0 | 129 | 1132 | 518 | 805 |
| locate-axios__axios-5085 | 6956 | 33 | 212 | 284 | 53 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 3664 | 0 | 23 | 1132 | 452 | 566 |
| locate-burntsushi__ripgrep-2209 | 34568 | 605 | 224 | 238 | 194 | 553 | 537 | 0 | 0 | 0 | 3940 | 255 | 21861 | 0 | 920 | 1132 | 1419 | 2690 |
| locate-caddyserver__caddy-5404 | 8966 | 0 | 216 | 283 | 70 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 4662 | 0 | 14 | 1132 | 1518 | 534 |
| locate-caddyserver__caddy-6370 | 6160 | 23 | 216 | 284 | 32 | 0 | 537 | 0 | 0 | 0 | 268 | 0 | 2326 | 0 | 183 | 1132 | 280 | 879 |
| locate-faker-ruby__faker-2705 | 4489 | 0 | 216 | 187 | 176 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 1307 | 0 | 35 | 1132 | 315 | 584 |
| locate-fastlane__fastlane-20958 | 6822 | 325 | 216 | 740 | 80 | 0 | 537 | 0 | 0 | 0 | 43 | 0 | 2327 | 0 | 62 | 1132 | 725 | 635 |
| locate-google__gson-2479 | 6869 | 0 | 212 | 259 | 107 | 0 | 537 | 0 | 0 | 0 | 0 | 0 | 3524 | 0 | 84 | 1132 | 362 | 652 |
| locate-immutable-js__immutable-js-2006 | 12865 | 608 | 218 | 258 | 128 | 0 | 537 | 0 | 0 | 0 | 836 | 0 | 7589 | 0 | 231 | 1132 | 240 | 1088 |
| locate-jqlang__jq-2839 | 15385 | 281 | 214 | 280 | 169 | 0 | 537 | 0 | 0 | 0 | 1141 | 0 | 8094 | 0 | 293 | 1132 | 2166 | 1078 |
| locate-laravel__framework-48636 | 9159 | 0 | 212 | 441 | 81 | 0 | 537 | 0 | 24 | 0 | 150 | 0 | 5215 | 0 | 171 | 1132 | 377 | 819 |
| locate-laravel__framework-52451 | 13665 | 121 | 212 | 474 | 119 | 0 | 537 | 0 | 0 | 406 | 79 | 0 | 8216 | 0 | 334 | 1132 | 853 | 1182 |
| locate-nushell__nushell-12950 | 31504 | 822 | 222 | 279 | 162 | 0 | 537 | 9097 | 0 | 0 | 7739 | 0 | 7867 | 70 | 1436 | 1132 | 526 | 1615 |
| locate-projectlombok__lombok-3371 | 18123 | 439 | 216 | 267 | 314 | 0 | 537 | 0 | 0 | 0 | 1628 | 0 | 11636 | 0 | 409 | 1132 | 359 | 1186 |
| locate-redis__redis-12472 | 19671 | 667 | 214 | 298 | 150 | 0 | 537 | 0 | 19 | 0 | 685 | 0 | 14073 | 0 | 304 | 1132 | 404 | 1188 |
| locate-sphinx-doc__sphinx-10325 | 13971 | 44 | 218 | 405 | 200 | 0 | 537 | 0 | 0 | 0 | 623 | 0 | 9307 | 0 | 265 | 1132 | 260 | 980 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 24af01913d7f4237.
