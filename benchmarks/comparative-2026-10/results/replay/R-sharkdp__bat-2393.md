# Engineering benchmark `suite-20261009T220850Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 22:08Z · finished 2026-10-09 22:10Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 40.29 |
| attempts / successful task | 2.00 |
| wall-clock / task | 89 s |
| processed local tokens / task (uncached prompt + generated) | 720 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| sharkdp__bat-2393 | rust | true | completed | false | 2 | 720 | 0 | 89 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| sharkdp__bat-2393 | 2 | 3649 | 1206 | 0 (0) | 0 | 6 | 69 | 0/2 | 0 | 6 |
