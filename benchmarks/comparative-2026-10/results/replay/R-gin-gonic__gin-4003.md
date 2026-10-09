# Engineering benchmark `suite-20261009T220652Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 22:06Z · finished 2026-10-09 22:07Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 64.20 |
| attempts / successful task | 1.00 |
| wall-clock / task | 56 s |
| processed local tokens / task (uncached prompt + generated) | 480 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-4003 | go-dev | true | completed | true | 1 | 480 | 0 | 56 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-4003 | 1 | 3020 | 2302 | 0 (0) | 0 | 5 | 50 | 0/1 | 0 | 4 |
