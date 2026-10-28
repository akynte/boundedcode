# Engineering benchmark `suite-20261028T170432Z`

model: `qwen3.6-35b-a3b` · started 2026-10-28 17:04Z · finished 2026-10-28 18:46Z

| metric | value |
|---|---|
| tasks | 19 |
| verified (hidden checks) | 19 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 11.23 |
| attempts / successful task | 2.00 |
| wall-clock / task | 321 s |
| processed local tokens / task (uncached prompt + generated) | 37279 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | api-contract | true | completed | false | 2 | 46531 | 0 | 496 |  |
| cross-service-idempotency | cross-service | true | completed | true | 3 | 177800 | 0 | 1237 |  |
| docker-hardening | docker | true | completed | false | 2 | 24779 | 0 | 250 |  |
| event-field-rename | cross-service-discovery | true | completed | true | 3 | 30313 | 0 | 324 |  |
| go-bugfix-ledger-balance | go-bugfix | true | completed | false | 2 | 20078 | 0 | 185 |  |
| go-debug-ledger-balance-query | debugging | true | completed | false | 2 | 22625 | 0 | 215 |  |
| go-feature-currency-validation | go-feature | true | completed | true | 1 | 21083 | 0 | 163 |  |
| go-iface-add-method | interface-change | true | completed | false | 2 | 31892 | 0 | 289 |  |
| go-large-iface-method | interface-change-large-repo | true | completed | false | 2 | 85356 | 0 | 588 |  |
| go-refactor-validate | refactoring | true | completed | false | 2 | 27581 | 0 | 250 |  |
| java-bugfix-paid-total | java-bugfix | true | completed | true | 2 | 20192 | 0 | 161 |  |
| k8s-health-and-resources | kubernetes | true | completed | true | 2 | 25653 | 0 | 212 |  |
| multi-feature-discount | multi-language-feature | true | completed | true | 1 | 28214 | 0 | 291 |  |
| py-bugfix-inventory | python-bugfix | true | completed | true | 2 | 22531 | 0 | 180 |  |
| rust-bugfix-transfer | rust-bugfix | true | completed | true | 2 | 16507 | 0 | 117 |  |
| sql-migration-idempotency-key | database-migration | true | completed | false | 2 | 22396 | 0 | 214 |  |
| terraform-topics | terraform | true | completed | false | 2 | 19283 | 0 | 180 |  |
| ts-bugfix-cart-discount | typescript-bugfix | true | completed | true | 2 | 16525 | 0 | 118 |  |
| ts-retry-5xx | typescript | true | completed | false | 2 | 48962 | 0 | 620 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | 2 | 4786 | 652 | 0 (0) | 0 | 8 | 84 | 0/3 | 0 | 38 |
| cross-service-idempotency | 3 | 15190 | 1814 | 0 (0) | 0 | 12 | 134 | 0/6 | 0 | 73 |
| docker-hardening | 2 | 2092 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 27 |
| event-field-rename | 3 | 4812 | 691 | 0 (0) | 0 | 6 | 67 | 0/3 | 0 | 29 |
| go-bugfix-ledger-balance | 2 | 2435 | 731 | 0 (0) | 0 | 4 | 43 | 0/2 | 0 | 16 |
| go-debug-ledger-balance-query | 2 | 2050 | 543 | 0 (0) | 0 | 4 | 46 | 0/2 | 0 | 19 |
| go-feature-currency-validation | 1 | 729 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 14 |
| go-iface-add-method | 2 | 3254 | 526 | 0 (0) | 0 | 6 | 63 | 0/2 | 0 | 26 |
| go-large-iface-method | 2 | 6916 | 3183 | 0 (0) | 0 | 20 | 253 | 0/10 | 0 | 40 |
| go-refactor-validate | 2 | 3412 | 986 | 0 (0) | 0 | 4 | 40 | 0/2 | 0 | 22 |
| java-bugfix-paid-total | 2 | 3132 | 1696 | 0 (0) | 0 | 8 | 85 | 0/4 | 0 | 16 |
| k8s-health-and-resources | 2 | 2515 | 0 | 0 (0) | 0 | 4 | 45 | 0/0 | 0 | 20 |
| multi-feature-discount | 1 | 1195 | 510 | 0 (0) | 0 | 6 | 62 | 0/3 | 0 | 21 |
| py-bugfix-inventory | 2 | 2722 | 436 | 0 (0) | 0 | 4 | 44 | 0/2 | 0 | 19 |
| rust-bugfix-transfer | 2 | 1522 | 0 | 0 (0) | 0 | 2 | 15 | 0/0 | 0 | 9 |
| sql-migration-idempotency-key | 2 | 1893 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 22 |
| terraform-topics | 2 | 1615 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 15 |
| ts-bugfix-cart-discount | 2 | 1333 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 13 |
| ts-retry-5xx | 2 | 2716 | 744 | 0 (0) | 0 | 4 | 42 | 0/2 | 0 | 26 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | go | 34 | 426 | 85 | 339 | 8 (8) | 20 | 2 | 586 | 28727 | verification missed | not recorded |
| cross-service-idempotency | go | 50 | 1123 | 258 | 859 | 10 (9) | 32 | 5 | 622 | 29685 | not recorded | not recorded |
| docker-hardening | dockerfile | 24 | 211 | 52 | 158 | 4 (4) | 17 | 1 | 484 | 28249 | verification missed | not recorded |
| event-field-rename | go | 30 | 243 | 68 | 173 | 11 (11) | 32 | 1 | 501 | 27699 | not recorded | not recorded |
| go-bugfix-ledger-balance | go | 16 | 145 | 41 | 102 | 4 (4) | 11 | 1 | 342 | 28914 | verification missed | not recorded |
| go-debug-ledger-balance-query | go | 19 | 172 | 47 | 124 | 4 (4) | 11 | 1 | 540 | 28914 | verification missed | not recorded |
| go-feature-currency-validation | go | 11 | 120 | 35 | 83 | 2 (2) | 12 | 1 | 549 | 27959 | not recorded | not recorded |
| go-iface-add-method | go | 20 | 234 | 57 | 176 | 4 (4) | 16 | 1 | 487 | 27721 | verification missed | not recorded |
| go-large-iface-method | go | 36 | 423 | 146 | 274 | 4 (4) | 33 | 3 | 1063 | 28209 | verification missed | not recorded |
| go-refactor-validate | go | 21 | 189 | 54 | 134 | 4 (4) | 17 | 1 | 602 | 29252 | verification missed | not recorded |
| java-bugfix-paid-total | java | 17 | 133 | 45 | 87 | 4 (4) | 4 | 1 | 406 | 29252 | not recorded | not recorded |
| k8s-health-and-resources | yaml | 16 | 160 | 44 | 115 | 4 (4) | 17 | 1 | 629 | 28041 | not recorded | not recorded |
| multi-feature-discount | multi | 18 | 241 | 46 | 193 | 2 (2) | 13 | 1 | 479 | 27512 | not recorded | not recorded |
| py-bugfix-inventory | python | 18 | 153 | 46 | 106 | 4 (4) | 3 | 1 | 284 | 28072 | not recorded | not recorded |
| rust-bugfix-transfer | rust | 10 | 90 | 29 | 60 | 4 (4) | 4 | 1 | 767 | 28219 | not recorded | not recorded |
| sql-migration-idempotency-key | sql | 22 | 169 | 50 | 117 | 4 (4) | 16 | 1 | 454 | 28219 | verification missed | not recorded |
| terraform-topics | terraform | 14 | 157 | 37 | 119 | 4 (4) | 2 | 1 | 285 | 28131 | verification missed | not recorded |
| ts-bugfix-cart-discount | typescript | 11 | 91 | 33 | 58 | 4 (4) | 5 | 1 | 316 | 27954 | not recorded | not recorded |
| ts-retry-5xx | typescript | 24 | 562 | 69 | 492 | 4 (4) | 5 | 1 | 333 | 29673 | verification missed | not recorded |

Environment: build dev f95dd3463ef6-dirty; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 99dd33b0a5b0a9a9.
