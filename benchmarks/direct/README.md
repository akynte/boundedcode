# Direct-mode suite

Everyday development tasks for the direct-mode agent, run with
`bcode bench direct`. Each task is one message to a fresh direct session in
a copy of its fixture, routed as the chat routes it (chat or code), on this
machine, with bounded mode unavailable. A change is judged by hidden checks
run in the checkout afterwards; a question by what the answer must say
(`expect`, case-insensitive regular expressions).

Categories: `explain` and `locate` (answers), `diagnose` (run the tests or
read an error and say what is wrong), `small-fix`, `add-test`, `refactor`,
`feature` (changes on the small `payment-platform` fixture), and
`large-locate` and `large-change` (the same kinds on `golang.org/x/tools`,
about 240K lines, copied from the Go module cache: run
`go mod download golang.org/x/tools@v0.50.0` once).

The report (`benchmarks/reports/<stamp>-direct-<model>.json` and `.md`)
records per task: pass or fail, model calls, tool calls by name, prompt
tokens and how many the server served from its cache, generated tokens,
tool-result bytes, results set aside and recalled, summaries, broken edits
caught by the post-change check, mode switches, and wall time.
