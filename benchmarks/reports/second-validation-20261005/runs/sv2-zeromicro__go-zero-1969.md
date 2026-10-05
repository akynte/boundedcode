# Engineering benchmark `suite-20261005T220211Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 22:02Z · finished 2026-10-05 22:14Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 4.76 |
| attempts / successful task | 1.00 |
| wall-clock / task | 757 s |
| processed local tokens / task (uncached prompt + generated) | 94725 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| zeromicro__go-zero-1969 | go | true | completed | true | 1 | 94725 | 0 | 757 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| zeromicro__go-zero-1969 | 1 | 468 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 42 |
