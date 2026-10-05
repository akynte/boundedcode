# Derivability screening — slot go-gin

Method: for each candidate, scratch copies of base and base+gold under ~/.cache (deleted afterwards); an issue-scenario
repro test run on both; hidden test patch applied and acceptance command run (GOFLAGS=-mod=mod GOPROXY=off).

| ID | Verdict | Reason |
|---|---|---|
| gin-gonic__gin-1957 | AMBIGUOUS | Test calls `c.BindHeader`, which the issue never names (issue names only `ShouldBindHeader`) |
| gin-gonic__gin-1805 | YES (env caveat) | Test checks that middleware runs once per StaticFS 404, which is the root cause of the double log. Unrelated `TestRouterMiddlewareAndStatic` fails on this host even with gold |
| gin-gonic__gin-2121 | YES | Issue gives the exact fix; tests only assert no crash, 200, body, Content-Type and Content-Length |
| gin-gonic__gin-2755 | NO | Hidden test calls internal `node.getValue` with zero-cap Params; a valid fix at the HandleContext level solves the issue but fails the test |

## gin-gonic__gin-1957 — support bind http header param
- **Issue:** add header binding. The example uses struct tags `header:"Rate"` and calls `c.ShouldBindHeader(&h)`.
- **Acceptance** (`go test -v . -run "^TestContext.*Bind"`, root package only): `TestContextBindHeader` calls `c.BindHeader(&s)` and `TestContextShouldBindHeader` calls `c.ShouldBindHeader(&s)`. Both send headers rate=8000, domain=music and limit=1000, with tags `Rate`, `Domain` and `limit`, so the tag lookup must ignore case (canonical header key). Both also assert an empty body. The patch also adds `binding.TestHeaderBinding` (`binding.Header`, `Name()=="header"`, a map field with invalid JSON must give an error), but the acceptance command does not run the binding package.
- **(a)** Base: `ShouldBindHeader` undefined. Gold: the issue's scenario returns `{"Domain":"music","Rate":300}`. Acceptance passes on gold.
- **(d)** `BindHeader` appears only in the gold patch and the test. I checked this: gold with `BindHeader` removed fails to build (`c.BindHeader undefined`). The name does follow a strict existing pattern: every `ShouldBindX` in context.go (JSON, XML, Query, YAML, Uri) has a matching `BindX`. A careful engineer would likely add both, but the issue does not ask for it.
- (b)(c)(e)(f): none. Header names are matched without regard to case, which a natural implementation does anyway (`http.Header.Get` canonicalizes the key).
- **Verdict: AMBIGUOUS.** The requested behaviour can be fully derived, but a correct solution that adds only the API the issue names fails to compile. Usable only if we accept "follow the obvious Bind/ShouldBind symmetry" as derivable.

## gin-gonic__gin-1805 — StaticFS 404 logs twice
- **Issue:** with `gin.Default()` and `StaticFS("/", ...)`, a 404 produces two log lines.
- **Acceptance** (`-run "^Test.*Router"`): new test `TestMiddlewareCalledOnceByRouterStaticFSNotFound`. With `New()`, a counting middleware and StaticFS on a missing directory, a GET and then a HEAD must leave the counter at 1 and then 2.
- **(a)** Repro with `LoggerWithWriter`: 2 log lines on base, 1 on gold. Hidden test fails on base and passes on gold.
- (b)(c)(d)(e): none. A double log line means the global middleware runs twice. The test only counts middleware calls and uses existing APIs. A "fix" that removed duplicates inside the logger would not count, because the issue clearly describes the chain running twice.
- **(f) Caveat:** the existing test `TestRouterMiddlewareAndStatic` matches the acceptance regex and FAILS on base and on gold on this host. It expects `text/plain` for `gin.go`, but `/usr/share/mime/globs2` maps `.go` to `text/x-go`. Unless the harness only scores fail-to-pass tests or runs in a clean image, this task can never pass. Fix the regex (for example `^TestMiddlewareCalledOnceByRouterStaticFSNotFound$`) or the environment before using it.
- **Verdict: YES** (derivable), but the acceptance command must be fixed first.

## gin-gonic__gin-2121 — DataFromReader with nil extraHeaders crashes
- **Issue:** `c.DataFromReader(..., nil)` panics and returns 500. The issue includes the exact nil-map fix in `render/reader.go`. It also asks a side question about extra headers vs. global headers.
- **Acceptance** (`go test ./... -run "^Test.*Reader"`): `render.TestReaderRenderNoHeaders` (`Reader{ContentLength, Reader}.Render` gives no error) and `TestContextRenderDataFromReaderNoHeaders` (200, body, the Content-Type, and Content-Length equal to the length).
- **(a)** Repro: 500 on base, 200 on gold. Hidden tests panic on base and pass on gold.
- (b)-(f): none. The side question about header precedence is not tested. Content-Length is set by the existing code path. The `Reader` fields already exist.
- **Verdict: YES.**

## gin-gonic__gin-2755 — HandleContext after CreateTestContext panics
- **Issue:** `CreateTestContext` creates the context before routes are registered. Then `engine.HandleContext(ctx)` on `/hello/:name` panics in tree.go `getValue` (slice bounds out of range, capacity 0). Expected: no panic.
- **Acceptance** (`-run "^TestTree"`): `TestTreeInvalidParamsType` builds a hand-made `node` (wildChild, child nType=2) and calls `tree.getValue("/test", &params, false)` with `make(Params,0,0)`. It must not panic.
- **(a)** Base panics. With gold there is no panic, but the response is `"Hello "`: the param is silently dropped (gold skips saving params when cap==0). So gold only partly serves the issue's handler.
- **(e)** I wrote an alternative fix in `HandleContext`: reallocate `c.params` when `cap < engine.maxParams`. It fixes the issue scenario properly (`"Hello something"`) but the hidden test still panics. The test accepts only a guard inside the internal `node.getValue`, the unexported function the issue names only in a stack trace.
- (d) The test calls the unexported internals `node`, `nType` and `getValue`. They exist in the base, but this ties the test to where the fix is made.
- **Verdict: NO.** The issue allows several reasonable fixes, including a better one, and the test accepts only the gold's tree-level guard.
