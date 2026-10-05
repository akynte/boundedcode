# Engineering benchmark `suite-20261005T103125Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 10:31Z · finished 2026-10-05 11:13Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 2510 s |
| processed local tokens / task (uncached prompt + generated) | 233016 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| vuejs__core-11899 | typescript | false | completed | true | 2 | 233016 | 0 | 2510 | core: sh -c node_modules/.bin/vitest run packages/compiler-sfc/__tests__/compile… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| vuejs__core-11899 | 2 | 7325 | 3496 | 6 (4) | 9302 | 10 | 111 | 0/4 | 4 | 57 |
