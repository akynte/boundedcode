# Direct-mode suite, first series (2026-10-26)

Five runs of the direct-mode suite (`bcode bench direct`,
[benchmarks/direct](../../benchmarks/direct/README.md): 13 everyday tasks,
hidden checks or expected answers) with Qwen3.6-35B-A3B (UD-Q4_K_M, llama.cpp
b11146 CUDA, 128K context, experts offloaded to system RAM) on the reference
laptop (RTX 4060 8 GB, 64 GB RAM, i7-13620H), approval `auto`, one task after
another on an otherwise idle machine. Reports: `benchmarks/reports/20261026T*-direct-*`.

Each run adds one change to the previous build; the last two runs share a
build, which sizes the run-to-run noise.

| Run | Build | Passed | Model calls | Tool calls | Prompt tokens | Cache hits | Generated | Tool results | s/task | Failed |
|---|---|---|---|---|---|---|---|---|---|---|
| baseline | stable prefix, working set, checks | 11/13 | 5.2 | 6.7 | 26.8K | 85% | 848 | 7.7 KB | 33 | fix-balance-query, large-change-txtar |
| seedpack | + request names looked up, streaming | 11/13 | 4.9 | 5.9 | 25.5K | 88% | 851 | 5.8 KB | 32 | fix-balance-query, large-change-txtar |
| projectmap | + project map on the first message | 12/13 | 4.4 | 5.3 | 23.2K | 87% | 769 | 5.6 KB | 29 | large-change-txtar |
| fixcause | + "fix the cause, not the symptom" rule | 13/13 | 4.5 | 5.4 | 26.1K | 87% | 781 | 6.0 KB | 30 | – |
| fixcause, repeat | same build | 12/13 | 5.1 | 5.6 | 27.3K | 89% | 837 | 5.8 KB | 31 | large-change-txtar |

Per task and per category figures, the agent's patch and its answer are in
the JSON reports; `bcode bench direct --compare OLD.json NEW.json` renders
two runs side by side.

## What the series shows

- **The prompt cache holds across turns.** 85–89% of prompt tokens are
  served from llama-server's cache in every run. Before the stable prefix
  (environment and mode framing moved out of the system prompt) the whole
  history was re-prefilled every turn; this is what makes a 30-second task
  possible at 777 prompt tokens per second.
- **Looking things up before the model asks cuts exploration.** The seed
  pack (names in the request looked up in the project) and the project map
  (build system, layout, test command on the first message) took tool calls
  per task from 6.7 to 5.3–5.6 and tool-result bytes from 7.7 KB to about
  5.8 KB. The locate tasks went from 4 and 5 tool calls to 1–2 and 2–4
  (13 and 43 s to 9 and 19–30 s).
- **Noise.** Two runs of the same build differ by 0.6 model calls, 0.2
  tool calls, 1.2K prompt tokens and 1 s per task, and by one task in the
  pass count. Differences smaller than that between adjacent runs are not
  evidence; the trend across the series is.
- **The two failures are model mistakes, and one of them is gone.**
  `fix-balance-query` plants two faults in one condition (`!=` and an
  extra currency clause); the baseline and seed-pack runs fixed only the
  operator, which passes the visible test and fails the hidden one. It
  passed in the three runs since the project map, including both with the
  fix-the-cause rule. `large-change-txtar` asks for a pointer into the
  archive; the model returns a pointer to its range-loop copy in four runs
  of five. No deterministic check can tell the two apart; this is the kind
  of mistake bounded mode's behavioural evidence exists for.
- **The post-change check caught one broken edit** (a syntax error in the
  refactor task) in two runs, before any test run.
- **Nothing was set aside or summarised**: every task stayed under the
  32K working set. Sessions below exercise setting results aside.

## Sessions (second series, same day)

Two `session` tasks were added: several messages in one conversation, each
routed on its own and with its own expected answer, checks after the last.
`session-ledger` (payment fixture: diagnose, fix, add a test, explain the
mistakes) and `session-x-tools` (`golang.org/x/tools`: four questions in
three packages, then a change that depends on the first answer, then a
question about earlier answers). Reports:
`benchmarks/reports/20261026T17*-direct-*-session*`.

