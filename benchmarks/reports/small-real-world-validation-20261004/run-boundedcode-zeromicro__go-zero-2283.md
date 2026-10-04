# Engineering benchmark `suite-20261004T195333Z`

model: `qwen3.6-35b-a3b` · started 2026-10-04 19:53Z · finished 2026-10-04 21:21Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 100% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 5255 s |
| processed local tokens / task (uncached prompt + generated) | 587340 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| zeromicro__go-zero-2283 | go | false | blocked | false | 6 | 587340 | 2 | 5255 | go-zero: sh -c go test -v ./rest -run '^(TestEngine_checkedMaxBytes/TestEngine_c… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| zeromicro__go-zero-2283 | 6 | 38552 | 12819 | 108 (30) | 61488 | 0 | 0 | 36/0 | 0 | 186 |
