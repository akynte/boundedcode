# Engineering benchmark `suite-20261005T222702Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 22:27Z · finished 2026-10-05 22:37Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 5.69 |
| attempts / successful task | 2.00 |
| wall-clock / task | 632 s |
| processed local tokens / task (uncached prompt + generated) | 60896 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| vuejs__core-11870 | typescript | true | completed | true | 2 | 60896 | 0 | 632 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| vuejs__core-11870 | 2 | 9531 | 5671 | 19 (2) | 10765 | 1 | 13 | 7/0 | 0 | 30 |
