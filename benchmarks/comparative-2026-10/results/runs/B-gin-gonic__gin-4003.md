# Engineering benchmark `suite-20261009T192629Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 19:26Z · finished 2026-10-09 19:34Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 7.93 |
| attempts / successful task | 2.00 |
| wall-clock / task | 454 s |
| processed local tokens / task (uncached prompt + generated) | 60900 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-4003 | go-dev | true | completed | true | 2 | 60900 | 0 | 454 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-4003 | 2 | 7112 | 4610 | 0 (0) | 0 | 10 | 86 | 0/2 | 0 | 35 |
