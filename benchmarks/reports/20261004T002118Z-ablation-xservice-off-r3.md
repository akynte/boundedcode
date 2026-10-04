# Engineering benchmark `suite-20261004T002118Z`

model: `qwen3.6-35b-a3b` · started 2026-10-04 00:21Z · finished 2026-10-04 00:23Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 143 s |
| processed local tokens / task (uncached prompt + generated) | 16973 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| event-field-rename | cross-service-discovery | false | completed | true | 1 | 16973 | 0 | 143 | ledger-service: go test ./...: --- FAIL: TestHiddenConsumesSchemaV2 (0.00s) |
