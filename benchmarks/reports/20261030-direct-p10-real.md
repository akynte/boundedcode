# Direct-mode suite: direct-20261030T005043Z

Model: qwen3.6-35b-a3b. Started 2026-10-30T00:50:43Z, finished 2026-10-30T01:35:50Z.

Settings: approval=auto, checks=true, compaction=structured, context_tokens=131072, history_notes=true, max_tool_output=0, max_turns=80, observation_bytes=0, parallel_tools=4, provider=local, prune=true, reasoning_history=auto, reasoning_round_trip=true, retrieval=adaptive, routing=off, seed_pack=true, speculative=off, timeout=20m0s, working_set_tokens=0.

**13/16 passed (81%)**, 10.2 model calls and 10.2 tool calls per task, 129194 prompt tokens per task with a 88% prompt-cache hit rate, 5059 generated tokens per task, 26 KB of tool results per task, 169 s per task, 31892 tokens in the largest single prompt; 21 results set aside, 0 recalled, 0 summaries, 0 broken edits caught by the check, 0 check notes, 0 history notes, 0 errors.

Calls run together: 18 replies, 38 calls, 0.440 s together against 0.575 s one after another.

Decode: 36.0 tokens/s over the run (80949 tokens in 2248 s of the server's decode time).

Model calls by role: plan 16 (0 without thinking, 3627 generated tokens, 143 s); explore 146 (0 without thinking, 77234 generated tokens, 2548 s); debug 2 (0 without thinking, 88 generated tokens, 4 s).

Where the agent's time went, per task: 168.4 s in model calls, 0.1 s in tools (with the checks after edits), 0.08 s in the harness itself; time to first token 2104 ms (median). 4036 reasoning tokens per task. 3 prompt-cache invalidations (3 after the history was rewritten), 61015 prompt tokens recomputed; 15317 prompt tokens evaluated by the server per task, in 27.1 s. Cost of a passed task: 20310 processed tokens and 139 s; of a failed one: 20662 and 301 s. Peak memory: harness 41 MiB, model server 28961 MiB, GPU 7155 MiB.

Failures by cause: model 3. Stalls: none.

| Language | Passed | Skipped | Calls | Tools | Prompt tok | Model s | Tool s | s/task |
|---|---|---|---|---|---|---|---|---|
| python | 2/2 | 0 | 6.5 | 6.0 | 38810 | 42.1 | 0.1 | 43 |
| javascript | 1/1 | 0 | 10.0 | 9.0 | 77230 | 144.5 | 0.0 | 145 |
| rust | 2/2 | 0 | 15.5 | 17.5 | 265776 | 225.1 | 0.3 | 226 |
| go | 1/2 | 0 | 4.5 | 3.0 | 48232 | 342.5 | 0.0 | 343 |
| ruby | 2/2 | 0 | 3.0 | 2.0 | 13676 | 76.4 | 0.0 | 78 |
| java | 2/2 | 0 | 22.5 | 23.0 | 380378 | 296.7 | 0.3 | 297 |
| typescript | 0/1 | 0 | 10.0 | 10.0 | 66783 | 95.4 | 0.0 | 96 |
| c | 1/2 | 0 | 8.5 | 9.5 | 104162 | 136.8 | 0.0 | 137 |
| php | 2/2 | 0 | 11.5 | 11.5 | 110516 | 107.7 | 0.0 | 109 |

| Task | Category | Mode | Result | Calls | Tools | Prompt tok | Cached | Peak prompt | Gen tok | Set aside | Recalls | Checks caught | s | Note |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | locate-heldout | code | pass | 4 | 3 | 18842 | 13389 | 5759 | 658 | 0 | 0 | 0 | 30 |  |
| locate-axios__axios-5085 | locate-heldout | code | pass | 10 | 9 | 77230 | 69768 | 10516 | 4712 | 0 | 0 | 0 | 145 |  |
| locate-burntsushi__ripgrep-2209 | locate-heldout | code | pass | 15 | 17 | 269107 | 241985 | 31892 | 8848 | 0 | 0 | 0 | 300 |  |
| locate-caddyserver__caddy-5404 | locate-heldout | code | FAIL | 6 | 4 | 85146 | 76755 | 24541 | 22420 | 0 | 0 | 0 | 651 | answer does not match /(?:caddyconfig/caddyfile/lexer\.go)/ |
| locate-caddyserver__caddy-6370 | locate-heldout | code | pass | 3 | 2 | 11317 | 8994 | 4585 | 1077 | 0 | 0 | 0 | 35 |  |
| locate-faker-ruby__faker-2705 | locate-heldout | code | pass | 3 | 2 | 11630 | 9305 | 4691 | 452 | 0 | 0 | 0 | 18 |  |
| locate-fastlane__fastlane-20958 | locate-heldout | code | pass | 3 | 2 | 15722 | 11124 | 6860 | 4815 | 0 | 0 | 0 | 137 |  |
| locate-google__gson-2479 | locate-heldout | code | pass | 7 | 7 | 45158 | 38975 | 8793 | 1186 | 0 | 0 | 0 | 44 |  |
| locate-immutable-js__immutable-js-2006 | locate-heldout | code | FAIL | 10 | 10 | 66783 | 60476 | 10113 | 2969 | 0 | 0 | 0 | 96 | answer does not match /(?:src/Operations\.js)/ |
| locate-jqlang__jq-2839 | locate-heldout | code | FAIL | 7 | 9 | 88813 | 71614 | 20325 | 4700 | 0 | 0 | 0 | 157 | answer does not match /(?:src/jv\.c)/ |
| locate-laravel__framework-48636 | locate-heldout | code | pass | 7 | 8 | 50650 | 44709 | 8972 | 1765 | 0 | 0 | 0 | 61 |  |
| locate-laravel__framework-52451 | locate-heldout | code | pass | 16 | 15 | 170383 | 158764 | 15763 | 4783 | 0 | 0 | 0 | 157 |  |
| locate-nushell__nushell-12950 | locate-heldout | code | pass | 16 | 18 | 262444 | 238193 | 28055 | 3796 | 0 | 0 | 0 | 152 |  |
| locate-projectlombok__lombok-3371 | locate-heldout | code | pass | 38 | 39 | 715598 | 622691 | 29551 | 14018 | 21 | 0 | 0 | 550 |  |
| locate-redis__redis-12472 | locate-heldout | code | pass | 10 | 10 | 119510 | 102646 | 19785 | 3243 | 0 | 0 | 0 | 118 |  |
| locate-sphinx-doc__sphinx-10325 | locate-heldout | code | pass | 9 | 9 | 58778 | 52650 | 9157 | 1507 | 0 | 0 | 0 | 56 |  |

The largest request of each task, in tokens by section (counting: the model's tokenizer):

| Task | Prompt | assistant | environment | project_map | seed_pack | set_aside | system | tool:bash | tool:glob | tool:grep | tool:read | tool_calls | tool_schemas | user | framing |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| locate-astropy__astropy-14365 | 5759 | 0 | 220 | 394 | 285 | 0 | 537 | 0 | 0 | 287 | 1323 | 64 | 1230 | 518 | 901 |
| locate-axios__axios-5085 | 10516 | 40 | 212 | 284 | 53 | 0 | 537 | 0 | 18 | 584 | 5010 | 164 | 1230 | 452 | 1932 |
| locate-burntsushi__ripgrep-2209 | 31892 | 49 | 224 | 238 | 205 | 0 | 537 | 0 | 0 | 456 | 18787 | 323 | 1230 | 1419 | 8424 |
| locate-caddyserver__caddy-5404 | 24541 | 53 | 216 | 276 | 83 | 0 | 537 | 64 | 0 | 103 | 5690 | 395 | 1230 | 1518 | 14376 |
| locate-caddyserver__caddy-6370 | 4585 | 0 | 216 | 277 | 41 | 0 | 537 | 0 | 0 | 0 | 1225 | 27 | 1230 | 280 | 752 |
| locate-faker-ruby__faker-2705 | 4691 | 0 | 216 | 187 | 176 | 0 | 537 | 0 | 0 | 0 | 1307 | 35 | 1230 | 315 | 688 |
| locate-fastlane__fastlane-20958 | 6860 | 60 | 216 | 740 | 80 | 0 | 537 | 0 | 0 | 0 | 2436 | 38 | 1230 | 725 | 798 |
| locate-google__gson-2479 | 8793 | 0 | 212 | 259 | 107 | 0 | 537 | 0 | 0 | 6 | 4500 | 183 | 1230 | 362 | 1397 |
| locate-immutable-js__immutable-js-2006 | 10113 | 58 | 218 | 258 | 32 | 0 | 537 | 0 | 0 | 566 | 4215 | 168 | 1230 | 240 | 2591 |
| locate-jqlang__jq-2839 | 20325 | 424 | 214 | 280 | 169 | 0 | 537 | 0 | 0 | 4362 | 6774 | 211 | 1230 | 2166 | 3958 |
| locate-laravel__framework-48636 | 8972 | 24 | 212 | 441 | 106 | 0 | 537 | 0 | 0 | 823 | 3469 | 217 | 1230 | 377 | 1536 |
| locate-laravel__framework-52451 | 15763 | 379 | 212 | 474 | 119 | 0 | 537 | 0 | 252 | 329 | 7125 | 344 | 1230 | 853 | 3909 |
| locate-nushell__nushell-12950 | 28055 | 26 | 222 | 279 | 162 | 0 | 537 | 16 | 4 | 1840 | 19678 | 489 | 1230 | 526 | 3046 |
| locate-projectlombok__lombok-3371 | 29551 | 40 | 216 | 267 | 314 | 494 | 537 | 16 | 21 | 6421 | 9308 | 929 | 1230 | 359 | 9399 |
| locate-redis__redis-12472 | 19785 | 359 | 214 | 298 | 150 | 0 | 537 | 0 | 134 | 0 | 13627 | 175 | 1230 | 404 | 2657 |
| locate-sphinx-doc__sphinx-10325 | 9157 | 0 | 218 | 405 | 200 | 0 | 537 | 0 | 0 | 3861 | 859 | 229 | 1230 | 260 | 1358 |

Environment: build dev b409ab02d7b9-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks c857abf9d6cf7e53.
