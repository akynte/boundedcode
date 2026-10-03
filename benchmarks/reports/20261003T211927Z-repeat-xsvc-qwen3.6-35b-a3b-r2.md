# Engineering benchmark `suite-20261003T211927Z`

model: `qwen3.6-35b-a3b` · started 2026-10-03 21:19Z · finished 2026-10-03 21:33Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 863 s |
| processed local tokens / task (uncached prompt + generated) | 90583 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| cross-service-idempotency | cross-service | false | completed | true | 1 | 90583 | 0 | 863 | ledger-service: go test ./...: …r.go:56 +0x26a |
