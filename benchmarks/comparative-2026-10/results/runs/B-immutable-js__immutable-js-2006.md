# Engineering benchmark `suite-20261009T194527Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 19:45Z · finished 2026-10-09 20:01Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 3.68 |
| attempts / successful task | 3.00 |
| wall-clock / task | 979 s |
| processed local tokens / task (uncached prompt + generated) | 126596 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| immutable-js__immutable-js-2006 | jsts | true | completed | true | 3 | 126596 | 0 | 979 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| immutable-js__immutable-js-2006 | 3 | 3586 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 45 |
