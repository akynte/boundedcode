# Retrospective: caddyserver__caddy-6288 (false pass)

Run: BoundedCode task `t20261004-325967`, 1 attempt, status `completed`/`full_pass`, hidden acceptance FAILED.
Archive: `~/.cache/boundedcode/bench-validation/archive/frozen-caddyserver__caddy-6288/`. Event numbers `#NNNNN` refer to
`state/data/tasks/t20261004-325967/runtime/82c7a3…/events/event-NNNNN-*.json`; `db:events.id` to `state.db`.

Empirical probes in this report ran on a scratch copy of the base tree (`go test`, host module cache, `GOPROXY=off`),
using the five Caddyfile inputs listed in section 3. No product file, archive or Docker was touched.

## Summary of findings

* **The agent's patch does nothing.** Base, agent patch and gold were run on the same five inputs. The agent patch
  gives the same result as base on all five; gold fixes the two snippet cases.
* **The stated inputs already work at base.** The issue's two inputs (top-level heredoc and top-level backtick)
  already adapt correctly at base. The bug only shows up when the shorthand appears inside a snippet or import.
* **Neither run reproduced the issue.** The BoundedCode agent and the baseline both diagnosed a heredoc/same-line
  problem that does not exist. Neither ran the reported input, partly because **Go dependencies were missing from the
  agent sandbox** while verification had them.
* **Verification only checked that nothing broke.** It ran build/vet/test and nothing showed that the patch changes
  behaviour, so a no-op patch passed.
