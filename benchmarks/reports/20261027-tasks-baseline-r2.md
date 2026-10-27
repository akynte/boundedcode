# Engineering benchmark `suite-20261027T142241Z`

model: `qwen3.6-35b-a3b` · started 2026-10-27 14:22Z · finished 2026-10-27 15:51Z

| metric | value |
|---|---|
| tasks | 19 |
| verified (hidden checks) | 17 (89%) |
| local-only completion rate | 89% |
| frontier escalation rate | 0% |
| verified tasks / hour | 11.44 |
| attempts / successful task | 2.12 |
| wall-clock / task | 282 s |
| processed local tokens / task (uncached prompt + generated) | 29951 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | api-contract | true | completed | true | 1 | 29626 | 0 | 247 |  |
| cross-service-idempotency | cross-service | false | completed | true | 2 | 56475 | 0 | 463 | payment-service: go test -race -count=3 ./...: ?   	example.com/payment-service/… |
| docker-hardening | docker | true | completed | false | 3 | 28394 | 0 | 282 |  |
| event-field-rename | cross-service-discovery | true | completed | true | 2 | 25219 | 0 | 237 |  |
| go-bugfix-ledger-balance | go-bugfix | true | completed | false | 2 | 21674 | 0 | 183 |  |
| go-debug-ledger-balance-query | debugging | true | completed | false | 2 | 19090 | 0 | 178 |  |
| go-feature-currency-validation | go-feature | true | completed | true | 1 | 24722 | 0 | 199 |  |
| go-iface-add-method | interface-change | true | completed | false | 2 | 29073 | 0 | 244 |  |
| go-large-iface-method | interface-change-large-repo | false |  | false | 0 | 0 | 0 | 0 | materialize: source temporal-sdk: ~/go/pkg/mod/go.temporal.io/sdk@v1.49.… |
| go-refactor-validate | refactoring | true | completed | false | 3 | 33432 | 0 | 295 |  |
| java-bugfix-paid-total | java-bugfix | true | completed | true | 2 | 19024 | 0 | 144 |  |
| k8s-health-and-resources | kubernetes | true | completed | true | 1 | 18825 | 0 | 160 |  |
| multi-feature-discount | multi-language-feature | true | completed | false | 2 | 44077 | 0 | 558 |  |
| py-bugfix-inventory | python-bugfix | true | completed | true | 2 | 22684 | 0 | 189 |  |
| rust-bugfix-transfer | rust-bugfix | true | completed | true | 2 | 17887 | 0 | 128 |  |
| sql-migration-idempotency-key | database-migration | true | completed | true | 2 | 25637 | 0 | 273 |  |
| terraform-topics | terraform | true | completed | false | 2 | 20049 | 0 | 203 |  |
| ts-bugfix-cart-discount | typescript-bugfix | true | completed | false | 5 | 34585 | 0 | 326 |  |
| ts-retry-5xx | typescript | true | completed | false | 2 | 98594 | 0 | 1042 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | 1 | 1123 | 225 | 0 (0) | 0 | 4 | 42 | 0/1 | 0 | 24 |
| cross-service-idempotency | 2 | 7016 | 1204 | 0 (0) | 0 | 8 | 85 | 0/4 | 0 | 38 |
| docker-hardening | 3 | 4018 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 28 |
| event-field-rename | 2 | 2656 | 454 | 0 (0) | 0 | 4 | 45 | 0/2 | 0 | 19 |
| go-bugfix-ledger-balance | 2 | 2520 | 731 | 0 (0) | 0 | 4 | 42 | 0/2 | 0 | 14 |
| go-debug-ledger-balance-query | 2 | 2154 | 543 | 0 (0) | 0 | 4 | 38 | 0/2 | 0 | 16 |
| go-feature-currency-validation | 1 | 754 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 18 |
| go-iface-add-method | 2 | 3352 | 514 | 0 (0) | 0 | 6 | 65 | 0/2 | 0 | 22 |
| go-large-iface-method | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 0 |
| go-refactor-validate | 3 | 6266 | 1477 | 0 (0) | 0 | 6 | 66 | 0/3 | 0 | 27 |
| java-bugfix-paid-total | 2 | 3136 | 1696 | 0 (0) | 0 | 8 | 85 | 0/4 | 0 | 14 |
| k8s-health-and-resources | 1 | 731 | 0 | 0 (0) | 0 | 2 | 14 | 0/0 | 0 | 17 |
| multi-feature-discount | 2 | 4671 | 1222 | 0 (0) | 0 | 12 | 132 | 0/6 | 0 | 29 |
| py-bugfix-inventory | 2 | 2700 | 436 | 0 (0) | 0 | 4 | 44 | 0/2 | 0 | 19 |
| rust-bugfix-transfer | 2 | 1605 | 0 | 0 (0) | 0 | 2 | 14 | 0/0 | 0 | 11 |
| sql-migration-idempotency-key | 2 | 1939 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 22 |
| terraform-topics | 2 | 1740 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 17 |
| ts-bugfix-cart-discount | 5 | 5855 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 33 |
| ts-retry-5xx | 2 | 2590 | 671 | 0 (0) | 0 | 4 | 42 | 0/2 | 0 | 37 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | go | 13 | 199 | 46 | 152 | 4 (4) | 13 | 1 | 652 | 28346 | not recorded | not recorded |
| cross-service-idempotency | go | 22 | 395 | 85 | 308 | 6 (5) | 21 | 1 | 537 | 28383 | verification_false_pass | not recorded |
| docker-hardening | dockerfile | 29 | 232 | 66 | 163 | 5 (4) | 19 | 1 | 532 | 28611 | verification missed | not recorded |
| event-field-rename | go | 20 | 173 | 49 | 122 | 7 (7) | 24 | 2 | 394 | 28611 | not recorded | not recorded |
| go-bugfix-ledger-balance | go | 13 | 144 | 39 | 104 | 4 (4) | 12 | 1 | 496 | 28506 | verification missed | not recorded |
| go-debug-ledger-balance-query | go | 16 | 140 | 40 | 99 | 4 (4) | 11 | 2 | 412 | 28506 | verification missed | not recorded |
| go-feature-currency-validation | go | 13 | 154 | 41 | 113 | 2 (2) | 13 | 1 | 477 | 28439 | not recorded | not recorded |
| go-iface-add-method | go | 17 | 196 | 52 | 144 | 4 (4) | 15 | 1 | 466 | 28967 | verification missed | not recorded |
| go-large-iface-method | go | 0 | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 28967 | environment | not recorded |
| go-refactor-validate | go | 27 | 238 | 70 | 167 | 5 (4) | 19 | 1 | 525 | 28983 | verification missed | not recorded |
| java-bugfix-paid-total | java | 15 | 117 | 40 | 76 | 4 (4) | 4 | 1 | 366 | 29611 | not recorded | not recorded |
| k8s-health-and-resources | yaml | 12 | 116 | 33 | 82 | 2 (2) | 12 | 1 | 651 | 28871 | not recorded | not recorded |
| multi-feature-discount | multi | 17 | 522 | 55 | 465 | 4 (4) | 1 | 1 | 429 | 29045 | verification missed | not recorded |
| py-bugfix-inventory | python | 20 | 163 | 49 | 113 | 4 (4) | 2 | 1 | 287 | 29045 | not recorded | not recorded |
| rust-bugfix-transfer | rust | 10 | 103 | 31 | 71 | 4 (4) | 3 | 1 | 319 | 27864 | not recorded | not recorded |
| sql-migration-idempotency-key | sql | 21 | 227 | 47 | 179 | 4 (4) | 17 | 1 | 458 | 28378 | not recorded | not recorded |
| terraform-topics | terraform | 19 | 180 | 44 | 134 | 4 (4) | 2 | 1 | 286 | 28536 | verification missed | not recorded |
| ts-bugfix-cart-discount | typescript | 30 | 286 | 74 | 211 | 7 (4) | 8 | 1 | 294 | 29598 | verification missed | not recorded |
| ts-retry-5xx | typescript | 40 | 882 | 137 | 741 | 4 (4) | 5 | 3 | 344 | 29598 | verification missed | not recorded |

Environment: build dev 8e4caea75a51; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server 0.00.000.210 I srv  llama_server: initializing ... | version: 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks c25975cd2d9b5657.
