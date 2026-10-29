# Engineering benchmark `suite-20261029T121724Z`

model: `qwen3.6-35b-a3b` · started 2026-10-29 12:17Z · finished 2026-10-29 12:24Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 8.31 |
| attempts / successful task | 2.00 |
| wall-clock / task | 433 s |
| processed local tokens / task (uncached prompt + generated) | 41238 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | cross-service | true | completed | true | 2 | 41238 | 0 | 433 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | 2 | 7864 | 1204 | 0 (0) | 0 | 8 | 83 | 0/4 | 0 | 32 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | go | 18 | 346 | 61 | 283 | 6 (4) | 20 | 1 | 396 | 28035 | not recorded | not recorded |

Environment: build dev 74e6f0c1c132; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 99dd33b0a5b0a9a9.
