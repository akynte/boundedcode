# Engineering benchmark `suite-20261009T162441Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 16:24Z · finished 2026-10-09 16:55Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 1877 s |
| processed local tokens / task (uncached prompt + generated) | 221524 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3741 | go-dev | false | completed | true | 4 | 221524 | 0 | 1877 | gin: sh -c go test -v . -run "^TestColor": …791563081454577305/state/data/task… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3741 | 4 | 11368 | 2718 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 106 |
