# Direct-mode suite: direct-20261028T081927Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T08:19:27Z, finished 2026-10-28T09:10:45Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**13/16 passed (81%)**, 8.4 model calls and 9.4 tool calls per task, 88304 prompt tokens per task with a 88% prompt-cache hit rate, 6116 generated tokens per task, 24 KB of tool results per task, 192 s per task, 32848 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 191.9 s in model calls, 0.1 s in tools (with the checks after edits), 0.09 s in the harness itself; time to first token 2232 ms (median). 5057 reasoning tokens per task. 0 prompt-cache invalidations (0 after the history was rewritten), 0 prompt tokens recomputed. Cost of a passed task: 12531 processed tokens and 134 s; of a failed one: 33747 and 447 s. Peak memory: harness 37 MiB, model server 28960 MiB, GPU 7161 MiB.

Failures by cause: model 3. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| python | 2/2 | 0 | 6.5 | 9.0 | 33233 | 56.1 | 0.0 | 57 |
| javascript | 1/1 | 0 | 8.0 | 10.0 | 77391 | 141.9 | 0.0 | 142 |
| rust | 1/2 | 0 | 16.5 | 19.0 | 282109 | 417.2 | 0.2 | 418 |
| go | 2/2 | 0 | 3.0 | 2.0 | 14330 | 52.8 | 0.0 | 53 |
| ruby | 2/2 | 0 | 3.0 | 2.0 | 13004 | 61.4 | 0.0 | 62 |
| java | 1/2 | 0 | 7.0 | 7.0 | 60628 | 176.9 | 0.1 | 177 |
| typescript | 1/1 | 0 | 10.0 | 10.0 | 64842 | 140.9 | 0.0 | 141 |
| c | 1/2 | 0 | 11.0 | 12.5 | 127631 | 484.0 | 0.1 | 484 |
| php | 2/2 | 0 | 11.5 | 13.5 | 104378 | 145.4 | 0.1 | 146 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | locate-heldout | code | pass | 5 | 4 | 22470 | 18587 | 6024 | 565 | 0 | 0 | 0 | 25 |  |
| locate-axios__axios-5085 | locate-heldout | code | pass | 8 | 10 | 77391 | 65007 | 14513 | 4379 | 0 | 0 | 0 | 142 |  |
| locate-burntsushi__ripgrep-2209 | locate-heldout | code | FAIL | 13 | 18 | 261683 | 230944 | 32848 | 12340 | 0 | 0 | 0 | 421 | answer does not match /(?:crates/printer/src/util\.rs)/ |
| locate-caddyserver__caddy-5404 | locate-heldout | code | pass | 2 | 1 | 13224 | 6411 | 8966 | 933 | 0 | 0 | 0 | 36 |  |
| locate-caddyserver__caddy-6370 | locate-heldout | code | pass | 4 | 3 | 15437 | 12884 | 4698 | 2351 | 0 | 0 | 0 | 70 |  |
| locate-faker-ruby__faker-2705 | locate-heldout | code | pass | 3 | 2 | 11133 | 8793 | 4489 | 540 | 0 | 0 | 0 | 20 |  |
| locate-fastlane__fastlane-20958 | locate-heldout | code | pass | 3 | 2 | 14875 | 10572 | 6452 | 3576 | 0 | 0 | 0 | 104 |  |
| locate-google__gson-2479 | locate-heldout | code | pass | 4 | 3 | 18830 | 14295 | 6680 | 907 | 0 | 0 | 0 | 33 |  |
| locate-immutable-js__immutable-js-2006 | locate-heldout | code | pass | 10 | 10 | 64842 | 57588 | 9375 | 4534 | 0 | 0 | 0 | 141 |  |
| locate-jqlang__jq-2839 | locate-heldout | code | pass | 12 | 15 | 153451 | 137187 | 18377 | 12213 | 0 | 0 | 0 | 371 |  |
| locate-laravel__framework-48636 | locate-heldout | code | pass | 6 | 7 | 43310 | 36029 | 9418 | 1018 | 0 | 0 | 0 | 43 |  |
| locate-laravel__framework-52451 | locate-heldout | code | pass | 17 | 20 | 165445 | 151994 | 15544 | 7867 | 0 | 0 | 0 | 249 |  |
| locate-nushell__nushell-12950 | locate-heldout | code | pass | 20 | 20 | 302535 | 280894 | 23722 | 13122 | 0 | 0 | 0 | 414 |  |
| locate-projectlombok__lombok-3371 | locate-heldout | code | FAIL | 10 | 11 | 102425 | 90944 | 13602 | 10644 | 0 | 0 | 0 | 322 | answer does not match /(?:src/core/lombok/eclipse/handlers/HandleNonNull\.java\|src/core/lombok/javac/handlers/HandleNonNull\.java)/ |
| locate-redis__redis-12472 | locate-heldout | code | FAIL | 10 | 10 | 101811 | 85907 | 18025 | 20134 | 0 | 0 | 0 | 598 | answer does not match /(?:src/acl\.c)/ |
| locate-sphinx-doc__sphinx-10325 | locate-heldout | code | pass | 8 | 14 | 43996 | 38530 | 7595 | 2733 | 0 | 0 | 0 | 88 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:find_symbol | tool:glob | tool:grep | tool:read | tool:web_search | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | 6024 | 0 | 220 | 394 | 285 | 537 | 0 | 0 | 0 | 0 | 2224 | 0 | 62 | 1132 | 518 | 652 |
| locate-axios__axios-5085 | 14513 | 34 | 212 | 284 | 53 | 537 | 0 | 0 | 0 | 503 | 10313 | 0 | 139 | 1132 | 452 | 854 |
| locate-burntsushi__ripgrep-2209 | 32848 | 92 | 224 | 238 | 194 | 537 | 0 | 0 | 0 | 2469 | 25037 | 0 | 312 | 1132 | 1419 | 1194 |
| locate-caddyserver__caddy-5404 | 8966 | 0 | 216 | 283 | 70 | 537 | 0 | 0 | 0 | 0 | 4662 | 0 | 14 | 1132 | 1518 | 534 |
| locate-caddyserver__caddy-6370 | 4698 | 307 | 216 | 284 | 32 | 537 | 0 | 0 | 0 | 28 | 1225 | 0 | 36 | 1132 | 280 | 621 |
| locate-faker-ruby__faker-2705 | 4489 | 0 | 216 | 187 | 176 | 537 | 0 | 0 | 0 | 0 | 1307 | 0 | 35 | 1132 | 315 | 584 |
| locate-fastlane__fastlane-20958 | 6452 | 25 | 216 | 740 | 80 | 537 | 0 | 0 | 0 | 0 | 2375 | 0 | 38 | 1132 | 725 | 584 |
| locate-google__gson-2479 | 6680 | 59 | 212 | 259 | 107 | 537 | 0 | 0 | 0 | 0 | 3306 | 0 | 74 | 1132 | 362 | 632 |
| locate-immutable-js__immutable-js-2006 | 9375 | 496 | 218 | 258 | 128 | 537 | 0 | 0 | 0 | 509 | 4764 | 0 | 157 | 1132 | 240 | 936 |
| locate-jqlang__jq-2839 | 18377 | 472 | 214 | 280 | 169 | 537 | 0 | 0 | 0 | 3196 | 8665 | 0 | 371 | 1132 | 2166 | 1175 |
| locate-laravel__framework-48636 | 9418 | 32 | 212 | 441 | 81 | 537 | 0 | 24 | 0 | 201 | 5488 | 0 | 143 | 1132 | 377 | 750 |
| locate-laravel__framework-52451 | 15544 | 845 | 212 | 474 | 119 | 537 | 0 | 19 | 235 | 35 | 9272 | 0 | 443 | 1132 | 853 | 1368 |
| locate-nushell__nushell-12950 | 23722 | 1633 | 222 | 279 | 162 | 537 | 16 | 0 | 9 | 2883 | 14292 | 64 | 539 | 1132 | 526 | 1428 |
| locate-projectlombok__lombok-3371 | 13602 | 108 | 216 | 267 | 314 | 537 | 0 | 248 | 0 | 1142 | 8119 | 0 | 221 | 1132 | 359 | 939 |
| locate-redis__redis-12472 | 18025 | 306 | 214 | 298 | 150 | 537 | 0 | 0 | 0 | 50 | 13773 | 0 | 196 | 1132 | 404 | 965 |
| locate-sphinx-doc__sphinx-10325 | 7595 | 0 | 218 | 405 | 200 | 537 | 0 | 0 | 0 | 627 | 2755 | 0 | 353 | 1132 | 260 | 1108 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 24af01913d7f4237.
