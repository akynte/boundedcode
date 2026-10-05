# Engineering benchmark `suite-20261005T140812Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 14:08Z · finished 2026-10-05 14:41Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 1998 s |
| processed local tokens / task (uncached prompt + generated) | 209279 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| vuejs__core-11899 | typescript | false | completed | true | 1 | 209279 | 0 | 1998 | core: sh -c node_modules/.bin/vitest run packages/compiler-sfc/__tests__/compile… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| vuejs__core-11899 | 1 | 3649 | 2577 | 3 (2) | 6141 | 5 | 55 | 0/2 | 2 | 77 |
