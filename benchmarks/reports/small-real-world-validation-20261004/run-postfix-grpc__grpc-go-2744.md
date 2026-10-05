# Engineering benchmark `suite-20261005T000407Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 00:04Z · finished 2026-10-05 00:33Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 1754 s |
| processed local tokens / task (uncached prompt + generated) | 211364 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| grpc__grpc-go-2744 | go | false | completed | true | 1 | 211364 | 0 | 1754 | grpc-go: sh -c go test -v ./credentials -run '^(TestAppendH2ToNextProtos/TestCli… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| grpc__grpc-go-2744 | 1 | 639 | 0 | 5 (0) | 4934 | 5 | 48 | 0/0 | 0 | 140 |
