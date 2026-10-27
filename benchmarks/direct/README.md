# Direct-mode suite

Everyday development tasks for the direct-mode agent, run with
`bcode bench direct`. Each task is one message (or, for a session, a few)
to a fresh direct session in a copy of its fixture, routed as the chat
routes it (chat or code), on this machine, with bounded mode unavailable. A
change is judged by hidden checks run in the checkout afterwards; a
question by what the answer must say
(`expect`, case-insensitive regular expressions).

Categories: `explain` and `locate` (answers), `diagnose` (run the tests or
read an error and say what is wrong), `small-fix`, `add-test`, `refactor`,
`feature` (changes on the small `payment-platform` fixture), and
`large-locate` and `large-change` (the same kinds on `golang.org/x/tools`,
about 240K lines, copied from the Go module cache: run
`go mod download golang.org/x/tools@v0.50.0` once), and `session`: several
messages in one conversation (`turns`, each routed on its own and with its
own `expect`), where a later turn depends on what an earlier one found and
the history outgrows the working set, so older tool results are set aside
and the agent must recall or re-read them. Checks run after the last turn.

`--working-set N` runs with a smaller (or larger) history budget than the
configuration's, to exercise setting results aside on purpose.

Each task names its `language` (`go`, `python`, `typescript`, `rust`,
`java`, `multi`); the `polyglot` fixture holds one small project per
language and a Go + TypeScript monorepo. A task whose checks need a
toolchain that is not installed (their first word, plus `requires`) is
skipped and reported as an environment gap, not run.

The report (`benchmarks/reports/<stamp>-direct-<model>.json` and `.md`)
records per task:
- the result: pass or fail, why it failed (model, timeout, interrupted,
  routing, environment, harness) and any stalls;
- calls and tokens: model calls with each call's figures (time to first
  token, prefill and decode time, cached and reasoning tokens), tool calls
  by name with their time, prompt tokens and the cache's share, generated
  tokens, tool-result bytes, the largest single prompt and what it was made
  of by section;
- time: model, tools and the harness's own work, split by phase;
- memory: peak memory of the harness, the model server and the GPU;
- the working set: results set aside and recalled, summaries, broken edits
  caught by the post-change check, mode switches, wall time;
- for a session: each turn's mode and answer.

The report also records the environment: build, machine, model and server,
toolchains, and hashes of the tasks and fixtures.

`bcode bench compare --base A.json --base B.json --base C.json --candidate
D.json …` compares runs. See docs/benchmarks/baseline-2026-10.md for the
baseline and how verdicts are made.
