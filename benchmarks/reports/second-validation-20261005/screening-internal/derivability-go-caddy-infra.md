# Derivability screening — slot go-caddy-infra

Screened 2026-10-05. Experiments ran in scratch copies under `~/.cache/bc-curator-caddyinfra` (now deleted). Toolchain: Go 1.27.1 with `GOFLAGS=-mod=mod GOPROXY=off`.

| ID | Verdict | Reason |
|---|---|---|
| caddyserver__caddy-6370 | YES | The test only requires `Caddyfile.<any-ext>` with no adapter to return `(true, nil)`. That is exactly the issue's `.serve/Caddyfile.preview` scenario. |
| caddyserver__caddy-6350 | YES | Repeated `client_ip`/`remote_ip` lines must merge into one `ranges` list. The JSON shape is forced by the matcher-set map, and the order is the source order. |
| caddyserver__caddy-6051 | AMBIGUOUS | The test pins one treatment of whitespace-only lines that the issue doesn't specify. Two plausible alternatives fail. |
| caddyserver__caddy-6345 | NO | The acceptance command runs the whole integration package, and its server-starting tests fail offline under Go 1.27 even with the gold patch. The issue also points at the wrong `sign_with_root`. |

## caddyserver__caddy-6370 — `isCaddyfile` "ambiguous config file format"

**Issue:** `caddy run --config .serve/Caddyfile.preview` fails with "ambiguous config file format; please specify adapter". The user expects the file to be used as a Caddyfile. The user guesses that `filepath.Base` is the cause.

**Test:** `Test_isCaddyfile` in `cmd/main_test.go`.
- New cases: `./Caddyfile.prd` and `Caddyfile.prd` with adapter `""` must return `want=true, wantErr=false`.
- One existing case changes from `Caddyfile.yaml` with no adapter (which expected an error) to `Caddyfile.yaml` with adapter `yaml` (expects `false, nil`). Base already passes the changed case.

**Checks:**
- (a) Reproduced. On base, `isCaddyfile(".serve/Caddyfile.preview","")` returns `false` plus the ambiguous-format error. With gold it returns `true, nil`. The test fails on base (2 cases) and passes with gold.
- (b) No unrelated behaviour.
- (c) No exact strings.
- (d) `isCaddyfile` already exists; nothing new is introduced.
- (e) Several fixes pass. For example, keeping the error only for well-known formats such as `.yaml`/`.toml` still passes, because the test no longer covers `Caddyfile.yaml` without an adapter.
- (f) None.

**Verdict: YES.** The hidden-folder idea in the issue is a red herring: `filepath.Base` already returns `Caddyfile.preview`. Any engineer who debugs it will find the extension rule. The only behaviour the test requires is "a `Caddyfile.*` name with no adapter is a Caddyfile". That follows directly from the issue's scenario.

## caddyserver__caddy-6350 — repeated `client_ip` not merged

**Issue:** Inside a `not { ... }` block, several `client_ip` lines are given, but only the first line's IPs take effect. `remote_ip` "used to work" this way.

**Test:** new matchers in `matcher_syntax.caddyfiletest` (adapt test).
- `@matcher13 { remote_ip 1.1.1.1; remote_ip 2.2.2.2 }` must adapt to `{"remote_ip":{"ranges":["1.1.1.1","2.2.2.2"]}}`.
- `@matcher14` must do the same for `client_ip`.

**Checks:**
- (a) Reproduced with `caddy adapt` on the issue's config. Base gives `not:[{client_ip:{ranges:[first line only]}}]`. Gold gives all four ranges, in order. The adapt test fails on base and passes with gold.
- (b) Mostly related. The test checks top-level matcher sets, not `not`, but the bug lives in the shared `UnmarshalCaddyfile`, so it's the same defect. It also requires `remote_ip`. The issue says `remote_ip` merging used to work, and base `remote_ip` has the identical bug in the code right next to it.
- (c) The JSON is forced. A matcher set is a map, so there is one `client_ip` key, and the ranges concatenate in source order. Nothing incidental is embedded.
- (d) No new identifiers. (e) Merging is the only sensible outcome. (f) The adapt-only tests run fine offline.

**Verdict: YES.** Small risk: an agent might fix only `client_ip`. But the issue names `remote_ip` as the behaviour that worked before, and the two functions are side by side.

## caddyserver__caddy-6051 — blank lines in heredoc

**Issue:** An indented heredoc that contains an empty line fails with "mismatched leading whitespace". The user asks to skip whitespace stripping on a blank line, and notes that `caddy fmt` strips whitespace-only padding.

**Test:** 4 new `TestLexer` cases.
1. An unindented heredoc with an empty line (base already passes).
2. A tab-indented heredoc with an empty line must produce `"...\n\n..."`.
3. An unindented heredoc whose blank line holds one tab must keep it: `"...\n\t\n..."`.
4. A two-tab-indented heredoc whose blank line has one tab must **error** with exactly `mismatched leading whitespace in heredoc <<EOF on line #3 [\t], expected whitespace [\t\t] to match the closing marker`. The message format already exists in the code.

**Checks:**
- (a) The gold patch fixes the issue scenario (case 2). The test fails on base and passes with gold.
- (e) Cases 3 and 4 decide how whitespace-only lines are handled, and the issue leaves that open. I tested two reasonable alternatives:
  - Treat whitespace-only lines as blank and emit `\n` (textwrap.dedent-style): fails case 3.
  - Tolerate short whitespace-only lines and keep them: fails case 4.

  Only the narrowest reading passes: a line that is exactly `""` is exempt, and anything else follows the old rule. The issue's remark about `caddy fmt` hints that "blank" means empty, but it doesn't rule out the other readings. Case 4 in particular requires keeping an error for a whitespace-only line, which the issue never mentions.
- (b, d, f) No problems.

**Verdict: AMBIGUOUS.** The issue's core request is derivable, but the test rejects reasonable designs for whitespace-only lines. Accept only if lenient on (e).

## caddyserver__caddy-6345 — `sign_with_root` via Caddyfile

**Issue:** "Please make the `sign_with_root` option from the JSON config available in the Caddyfile." The link points to `apps/tls/automation/policies/issuers/internal/#sign_with_root`. The issue also says "maybe related" to a comment on another issue (#6290), which is not readable offline.

**Test:** a new adapt test `acme_server_sign_with_root.caddyfiletest`. `acme_server { ca internal; sign_with_root }` must adapt to `"sign_with_root": true` on the `acme_server` handler.

**Checks:**
- (b) The test targets a different place from the issue's link. At base, the internal issuer already parses `sign_with_root` in the Caddyfile (`modules/caddytls/internalissuer.go:176`). So the option the issue links to is already supported. The real target, `acme_server`, appears only behind the unreadable "maybe related" link. Grepping for `SignWithRoot` does reveal that `acmeserver.Handler` has the JSON field but no Caddyfile parsing, so a careful engineer could infer it.
- (c) The bare-flag syntax matches the internal issuer's convention. The JSON follows from that.
- (d) No new identifiers.
- (f) **Fails.** The acceptance command is `go test -v ./caddytest/integration` with no `-run` filter, so it also runs the server-starting tests. Even with the gold patch these fail locally under Go 1.27 (TestACMEServer*, TestAutoHTTP*, and others). The adapt subset passes with gold and fails on base with "unrecognized ACME server directive: sign_with_root".

**Verdict: NO.** The acceptance command cannot pass offline even with the gold patch, and the issue's own pointer refers to an option that is already supported. If the command were narrowed to `-run TestCaddyfileAdapt`, this would be AMBIGUOUS, leaning yes.
