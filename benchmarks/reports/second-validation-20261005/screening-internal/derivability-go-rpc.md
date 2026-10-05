# Derivability screening — slot go-rpc

Method: for each candidate, applied the hidden test patch to a scratch copy of the base source (under ~/.cache, since deleted) and ran the acceptance package with `GOFLAGS=-mod=mod GOPROXY=off`, on base and on base+gold. All four build and pass with gold offline.

| ID | Verdict | Reason |
|---|---|---|
| zeromicro__go-zero-1969 | YES | The test uses only identifiers that already exist at base. It asserts the issue's own behaviour. |
| zeromicro__go-zero-2363 | NO | The test calls `DontTracingSpanName`, a function the gold patch adds and the issue never names. |
| zeromicro__go-zero-2032 | NO | The test needs the gold patch's signature `checkedTimeout(d, bool)`, function `WithIgnoreTimeout()` and field `featuredRoutes.ignoreTimeout`. The issue names none of them. |
| grpc__grpc-go-3476 | NO | The test needs `Equal` methods on `BuilderMap`, `builder` and `matcher`, which the issue never names. It also requires nil and empty slices to compare unequal. |

## zeromicro__go-zero-1969 — YES
- **Issue:** an `options=0|1` tag on an int field is not enforced when the value comes from JSON. Input `{"sex":2}` passes validation.
- **Test:** `TestUnmarshalWithJsonNumberOptionsIncorrect` calls `UnmarshalKey` with `json.Number("3")` for an `int` field tagged `options=1|2` and expects an error.
- (a) I reproduced the issue on base: `UnmarshalJsonBytes({"sex":2})` with `options=0|1` returns nil. With gold it returns `value "2" is not defined in options "[0 1]"`. The acceptance test fails on base and passes with gold.
- (b) No unrelated behaviour is required.
- (c) Only a non-nil error is checked, so the message format does not matter.
- (d) The test references no new identifiers. `UnmarshalKey` and `json.Number` both exist at base.
- (e) Error vs no error is the only outcome tested.
- (f) No offline problems.
- **Residual risk:** the test goes through `UnmarshalKey` instead of JSON bytes. Both use the shared JSON-number path (`processFieldPrimitiveWithJSONNumber`), so a fix in the natural place covers both. A fix confined to the JSON-bytes entry point would fail, but such a fix would be unusual.

## zeromicro__go-zero-2363 — NO
- **Issue:** a question about how to stop health-check requests from producing Jaeger traces. It proposes no API.
- **Test:** `TestDontTracingSpanName` calls `DontTracingSpanName("bar")`, then checks that `TracingHandler("foo","bar")` produces no valid span context.
- (a) The gold patch adds an opt-out registry for span names, which addresses the issue.
- (d) `DontTracingSpanName` is introduced only by the gold patch and is absent at base. The issue does not name it, and the base test build fails with `undefined: DontTracingSpanName`. Other designs would be just as valid: a config list, a filter function, or a skip-by-path option.
- (e) Many API shapes are possible, but the test accepts exactly one.
- (f) The Jaeger endpoint is localhost and exports asynchronously. The test passes offline with gold.

## zeromicro__go-zero-2032 — NO
- **Issue:** a feature request to support Server-Sent Events by skipping the timeout handler for some routes. It suggests a header or config. The issue body links a PR, but the agent cannot see the PR.
- **Test changes:**
  - The existing test now calls `ng.checkedTimeout(d, false)` instead of `ng.checkedTimeout(d)`.
  - New `TestEngine_checkedTimeout_ignoreTimeout` calls `checkedTimeout(d, true)` and expects 0.
  - New `TestWithIgnoreTimeout` calls `WithIgnoreTimeout()(&fr)` and asserts `fr.ignoreTimeout`.
- (d) Three things come only from the gold patch, and the issue names none of them:
  - the 2-argument `checkedTimeout` (unexported)
  - the exported `WithIgnoreTimeout` RouteOption
  - the unexported field `featuredRoutes.ignoreTimeout`

  The base build fails with "too many arguments in call to ng.checkedTimeout", `undefined: WithIgnoreTimeout` and "fr.ignoreTimeout undefined".
- (e) The issue itself proposes other designs (a header, an SSE config), and the test rejects them.
- (b) The test also edits `io.Copy`/`w.Close` in `server_test.go`, which is harmless.

## grpc__grpc-go-3476 — NO
- **Issue:** remove the use of `cmp.Equal` from the non-test file `rls/internal/keys/builder.go`.
- **Test:**
  - `TestMakeBuilderMap` now calls `builderMap.Equal(want)`.
  - New `TestBuilderMapEqual`, `TestBuilderEqual` and `TestMatcherEqual` call `.Equal` on `BuilderMap`, the unexported `builder` and the unexported `matcher`.
- (d) Three methods are introduced only by the gold patch and are not in the issue:
  - `BuilderMap.Equal`
  - `builder.Equal`
  - `matcher.Equal`

  The base build fails with "...Equal undefined" on all three types. The obvious independent fix would keep `BuilderMapEqual(a, b)` and reimplement it without `cmp`. That fix cannot compile against this test.
- (e) Beyond naming, the test fixes some semantics:
  - `builder{matchers: nil}` must not equal `builder{matchers: []matcher{}}`, and `matcher` names are compared the same way.
  - Matcher order matters.

  These are defensible choices, but the issue does not specify them.
