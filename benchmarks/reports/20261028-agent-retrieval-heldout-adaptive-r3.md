# Direct-mode suite: direct-20261028T103249Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T10:32:49Z, finished 2026-10-28T11:33:17Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=adaptive, seed_pack=true, working_set_tokens=0.

**9/16 passed (56%)**, 8.9 model calls and 10.2 tool calls per task, 94802 prompt tokens per task with a 89% prompt-cache hit rate, 7339 generated tokens per task, 24 KB of tool results per task, 227 s per task, 32863 tokens in the largest single prompt; 0 results set aside, 0 recalled, 0 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 226.2 s in model calls, 0.2 s in tools (with the checks after edits), 0.09 s in the harness itself; time to first token 2088 ms (median). 6417 reasoning tokens per task. 0 prompt-cache invalidations (0 after the history was rewritten), 0 prompt tokens recomputed. Cost of a passed task: 13808 processed tokens and 144 s; of a failed one: 23296 and 333 s. Peak memory: harness 38 MiB, model server 29104 MiB, GPU 7161 MiB.

Failures by cause: model 7. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| python | 2/2 | 0 | 8.5 | 7.5 | 54758 | 60.4 | 0.0 | 61 |
| javascript | 1/1 | 0 | 5.0 | 4.0 | 29958 | 58.2 | 0.0 | 58 |
| rust | 1/2 | 0 | 24.5 | 30.5 | 408788 | 552.9 | 0.9 | 554 |
| go | 1/2 | 0 | 5.5 | 6.0 | 30998 | 369.3 | 0.1 | 370 |
| ruby | 1/2 | 0 | 3.0 | 2.0 | 13010 | 125.8 | 0.0 | 127 |
| java | 1/2 | 0 | 5.5 | 8.5 | 46446 | 168.8 | 0.1 | 169 |
| typescript | 1/1 | 0 | 11.0 | 13.0 | 99317 | 177.6 | 0.0 | 178 |
| c | 0/2 | 0 | 11.0 | 13.0 | 100736 | 262.2 | 0.1 | 263 |
| php | 1/2 | 0 | 5.5 | 6.0 | 39044 | 151.9 | 0.0 | 153 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | locate-heldout | code | pass | 9 | 8 | 45910 | 38444 | 7434 | 874 | 0 | 0 | 0 | 44 |  |
| locate-axios__axios-5085 | locate-heldout | code | pass | 5 | 4 | 29958 | 24882 | 7217 | 1783 | 0 | 0 | 0 | 58 |  |
| locate-burntsushi__ripgrep-2209 | locate-heldout | code | FAIL | 22 | 28 | 344008 | 313237 | 32844 | 23138 | 0 | 0 | 0 | 745 | answer does not match /(?:crates/printer/src/util\.rs)/ |
| locate-caddyserver__caddy-5404 | locate-heldout | code | FAIL | 2 | 1 | 13210 | 6404 | 8959 | 8266 | 0 | 0 | 0 | 237 | answer does not match /(?:caddyconfig/caddyfile/lexer\.go)/ |
| locate-caddyserver__caddy-6370 | locate-heldout | code | pass | 9 | 11 | 48786 | 41872 | 9039 | 18031 | 0 | 0 | 0 | 502 |  |
| locate-faker-ruby__faker-2705 | locate-heldout | code | pass | 3 | 2 | 11133 | 8793 | 4489 | 391 | 0 | 0 | 0 | 17 |  |
| locate-fastlane__fastlane-20958 | locate-heldout | code | FAIL | 3 | 2 | 14886 | 10547 | 6488 | 8442 | 0 | 0 | 0 | 236 | answer does not match /(?:fastlane/lib/fastlane/fast_file\.rb)/ |
| locate-google__gson-2479 | locate-heldout | code | pass | 3 | 2 | 12427 | 9035 | 5541 | 539 | 0 | 0 | 0 | 21 |  |
| locate-immutable-js__immutable-js-2006 | locate-heldout | code | pass | 11 | 13 | 99317 | 86296 | 15138 | 5485 | 0 | 0 | 0 | 178 |  |
| locate-jqlang__jq-2839 | locate-heldout | code | FAIL | 7 | 10 | 59620 | 50576 | 11177 | 3940 | 0 | 0 | 0 | 124 | answer does not match /(?:src/jv\.c)/ |
| locate-laravel__framework-48636 | locate-heldout | code | pass | 4 | 5 | 24864 | 18584 | 8425 | 944 | 0 | 0 | 0 | 37 |  |
| locate-laravel__framework-52451 | locate-heldout | code | FAIL | 7 | 7 | 53225 | 44764 | 10594 | 9190 | 0 | 0 | 0 | 268 | answer does not match /(?:src/Illuminate/Validation/Concerns/FormatsMessages\.php)/ |
| locate-nushell__nushell-12950 | locate-heldout | code | pass | 27 | 33 | 473567 | 442757 | 32863 | 10464 | 0 | 0 | 0 | 363 |  |
| locate-projectlombok__lombok-3371 | locate-heldout | code | FAIL | 8 | 15 | 80464 | 66994 | 14982 | 10491 | 0 | 0 | 0 | 318 | answer does not match /(?:src/core/lombok/eclipse/handlers/HandleNonNull\.java\|src/core/lombok/javac/handlers/HandleNonNull\.java)/ |
| locate-redis__redis-12472 | locate-heldout | code | FAIL | 15 | 16 | 141851 | 128392 | 15560 | 13255 | 0 | 0 | 0 | 402 | answer does not match /(?:src/acl\.c)/ |
| locate-sphinx-doc__sphinx-10325 | locate-heldout | code | pass | 8 | 7 | 63606 | 55326 | 10409 | 2185 | 0 | 0 | 0 | 78 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:find_symbol | tool:glob | tool:grep | tool:read | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | 7434 | 14 | 220 | 394 | 285 | 537 | 0 | 0 | 0 | 0 | 3432 | 114 | 1132 | 518 | 788 |
| locate-axios__axios-5085 | 7217 | 17 | 212 | 284 | 53 | 537 | 0 | 0 | 0 | 164 | 3664 | 48 | 1132 | 452 | 654 |
| locate-burntsushi__ripgrep-2209 | 32844 | 149 | 224 | 238 | 194 | 537 | 0 | 0 | 0 | 3664 | 23156 | 486 | 1132 | 1419 | 1645 |
| locate-caddyserver__caddy-5404 | 8959 | 0 | 216 | 276 | 70 | 537 | 0 | 0 | 0 | 0 | 4662 | 14 | 1132 | 1518 | 534 |
| locate-caddyserver__caddy-6370 | 9039 | 246 | 216 | 277 | 41 | 537 | 0 | 0 | 0 | 322 | 4985 | 118 | 1132 | 280 | 885 |
| locate-faker-ruby__faker-2705 | 4489 | 0 | 216 | 187 | 176 | 537 | 0 | 0 | 0 | 0 | 1307 | 35 | 1132 | 315 | 584 |
| locate-fastlane__fastlane-20958 | 6488 | 0 | 216 | 740 | 80 | 537 | 0 | 0 | 0 | 0 | 2436 | 38 | 1132 | 725 | 584 |
| locate-google__gson-2479 | 5541 | 0 | 212 | 259 | 107 | 537 | 0 | 0 | 0 | 0 | 2300 | 48 | 1132 | 362 | 584 |
| locate-immutable-js__immutable-js-2006 | 15138 | 403 | 218 | 258 | 128 | 537 | 0 | 0 | 112 | 129 | 10767 | 201 | 1132 | 240 | 1013 |
| locate-jqlang__jq-2839 | 11177 | 72 | 214 | 280 | 169 | 537 | 0 | 0 | 0 | 1379 | 4056 | 227 | 1132 | 2166 | 945 |
| locate-laravel__framework-48636 | 8425 | 25 | 212 | 441 | 81 | 537 | 0 | 24 | 0 | 0 | 4829 | 100 | 1132 | 377 | 667 |
| locate-laravel__framework-52451 | 10594 | 209 | 212 | 474 | 119 | 537 | 0 | 0 | 406 | 0 | 5723 | 142 | 1132 | 853 | 787 |
| locate-nushell__nushell-12950 | 32863 | 253 | 222 | 279 | 162 | 537 | 64 | 0 | 114 | 6891 | 18566 | 2160 | 1132 | 526 | 1957 |
| locate-projectlombok__lombok-3371 | 14982 | 54 | 216 | 267 | 314 | 537 | 0 | 0 | 175 | 1138 | 9521 | 289 | 1132 | 359 | 980 |
| locate-redis__redis-12472 | 15560 | 523 | 214 | 298 | 150 | 537 | 0 | 7696 | 130 | 331 | 2438 | 430 | 1132 | 404 | 1277 |
| locate-sphinx-doc__sphinx-10325 | 10409 | 0 | 218 | 405 | 200 | 537 | 0 | 0 | 0 | 1280 | 5343 | 214 | 1132 | 260 | 820 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 24af01913d7f4237.