| Run | Build | Result | Model calls | Tool calls | Prompt tokens | Cache hits | Largest prompt | Set aside | s |
|---|---|---|---|---|---|---|---|---|---|
| session-x-tools, 32K working set | router fix | pass | 19 | 15 | 295K | 92% | 24.7K | 0 | 155 |
| session-x-tools, 16K working set | router fix | pass | 19 | 15 | 220K | 89% | 16.4K | 5 | 159 |
| session-x-tools, 32K, full-suite run | router fix | pass | 21 | 16 | 277K | 93% | 22.3K | 0 | 187 |
| session-ledger, two runs | revised task | pass, pass | 10, 10 | 9, 9 | 60K, 62K | 92% | 7.6K | 0 | 69, 76 |

- **A long session stays correct when older results are set aside.**
  With the working set halved to 16K, five older tool results were set
  aside, the largest prompt fell from 24.7K to 16.4K tokens and the
  session's prompt tokens from 295K to 220K, in the same time and with the
  same answers. The model did not call `recall`; where it needed a file
  again it re-read it. One run per setting: this shows the path works on
  a real session, not that it is faster.
- **The change that depends on an earlier answer was right in every run**:
  `Inspector.Len` counts nodes, not events (an Inspector holds a push and
  a pop per node, which the first answer established).
- **Routing.** The first session run showed two router mistakes, both
  fixed: "Do not change anything yet" was read as a request for a change,
  and "In go/ast/astutil, what can Apply do …?" as a change (the question
  came after a scene-setting clause, the first sentence was cut at the dot
  in `ast.Inspect`, and "Apply" is a change verb). Since the fix every
  question turn is routed as a question.
- **Task corrections, stated plainly.** The first runs scored a task's
  `expect` against the last turn's answer instead of the first's (a
  harness bug; that report was discarded). The first `session-ledger`
  planted `AccountID != accountID || Currency == ""`, the exact negation of
  a defensible currency filter; the model fixed it to
  `== && Currency != ""` in two runs and argued for that reading. A task
  must not admit a reasonable alternative, so the second fault became
  `|| AmountCents < 0` (debits counted for every account), which the
  visible tests and the test asked for in the third turn both expose
  (`20261026T173037Z` still shows the old task). In the full-suite run the
  revised task's code passed the hidden test but its last answer, which
  named both mistakes correctly, said `ledger.go` rather than `Balance`;
  the expected answer now accepts either.

The 13 original tasks in the full-suite run (`20261026T174314Z`, router
fix, streaming refactor): 12/13, 4.5 model calls and 4.6 tool calls per
task, 23.1K prompt tokens, 88% cache hits, 661 generated tokens, 5.1 KB of
tool results, 26 s per task; `large-change-txtar` failed as before. Tool
calls are 0.7 below the previous best, more than the 0.2 noise between
identical builds; `diagnose-failing-test` is now routed as a question.

## Pitfall note (third series, same day)

`large-change-txtar` failed in 5 of 6 runs with the same code:
`for _, f := range a.Files { if f.Name == name { return &f, true } }`, a
pointer to the loop's copy. The post-change Go check now adds a note when
an edit introduces a `return &v` of a range value variable (not for a
slice the function built itself, not inside closures); the note says the
pointer is to a copy and how to return the element.

| Run | Result | Model calls | Notes |
|---|---|---|---|
| large-change-txtar, three runs | pass, pass, pass | 6, 8, 7 | 1 each; the model changed to `&a.Files[i]` every time |
| full suite (`20261026T*-pitfall-suite`) | 14/15 | 6.1 per task | 1, on large-change-txtar (passed) |

In the full suite the 13 original tasks passed 12/13 with 4.6 model calls,
5.0 tool calls, 24.4K prompt tokens and 29 s per task; the failure was
`fix-balance-query` fixing one of its two planted faults (it passed 4 of
the 5 runs before). The note fired on no other task. A first attempt at
this run had Go missing from the PATH, so every check failed; it was
discarded, and `bench direct` now refuses to start in that state.

The rule was sized on real code first: in 20,031 Go files (x/tools,
cobra, the module cache, the standard library) it finds 11 places, mostly
deliberate copies such as `crypto/tls` returning a certificate copy. Since
a note is only given for code the edit added, these never appear.

## Not measured yet

Cloud providers (their streaming is tested against recorded event
sequences only), repositories larger than `golang.org/x/tools`, and
sessions long enough to need `recall` rather than a re-read. The suite has
15 tasks; a pass-rate difference of one task is within noise.
