# Engineering benchmark `suite-20261005T120556Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 12:05Z · finished 2026-10-05 12:13Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 451 s |
| processed local tokens / task (uncached prompt + generated) | 42322 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3227 | go | false | completed | true | 1 | 42322 | 0 | 451 | gin: sh -c go test -v . -run "^TestRedirect": === RUN   TestRedirectTrailingSlas… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3227 | 1 | 3860 | 3100 | 9 (0) | 5200 | 3 | 26 | 3/0 | 0 | 22 |
