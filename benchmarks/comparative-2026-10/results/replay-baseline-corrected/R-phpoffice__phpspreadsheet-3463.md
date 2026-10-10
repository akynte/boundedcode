# Engineering benchmark `suite-20261010T113755Z`

model: `qwen3.6-35b-a3b` · started 2026-10-10 11:37Z · finished 2026-10-10 11:38Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 98.51 |
| attempts / successful task | 2.00 |
| wall-clock / task | 37 s |
| processed local tokens / task (uncached prompt + generated) | 720 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| phpoffice__phpspreadsheet-3463 | php | true | completed | false | 2 | 720 | 0 | 37 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| phpoffice__phpspreadsheet-3463 | 2 | 13866 | 11082 | 0 (0) | 0 | 20 | 292 | 0/10 | 0 | 6 |
