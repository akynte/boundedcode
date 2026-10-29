# Pruning benchmark

42 cases; budgets [4096] bytes.

| Set | Strategy | Budget | Cases with all evidence | Evidence kept | Tokens (estimate) | Reduction | Median ms | Model prompt tokens | Errors |
|---|---|---|---|---|---|---|---|---|---|
| dev | model | 4096 | 9 / 23 | 34 / 71 | 184485 → 63251 | 66% | 11536.0 | 213755 | 2 |
| dev | prune | 4096 | 23 / 23 | 71 / 71 | 184485 → 18376 | 90% | 3.8 | 0 | 0 |
| heldout | model | 4096 | 7 / 19 | 25 / 64 | 76379 → 52862 | 31% | 6789.2 | 104186 | 0 |
| heldout | prune | 4096 | 19 / 19 | 64 / 64 | 76379 → 14095 | 82% | 0.5 | 0 | 0 |

Evidence lost (case: strategy budget: patterns missing):

- go-build-errors (dev): model 4096: `build\.go:6:3: invalid operation`, `build\.go:8:9: cannot use t`, `undefined: logf`, `build\.go:35:\d+: cannot use 42`, `exit status 1`
- go-vet (dev): model 4096: `vet\.go:3:24: fmt\.Printf format %d has arg`, `vet\.go:3:55: unreachable code`
- pip-install (dev): model 4096: `Successfully installed .*pytest-8\.3\.3`, `exit status 0`
- pip-install-error (dev): model 4096: `Could not find a version that satisfies the requirement requests==0\.0\.999`, `No matching distribution found for requests==0\.0\.999`, `exit status 1`
- pytest-verbose (dev): model 4096: `FAILED test_inventory\.py::test_reorder_point`, `assert 7 == 8`, `test_reorder_zero_safety\[5-2-10\]`, `3 failed, 120 passed`, `test_inventory\.py:\d+`
- python-traceback (dev): model 4096: `DeprecationWarning: config key`
- tsc-errors (dev): model 4096: `cart\.ts\(3,3\): error TS2322`, `cart\.ts\(7,19\): error TS2363`, `TS2304: Cannot find name .logger.`
- eslint-output (dev): model 4096: `83 problems \(2 errors, 81 warnings\)`
- node-test (dev): model 4096: `fail 1`
- node-stack (dev): model 4096: `TypeError: Cannot read properties of undefined \(reading .amount.\)`, `app\.js:2:71`, `at load .*app\.js:3`
- cargo-build-deps (dev): model 4096: `error\[E0308\]: mismatched types`, `src/lib\.rs:9:25`, `could not compile .rledger.`
- cargo-test-backtrace (dev): model 4096: `test tests::transfer_keeps_total \.\.\. FAILED`
- gradle-compile-errors (dev): model 4096: `Orders\.java:4: error: cannot find symbol`, `Orders\.java:5: error: cannot find symbol`, `Orders\.java:6: error: cannot find symbol`, `Compilation failed`
- java-exception (dev): model 4096: `Caused by: java\.lang\.IllegalStateException: no balance for account b`, `Main\.java:6`, `Main\.java:15`
- make-c-errors (heldout): model 4096: `main\.c:6:20: error: implicit declaration of function .variance.`, `stats\.c:11:63: error: expected .;. before`, `main\.c:5:14: warning: format`, `not remade because of errors`
- mypy-errors (heldout): model 4096: `billing\.py:9: error: Unsupported operand types`, `billing\.py:13: error: Incompatible return value type`, `Found 5 errors in 1 file`
- ruff-check (heldout): model 4096: `Found 16 errors`
- unittest-verbose (heldout): model 4096: `ERROR: test_no_discount`, `billing\.py", line 9, in invoice_total`, `TypeError: unsupported operand type`, `FAILED \(errors=1\)`
- jest-failures (heldout): model 4096: `✕ percent coupon`, `Expected: 900`, `Received: -9000`, `price\.test\.js:42`, `Received: "\$12\.3"`, `Tests:\s+2 failed, 40 passed`
- npm-install-missing (heldout): model 4096: `npm error code E404`, `404 Not Found - GET https://registry\.npmjs\.org/left-padd-not-a-real-package-xyz`, `exit status 1`
- tsc-project (heldout): model 4096: `src/app\.ts\(2,23\): error TS2322`, `src/user\.ts\(2,69\): error TS2345`, `src/user\.ts\(3,56\): error TS2322`, `TS2339: Property .name. does not exist`
- cargo-clippy (heldout): model 4096: `unneeded .return. statement`, `src/lib\.rs:6:5`, `generated 8 warnings`
- cargo-test-two-failures (heldout): model 4096: `test tests::by_zero \.\.\. FAILED`, `panicked at src/lib\.rs:2:16`, `attempt to divide by zero`, `panicked at src/lib\.rs:9:32`, `left: 9`, `test result: FAILED`
- gradle-test-failure (heldout): model 4096: `2 tests completed, 1 failed`
- mvn-compile-error (heldout): model 4096: `Report\.java:\[5,46\] .;. expected`, `COMPILATION ERROR`, `BUILD FAILURE`
- rg-python-stdlib (heldout): model 4096: `def __repr__`
