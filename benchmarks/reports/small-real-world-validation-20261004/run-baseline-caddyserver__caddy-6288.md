# Engineering benchmark `suite-20261005T010614Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 01:06Z · finished 2026-10-05 01:20Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 835 s |
| processed local tokens / task (uncached prompt + generated) | 113717 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| caddyserver__caddy-6288 | devops-infra | false | finished | false | 1 | 113717 | 0 | 835 | caddy: …: Caddyfile input is not formatted; run 'caddy fmt --overwrite' to fix… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| caddyserver__caddy-6288 | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 0 |
