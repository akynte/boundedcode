# Engineering benchmark `suite-20261004T212250Z`

model: `qwen3.6-35b-a3b` · started 2026-10-04 21:22Z · finished 2026-10-04 21:23Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 13 s |
| processed local tokens / task (uncached prompt + generated) | 0 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| zeromicro__go-zero-990 | difficult | false | active | false | 0 | 0 | 0 | 13 | go-zero: sh -c go test -v ./tools/goctl/util -run '^(TestIndex/TestReadLink/Test… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| zeromicro__go-zero-990 | 0 | 0 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 0 |
