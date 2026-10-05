# Retrospective: vuejs__core-11899 (post-fix rerun), a false pass

Outcome: BoundedCode reported success after 1 attempt, with both targeted and full verification passing. Hidden acceptance failed: 25 tests passed and 1 failed. The failing test was the dataset's fail-to-pass test `SFC scoped CSS > nesting selector with atrule and comment`.

Evidence roots:
- `ARCH` = `~/.cache/boundedcode/bench-validation/archive/postfix-vuejs__core-11899/`
- `EV` = `ARCH/state/data/tasks/t20261005-545fc1/runtime/ddd6b970.../events/`

The reproduction below was done in a scratch copy of the base repository. The agent patch was applied and only `compileStyle.spec.ts` plus a probe spec were run. No Docker was used and the agent was not run.

## 1. Task statement and hidden requirement

**The issue.** In scoped CSS, a rule can contain a nested rule (for example `.foo { background: red; .bar {} @media(...) { background: green } }`). When it does, Vue wraps the parent's declarations in `&[data-v-x] {}`. Declarations directly inside a nested `@media` or `@container` block are not wrapped. Because of that, the media query loses on specificity.

The issue says nothing about comments. Its examples contain no comments.

**The hidden test** (`hidden[0].patch`) uses this input:

```
h1 { color: red; /*background-color: pink;*/ @media ... { background-color: green; .bar{...} } .foo{...} }
```

It requires two things, matched as an exact string with `toMatch`:
- (a) The `@media` declarations get wrapped in `&[data-v-test]`. This is derivable from the issue.
- (b) The comment `/*background-color: pink;*/` moves into the host `&[data-v-test] { ... }` wrapper together with `color: red`. This is **not derivable** from the issue.

Point (b) changes behaviour that already existed on the base code's path for plain declarations. The pre-existing `decl`-only filter in `rewriteSelector` (pluginScoped.ts:234-248) already left comments outside the wrapper.

**The gold fix.** It extracts `extractAndWrapNodes()`, filters `decl || comment`, and applies it to the rule and to each child at-rule.

## 2. Retrieved context: MISSING + NOISY (failure in the retrieval layer, but harmless here)

The `context.pack` event shows:
- `graph_calls 5`, `graph_symbols 2`
- `nav_calls 3`, `nav_errors 2`, `nav_symbols 0`, `nav_ms 6131`
- `lexical_calls 1`, `lexical_symbols 0`
- `fallbacks 2`

The RELEVANT CODE section was 2,577 tokens and contained only graph lookups of `foo` and `bar`. These are CSS class names taken from the issue text, and the graph resolved them to unrelated test functions (`apiSetupContext.spec.setup` and others) (`EV/event-00001`).

`pluginScoped.ts`, `rewriteSelector` and `compileStyle.spec.ts` were all absent. Serena (TypeScript LSP) made 3 calls, 2 of them errors, and returned 0 symbols. Lexical search returned 0 symbols.

The failing layer is query/seed extraction. It treats identifier-like tokens inside code blocks and prose (`foo`, `bar`) as symbols and does not demote them when they resolve ambiguously (18 and 6 matches). It ignored the domain terms `scoped`, `@media`, `nesting selector` and `data-v-`.

**Impact was low.** The agent found `compiler-sfc/src/style/pluginScoped.ts` on its own with directory listings within 50 s (events 3-13). The agent made no Serena or graph tool calls itself; it used only `file_editor`, `terminal` and `think`.

## 3. Agent trajectory (events 2-99; 00:34:10 to 00:54:47)

**Exploration.** The agent listed `packages/compiler-sfc` and `src/style`. It then viewed `pluginScoped.ts`, correctly identifying the `rule.nodes.some(type==='rule')` branch, and viewed `compileStyle.spec.ts`. It also ran `git log`, which was useless because the history is squashed.

**First hypothesis (wrong layer).** The agent assumed that declarations inside `@media` form an "anonymous Rule". It added the wrapping logic to `processRule` (00:44:52). Its tests still failed. It added `console.log` debugging, but the output was swallowed.

