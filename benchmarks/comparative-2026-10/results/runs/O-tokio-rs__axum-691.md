# Engineering benchmark `suite-20261009T204605Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 20:46Z · finished 2026-10-09 20:53Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 446 s |
| processed local tokens / task (uncached prompt + generated) | 60571 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| tokio-rs__axum-691 | rust | false | finished | false | 1 | 60571 | 0 | 446 | axum: …             ---------------------- expected because of this |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| tokio-rs__axum-691 | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 0 |
