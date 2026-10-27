# Engineering benchmark `suite-20261027T124305Z`

model: `qwen3.6-35b-a3b` · started 2026-10-27 12:43Z · finished 2026-10-27 14:22Z

| metric | value |
|---|---|
| tasks | 19 |
| verified (hidden checks) | 16 (84%) |
| local-only completion rate | 84% |
| frontier escalation rate | 0% |
| verified tasks / hour | 9.65 |
| attempts / successful task | 2.00 |
| wall-clock / task | 314 s |
| processed local tokens / task (uncached prompt + generated) | 36558 |
| self-verified but hidden checks failed | 2 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | api-contract | true | completed | true | 1 | 38651 | 0 | 460 |  |
| cross-service-idempotency | cross-service | false | completed | true | 2 | 96609 | 0 | 613 | ledger-service: go test ./...: …r.go:44 +0x2b0 |
| docker-hardening | docker | true | completed | false | 2 | 22156 | 0 | 233 |  |
| event-field-rename | cross-service-discovery | true | completed | true | 1 | 23820 | 0 | 219 |  |
| go-bugfix-ledger-balance | go-bugfix | true | completed | true | 2 | 20261 | 0 | 185 |  |
| go-debug-ledger-balance-query | debugging | false | completed | true | 2 | 23382 | 0 | 239 | ledger-service: sh .bench/check_tests_unchanged.sh: …ing.T) { |
| go-feature-currency-validation | go-feature | true | completed | true | 1 | 26236 | 0 | 211 |  |
| go-iface-add-method | interface-change | true | completed | false | 2 | 25088 | 0 | 217 |  |
| go-large-iface-method | interface-change-large-repo | false |  | false | 0 | 0 | 0 | 0 | materialize: source temporal-sdk: ~/go/pkg/mod/go.temporal.io/sdk@v1.49.… |
| go-refactor-validate | refactoring | true | completed | false | 2 | 25952 | 0 | 217 |  |
| java-bugfix-paid-total | java-bugfix | true | completed | true | 2 | 19722 | 0 | 161 |  |
| k8s-health-and-resources | kubernetes | true | completed | true | 2 | 25395 | 0 | 218 |  |
| multi-feature-discount | multi-language-feature | true | completed | false | 2 | 97241 | 0 | 746 |  |
| py-bugfix-inventory | python-bugfix | true | completed | true | 2 | 62672 | 0 | 503 |  |
| rust-bugfix-transfer | rust-bugfix | true | completed | true | 2 | 17081 | 0 | 115 |  |
| sql-migration-idempotency-key | database-migration | true | completed | false | 2 | 29830 | 0 | 346 |  |
| terraform-topics | terraform | true | completed | false | 2 | 37485 | 0 | 302 |  |
| ts-bugfix-cart-discount | typescript-bugfix | true | completed | false | 5 | 55442 | 0 | 447 |  |
| ts-retry-5xx | typescript | true | completed | false | 2 | 47587 | 0 | 538 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | 1 | 1206 | 225 | 0 (0) | 0 | 4 | 45 | 0/1 | 0 | 26 |
| cross-service-idempotency | 2 | 7520 | 1204 | 0 (0) | 0 | 8 | 96 | 0/4 | 0 | 40 |
| docker-hardening | 2 | 2092 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 22 |
| event-field-rename | 1 | 1036 | 227 | 0 (0) | 0 | 2 | 15 | 0/1 | 0 | 20 |
| go-bugfix-ledger-balance | 2 | 2497 | 731 | 0 (0) | 0 | 4 | 47 | 0/2 | 0 | 13 |
| go-debug-ledger-balance-query | 2 | 2088 | 543 | 0 (0) | 0 | 4 | 41 | 0/2 | 0 | 15 |
| go-feature-currency-validation | 1 | 726 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 20 |
| go-iface-add-method | 2 | 3254 | 514 | 0 (0) | 0 | 6 | 61 | 0/2 | 0 | 18 |
| go-large-iface-method | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 0 |
| go-refactor-validate | 2 | 3493 | 986 | 0 (0) | 0 | 4 | 47 | 0/2 | 0 | 14 |
| java-bugfix-paid-total | 2 | 3139 | 1696 | 0 (0) | 0 | 8 | 93 | 0/4 | 0 | 17 |
| k8s-health-and-resources | 2 | 2542 | 0 | 0 (0) | 0 | 4 | 42 | 0/0 | 0 | 20 |
| multi-feature-discount | 2 | 4625 | 1220 | 0 (0) | 0 | 12 | 137 | 0/6 | 0 | 65 |
| py-bugfix-inventory | 2 | 2836 | 436 | 0 (0) | 0 | 4 | 38 | 0/2 | 0 | 51 |
| rust-bugfix-transfer | 2 | 1434 | 0 | 0 (0) | 0 | 2 | 22 | 0/0 | 0 | 10 |
| sql-migration-idempotency-key | 2 | 1907 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 27 |
| terraform-topics | 2 | 1696 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 22 |
| ts-bugfix-cart-discount | 5 | 5439 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 38 |
| ts-retry-5xx | 2 | 2796 | 758 | 0 (0) | 0 | 4 | 40 | 0/2 | 0 | 25 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | go | 21 | 410 | 57 | 352 | 4 (4) | 14 | 2 | 668 | 28073 | not recorded | not recorded |
| cross-service-idempotency | go | 25 | 542 | 138 | 402 | 6 (4) | 22 | 3 | 401 | 29216 | verification_false_pass | not recorded |
| docker-hardening | dockerfile | 23 | 189 | 51 | 137 | 4 (4) | 16 | 1 | 637 | 27746 | verification missed | not recorded |
| event-field-rename | go | 15 | 165 | 41 | 123 | 4 (4) | 17 | 1 | 558 | 28630 | not recorded | not recorded |
| go-bugfix-ledger-balance | go | 12 | 145 | 35 | 108 | 4 (4) | 12 | 1 | 579 | 28630 | not recorded | not recorded |
| go-debug-ledger-balance-query | go | 17 | 197 | 41 | 155 | 4 (4) | 12 | 1 | 342 | 28742 | verification_false_pass | not recorded |
| go-feature-currency-validation | go | 16 | 167 | 45 | 121 | 2 (2) | 12 | 1 | 576 | 28742 | not recorded | not recorded |
| go-iface-add-method | go | 16 | 168 | 43 | 123 | 4 (4) | 16 | 1 | 626 | 29138 | verification missed | not recorded |
| go-large-iface-method | go | 0 | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 29138 | environment | not recorded |
| go-refactor-validate | go | 14 | 166 | 44 | 121 | 4 (4) | 17 | 1 | 541 | 29138 | verification missed | not recorded |
| java-bugfix-paid-total | java | 17 | 135 | 45 | 89 | 4 (4) | 4 | 1 | 287 | 28277 | not recorded | not recorded |
| k8s-health-and-resources | yaml | 16 | 165 | 46 | 118 | 4 (4) | 17 | 1 | 578 | 28528 | not recorded | not recorded |
| multi-feature-discount | multi | 52 | 704 | 165 | 535 | 4 (4) | 1 | 3 | 500 | 29194 | verification missed | not recorded |
| py-bugfix-inventory | python | 46 | 474 | 122 | 348 | 4 (4) | 3 | 3 | 291 | 27898 | not recorded | not recorded |
| rust-bugfix-transfer | rust | 9 | 90 | 29 | 60 | 4 (4) | 4 | 1 | 291 | 26314 | not recorded | not recorded |
| sql-migration-idempotency-key | sql | 26 | 301 | 57 | 243 | 4 (4) | 16 | 1 | 483 | 28357 | verification missed | not recorded |
| terraform-topics | terraform | 23 | 277 | 68 | 208 | 4 (4) | 2 | 1 | 287 | 29035 | verification missed | not recorded |
| ts-bugfix-cart-discount | typescript | 37 | 409 | 108 | 297 | 7 (4) | 8 | 2 | 308 | 29553 | verification missed | not recorded |
| ts-retry-5xx | typescript | 25 | 440 | 75 | 364 | 4 (4) | 5 | 1 | 336 | 27756 | verification missed | not recorded |

Environment: build dev 8e4caea75a51; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server 0.00.000.241 I srv  llama_server: initializing ... | version: 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks c25975cd2d9b5657.
