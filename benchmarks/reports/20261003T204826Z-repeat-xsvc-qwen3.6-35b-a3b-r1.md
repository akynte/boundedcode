# Engineering benchmark `suite-20261003T204826Z`

model: `qwen3.6-35b-a3b` · started 2026-10-03 20:48Z · finished 2026-10-03 21:05Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 1022 s |
| processed local tokens / task (uncached prompt + generated) | 157203 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | cross-service | false | completed | true | 2 | 157203 | 0 | 1022 | payment-service: go test -race -count=3 ./...: ?   	example.com/payment-service/… |
