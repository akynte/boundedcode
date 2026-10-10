# Engineering benchmark `suite-20261010T113456Z`

model: `qwen3.6-35b-a3b` · started 2026-10-10 11:34Z · finished 2026-10-10 11:37Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 20.17 |
| attempts / successful task | 1.00 |
| wall-clock / task | 178 s |
| processed local tokens / task (uncached prompt + generated) | 480 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| tokio-rs__axum-691 | rust | true | completed | true | 1 | 480 | 0 | 178 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| tokio-rs__axum-691 | 1 | 1664 | 828 | 0 (0) | 0 | 6 | 73 | 0/2 | 0 | 4 |
