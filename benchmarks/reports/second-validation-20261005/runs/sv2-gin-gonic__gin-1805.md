# Engineering benchmark `suite-20261005T212604Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 21:26Z · finished 2026-10-05 21:40Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 4.15 |
| attempts / successful task | 1.00 |
| wall-clock / task | 867 s |
| processed local tokens / task (uncached prompt + generated) | 77584 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-1805 | go | true | completed | true | 1 | 77584 | 0 | 867 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-1805 | 1 | 557 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 29 |
