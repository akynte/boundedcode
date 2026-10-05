# Engineering benchmark `suite-20261005T100838Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 10:08Z · finished 2026-10-05 10:31Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 2.63 |
| attempts / successful task | 1.00 |
| wall-clock / task | 1367 s |
| processed local tokens / task (uncached prompt + generated) | 103919 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| prometheus__prometheus-13845 | large-repo | true | completed | true | 1 | 103919 | 0 | 1367 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| prometheus__prometheus-13845 | 1 | 4115 | 3077 | 19 (2) | 27029 | 1 | 6 | 5/0 | 0 | 43 |
