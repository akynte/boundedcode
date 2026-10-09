# Engineering benchmark `suite-20261009T220559Z`

model: `qwen3.6-35b-a3b` · started 2026-10-09 22:05Z · finished 2026-10-09 22:06Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 52 s |
| processed local tokens / task (uncached prompt + generated) | 840 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3820 | go-dev | false | blocked | false | 2 | 840 | 0 | 52 | gin: sh -c go test -v ./binding -run "^TestMapping": …tMappingTime |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| gin-gonic__gin-3820 | 2 | 11202 | 6196 | 0 (0) | 0 | 14 | 125 | 0/2 | 0 | 7 |
