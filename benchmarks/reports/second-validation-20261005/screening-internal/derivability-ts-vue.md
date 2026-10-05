# Derivability screening: slot ts-vue

Date: 2026-10-05. Method: copied each base source (with node_modules) to a scratch directory under ~/.cache, applied the hidden test patch, and ran the acceptance file on base, then on base+gold. Scratch copies were deleted afterwards. None of the four hidden tests is a snapshot or exact-string test of generated code.

| ID | Base | Base+gold | Verdict |
|---|---|---|---|
| vuejs__core-11870 | 1 fail | 8/8 pass | YES |
| vuejs__core-11915 | 2 fail (one is knock-on) | 45 pass | YES |
| vuejs__core-11739 | 1 fail | 41 pass | YES |
| vuejs__core-11589 | 1 fail ('21' vs '12') | 79/79 pass | YES |

## vuejs__core-11870: renderList wraps shallowReactive array items as reactive
- **Issue:** `v-for` over a `shallowReactive` array gives items that are reactive (so refs inside get unwrapped). The expected result is that items stay raw.
- **Test:** `renderList(reactive([{foo:1}]), isReactive)` returns `[true]`, and `renderList(shallowReactive([{foo:1}]), isReactive)` returns `[false]`.
- **Checks:**
  - (a) On base the test fails with `[true]` vs `[false]`. With gold it passes.
  - (b) None. The reactive case is just the existing behaviour, kept.
  - (c) None. It is a plain boolean-array comparison.
  - (d) None. `renderList` and `isReactive` are existing APIs.
  - (e) Only one outcome is allowed, and it is the one the issue asks for.
  - (f) None.
- **Verdict: YES.** The test is the issue's own reproduction, reduced to a unit test.

## vuejs__core-11915: v-pre does not stop `{{` inside textarea
- **Issue:** inside a `v-pre` element, an unclosed `{{` in a `<textarea>` is treated as interpolation. It should be kept as literal text.
- **Test:** parsing `<div v-pre><textarea>{{ foo </textarea></div>` in html mode gives a textarea whose children match `[{TEXT, content: '{{ foo '}]` (`toMatchObject`).
- **Checks:**
  - (a) Base fails. The knock-on failure of "self-closing v-pre" also clears with gold.
  - (b) None.
  - (c) The test only requires literal text that the issue directly implies. The parser merges adjacent text nodes, so any fix that does not interpolate passes.
  - (d) None.
  - (e) No.
  - (f) None. The issue mentions the CDN browser build, but the bug is in the shared tokenizer path.
- **Verdict: YES.**

## vuejs__core-11739: SSR hydration style mismatch with parent v-bind CSS vars
- **Issue:** the reproduction uses `v-bind('props.backgroundColor')` and `v-bind('props.padding')` in the parent, plus a dynamic `:style` on the child root. This gives a hydration style mismatch warning. The reporter blames "style merging".
- **Test:** `useCssVars(() => ({'foo.bar':'red'}))`, a child with style `padding: 4px`, and SSR HTML containing `--foo\.bar:red`. The test expects no "Hydration style mismatch" warning.
- **Checks:**
  - (a) I added a control test identical to the hidden one but with the key `foobar`. It passes on base, while `foo.bar` fails. So the only base bug is the missing escape of CSS var names during the hydration compare.
  - The issue's own reproduction hits exactly this bug. In dev mode the compiler names the vars `<id>-props\.padding`, the SSR attribute keeps the backslash, and the runtime key does not.
  - The reporter's diagnosis is somewhat misleading, but the hydration warning diff would point an engineer straight at the backslash. The style comparison only runs when the child has a `:style`, which is why the reporter saw it only in that case.
  - (b) None.
  - (c) None. The test only checks that no warning is emitted.
  - (d) Gold moves `getEscapedCssVarName` into `@vue/shared`, but the test does not use that name.
  - (e) A fix in the compiler would not pass, because the test calls `useCssVars` directly. That fix would also break the CSS, so the runtime hydration fix is effectively forced.
  - (f) None.
- **Verdict: YES**, with one caveat: the issue's diagnosis is imprecise, so the engineer has to reproduce the bug to find the real cause.

## vuejs__core-11589: execution order of sync watchers
- **Issue:** sync watchers should run in the order they were defined. The expected output is `compN (0)`…`(4)` in order. The issue's update notes that 3.5 runs both ref and computed sync watchers in LIFO order.
- **Test:** two `flush:'sync'` watchers on one ref. `foo` is `''` before the change and `'12'` after `v.value++`. The whole apiWatch spec must also pass.
- **Checks:**
  - (a) Base gives `'21'`, which is the LIFO bug the issue describes for 3.5. Gold passes 79/79.
  - (b) None.
  - (c) None.
  - (d) Gold removes the internal `EffectFlags.NO_BATCH` flag, but the test does not depend on it.
  - (e) The issue explicitly expects definition (FIFO) order.
  - (f) None.
- **Verdict: YES.**
