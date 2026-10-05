# Engineering benchmark `suite-20261005T223734Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 22:37Z · finished 2026-10-05 23:07Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 2.04 |
| attempts / successful task | 2.00 |
| wall-clock / task | 1768 s |
| processed local tokens / task (uncached prompt + generated) | 136815 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| prometheus__prometheus-10720 | large-repo | true | completed | false | 2 | 136815 | 0 | 1768 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| prometheus__prometheus-10720 | 2 | 2164 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 61 |
