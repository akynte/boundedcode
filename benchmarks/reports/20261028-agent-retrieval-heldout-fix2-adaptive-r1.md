# Direct-mode suite: direct-20261028T124604Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T12:46:04Z, finished 2026-10-28T14:04:17Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**9/16 passed (56%)**, 7.8 model calls and 9.6 tool calls per task, 109206 prompt tokens per task with a 87% prompt-cache hit rate, 9491 generated tokens per task, 29 KB of tool results per task, 293 s per task, 35795 tokens in the largest single prompt; 3 results set aside, 0 recalled, 0 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 1 errors.

Where the agent's time went, per task: 292.8 s in model calls, 0.1 s in tools (with the checks after edits), 0.12 s in the harness itself; time to first token 2374 ms (median). 8446 reasoning tokens per task. 1 prompt-cache invalidations (1 after the history was rewritten), 20807 prompt tokens recomputed. Cost of a passed task: 14927 processed tokens and 183 s; of a failed one: 33959 and 435 s. Peak memory: harness 40 MiB, model server 28625 MiB, GPU 7161 MiB.

Failures by cause: environment 1, model 6. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| python | 2/2 | 0 | 4.0 | 5.0 | 26418 | 43.0 | 0.1 | 44 |
| javascript | 1/1 | 0 | 5.0 | 6.0 | 30933 | 68.8 | 0.0 | 69 |
| rust | 1/2 | 0 | 21.5 | 31.0 | 502949 | 842.8 | 0.3 | 843 |
| go | 2/2 | 0 | 5.0 | 3.5 | 33232 | 281.6 | 0.0 | 282 |
| ruby | 1/2 | 0 | 3.0 | 2.0 | 13010 | 126.1 | 0.0 | 127 |
| java | 1/2 | 0 | 5.0 | 5.0 | 34890 | 192.4 | 0.0 | 193 |
| typescript | 0/1 | 0 | 15.0 | 22.0 | 280996 | 550.1 | 0.0 | 550 |
| c | 0/2 | 0 | 7.5 | 9.0 | 63838 | 343.1 | 0.0 | 344 |
| php | 1/2 | 0 | 6.0 | 7.5 | 43348 | 203.9 | 0.0 | 205 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | locate-heldout | code | pass | 3 | 2 | 13390 | 7517 | 5865 | 490 | 0 | 0 | 0 | 24 |  |
| locate-axios__axios-5085 | locate-heldout | code | pass | 5 | 6 | 30933 | 25671 | 7403 | 2178 | 0 | 0 | 0 | 69 |  |
| locate-burntsushi__ripgrep-2209 | locate-heldout | code | FAIL | 20 | 34 | 529496 | 464954 | 35795 | 24203 | 3 | 0 | 0 | 832 | answer does not match /(?:crates/printer/src/util\.rs)/ |
| locate-caddyserver__caddy-5404 | locate-heldout | code | pass | 3 | 2 | 23086 | 15397 | 9838 | 6992 | 0 | 0 | 0 | 202 |  |
| locate-caddyserver__caddy-6370 | locate-heldout | code | pass | 7 | 5 | 43377 | 35283 | 10227 | 12758 | 0 | 0 | 0 | 361 |  |
| locate-faker-ruby__faker-2705 | locate-heldout | code | pass | 3 | 2 | 11133 | 8793 | 4489 | 497 | 0 | 0 | 0 | 19 |  |
| locate-fastlane__fastlane-20958 | locate-heldout | code | FAIL | 3 | 2 | 14886 | 10547 | 6488 | 8386 | 0 | 0 | 0 | 234 | answer does not match /(?:fastlane/lib/fastlane/fast_file\.rb)/ |
| locate-google__gson-2479 | locate-heldout | code | pass | 3 | 2 | 12427 | 9035 | 5541 | 554 | 0 | 0 | 0 | 21 |  |
| locate-immutable-js__immutable-js-2006 | locate-heldout | code | FAIL | 15 | 22 | 280996 | 254357 | 28740 | 17277 | 0 | 0 | 0 | 550 | answer does not match /(?:src/Operations\.js)/ |
| locate-jqlang__jq-2839 | locate-heldout | code | FAIL | 5 | 7 | 51967 | 39290 | 14818 | 4912 | 0 | 0 | 0 | 154 | answer does not match /(?:src/jv\.c)/ |
| locate-laravel__framework-48636 | locate-heldout | code | pass | 3 | 4 | 19551 | 13189 | 8511 | 789 | 0 | 0 | 0 | 32 |  |
| locate-laravel__framework-52451 | locate-heldout | code | FAIL | 9 | 11 | 67146 | 57347 | 11928 | 13023 | 0 | 0 | 0 | 377 | …ption.parse_error.101] parse error at line 1, column 25: syntax error while parsing value - invalid string: missing closing quote; last read: '"src/Illuminate/' |
| locate-nushell__nushell-12950 | locate-heldout | code | pass | 23 | 28 | 476402 | 444080 | 34391 | 26885 | 0 | 0 | 0 | 855 |  |
| locate-projectlombok__lombok-3371 | locate-heldout | code | FAIL | 7 | 8 | 57352 | 48355 | 11130 | 12651 | 0 | 0 | 0 | 364 | answer does not match /(?:src/core/lombok/eclipse/handlers/HandleNonNull\.java\|src/core/lombok/javac/handlers/HandleNonNull\.java)/ |
| locate-redis__redis-12472 | locate-heldout | code | FAIL | 10 | 11 | 75710 | 63962 | 13869 | 18518 | 0 | 0 | 0 | 533 | answer does not match /(?:src/acl\.c)/ |
| locate-sphinx-doc__sphinx-10325 | locate-heldout | code | pass | 5 | 8 | 39446 | 29319 | 12268 | 1738 | 0 | 0 | 0 | 63 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:find_symbol | tool:glob | tool:grep | tool:ls | tool:read | tool:web_search | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | 5865 | 0 | 220 | 394 | 285 | 537 | 0 | 0 | 0 | 0 | 0 | 2159 | 0 | 36 | 1132 | 518 | 584 |
| locate-axios__axios-5085 | 7403 | 61 | 212 | 284 | 53 | 537 | 0 | 0 | 0 | 165 | 0 | 3664 | 0 | 97 | 1132 | 452 | 746 |
| locate-burntsushi__ripgrep-2209 | 35795 | 98 | 224 | 238 | 205 | 537 | 0 | 0 | 0 | 352 | 0 | 29973 | 0 | 368 | 1132 | 1419 | 1249 |
| locate-caddyserver__caddy-5404 | 9838 | 248 | 216 | 276 | 83 | 537 | 0 | 0 | 0 | 0 | 0 | 5207 | 0 | 39 | 1132 | 1518 | 582 |
| locate-caddyserver__caddy-6370 | 10227 | 384 | 216 | 277 | 41 | 537 | 16 | 0 | 0 | 118 | 0 | 6081 | 0 | 448 | 1132 | 280 | 697 |
| locate-faker-ruby__faker-2705 | 4489 | 0 | 216 | 187 | 176 | 537 | 0 | 0 | 0 | 0 | 0 | 1307 | 0 | 35 | 1132 | 315 | 584 |
| locate-fastlane__fastlane-20958 | 6488 | 0 | 216 | 740 | 80 | 537 | 0 | 0 | 0 | 0 | 0 | 2436 | 0 | 38 | 1132 | 725 | 584 |
| locate-google__gson-2479 | 5541 | 0 | 212 | 259 | 107 | 537 | 0 | 0 | 0 | 0 | 0 | 2300 | 0 | 48 | 1132 | 362 | 584 |
| locate-immutable-js__immutable-js-2006 | 28740 | 1250 | 218 | 258 | 32 | 537 | 0 | 0 | 0 | 927 | 0 | 22480 | 0 | 337 | 1132 | 240 | 1329 |
| locate-jqlang__jq-2839 | 14818 | 46 | 214 | 280 | 169 | 537 | 0 | 0 | 0 | 768 | 0 | 8562 | 0 | 157 | 1132 | 2166 | 787 |
| locate-laravel__framework-48636 | 8511 | 21 | 212 | 441 | 106 | 537 | 0 | 0 | 0 | 0 | 0 | 4979 | 0 | 82 | 1132 | 377 | 624 |
| locate-laravel__framework-52451 | 11928 | 80 | 212 | 474 | 119 | 537 | 0 | 0 | 0 | 216 | 97 | 7021 | 0 | 237 | 1132 | 853 | 950 |
| locate-nushell__nushell-12950 | 34391 | 728 | 222 | 279 | 162 | 537 | 0 | 0 | 9 | 1656 | 0 | 25584 | 64 | 1833 | 1132 | 526 | 1659 |
| locate-projectlombok__lombok-3371 | 11130 | 45 | 216 | 267 | 314 | 537 | 0 | 248 | 0 | 109 | 0 | 6982 | 0 | 138 | 1132 | 359 | 783 |
| locate-redis__redis-12472 | 13869 | 473 | 214 | 298 | 150 | 537 | 0 | 0 | 0 | 1701 | 0 | 7706 | 0 | 222 | 1132 | 404 | 1032 |
| locate-sphinx-doc__sphinx-10325 | 12268 | 0 | 218 | 405 | 200 | 537 | 0 | 0 | 0 | 1348 | 0 | 7085 | 0 | 267 | 1132 | 260 | 816 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 24af01913d7f4237.
