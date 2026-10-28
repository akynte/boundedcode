# Direct-mode suite: direct-20261028T070916Z

Model: qwen3.6-35b-a3b. Started 2026-10-28T07:09:16Z, finished 2026-10-28T08:19:27Z.

Settings: approval=auto, checks=true, context_tokens=131072, history_notes=true, max_tool_output=0, provider=local, retrieval=basic, seed_pack=true, working_set_tokens=0.

**11/16 passed (69%)**, 8.7 model calls and 10.0 tool calls per task, 88747 prompt tokens per task with a 87% prompt-cache hit rate, 8429 generated tokens per task, 25 KB of tool results per task, 263 s per task, 36627 tokens in the largest single prompt; 6 results set aside, 0 recalled, 0 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Where the agent's time went, per task: 262.3 s in model calls, 0.1 s in tools (with the checks after edits), 0.48 s in the harness itself; time to first token 2154 ms (median). 7495 reasoning tokens per task. 1 prompt-cache invalidations (1 after the history was rewritten), 22612 prompt tokens recomputed. Cost of a passed task: 14075 processed tokens and 180 s; of a failed one: 32945 and 446 s. Peak memory: harness 68 MiB, model server 28843 MiB, GPU 7162 MiB.

Failures by cause: model 5. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| python | 2/2 | 0 | 8.5 | 9.5 | 45896 | 51.1 | 0.1 | 52 |
| javascript | 0/1 | 0 | 6.0 | 11.0 | 56830 | 361.8 | 0.1 | 362 |
| rust | 1/2 | 0 | 20.5 | 25.5 | 320820 | 520.7 | 0.4 | 521 |
| go | 2/2 | 0 | 3.5 | 1.5 | 21398 | 58.0 | 0.0 | 58 |
| ruby | 2/2 | 0 | 4.0 | 4.0 | 19870 | 64.4 | 0.0 | 66 |
| java | 2/2 | 0 | 12.5 | 15.5 | 144860 | 383.7 | 0.3 | 385 |
| typescript | 0/1 | 0 | 8.0 | 8.0 | 38674 | 95.2 | 0.0 | 95 |
| c | 1/2 | 0 | 7.5 | 8.0 | 72710 | 511.2 | 0.1 | 512 |
| php | 1/2 | 0 | 6.0 | 6.5 | 36672 | 280.6 | 0.0 | 282 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | locate-heldout | code | pass | 12 | 11 | 63290 | 57523 | 7880 | 1189 | 0 | 0 | 0 | 54 |  |
| locate-axios__axios-5085 | locate-heldout | code | FAIL | 6 | 11 | 56830 | 41064 | 17903 | 12009 | 0 | 0 | 0 | 362 | answer does not match /(?:index\.d\.ts\|lib/core/AxiosHeaders\.js)/ |
| locate-burntsushi__ripgrep-2209 | locate-heldout | code | FAIL | 19 | 24 | 421232 | 364082 | 36627 | 23668 | 6 | 0 | 0 | 786 | answer does not match /(?:crates/printer/src/util\.rs)/ |
| locate-caddyserver__caddy-5404 | locate-heldout | code | pass | 4 | 1 | 31891 | 24683 | 9353 | 2776 | 0 | 0 | 0 | 89 |  |
| locate-caddyserver__caddy-6370 | locate-heldout | code | pass | 3 | 2 | 10906 | 8665 | 4390 | 796 | 0 | 0 | 0 | 28 |  |
| locate-faker-ruby__faker-2705 | locate-heldout | code | pass | 4 | 4 | 14182 | 11802 | 4525 | 540 | 0 | 0 | 0 | 22 |  |
| locate-fastlane__fastlane-20958 | locate-heldout | code | pass | 4 | 4 | 25559 | 19532 | 8172 | 3534 | 0 | 0 | 0 | 110 |  |
| locate-google__gson-2479 | locate-heldout | code | pass | 5 | 5 | 23113 | 18631 | 6623 | 835 | 0 | 0 | 0 | 33 |  |
| locate-immutable-js__immutable-js-2006 | locate-heldout | code | FAIL | 8 | 8 | 38674 | 33097 | 7706 | 2938 | 0 | 0 | 0 | 95 | answer does not match /(?:src/Operations\.js)/ |
| locate-jqlang__jq-2839 | locate-heldout | code | pass | 9 | 9 | 109693 | 95413 | 16405 | 19006 | 0 | 0 | 0 | 572 |  |
| locate-laravel__framework-48636 | locate-heldout | code | pass | 4 | 5 | 22921 | 18103 | 6963 | 691 | 0 | 0 | 0 | 31 |  |
| locate-laravel__framework-52451 | locate-heldout | code | FAIL | 8 | 8 | 50422 | 44076 | 8475 | 18117 | 0 | 0 | 0 | 533 | answer does not match /(?:src/Illuminate/Validation/Concerns/FormatsMessages\.php)/ |
| locate-nushell__nushell-12950 | locate-heldout | code | pass | 22 | 27 | 220408 | 201317 | 21164 | 7424 | 0 | 0 | 0 | 256 |  |
| locate-projectlombok__lombok-3371 | locate-heldout | code | pass | 20 | 26 | 266607 | 245809 | 22879 | 24013 | 0 | 0 | 0 | 736 |  |
| locate-redis__redis-12472 | locate-heldout | code | FAIL | 6 | 7 | 35727 | 28479 | 9385 | 15904 | 0 | 0 | 0 | 451 | answer does not match /(?:src/acl\.c)/ |
| locate-sphinx-doc__sphinx-10325 | locate-heldout | code | pass | 5 | 8 | 28503 | 23003 | 7641 | 1430 | 0 | 0 | 0 | 50 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | system | tool:bash | tool:find_symbol | tool:glob | tool:grep | tool:read | tool:web_fetch | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | 7880 | 101 | 220 | 394 | 0 | 537 | 0 | 0 | 0 | 184 | 3734 | 0 | 159 | 1132 | 518 | 901 |
| locate-axios__axios-5085 | 17903 | 216 | 212 | 284 | 173 | 537 | 0 | 0 | 0 | 1743 | 12179 | 0 | 155 | 1132 | 452 | 820 |
| locate-burntsushi__ripgrep-2209 | 36627 | 101 | 224 | 238 | 289 | 537 | 0 | 0 | 0 | 269 | 30586 | 0 | 437 | 1132 | 1419 | 1395 |
| locate-caddyserver__caddy-5404 | 9353 | 19 | 216 | 283 | 131 | 537 | 0 | 0 | 0 | 0 | 4662 | 146 | 103 | 1132 | 1518 | 606 |
| locate-caddyserver__caddy-6370 | 4390 | 26 | 216 | 284 | 79 | 537 | 0 | 0 | 0 | 0 | 1225 | 0 | 27 | 1132 | 280 | 584 |
| locate-faker-ruby__faker-2705 | 4525 | 0 | 216 | 187 | 52 | 537 | 0 | 0 | 32 | 38 | 1307 | 0 | 56 | 1132 | 315 | 653 |
| locate-fastlane__fastlane-20958 | 8172 | 256 | 216 | 740 | 152 | 537 | 0 | 0 | 0 | 0 | 3685 | 0 | 77 | 1132 | 725 | 652 |
| locate-google__gson-2479 | 6623 | 0 | 212 | 259 | 74 | 537 | 0 | 24 | 0 | 113 | 3089 | 0 | 115 | 1132 | 362 | 706 |
| locate-immutable-js__immutable-js-2006 | 7706 | 24 | 218 | 258 | 0 | 537 | 0 | 0 | 0 | 298 | 4006 | 0 | 136 | 1132 | 240 | 857 |
| locate-jqlang__jq-2839 | 16405 | 207 | 214 | 280 | 91 | 537 | 0 | 0 | 0 | 0 | 10738 | 0 | 162 | 1132 | 2166 | 878 |
| locate-laravel__framework-48636 | 6963 | 15 | 212 | 441 | 394 | 537 | 0 | 24 | 0 | 0 | 3061 | 0 | 105 | 1132 | 377 | 665 |
| locate-laravel__framework-52451 | 8475 | 102 | 212 | 474 | 154 | 537 | 0 | 0 | 0 | 248 | 3732 | 0 | 179 | 1132 | 853 | 852 |
| locate-nushell__nushell-12950 | 21164 | 780 | 222 | 279 | 0 | 537 | 32 | 0 | 0 | 2800 | 12653 | 0 | 605 | 1132 | 526 | 1598 |
| locate-projectlombok__lombok-3371 | 22879 | 1338 | 216 | 267 | 0 | 537 | 0 | 0 | 663 | 1503 | 14832 | 0 | 540 | 1132 | 359 | 1492 |
| locate-redis__redis-12472 | 9385 | 104 | 214 | 298 | 98 | 537 | 0 | 0 | 0 | 1542 | 4120 | 0 | 128 | 1132 | 404 | 808 |
| locate-sphinx-doc__sphinx-10325 | 7641 | 37 | 218 | 405 | 0 | 537 | 0 | 0 | 0 | 1066 | 2877 | 0 | 279 | 1132 | 260 | 830 |

Environment: build dev b158a2ffa4b5-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 24af01913d7f4237.
