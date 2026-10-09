# Engineering benchmark `suite-20261009T205331Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 20:53Z · finished 2026-10-09 21:04Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 5.35 |
| attempts / successful task | 1.00 |
| wall-clock / task | 672 s |
| processed local tokens / task (uncached prompt + generated) | 62673 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| tokio-rs__axum-691 | rust | true | completed | true | 1 | 62673 | 0 | 672 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| tokio-rs__axum-691 | 1 | 1782 | 826 | 0 (0) | 0 | 6 | 75 | 0/2 | 0 | 28 |
