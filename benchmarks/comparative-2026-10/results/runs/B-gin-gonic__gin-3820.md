# Engineering benchmark `suite-20261009T183154Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 18:31Z · finished 2026-10-09 19:12Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 2454 s |
| processed local tokens / task (uncached prompt + generated) | 326657 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3820 | go-dev | false | completed | true | 3 | 326657 | 0 | 2454 | gin: sh -c go test -v ./binding -run "^TestMapping": …tMappingTime |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3820 | 3 | 15563 | 7490 | 0 (0) | 0 | 21 | 202 | 0/3 | 0 | 89 |
