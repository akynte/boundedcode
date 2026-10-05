# Derivability screening: slot js-axios

Method: for each task I applied the hidden test patch to a scratch copy of the base source (~/.cache/bc-curator-axios, now deleted), ran the acceptance command on base, then on base plus the gold patch.

| ID | Verdict | Base | Base+gold | Env |
|---|---|---|---|---|
| axios__axios-5892 | YES | 3 GZIP tests fail (rc 3) | 33/33 pass (rc 0) | OK |
| axios__axios-4731 | YES (minor caveat) | new test fails (rc 1) | 2/2 pass (rc 0) | OK; 100 ms timing window |
| axios__axios-4738 | YES | target test fails, rc 124 | 4/4 pass but **rc 124** | BROKEN: mocha never exits |
| axios__axios-5085 | YES | 5028 test fails (rc 1) | 2/2 pass (rc 0) | 4999 test needs internet |

## axios__axios-5892: Content-Encoding is case-sensitive
- Issue: when a response has `content-encoding: GZIP` (or another casing), axios does not decompress it. The value should be matched case-insensitively.
- Test: adds `GZIP` to the decompression algorithm matrix. The server sends `Content-Encoding: GZIP`, and `data` must equal `'str'`. The Content-Length-missing and chunked variants are checked too.
- Gold: `switch ((res.headers['content-encoding'] || '').toLowerCase())`.
- (a) Gold fixes it: base fails 3 GZIP tests, gold passes 33/33. (b)–(e) none: the test checks only the scenario the issue describes, any case-insensitive comparison passes, and there are no new names or messages. (f) Everything runs on a local server.
- **YES.** The test is exactly the issue's scenario (an upper-case `GZIP` encoding must still be decompressed), and any reasonable case-folding fix passes it.

## axios__axios-4731: follow-redirects enforces a 10 MB maxBodyLength by default
- Issue: since follow-redirects was upgraded, it enforces a 10 MB request-body default. Even `maxBodyLength: -1` errors, while `maxRedirects: 0` is unlimited. The default should be consistent regardless of `maxRedirects`.
- Test: a default `axios.post` of about 40 MB of JSON (20M chars of `'ж'`) to a local server must succeed with no error. The result is checked 100 ms after the request.
- Gold: when `config.maxBodyLength` is -1 (axios's default in `lib/defaults`), pass `options.maxBodyLength = Infinity` to follow-redirects.
- (a) Gold fixes it: base fails, gold passes. Repeated 3x with gold: 2/2 passing each run (~0.4 s).
- (e) Caveat: "consistent and documented" could in principle be read as "enforce and document 10 MB everywhere", which would fail the test. But the issue says that before the upgrade there was no limit, calls the change an unannounced breaking change, and shows `maxBodyLength: -1` (axios's own "unlimited" value, already honoured at http.js:201 on the `maxRedirects: 0` path) wrongly erroring. Restoring "unlimited" is the clearly intended fix. (b)(c)(d) none.
- (f) Offline is fine. The 100 ms success window for a 40 MB upload is timing-sensitive under heavy load (it passed at load average 5.5 on 16 cores).
- **YES.** The issue identifies -1/default as unlimited and the 10 MB limit as an unintended regression; the test only requires the default post to succeed.

## axios__axios-4738: timeoutErrorMessage is ignored by the Node http adapter
- Issue: with `timeout` + `timeoutErrorMessage: 'Custom Timeout Error Message'`, Node rejects with "timeout of 5000ms exceeded" instead of the custom message.
- Test: the existing test "should respect the timeoutErrorMessage property" already passes `timeoutErrorMessage: 'oops, timeout'`. Its assertion changes from `'timeout of 250ms exceeded'` to `'oops, timeout'`, and `code` stays `ECONNABORTED`.
- Gold: mirrors xhr.js. It uses `config.timeoutErrorMessage` if set, otherwise the default message.
- (a)–(e): the test asserts exactly the issue's expectation, with the message coming from the test's own config, and the code is unchanged. Nothing extra is required.
- (f) **Environment problem, not caused by the network.** With gold, all 4 grep-matched tests pass (TAP `# pass 4 # fail 0`), but mocha never exits: it still hung after 60 s, while with `--exit` it finishes in 1.5 s. So `timeout 10s ...` returns rc 124 on both base and gold, and the acceptance check can never pass as written. A likely cause is lingering sockets or servers from the 1000 ms delayed-response servers. Fix the check (`--exit`, or judge by TAP results) or drop the task.
- **YES** for derivability, **unusable as-is** because of the acceptance-command hang.

## axios__axios-5085: AxiosHeaders 'set-cookie' returns a string instead of an array
- Issue: `response.headers.get('set-cookie')` returns a comma-joined string in 1.x. It should return an array of cookies, as in 0.27.
- Test (run command covers only test/unit/regression/bugs.js): a local server sends two `Set-Cookie` headers, and a response interceptor asserts `res.headers['set-cookie']` deep-equals `[cookie1, cookie2]`. The AxiosHeaders.js `normalize().toJSON()` array test is in the patch but is **not** run by the acceptance command.
- Root cause on base: `set()` keeps arrays, but `normalize()` (called from transformData on every response) does `String(value)`, which gives `'a=1,b=2'`. This matches the string shown in the issue. `get()` and property access read the same storage, so fixing what `get()` returns also fixes property access.
- Gold: `normalizeValue` maps arrays recursively. `toJSON` joins arrays only when the new `asStrings` flag is set, and index.d.ts gets an `asStrings?` param. The `toJSON(asStrings)` shape is gold-only, but it is not exercised by the acceptance command. (b)–(e) none affect the run.
- (f) The pass-to-pass test `issues 4999` calls postman-echo.com. In this curator environment it passed (network reachable here), but it will fail in the offline sandbox. Either remove it from the run or grep for `5028`.
- **YES** for derivability. The environment fix is needed (internet-dependent 4999 test).
