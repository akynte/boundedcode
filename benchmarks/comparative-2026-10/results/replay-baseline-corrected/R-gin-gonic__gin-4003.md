# Engineering benchmark `suite-20261009T231401Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 23:14Z · finished 2026-10-09 23:14Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 66.53 |
| attempts / successful task | 2.00 |
| wall-clock / task | 54 s |
| processed local tokens / task (uncached prompt + generated) | 720 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-4003 | go-dev | true | blocked | false | 2 | 720 | 0 | 54 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-4003 | 2 | 7179 | 4614 | 0 (0) | 0 | 10 | 100 | 0/2 | 0 | 6 |
