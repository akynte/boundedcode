# Engineering benchmark `suite-20261009T221158Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 22:11Z · finished 2026-10-09 22:12Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 95.79 |
| attempts / successful task | 2.00 |
| wall-clock / task | 38 s |
| processed local tokens / task (uncached prompt + generated) | 720 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| phpoffice__phpspreadsheet-3463 | php | true | completed | false | 2 | 720 | 0 | 38 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| phpoffice__phpspreadsheet-3463 | 2 | 13809 | 11060 | 0 (0) | 0 | 20 | 291 | 0/10 | 0 | 6 |
