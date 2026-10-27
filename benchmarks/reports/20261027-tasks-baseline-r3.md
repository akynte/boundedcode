# Engineering benchmark `suite-20261027T155311Z`

model: `qwen3.6-35b-a3b` · started 2026-10-27 15:53Z · finished 2026-10-27 17:48Z

| metric | value |
|---|---|
| tasks | 19 |
| verified (hidden checks) | 16 (84%) |
| local-only completion rate | 84% |
| frontier escalation rate | 0% |
| verified tasks / hour | 8.33 |
| attempts / successful task | 2.06 |
| wall-clock / task | 364 s |
| processed local tokens / task (uncached prompt + generated) | 40408 |
| self-verified but hidden checks failed | 2 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | api-contract | true | completed | false | 2 | 76632 | 0 | 501 |  |
| cross-service-idempotency | cross-service | false | completed | true | 2 | 95392 | 0 | 589 | ledger-service: go test ./...: --- FAIL: TestHiddenDuplicateEvent (0.00s) |
| docker-hardening | docker | true | completed | false | 2 | 22161 | 0 | 227 |  |
| event-field-rename | cross-service-discovery | true | completed | true | 2 | 24195 | 0 | 227 |  |
| go-bugfix-ledger-balance | go-bugfix | true | completed | false | 2 | 26810 | 0 | 399 |  |
| go-debug-ledger-balance-query | debugging | false | completed | true | 2 | 21366 | 0 | 273 | ledger-service: sh .bench/check_tests_unchanged.sh: …internal/ledger/ledger_te… |
| go-feature-currency-validation | go-feature | true | completed | true | 1 | 22423 | 0 | 182 |  |
| go-iface-add-method | interface-change | true | completed | false | 2 | 38177 | 0 | 350 |  |
| go-large-iface-method | interface-change-large-repo | false |  | false | 0 | 0 | 0 | 0 | materialize: source temporal-sdk: ~/go/pkg/mod/go.temporal.io/sdk@v1.49.… |
| go-refactor-validate | refactoring | true | completed | false | 2 | 25242 | 0 | 218 |  |
| java-bugfix-paid-total | java-bugfix | true | completed | true | 2 | 19450 | 0 | 157 |  |
| k8s-health-and-resources | kubernetes | true | completed | true | 1 | 21986 | 0 | 185 |  |
| multi-feature-discount | multi-language-feature | true | completed | false | 2 | 31119 | 0 | 281 |  |
| py-bugfix-inventory | python-bugfix | true | completed | true | 3 | 151708 | 0 | 1724 |  |
| rust-bugfix-transfer | rust-bugfix | true | completed | true | 2 | 16419 | 0 | 111 |  |
| sql-migration-idempotency-key | database-migration | true | completed | false | 2 | 21740 | 0 | 222 |  |
| terraform-topics | terraform | true | completed | false | 2 | 18800 | 0 | 187 |  |
| ts-bugfix-cart-discount | typescript-bugfix | true | completed | false | 4 | 31134 | 0 | 282 |  |
| ts-retry-5xx | typescript | true | completed | false | 2 | 102991 | 0 | 796 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | 2 | 5296 | 655 | 0 (0) | 0 | 8 | 88 | 0/3 | 0 | 42 |
| cross-service-idempotency | 2 | 8146 | 1296 | 0 (0) | 0 | 8 | 86 | 0/4 | 0 | 41 |
| docker-hardening | 2 | 2076 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 21 |
| event-field-rename | 2 | 2620 | 454 | 0 (0) | 0 | 4 | 40 | 0/2 | 0 | 19 |
| go-bugfix-ledger-balance | 2 | 2499 | 731 | 0 (0) | 0 | 4 | 45 | 0/2 | 0 | 13 |
| go-debug-ledger-balance-query | 2 | 2032 | 543 | 0 (0) | 0 | 4 | 47 | 0/2 | 0 | 9 |
| go-feature-currency-validation | 1 | 724 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 16 |
| go-iface-add-method | 2 | 3369 | 526 | 0 (0) | 0 | 6 | 63 | 0/2 | 0 | 34 |
| go-large-iface-method | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 0 |
| go-refactor-validate | 2 | 3516 | 986 | 0 (0) | 0 | 4 | 42 | 0/2 | 0 | 15 |
| java-bugfix-paid-total | 2 | 3133 | 1696 | 0 (0) | 0 | 8 | 93 | 0/4 | 0 | 15 |
| k8s-health-and-resources | 1 | 728 | 0 | 0 (0) | 0 | 2 | 14 | 0/0 | 0 | 18 |
| multi-feature-discount | 2 | 4626 | 1220 | 0 (0) | 0 | 12 | 130 | 0/6 | 0 | 25 |
| py-bugfix-inventory | 3 | 3777 | 649 | 0 (0) | 0 | 6 | 56 | 0/3 | 0 | 98 |
| rust-bugfix-transfer | 2 | 1437 | 0 | 0 (0) | 0 | 2 | 15 | 0/0 | 0 | 10 |
| sql-migration-idempotency-key | 2 | 1892 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 22 |
| terraform-topics | 2 | 1698 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 15 |
| ts-bugfix-cart-discount | 4 | 4158 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 33 |
| ts-retry-5xx | 2 | 2681 | 717 | 0 (0) | 0 | 4 | 42 | 0/2 | 0 | 56 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | go | 31 | 443 | 120 | 320 | 8 (8) | 18 | 3 | 546 | 28983 | verification missed | not recorded |
| cross-service-idempotency | go | 21 | 516 | 133 | 381 | 6 (4) | 22 | 3 | 549 | 28115 | verification_false_pass | not recorded |
| docker-hardening | dockerfile | 17 | 189 | 43 | 145 | 4 (4) | 18 | 1 | 596 | 28413 | verification missed | not recorded |
| event-field-rename | go | 16 | 162 | 45 | 117 | 7 (7) | 23 | 1 | 528 | 28844 | not recorded | not recorded |
| go-bugfix-ledger-balance | go | 12 | 359 | 33 | 325 | 4 (4) | 11 | 1 | 342 | 28844 | verification missed | not recorded |
| go-debug-ledger-balance-query | go | 10 | 232 | 30 | 201 | 4 (4) | 11 | 1 | 424 | 28337 | verification_false_pass | not recorded |
| go-feature-currency-validation | go | 11 | 136 | 35 | 100 | 2 (2) | 13 | 1 | 580 | 28337 | not recorded | not recorded |
| go-iface-add-method | go | 26 | 297 | 72 | 223 | 4 (4) | 16 | 2 | 472 | 28977 | verification missed | not recorded |
| go-large-iface-method | go | 0 | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 28977 | environment | not recorded |
| go-refactor-validate | go | 14 | 167 | 44 | 121 | 4 (4) | 18 | 1 | 519 | 28977 | verification missed | not recorded |
| java-bugfix-paid-total | java | 16 | 131 | 42 | 88 | 4 (4) | 4 | 1 | 286 | 28066 | not recorded | not recorded |
| k8s-health-and-resources | yaml | 14 | 139 | 38 | 100 | 2 (2) | 12 | 1 | 567 | 27958 | not recorded | not recorded |
| multi-feature-discount | multi | 19 | 243 | 53 | 188 | 4 (4) | 1 | 1 | 501 | 28633 | verification missed | not recorded |
| py-bugfix-inventory | python | 104 | 1656 | 261 | 1385 | 4 (4) | 3 | 6 | 318 | 28633 | not recorded | not recorded |
| rust-bugfix-transfer | rust | 10 | 87 | 31 | 55 | 4 (4) | 3 | 1 | 298 | 26728 | not recorded | not recorded |
| sql-migration-idempotency-key | sql | 19 | 175 | 45 | 129 | 4 (4) | 17 | 1 | 529 | 28264 | verification missed | not recorded |
| terraform-topics | terraform | 16 | 164 | 39 | 124 | 4 (4) | 2 | 1 | 292 | 28271 | verification missed | not recorded |
| ts-bugfix-cart-discount | typescript | 30 | 245 | 71 | 172 | 6 (4) | 7 | 1 | 316 | 29230 | verification missed | not recorded |
| ts-retry-5xx | typescript | 41 | 694 | 154 | 536 | 4 (4) | 5 | 3 | 329 | 29672 | verification missed | not recorded |

Environment: build dev 8e4caea75a51; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server 0.00.000.172 I srv  llama_server: initializing ... | version: 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks c25975cd2d9b5657.