At 00:49 it printed the PostCSS AST with `node -e`. That showed the declarations sit directly under the AtRule. The agent then moved the logic into the plugin's `AtRule` visitor (00:50:52) and reverted the `processRule` edit.

**Final source change** (24 lines in the `AtRule()` visitor). If the at-rule's parent is a rule that has a child `rule`, the visitor moves the at-rule's `decl` children into a new `Rule({selector:'&'})`. The changes compared with the gold fix:

| Aspect | Agent | Gold |
|---|---|---|
| Child types moved | `decl` only | `decl` and `comment` |
| Where the logic lives | A copy of the existing wrapper logic in a second location | One shared helper |
| Existing host-rule branch | Left untouched, so comments are still left outside the wrapper | Fixed through the same helper |
| `:deep` guard | None | Has one |

**Tests.** The agent added 3 inline-snapshot tests: `@media`, `@supports` and `@container`, each using the input shape `.foo { decl; .bar {} @X { decl } }`. None contains a comment. All three used the same shape: one declaration, an empty nested rule, and an at-rule holding one declaration.

When the snapshots mismatched, the agent changed the expected text to the actual output (event 85: "update the test snapshots to match the actual output"). It did check that the `&[data-v-test]` wrapper was present before doing this.

It ran `vitest` itself: `compileStyle.spec.ts` gave 28/28 passing, and `packages/compiler-sfc/__tests__/` gave 415 passed and 1 skipped (events 91-96).

**Process noise:**
- 3 empty responses, each followed by an environment nudge (events 24, 42, 47).
- 1 condensation (event 87).

**Archive artefact.** `ARCH/agent.patch` shows the hidden test hunk (blob `b764143`) instead of the agent's 3 tests (blob `610b03f` in event 98). The archive diff was apparently captured after the acceptance test overlay. The source hunk is the agent's.

## 4. Incomplete fix and impact analysis

Reproduction (agent patch plus hidden spec): the at-rule part of the hidden test matches exactly. The **only** mismatch is the placement of the comment:

```
  &[data-v-test] {
  color: red
+ }
  /*background-color: pink;*/
- }
```

A probe comparing agent and gold outputs on 5 inputs shows that the remaining differences are all non-semantic:
- Comment placement, in the host rule and inside the at-rule.
- Whitespace in raws.
- `:deep(.foo){...@media{decl}}`: the agent emits a redundant `& {}` and gold leaves the declarations bare. The rendered CSS is equivalent.
- Doubly nested at-rules are unhandled by both.

**Conclusion:** the agent's fix resolves the reported bug. The hidden test also fixes a formatting behaviour of comments that was never requested.

**Would generic impact expansion have revealed the gap?** Only partly. The three expansion options were:

- *Callers of the modified function.* No. `AtRule()` is a PostCSS visitor and has no callers.
- *Existing tests of the function.* No. None contain comments.
- *Sibling branches doing the same job.* Yes, this would have helped. The agent copied the `decl`-filter-and-wrap block. A "duplicated logic, so refactor or review both sites" check, combined with "enumerate the child node kinds the filter drops", would have raised the question of what happens to a `comment` child. A reviewer who sees `filter(type==='decl')` over PostCSS `ChildNode` (`decl | rule | atrule | comment`) has an obvious question to ask.

Even so, deciding that comments must move *into* the wrapper is a stylistic choice. An agent could reasonably have decided either way.

## 5. Verification: why the patch passed

Verification ran these stages (`verification_runs`):

| Stage | Targeted (67.6 s) | Full (104.7 s) |
|---|---|---|
| diff-scope | pass | pass |
| secret-scan | pass | pass |
| tsc | pass | pass |
| lint | pass | pass |
| vitest unit (repo-wide: 179 files, 3,227 tests passed) | pass | pass |
| build | not in this scope | pass |

The task's `acceptance_criteria` is `[]`.

**Why it passed.** The only behavioural oracle was the repository's existing suite plus the agent's own tests. Those tests were snapshots of the agent's own output, and their input shape contained no comments. Nothing compared the output against an independently derived expectation for input shapes outside what the agent itself picked.

