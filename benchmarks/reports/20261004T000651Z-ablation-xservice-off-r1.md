# Engineering benchmark `suite-20261004T000651Z`

model: `qwen3.6-35b-a3b` · started 2026-10-04 00:06Z · finished 2026-10-04 00:09Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 182 s |
| processed local tokens / task (uncached prompt + generated) | 18909 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| event-field-rename | cross-service-discovery | false | completed | true | 1 | 18909 | 0 | 182 | ledger-service: go test ./...: --- FAIL: TestHiddenConsumesSchemaV2 (0.00s) |
