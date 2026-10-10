# Engineering benchmark `suite-20261010T105247Z`

model: `qwen3.6-35b-a3b` · started 2026-10-10 10:52Z · finished 2026-10-10 11:33Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 2450 s |
| processed local tokens / task (uncached prompt + generated) | 720 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| immutable-js__immutable-js-2006 | jsts | false | blocked | false | 2 | 720 | 0 | 2450 | immutable-js: sh -c npx jest __tests__/Range.ts --verbose: … 2041.8 (2044.3) M… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| immutable-js__immutable-js-2006 | 2 | 2681 | 0 | 0 (0) | 0 | 0 | 0 | 0/0 | 0 | 6 |
