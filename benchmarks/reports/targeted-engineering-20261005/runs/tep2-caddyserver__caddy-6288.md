# Engineering benchmark `suite-20261005T185722Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 18:57Z · finished 2026-10-05 19:39Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 2517 s |
| processed local tokens / task (uncached prompt + generated) | 271812 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| caddyserver__caddy-6288 | devops-infra | false | completed | false | 2 | 271812 | 0 | 2517 | caddy: sh -c go test -v ./caddytest/integration -run "TestCaddyfileAdapt*": …:… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| caddyserver__caddy-6288 | 2 | 5023 | 538 | 2 (0) | 7724 | 2 | 18 | 0/0 | 0 | 81 |
