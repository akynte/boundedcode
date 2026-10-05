# Engineering benchmark `suite-20261005T182009Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 18:20Z · finished 2026-10-05 18:51Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 1853 s |
| processed local tokens / task (uncached prompt + generated) | 137208 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | javascript | false | completed | true | 2 | 137208 | 0 | 1853 | axios: sh -c npx mocha -R tap test/unit/regression/SNYK-JS-AXIOS-7361793.js: …… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | 2 | 18267 | 13554 | 22 (2) | 7870 | 8 | 65 | 6/0 | 0 | 47 |
