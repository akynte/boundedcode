# Engineering benchmark `suite-20261009T211143Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 21:11Z · finished 2026-10-09 21:17Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 9.72 |
| attempts / successful task | 2.00 |
| wall-clock / task | 370 s |
| processed local tokens / task (uncached prompt + generated) | 46017 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| phpoffice__phpspreadsheet-3463 | php | true | completed | false | 2 | 46017 | 0 | 370 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| phpoffice__phpspreadsheet-3463 | 2 | 13825 | 11047 | 0 (0) | 0 | 20 | 293 | 0/10 | 0 | 19 |
