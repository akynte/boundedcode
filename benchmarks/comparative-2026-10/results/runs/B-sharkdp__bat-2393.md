# Engineering benchmark `suite-20261009T202000Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 20:20Z · finished 2026-10-09 20:37Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 3.36 |
| attempts / successful task | 1.00 |
| wall-clock / task | 1073 s |
| processed local tokens / task (uncached prompt + generated) | 63284 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| sharkdp__bat-2393 | rust | true | completed | true | 1 | 63284 | 0 | 1073 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| sharkdp__bat-2393 | 1 | 1230 | 595 | 0 (0) | 0 | 3 | 35 | 0/1 | 0 | 28 |