**The chain:**
1. Requirement: wrap at-rule declarations in `&`.
2. Verified: lint, types, build, 3,227 existing tests and 3 self-snapshotted tests.
3. Rejected by the hidden test: exact-string output with a comment beside the declarations.
4. Missing signal: an input-variation or regression test that contains sibling node kinds such as comments.

**Categories:**
- **insufficient task-derived acceptance criteria** (primary within verification): criteria were empty.
- **missing regression test generation**: tests were snapshot-adopted, not specified independently.
- **incomplete diff review**: the duplicated logic and the decl-only filter were not questioned.

Test selection and verification config were adequate: the relevant spec file ran. Not a case of claiming success without proof: the agent did run tests.

**Caveat:** the missing signal is not derivable from the issue. No reasonable verification can guarantee this particular check.

## 6. Time breakdown (wall clock 1,419.6 s)

| Phase | Time |
|---|---|
| Setup to context pack | 9 s (Serena navigation 6.1 s of this, which produced nothing) |
| Agent loop | 1,237 s (00:34:10 to 00:54:47) |
| Model time | 48 calls, 1,215 s (prompt 167 s, decode 1,043 s; 34.9 k completion tokens, about 33 tok/s) |
| Four long reasoning turns | 542 s (203, 145, 103 and 91 s), 45% of model time |
| Tool execution | about 20 s |
| Wrong-layer detour (00:44:52 to 00:50:52) | about 6 min |
| Verification | 172 s (targeted 67.6 s, full 104.7 s), 12% of wall clock; full verification repeated tsc, lint and vitest |

No escalation and no retry.

## 7. Classification

- **Primary: OTHER (oracle over-specification / non-derivable requirement).** The patch fixes the issue as written. The single hidden failure concerns where a CSS comment is placed, which the issue never mentions and which has no effect on rendering.
- **Secondary: IMPACT_ANALYSIS_MISS.** The agent duplicated the `decl`-only wrapping logic instead of generalising the existing branch, and never asked which other child node kinds the filter drops. This is the only generic route that might have reached gold's behaviour.
- **Tertiary: VERIFICATION_GAP** (insufficient task-derived acceptance criteria and self-adopted snapshots).
- **Noted but not causal: RETRIEVAL_NOISE.** The pack was 100% noise, built from symbols seeded from CSS class names, but the agent recovered in 50 s. Also not causal: the MODEL_REASONING wrong-layer detour (about 6 min) and 3 empty responses.

## 8. Generic product improvements (ranked)

1. **Seed hygiene for retrieval.**
   - Do not treat tokens from fenced code or snippets as symbol seeds when the graph returns them as `ambiguous` with more than N matches, or when they resolve only to test files.
   - Add domain-term lexical search (ripgrep for distinctive phrases and identifiers such as `scoped` or `data-v-`) as a seed fallback.
   - Drop sections whose content is an `ambiguous` JSON error, instead of spending 2.6 k tokens on them.
   - The cheapest fix, and it applies to every repository.
2. **Diff-review impact prompt after edits.**
   - Detect newly added code that duplicates an existing block (clone detection on the diff against the same file).
   - Ask the agent to either unify the two or justify keeping them separate.
   - For any type-discriminating filter in the diff, list the union members it excludes.
   - Catches incomplete generalisations in any language.
3. **Anti-self-snapshot rule for agent-written tests.**
   - Flag tests whose expected values were edited to match observed output after a failure.
   - Require at least one input variant that mixes in sibling or edge node kinds before accepting the fix.
   - Record expected behaviour as targeted assertions rather than whole-output snapshots.
4. **Task-derived acceptance criteria when `acceptance_criteria` is empty.** Turn the issue's "expected vs actual" blocks into a runnable check and add the bug's input variants. Here that would mean `@container` and a non-trivial body.
5. **Verification de-duplication.** Full scope re-ran tsc, lint and vitest on an unchanged tree (+105 s). Reuse the targeted results when the tree hash matches and add only the extra stages.
6. **Evidence hygiene.** Capture `agent.patch` before the acceptance overlay. In this run the archived patch shows hidden-test content as if the agent wrote it.
