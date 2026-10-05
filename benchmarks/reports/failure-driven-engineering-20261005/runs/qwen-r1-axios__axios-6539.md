# Engineering benchmark `suite-20261005T111315Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 11:13Z · finished 2026-10-05 12:00Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 1.28 |
| attempts / successful task | 2.00 |
| wall-clock / task | 2821 s |
| processed local tokens / task (uncached prompt + generated) | 187175 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | javascript | true | completed | true | 2 | 187175 | 0 | 2821 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | 2 | 18689 | 14394 | 22 (2) | 15183 | 8 | 67 | 6/0 | 0 | 71 |
