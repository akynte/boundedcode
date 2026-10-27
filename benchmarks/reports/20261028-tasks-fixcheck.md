# Engineering benchmark `suite-20261027T185525Z`

model: `qwen3.6-35b-a3b` · started 2026-10-27 18:55Z · finished 2026-10-27 19:51Z

| metric | value |
|---|---|
| tasks | 6 |
| verified (hidden checks) | 4 (67%) |
| local-only completion rate | 67% |
| frontier escalation rate | 0% |
| verified tasks / hour | 4.26 |
| attempts / successful task | 1.75 |
| wall-clock / task | 564 s |
| processed local tokens / task (uncached prompt + generated) | 85260 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | cross-service | false | completed | true | 2 | 66286 | 0 | 742 | payment-service: go test -race -count=3 ./...: …ent (0.00s) |
| go-large-iface-method | interface-change-large-repo | false | blocked | false | 6 | 339970 | 0 | 1772 | temporal-sdk: go test -count=1 -run TestHiddenMediaType ./converter/: …nload/g… |
| java-bugfix-paid-total | java-bugfix | true | completed | true | 2 | 32477 | 0 | 233 |  |
| multi-feature-discount | multi-language-feature | true | completed | true | 1 | 30671 | 0 | 301 |  |
| py-bugfix-inventory | python-bugfix | true | completed | true | 2 | 26257 | 0 | 224 |  |
| ts-bugfix-cart-discount | typescript-bugfix | true | completed | true | 2 | 15901 | 0 | 111 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | 2 | 7522 | 1204 | 0 (0) | 0 | 8 | 88 | 0/4 | 0 | 36 |
| go-large-iface-method | 6 | 71388 | 17902 | 0 (0) | 0 | 60 | 777 | 0/30 | 0 | 109 |
| java-bugfix-paid-total | 2 | 3131 | 1696 | 0 (0) | 0 | 8 | 89 | 0/4 | 0 | 22 |
| multi-feature-discount | 1 | 1202 | 510 | 0 (0) | 0 | 6 | 67 | 0/3 | 0 | 27 |
| py-bugfix-inventory | 2 | 2889 | 436 | 0 (0) | 0 | 4 | 40 | 0/2 | 0 | 29 |
| ts-bugfix-cart-discount | 2 | 1385 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 10 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | go | 23 | 670 | 86 | 581 | 6 (5) | 22 | 1 | 593 | 26750 | verification_false_pass | not recorded |
| go-large-iface-method | go | 105 | 1694 | 546 | 1135 | 6 (0) | 13 | 9 | 438 | 29118 | model | blocked: attempt budget exhausted (6) |
| java-bugfix-paid-total | java | 22 | 205 | 65 | 139 | 4 (4) | 4 | 1 | 312 | 28268 | not recorded | not recorded |
| multi-feature-discount | multi | 21 | 254 | 55 | 198 | 2 (2) | 13 | 1 | 523 | 28217 | not recorded | not recorded |
| py-bugfix-inventory | python | 27 | 199 | 60 | 137 | 4 (4) | 3 | 2 | 288 | 27522 | not recorded | not recorded |
| ts-bugfix-cart-discount | typescript | 10 | 85 | 30 | 54 | 4 (4) | 5 | 1 | 286 | 28031 | not recorded | not recorded |

Environment: build dev 63b72d496420; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks c25975cd2d9b5657.
