# Engineering benchmark `suite-20261009T221019Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 22:10Z · finished 2026-10-09 22:11Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 99 s |
| processed local tokens / task (uncached prompt + generated) | 720 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| tokio-rs__axum-691 | rust | false | blocked | false | 2 | 720 | 0 | 99 | axum: sh -c RUSTFLAGS=-Awarnings cargo test --package axum --lib -- routing::tes… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| tokio-rs__axum-691 | 2 | 7459 | 2879 | 0 (0) | 0 | 11 | 128 | 0/3 | 0 | 6 |