* **A correction to the existing failure note.** The BoundedCode agent did **not** write its own
  `expression_quotes.caddyfiletest`. It only viewed the file (#00080). Its only edits are #00038, #00046 and #00048,
  in `httptype.go` and `lexer.go`. The test hunk in `agent.patch` is the hidden test patch applied on top for
  acceptance. The baseline's `agent.patch` contains a byte-identical hunk. So the row in
  `small-real-world-validation-20261004/failure-notes.md` that says "the agent's own expression_quotes.caddyfiletest"
  is wrong, and so is its label `MODEL_CODING`.

## 1. Task statement and what the hidden test requires

**What the issue says.** The issue reports this error:
"`wrong argument count or unexpected line ending after 'expression'`".

It says the error happens for a named matcher whose expression is given as a heredoc (`@cel <<CEL … CEL`) or as a
backtick string (`` @cel `…` ``), without the explicit `expression` keyword. The explicit form works.

**What the hidden test adds.** The hidden test adds two cases to `caddytest/integration/caddyfile_adapt/expression_quotes.caddyfiletest`:

* `` @f `{…} == 404` `` at top level. This **already passes at base**.
* `` @g `{…} == 404` `` defined inside `(snippet) { … }` and pulled in with `import snippet`. This fails at
  `Caddyfile:2`, the snippet line.

**Probe results on the base tree.** The scratch probe adapted five inputs with `caddyfile.Adapter`:

| input shape | base | agent patch | gold |
|---|---|---|---|
| top-level backtick shorthand (issue example 2) | ok | ok | ok |
| top-level heredoc shorthand (issue example 1) | ok | ok | ok |
| snippet + backtick shorthand (hidden `@g`) | **ArgErr at Caddyfile:2** | **ArgErr** | ok |
| snippet + heredoc shorthand | **ArgErr** | **ArgErr** | ok |
| snippet + explicit `expression` | ok | ok | ok |

**Root cause (from the gold patch).** `parseMatcherDefinitions` (httptype.go:1434) builds a synthetic token
`{Text:"expression", File, Line}` with no `imports` chain. It then passes that token plus the real expression token
to `MatchExpression.UnmarshalCaddyfile`. That function calls `d.NextArg()`, which uses `isNextOnNewLine`
(lexer.go:362). `isNextOnNewLine` treats two tokens with different `imports` as being on different lines. A token
expanded from a snippet carries an import chain (parse.go:458-462), so `NextArg` returns false and `ArgErr` fires.
Hence the error mentions `'expression'`, a word the user never typed. Gold fixes this by building the synthetic
token with `Token.Clone()` of the real token, so the import chain carries over.

**Can the dataset case be derived from the issue?** Partially. The issue never mentions snippets or imports, and
its literal examples work at base. But two things point straight at the synthetic token:

* The error names `'expression'`, which the user did not type.
* There is exactly one place in non-test code that builds a `caddyfile.Token{…}` literal (httptype.go:1434-1435).

An agent that reproduced the issue first would have seen the examples pass. That should have forced the question
"what context differs?". `isNextOnNewLine`, which the agent read in full at #00020, answers it: File, imports, Line.

## 2. Retrieved context: **PARTIAL + NOISY**

The context pack is `db:events.id=4` and is rendered in #00001. It was 1,735 tokens: TASK 362, RELEVANT CODE 1,195,
RULES 178.

**Retrieval stats.** `nav_calls 1, nav_symbols 0` (Serena), `graph_calls 1, graph_symbols 0` (codebase-memory),
`lexical_calls 1, lexical_symbols 1`. The single identifier extracted from the request was `` `expression` ``.
`identRE` in contextplan/plan.go only takes backticked identifiers and CamelCase.

**Lexical results.** The search gave 10 whole-word hits for `expression`:

* **Relevant (3 hits).** httptype.go:1429, 1433, 1434. These cover the exact code that gold changes
  (the synthetic token at 1434-1436).
* **Noise (7 hits).** admin.go:1269 (a comment, "regular expression"), builtins.go:758/772/773, and three JSON
  lines in error_multi_site_blocks.caddyfiletest.

**What was missing:**

* where the error string comes from: `Dispenser.ArgErr`, dispenser.go:400, `"wrong argument count or unexpected line ending after '%s'"`;
* the consumer `MatchExpression.UnmarshalCaddyfile` (celmatcher.go:212-226, `if !d.NextArg() { return d.ArgErr() }`);
* the rule that decides "same line": `isNextOnNewLine`, which compares `imports`;
* the code that sets `imports` during import/snippet expansion (parse.go:458-462);
* `Token.Clone` did not exist yet; the gold adds it.

**Which layer failed:**

* **Planner (CONTEXT_PLANNING).** The request has a distinctive error string inside a fenced code block, and that
  string is a fixed-string match for exactly one line in the repo (dispenser.go:400). The planner does not extract
  error messages or quoted log lines as lexical queries.
* **Graph and Serena** returned 0 symbols for the lowercase common word `expression`. That is expected, since it is
  not a symbol name.

The pack was a lucky partial hit: the edit site was there, but the mechanism was not. The agent fetched the
mechanism files itself (lexer.go #00020, celmatcher.go #00026, dispenser.go #00030), so **retrieval was not the
deciding failure**. The agent had `isNextOnNewLine` with the `imports` check in front of it and did not use it.

## 3. Agent trajectory (Qwen3.6-35B-A3B via OpenHands, 105 events, 1 condensation at #00082)

**Exploration (#00002-#00031).** The agent opened the task tracker, then httptype.go:1410-1500 (#00010), the whole
of lexer.go (#00020), grepped for `expression` (#00024), and read celmatcher.go (#00026) and dispenser.go (#00030).

**Contradictory evidence, then a wrong hypothesis (#00022-#00038).** The agent noticed several times that
`Quoted()` already returns true for heredocs (`wasQuoted='<'`, "So `Quoted()` should return true for heredoc
tokens" in #00030, #00032 and #00034). A `think` call failed with malformed JSON (#00033). It then settled without
evidence on "NextArg() returns false because heredoc content spans to a new line" (#00034, #00038). It never
considered the backtick case, even though the issue says backticks fail too and its own theory cannot explain that.
It never considered snippets or imports.

**Edits.** There were three edits and no new files:

* #00038: httptype.go uses the unexported `d.Token().heredocMarker`. This would not compile across packages. The
  agent noticed by reasoning (#00040, #00044), not by building.
* #00046: lexer.go adds `Token.IsHeredoc()` (`wasQuoted == '<'`).
* #00048: httptype.go adds `if d.Token().IsHeredoc() {…}` before `NextArg`, and `|| d.Token().IsHeredoc()` inside it.
  Both are dead code:
  * At that point `d.Token()` is the definition name (`@name`), which is never a heredoc.
  * `IsHeredoc()` is a strict subset of `Quoted()`.

  So the patch has no effect on behaviour, which the probe table confirms.

**Testing.** No test was run and no test was written.

* `go build ./...` failed: "module lookup disabled by GOPROXY=off" (#00053).
* The agent found an empty module cache, `/home/agent/go/pkg/mod` containing only `cache` (#00059-#00063).
* Retrying with a proxy hit "network is unreachable" (#00067). `go vet` failed the same way (#00069).
* Only `gofmt -l` succeeded (#00071, #00098).
* It viewed the existing `expression_quotes.caddyfiletest` (#00080) and `matcher_syntax.caddyfiletest` (#00084), and
  said it would "create a test for heredoc expressions" (#00083), but never did.
* #00101 marks "Run tests to verify the fix" as **done**, and the finish message (#00103) claims a root cause.

**Input shapes missed.** The agent covered no shapes with a test. Its hypothesis covered only the top-level heredoc,
which already works. It missed:

* the backtick shorthand, which is stated in the issue;
* the snippet/import context, which is the real failure;
* nested `handle`/route blocks (directives.go:390, the second caller).

**Why.** It never reproduced the issue. Doing so was impossible in the sandbox (no Go modules), and the model did not
treat that as a blocker. It reported success anyway.

## 4. Impact analysis of the modified symbols (base repo)

**`parseMatcherDefinitions`** (httptype.go:1386).

* Callers: httptype.go:111 (site-block segments, including tokens expanded from `import`) and directives.go:390
  (matcher definitions inside directive blocks, via `NewDispenser(seg)`).
* Consumer of the token slice it builds: `makeMatcher` → `MatchExpression.UnmarshalCaddyfile` → `Dispenser.NextArg`
  → `nextOnSameLine` → `isNextOnNewLine`.
* Related tests: `expression_quotes.caddyfiletest` (top level only), `matcher_syntax.caddyfiletest` (`@matcher7`
  inline shortcut, top level), and `import_args_snippet*.caddyfiletest` / `import_args_file.caddyfiletest`
  (snippet/import contexts, but no shorthand matcher).

**`Token`** (lexer.go:40; `IsHeredoc` was added).

* Non-test struct literals: only httptype.go:1434.
* Readers of `imports`: `isNextOnNewLine` (lexer.go:362) and parse.go:458-469.
* Readers of `wasQuoted`: parse.go:579/586 and `NumLineBreaks`.

**Would generic impact expansion have exposed the missed case?**

* **Callers alone: no.** Both callers are listed, but neither one points to "snippet".
* **Data flow / field invariants: yes.** "Where a value of type T is built from a partial struct literal, list the
  functions that read T's other fields." That would show `isNextOnNewLine` reading `File`/`imports`/`Line`, and that
  the literal at httptype.go:1435 leaves `imports` empty. That is the gold fix exactly.
* **Context cross-product: yes.** "Existing tests of the same function or format, crossed with the contexts covered
  by sibling fixtures." Combining `expression_quotes` shapes with `import_args_snippet` contexts produces exactly the
  hidden `@g` case.
* **Prediction versus reality.** Impact analysis of the agent's own diff would predict "no behaviour change". The
  added branches are unreachable or subsumed, a cheap signal that the diff cannot fix anything. The hidden test showed
  the change was needed in the code path the diff did not touch: the fields of the synthetic token.
* **BoundedCode's `impactSection` was never used.** It (contextplan/plan.go:324) only feeds retry attempts, and there
  was no retry because verification passed.

## 5. Verification: why a no-op patch passed

| run | scope | stages | result | duration |
|---|---|---|---|---|
| 1 | targeted | diff-scope, secret-scan, gofmt, go build ./..., go vet (19 pkgs), go test -skip <55 base-failing tests> | pass | 99.5 s |
| 2 | full | same + go vet ./..., golangci-lint (optional, skipped: not installed) | pass | 94.2 s |

`go test` passed, including `caddytest/integration` (1.14 s). The suite contains only top-level expression cases,
which pass at base and still pass. `acceptance_criteria` was `[]` and no task-derived check ran. The policy decision
was "merge candidate: all verification passed".

**False-pass chain.** The issue says shorthand expression matchers fail → verified: the existing suite still passes
(it passes at base too) → hidden: the snippet-imported shorthand still throws ArgErr → missing signal: no
reproduction of the issue that fails at base and passes on the candidate.

**Categories, in order:**

1. **Insufficient task-derived acceptance criteria / missing regression test generation.** Nothing turned the
   issue's literal Caddyfile examples plus its error string into an executable check. Run on base, that check would
   have *passed*, which itself shows the diagnosis was incomplete before any code was written.
2. **Missing behavioural check.** No stage checks that some test outcome differs between base and candidate. A
   candidate with zero behavioural delta passed.
3. **Model claiming success without proof.** #00101 marks "Run tests" done after every test run failed for missing
   modules.
4. **Incomplete diff review.** Unreachable and subsumed branches were not flagged.

The verification config is not at fault. The 55 skipped tests are server-start tests that fail at base; the relevant
adapt test ran. Test selection is not the gap either: `TestCaddyfileAdaptToJSON` ran, but it had no failing case.

## 6. Baseline (raw OpenHands, same model)

**What it changed.** `Dispenser.nextOnSameLine` now returns true whenever the next token is a heredoc. It also added
`TestDispenser_NextArg_Heredoc` (dispenser_test.go).

**Same failure mode.** It reached the same wrong heredoc/same-line diagnosis from the same lexical entry point
(`grep -n expression httptype.go`, step 18).

**Same blocker.** Running `go test` failed with `GOPROXY=off` and network unreachable (steps 58-64). It then tried to
simulate the lexer in a hand-copied `/tmp/test_heredoc.go` / `test_fix.go` (steps 70-85). That reimplementation
"showed" a bug that does not exist in the real code.

**Riskier change.** Its change alters dispenser semantics for every directive (a heredoc on the next line now becomes
an argument). It does not touch the import chain, so the snippet case still fails.

**Timing.** 52 model calls, 787.6 s total.

## 7. Time breakdown (BoundedCode)

| part | time |
|---|---|
| total wall time | 1,750 s (29.2 min) |
| context pack | 5.75 s (Serena nav 5.68 s; graph 5 ms; lexical 6 ms) |
| agent session | 13:52:19 → 14:18:09, 25.8 min |
| model calls | 52, total_ms 1,533.6 s (prompt 193.2 s, decode 1,335.7 s) |
| tokens | 1.96 M prompt, 43.3 k completion, max prompt 63 k |
| verification | targeted 99.5 s + full 94.2 s = 193.7 s, 11% of wall time; each run includes ~17 s gofmt over all files and ~18-35 s build |
| frontier escalations | none |

Decode time dominates. Roughly 12 calls (#00052-#00075) were spent on failed attempts to build and test in a sandbox
with no modules.

## 8. Classification

**Primary causes:**

* **VERIFICATION_GAP.** The task was accepted with no task-derived reproduction and no behavioural-delta check. The
  issue's literal inputs plus its error string were directly executable, and a passing run of them at base would have
  been decisive evidence that the diagnosis was wrong. Evidence: `acceptance_criteria=[]`; stages in
  `verification_runs` 1 and 2; probe table.
* **OTHER: agent and verifier environments differ.** Verification gets `GOMODCACHE` (internal/verify/verify.go:531,
  internal/cli/task.go:135-138). The agent sandbox gets only `node_modules` mounts (internal/sandbox/deps.go:16,
  internal/agent/openhands/runtime.go:111-114). So the agent could not compile, test or reproduce anything
  (#00053-#00069). The baseline hit the same wall.

**Secondary causes:**

* **MODEL_REASONING.** The model held a hypothesis that contradicted its own observations (`Quoted()` is true for
  heredocs, #00030-#00036) and ignored the backtick half of the issue. It shipped dead code and claimed tests ran
  (#00101).
* **IMPACT_ANALYSIS_MISS.** The partial `Token` literal and the field readers of its zero-valued fields were never
  examined. The no-op diff was never detected.
* **CONTEXT_PLANNING (minor).** The error string was not used as a retrieval query. The pack was 7/10 noise. This was
  not decisive, because the agent read the mechanism files itself.

**Not primary:**

* RETRIEVAL_MISS: the edit site was in the pack.
* RETRY_POLICY: there was no failure to retry on.
* FRONTIER: not involved.
* MODEL_CODING: the code compiles and is "correct" for its wrong goal.

## 9. Generic product improvements, ranked

These are not specific to caddy.

1. **Reproduce-first acceptance check.** Before implementation, the planner or agent turns the request into an
   executable check: inputs or commands quoted in the request, plus the expected or forbidden error text. The check
   runs **on base and must fail**.
   * If it passes on base, report "stated reproduction does not reproduce", feed that into the context pack
     ("the literal inputs work; find the differing context"), and do not accept a patch until a failing reproduction
     exists.
   * After the patch, the same check must pass. Record it in `acceptance_criteria` so verification enforces it.
2. **Behavioural-delta gate in verification.** Require at least one test (agent-written or task-derived) that fails on
   base and passes on the candidate (fail-to-pass). As a fallback, treat "no test outcome changed between base and
   candidate" as *unverified*, not *pass*. This needs only a base run of the selected tests, and it would have caught
   both this no-op and the baseline.
3. **Make the agent's environment match the verifier's.** Mount the same language dependency caches that
   verification uses (`GOMODCACHE` read-only with `GOFLAGS=-mod=mod GOPROXY=off`; the same idea for Cargo, Maven, pip)
   into the agent sandbox. Do a preflight `go build`/`test --list` before the attempt. If the agent cannot run tests,
   say so in the context pack and refuse "tests done" claims that have no successful test command in the event log.
4. **Data-flow impact expansion.** For each changed function, list:
   * callers;
   * existing tests and fixtures of the same function or format;
   * for struct literals in or near the diff, the functions that read the literal's *omitted* fields.

   Flag diffs whose added branches are unreachable or subsumed (cheap static checks such as "the condition implies an
   existing earlier condition"). Offer an impact pass at self-verification time, not only on retry.
5. **Retrieval of error strings and code blocks.** Extract quoted error messages and log lines from the request's
   fenced blocks and run fixed-string ripgrep (with format placeholders like `%s` stripped) to find where the error
   is raised and its callers. Down-rank lexical hits in comments and test JSON for common English words.
6. **Context cross-product test generation.** When the agent writes a regression test from fixture-style suites (for
   example `*.caddyfiletest`, golden files, snapshot tests), suggest crossing the new case with contexts used by
   sibling fixtures: include/import, nesting, other files. This is a generic "where else can this construct appear"
   prompt.
