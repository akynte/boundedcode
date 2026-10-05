# Second validation: candidate screening (frozen 2026-10-05, before any run)

Selection rule: [`candidates-manifest.json`](candidates-manifest.json). Per slot, the four best-ranked
candidates (sha256 of `boundedcode-second-validation-2026-10-05:` + instance id) were screened; the
best-ranked candidate passing both gates was selected. Curator analysis (benchmark-internal, never
shown to the agent): [`screening-internal/`](screening-internal/).

Gate 1, environment: acceptance fails on base and passes with the reference patch in the offline sandbox.
Gate 2, derivability: could a competent engineer derive every behaviour the acceptance test requires
from the issue alone? (YES / NO / AMBIGUOUS; only YES enters.)

| Slot | Rank | Candidate | Derivable | Environment | Selected | Reason |
|---|---|---|---|---|---|---|
| Go (gin) | 1 | gin-1957 | AMBIGUOUS | – | no | Test calls `c.BindHeader`; the issue names only `ShouldBindHeader` |
| | 2 | **gin-1805** | YES | valid | **yes** | Issue's double middleware call reproduced (2 log lines base, 1 gold); test counts calls |
| | 3 | gin-2121 | YES | valid | – | (not needed) |
| | 4 | gin-2755 | NO | – | no | Test calls internal `node.getValue`; a fuller fix of the issue still panics the test |
| Go + infra (caddy) | 1 | **caddy-6370** | YES | valid | **yes** | Test is the issue's `Caddyfile.<ext>` case (error on base, true with gold) |
| | 2 | caddy-6350 | YES | – | – | (not needed) |
| | 3 | caddy-6051 | AMBIGUOUS | – | no | Test pins whitespace-only-line behaviour and an exact error message |
| | 4 | caddy-6345 | NO | invalid | no | Unfiltered acceptance runs server tests failing under Go 1.27 even with gold |
| Go (RPC) | 1 | **go-zero-1969** | YES | valid | **yes** | Issue scenario reproduced; test uses only base APIs and checks an error is returned |
| | 2 | go-zero-2363 | NO | – | no | Test calls `DontTracingSpanName`, never named in the issue |
| | 3 | go-zero-2032 | NO | – | no | Test needs `WithIgnoreTimeout`, 2-arg `checkedTimeout`, an unexported field |
| | 4 | grpc-go-3476 | NO | – | no | Test needs `Equal` methods the issue never names |
| JavaScript (axios) | 1 | **axios-5892** | YES | valid | **yes** | Test adds the issue's `Content-Encoding: GZIP` response; any case-insensitive fix passes |
| | 2 | axios-4731 | YES | – | – | (not needed) |
| | 3 | axios-4738 | YES | invalid | no | Acceptance never exits (mocha without `--exit`): rc 124 on base and gold |
| | 4 | axios-5085 | YES | invalid | no | Pass-to-pass test calls postman-echo.com (offline sandbox) |
| TypeScript (vue) | 1 | **vuejs-11870** | YES | valid | **yes** | Test is the issue's `renderList(shallowReactive(...))` reproduction |
| | 2-4 | vuejs-11915, 11739, 11589 | YES | – | – | (not needed) |
| Large repo (prometheus) | 1 | prometheus-15142 | AMBIGUOUS | – | no | Test detects a lost update only the reference's specific fix avoids |
| | 2 | **prometheus-10720** | YES | valid | **yes** | Issue names `day_of_year`; expected values are standard day-of-year numbers |
| | 3 | prometheus-11859 | AMBIGUOUS | – | no | Test requires one specific snapshot-cleanup policy |
| | 4 | prometheus-10633 | NO | – | no | Expected labels come from fixtures only the gold patch adds |

Totals over 24 candidates: 12 YES, 5 AMBIGUOUS, 5 NO, 2 derivable but environment-invalid.
Acceptance commands were not modified; candidates whose command did not work offline were rejected.

Known risks recorded before the run (from the curators):
- go-zero-1969: the test goes through `UnmarshalKey`; a fix confined to the JSON-bytes entry point would fail it.
- caddy-6370: none beyond the issue.
- gin-1805: its acceptance regex also runs `TestRouterMiddlewareAndStatic`, which fails on a host whose
  MIME database maps `.go` to `text/x-go`; it passes in the sandbox (environment screening valid).

Environment-only verification configs (`.boundedcode/verification.yaml`, committed into each base as
task setup; see `tasks/*.yaml`) are the default presets minus checks that fail on the untouched base in
the offline sandbox. Each was confirmed to pass on its base before freezing; acceptance re-screened valid
with the configs applied ([`environment-screening-frozen.json`](environment-screening-frozen.json)).
Notable: axios runs mocha with `--retries 2` (one body-upload test fails with ECONNRESET under Node 24 at
base, a different one each time one is excluded); gofmt skips files Go 1.27's gofmt reformats at base
(gin 17, prometheus 12 including `promql/functions.go`, go-zero 2); prometheus leaves out `cmd/prometheus`
and `cmd/promtool` (do not link under Go 1.27). One go-zero full-suite run at base failed once without a
named test and passed on rerun (recorded as a possible flake).
