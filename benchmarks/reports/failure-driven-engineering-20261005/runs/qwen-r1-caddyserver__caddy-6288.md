# Engineering benchmark `suite-20261005T083735Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 08:37Z · finished 2026-10-05 10:08Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 5462 s |
| processed local tokens / task (uncached prompt + generated) | 568058 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| caddyserver__caddy-6288 | devops-infra | false | active | false | 2 | 568058 | 0 | 5462 | caddy: sh -c go test -v ./caddytest/integration -run "TestCaddyfileAdapt*": …:… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| caddyserver__caddy-6288 | 2 | 8527 | 2390 | 2 (0) | 7697 | 2 | 20 | 0/0 | 0 | 204 |
