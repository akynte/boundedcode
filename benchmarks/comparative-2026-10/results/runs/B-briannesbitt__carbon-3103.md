# Engineering benchmark `suite-20261009T212506Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 21:25Z · finished 2026-10-09 21:43Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 3.35 |
| attempts / successful task | 2.00 |
| wall-clock / task | 1076 s |
| processed local tokens / task (uncached prompt + generated) | 117683 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| briannesbitt__carbon-3103 | php | true | completed | false | 2 | 117683 | 0 | 1076 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| briannesbitt__carbon-3103 | 2 | 6042 | 2271 | 0 (0) | 0 | 14 | 151 | 0/6 | 0 | 44 |
