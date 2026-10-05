# Engineering benchmark `suite-20261004T234808Z`

model: `qwen3.6-35b-a3b` · started 2026-10-04 23:48Z · finished 2026-10-05 00:03Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 3.86 |
| attempts / successful task | 1.00 |
| wall-clock / task | 933 s |
| processed local tokens / task (uncached prompt + generated) | 57527 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | javascript | true | completed | true | 1 | 57527 | 0 | 933 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | 1 | 8661 | 7197 | 11 (1) | 6366 | 4 | 19 | 3/0 | 0 | 29 |
