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
  32K working set. The eviction and recall path is exercised by unit
  tests only until a longer session is measured.

## Not measured yet

Multi-turn sessions (where the working set and recall matter), cloud
providers, and repositories larger than `golang.org/x/tools`. The suite
has 13 tasks; a pass-rate difference of one task is within noise.
