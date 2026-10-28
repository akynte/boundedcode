# Engineering benchmark `suite-20261028T202807Z`

model: `qwen3.6-35b-a3b` · started 2026-10-28 20:28Z · finished 2026-10-28 20:31Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 174 s |
| processed local tokens / task (uncached prompt + generated) | 18420 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| go-debug-ledger-balance-query | debugging | false | completed | true | 2 | 18420 | 0 | 174 | ledger-service: sh .bench/check_tests_unchanged.sh: … @@ func TestBalance(t *t… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| go-debug-ledger-balance-query | 2 | 2102 | 543 | 0 (0) | 0 | 4 | 44 | 0/2 | 0 | 10 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| go-debug-ledger-balance-query | go | 13 | 131 | 32 | 99 | 4 (4) | 11 | 1 | 372 | 26569 | verification_false_pass | not recorded |

Environment: build dev f95dd3463ef6-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 99dd33b0a5b0a9a9.
