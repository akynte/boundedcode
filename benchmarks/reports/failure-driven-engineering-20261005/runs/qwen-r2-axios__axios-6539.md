# Engineering benchmark `suite-20261005T144131Z`

model: `qwen3.6-35b-a3b` · started 2026-10-05 14:41Z · finished 2026-10-05 15:15Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 0 (0%) |
| local-only completion rate | 0% |
| frontier escalation rate | 0% |
| verified tasks / hour | 0.00 |
| attempts / successful task | 0.00 |
| wall-clock / task | 2069 s |
| processed local tokens / task (uncached prompt + generated) | 154478 |
| self-verified but hidden checks failed | 1 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | javascript | false | completed | true | 2 | 154478 | 0 | 2069 | axios: sh -c npx mocha -R tap test/unit/regression/SNYK-JS-AXIOS-7361793.js: …… |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| axios__axios-6539 | 2 | 17919 | 14394 | 22 (2) | 14865 | 8 | 58 | 6/0 | 0 | 57 |
