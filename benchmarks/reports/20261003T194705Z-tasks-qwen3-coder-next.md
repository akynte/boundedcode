# Engineering benchmark `suite-20261003T194705Z`

model: `qwen3-coder-next` · started 2026-10-03 19:47Z · finished 2026-10-03 20:48Z

| metric | value |
|---|---|
| tasks | 11 |
| verified (hidden checks) | 10 (91%) |
| local-only completion rate | 91% |
| frontier escalation rate | 0% |
| verified tasks / hour | 9.84 |
| attempts / successful task | 1.00 |
| wall-clock / task | 333 s |
| processed local tokens / task (uncached prompt + generated) | 26074 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| api-contract-description | api-contract | true | completed | true | 1 | 59128 | 0 | 787 |  |
| cross-service-idempotency | cross-service | false | completed | true | 2 | 70069 | 0 | 798 | ledger-service: go test ./...: …r.go:44 +0x2b0 |
| docker-hardening | docker | true | completed | true | 1 | 14588 | 0 | 214 |  |
| go-bugfix-ledger-balance | go-bugfix | true | completed | true | 1 | 17528 | 0 | 255 |  |
| go-debug-ledger-balance-query | debugging | true | completed | true | 1 | 17845 | 0 | 219 |  |
| go-feature-currency-validation | go-feature | true | completed | true | 1 | 23602 | 0 | 323 |  |
| go-refactor-validate | refactoring | true | completed | true | 1 | 26695 | 0 | 408 |  |
| k8s-health-and-resources | kubernetes | true | completed | true | 1 | 23685 | 0 | 325 |  |
| sql-migration-idempotency-key | database-migration | true | completed | true | 1 | 10951 | 0 | 117 |  |
| terraform-topics | terraform | true | completed | true | 1 | 10937 | 0 | 102 |  |
| ts-retry-5xx | typescript | true | completed | true | 1 | 11789 | 0 | 111 |  |
