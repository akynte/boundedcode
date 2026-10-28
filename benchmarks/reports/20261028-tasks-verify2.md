# Engineering benchmark `suite-20261028T011927Z`

model: `qwen3.6-35b-a3b` · started 2026-10-28 01:19Z · finished 2026-10-28 01:57Z

| metric | value |
|---|---|
| tasks | 6 |
| verified (hidden checks) | 6 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 9.59 |
| attempts / successful task | 2.00 |
| wall-clock / task | 375 s |
| processed local tokens / task (uncached prompt + generated) | 41472 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | cross-service | true | completed | true | 3 | 133095 | 0 | 1233 |  |
| go-bugfix-ledger-balance | go-bugfix | true | completed | false | 2 | 18598 | 0 | 165 |  |
| java-bugfix-paid-total | java-bugfix | true | completed | true | 2 | 19924 | 0 | 157 |  |
| multi-feature-discount | multi-language-feature | true | completed | true | 1 | 29605 | 0 | 284 |  |
| py-bugfix-inventory | python-bugfix | true | completed | true | 2 | 30700 | 0 | 289 |  |
| ts-bugfix-cart-discount | typescript-bugfix | true | completed | true | 2 | 16910 | 0 | 124 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | 3 | 15502 | 1882 | 0 (0) | 0 | 12 | 133 | 0/6 | 0 | 56 |
| go-bugfix-ledger-balance | 2 | 2494 | 731 | 0 (0) | 0 | 4 | 40 | 0/2 | 0 | 14 |
| java-bugfix-paid-total | 2 | 3145 | 1696 | 0 (0) | 0 | 8 | 93 | 0/4 | 0 | 19 |
| multi-feature-discount | 1 | 1209 | 510 | 0 (0) | 0 | 6 | 70 | 0/3 | 0 | 30 |
| py-bugfix-inventory | 2 | 2719 | 436 | 0 (0) | 0 | 4 | 46 | 0/2 | 0 | 33 |
| ts-bugfix-cart-discount | 2 | 1385 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 15 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | go | 40 | 1108 | 184 | 919 | 10 (9) | 31 | 3 | 423 | 27982 | not recorded | not recorded |
| go-bugfix-ledger-balance | go | 12 | 127 | 35 | 90 | 4 (4) | 11 | 1 | 354 | 25267 | verification missed | not recorded |
| java-bugfix-paid-total | java | 18 | 129 | 46 | 82 | 4 (4) | 4 | 1 | 355 | 26811 | not recorded | not recorded |
| multi-feature-discount | multi | 21 | 229 | 55 | 173 | 2 (2) | 13 | 1 | 506 | 28500 | not recorded | not recorded |
| py-bugfix-inventory | python | 33 | 260 | 72 | 186 | 4 (4) | 3 | 1 | 289 | 28500 | not recorded | not recorded |
| ts-bugfix-cart-discount | typescript | 12 | 97 | 34 | 62 | 4 (4) | 6 | 1 | 288 | 28288 | not recorded | not recorded |

Environment: build dev 7d4ab28299f0-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 99dd33b0a5b0a9a9.
