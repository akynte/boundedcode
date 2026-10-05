# Engineering benchmark `suite-20261005T214033Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 21:40Z · finished 2026-10-05 22:02Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 2.77 |
| attempts / successful task | 1.00 |
| wall-clock / task | 1298 s |
| processed local tokens / task (uncached prompt + generated) | 78596 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| caddyserver__caddy-6370 | devops-infra | true | completed | true | 1 | 78596 | 0 | 1298 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| caddyserver__caddy-6370 | 1 | 1193 | 715 | 2 (0) | 6316 | 1 | 6 | 0/0 | 0 | 20 |
