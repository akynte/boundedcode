# Engineering benchmark `suite-20261029T123611Z`

model: `qwen3.6-35b-a3b` · started 2026-10-29 12:36Z · finished 2026-10-29 12:54Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 1082 s |
| processed local tokens / task (uncached prompt + generated) | 115843 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | cross-service | false | completed | false | 3 | 115843 | 0 | 1082 | payment-service: go test -race -count=3 ./...: ?   	example.com/payment-service/… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | 3 | 14925 | 1814 | 0 (0) | 0 | 12 | 128 | 0/6 | 0 | 55 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | go | 42 | 979 | 169 | 806 | 10 (8) | 29 | 3 | 532 | 27565 | model | not recorded |

Environment: build dev 74e6f0c1c132; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 99dd33b0a5b0a9a9.
