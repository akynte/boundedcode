# Engineering benchmark `suite-20261028T184602Z`

model: `qwen3.6-35b-a3b` · started 2026-10-28 18:46Z · finished 2026-10-28 20:11Z

| metric | value |
|---|---|
| tasks | 19 |
| verified (hidden checks) | 18 (95%) |
| local-only completion rate | 95% |
| frontier escalation rate | 0% |
| verified tasks / hour | 12.68 |
| attempts / successful task | 1.89 |
| wall-clock / task | 269 s |
| processed local tokens / task (uncached prompt + generated) | 27382 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | api-contract | true | completed | false | 2 | 40996 | 0 | 452 |  |
| cross-service-idempotency | cross-service | true | completed | true | 2 | 83137 | 0 | 604 |  |
| docker-hardening | docker | true | completed | false | 2 | 19269 | 0 | 214 |  |
| event-field-rename | cross-service-discovery | true | completed | true | 3 | 27367 | 0 | 305 |  |
| go-bugfix-ledger-balance | go-bugfix | true | completed | false | 2 | 20001 | 0 | 175 |  |
| go-debug-ledger-balance-query | debugging | false | completed | true | 2 | 19632 | 0 | 222 | ledger-service: sh .bench/check_tests_unchanged.sh: …{AccountID: "a", AmountCe… |
| go-feature-currency-validation | go-feature | true | completed | true | 1 | 17682 | 0 | 157 |  |
| go-iface-add-method | interface-change | true | completed | false | 2 | 20829 | 0 | 204 |  |
| go-large-iface-method | interface-change-large-repo | true | completed | false | 2 | 48260 | 0 | 371 |  |
| go-refactor-validate | refactoring | true | completed | false | 2 | 29616 | 0 | 336 |  |
| java-bugfix-paid-total | java-bugfix | true | completed | true | 2 | 19227 | 0 | 183 |  |
| k8s-health-and-resources | kubernetes | true | completed | true | 1 | 19630 | 0 | 181 |  |
| multi-feature-discount | multi-language-feature | true | completed | true | 1 | 22482 | 0 | 258 |  |
| py-bugfix-inventory | python-bugfix | true | completed | true | 2 | 22781 | 0 | 235 |  |
| rust-bugfix-transfer | rust-bugfix | true | completed | true | 2 | 15122 | 0 | 118 |  |
| sql-migration-idempotency-key | database-migration | true | completed | false | 2 | 24925 | 0 | 279 |  |
| terraform-topics | terraform | true | completed | false | 2 | 23623 | 0 | 272 |  |
| ts-bugfix-cart-discount | typescript-bugfix | true | completed | true | 2 | 15413 | 0 | 129 |  |
| ts-retry-5xx | typescript | true | completed | false | 2 | 30264 | 0 | 416 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | 2 | 4968 | 652 | 0 (0) | 0 | 8 | 83 | 0/3 | 0 | 37 |
| cross-service-idempotency | 2 | 7664 | 1273 | 0 (0) | 0 | 8 | 91 | 0/4 | 0 | 40 |
| docker-hardening | 2 | 2092 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 19 |
| event-field-rename | 3 | 5038 | 691 | 0 (0) | 0 | 6 | 68 | 0/3 | 0 | 28 |
| go-bugfix-ledger-balance | 2 | 2487 | 731 | 0 (0) | 0 | 4 | 41 | 0/2 | 0 | 14 |
| go-debug-ledger-balance-query | 2 | 2088 | 543 | 0 (0) | 0 | 4 | 41 | 0/2 | 0 | 19 |
| go-feature-currency-validation | 1 | 728 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 13 |
| go-iface-add-method | 2 | 3358 | 514 | 0 (0) | 0 | 6 | 64 | 0/2 | 0 | 15 |
| go-large-iface-method | 2 | 6890 | 3184 | 0 (0) | 0 | 20 | 255 | 0/10 | 0 | 29 |
| go-refactor-validate | 2 | 3413 | 986 | 0 (0) | 0 | 4 | 55 | 0/2 | 0 | 32 |
| java-bugfix-paid-total | 2 | 3138 | 1696 | 0 (0) | 0 | 8 | 89 | 0/4 | 0 | 19 |
| k8s-health-and-resources | 1 | 748 | 0 | 0 (0) | 0 | 2 | 15 | 0/0 | 0 | 20 |
| multi-feature-discount | 1 | 1212 | 510 | 0 (0) | 0 | 6 | 60 | 0/3 | 0 | 24 |
| py-bugfix-inventory | 2 | 2846 | 436 | 0 (0) | 0 | 4 | 37 | 0/2 | 0 | 27 |
| rust-bugfix-transfer | 2 | 1506 | 0 | 0 (0) | 0 | 2 | 18 | 0/0 | 0 | 10 |
| sql-migration-idempotency-key | 2 | 1927 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 23 |
| terraform-topics | 2 | 1668 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 26 |
| ts-bugfix-cart-discount | 2 | 1351 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 13 |
| ts-retry-5xx | 2 | 2756 | 751 | 0 (0) | 0 | 4 | 37 | 0/2 | 0 | 28 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | go | 28 | 384 | 72 | 309 | 8 (8) | 18 | 2 | 559 | 29673 | verification missed | not recorded |
| cross-service-idempotency | go | 25 | 515 | 118 | 393 | 6 (5) | 21 | 3 | 559 | 29085 | not recorded | not recorded |
| docker-hardening | dockerfile | 21 | 176 | 42 | 134 | 4 (4) | 16 | 1 | 439 | 27688 | verification missed | not recorded |
| event-field-rename | go | 28 | 226 | 62 | 162 | 11 (11) | 31 | 2 | 514 | 27688 | not recorded | not recorded |
| go-bugfix-ledger-balance | go | 13 | 137 | 34 | 102 | 4 (4) | 11 | 1 | 341 | 27460 | verification missed | not recorded |
| go-debug-ledger-balance-query | go | 20 | 177 | 42 | 134 | 4 (4) | 12 | 1 | 474 | 29191 | verification_false_pass | not recorded |
| go-feature-currency-validation | go | 11 | 112 | 30 | 81 | 2 (2) | 13 | 1 | 594 | 29191 | not recorded | not recorded |
| go-iface-add-method | go | 11 | 151 | 32 | 117 | 4 (4) | 16 | 1 | 529 | 29095 | verification missed | not recorded |
| go-large-iface-method | go | 21 | 256 | 77 | 177 | 4 (4) | 34 | 1 | 620 | 29095 | verification missed | not recorded |
| go-refactor-validate | go | 32 | 273 | 65 | 206 | 4 (4) | 17 | 1 | 540 | 29633 | verification missed | not recorded |
| java-bugfix-paid-total | java | 20 | 153 | 42 | 109 | 4 (4) | 4 | 1 | 286 | 29737 | not recorded | not recorded |
| k8s-health-and-resources | yaml | 13 | 136 | 34 | 101 | 2 (2) | 12 | 1 | 659 | 29736 | not recorded | not recorded |
| multi-feature-discount | multi | 17 | 206 | 41 | 164 | 2 (2) | 13 | 1 | 620 | 28343 | not recorded | not recorded |
| py-bugfix-inventory | python | 29 | 207 | 55 | 150 | 4 (4) | 3 | 2 | 285 | 28101 | not recorded | not recorded |
| rust-bugfix-transfer | rust | 10 | 91 | 28 | 62 | 4 (4) | 4 | 1 | 290 | 28101 | not recorded | not recorded |
| sql-migration-idempotency-key | sql | 23 | 233 | 48 | 184 | 4 (4) | 17 | 1 | 548 | 29614 | verification missed | not recorded |
| terraform-topics | terraform | 29 | 249 | 55 | 192 | 4 (4) | 2 | 1 | 288 | 29614 | verification missed | not recorded |
| ts-bugfix-cart-discount | typescript | 11 | 101 | 30 | 69 | 4 (4) | 5 | 1 | 287 | 27802 | not recorded | not recorded |
| ts-retry-5xx | typescript | 30 | 358 | 57 | 298 | 4 (4) | 5 | 2 | 328 | 29416 | verification missed | not recorded |

Environment: build dev f95dd3463ef6-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 99dd33b0a5b0a9a9.
