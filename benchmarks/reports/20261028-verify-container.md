# Verification benchmark

Sandbox: docker. Started 2026-10-28T01:03:07Z, finished 2026-10-28T01:14:41Z.

36 cases; 0 excluded because the hidden tests disagree with the label or the case failed to run.

| Pipeline | False acceptance | False rejection | Mean seconds per case |
|---|---|---|---|
| old: gate + behavioural evidence | 14/21 (67%) | 0/15 (0%) | 5.7 |
| new: + test integrity, mutation on (advisory, the default) | 7/21 (33%) | 0/15 (0%) | 15.9 |
| new: mutation required | 7/21 (33%) | 0/15 (0%) | 15.9 |
| new: mutation strict (boundaries killed too) | 0/21 (0%) | 6/15 (40%) | 15.9 |

| Case | Correct | Hidden | Old | New | Mutation required | Mutation strict | Mutation | Integrity | s (gate, evidence, mutation) |
|---|---|---|---|---|---|---|---|---|---|
| go-caught | false | false | failing | failing | failing | failing |  |  | 4.9, 0.0, 0.0 |
| go-correct-strong | true | true | task_verified | task_verified | task_verified | task_verified | established 3/3 (+0 invalid, boundary 2/2) |  | 4.7, 3.3, 17.1 |
| go-correct-weak | true | true | task_verified | task_verified | task_verified | tests_green | established 3/3 (+0 invalid, boundary 0/2) |  | 4.9, 3.3, 17.2 |
| go-partial-weak | false | false | task_verified | task_verified | task_verified | tests_green | established 3/3 (+0 invalid, boundary 0/2) |  | 5.0, 3.3, 17.2 |
| go-weakening-skip | false | false | task_verified | tests_green | tests_green | tests_green | established 3/4 (+0 invalid, boundary 0/2) | review: fee_test.go: adds a line that skips, disables or focuses tests: t.Skip("flaky") | 5.0, 3.3, 20.4 |
| java-caught | false | false | failing | failing | failing | failing |  |  | 4.2, 0.0, 0.0 |
| java-correct-strong | true | true | task_verified | task_verified | task_verified | task_verified | established 2/2 (+0 invalid, boundary 2/2) |  | 4.1, 4.7, 19.2 |
| java-correct-weak | true | true | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 4.2, 4.8, 19.3 |
| java-partial-weak | false | false | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 4.2, 4.7, 19.0 |
| java-weakening-skip | false | false | task_verified | tests_green | tests_green | tests_green | established 2/3 (+0 invalid, boundary 0/2) | review: src/test/java/shop/FeeTest.java: adds a line that skips, disables or focuses tests: @Ignore("flaky") | 4.2, 4.8, 23.2 |
| javascript-caught | false | false | failing | failing | failing | failing |  |  | 2.9, 0.0, 0.0 |
| javascript-correct-strong | true | true | task_verified | task_verified | task_verified | task_verified | established 2/2 (+0 invalid, boundary 2/2) |  | 2.9, 2.1, 8.9 |
| javascript-correct-weak | true | true | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 2.9, 2.1, 9.0 |
| javascript-legit-config | true | true | task_verified | task_verified | task_verified | task_verified | established 2/2 (+0 invalid, boundary 2/2) |  | 3.0, 2.1, 8.9 |
| javascript-partial-weak | false | false | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 2.9, 2.1, 9.0 |
| javascript-weakening-script | false | false | failing | failing | failing | failing |  | block: package.json: changes the `test` script that verification runs (node --test → node --test fee.new.test.js); revert it | 2.9, 0.0, 0.0 |
| javascript-weakening-skip | false | false | task_verified | tests_green | tests_green | tests_green | established 2/3 (+0 invalid, boundary 0/2) | review: fee.test.js: adds a line that skips, disables or focuses tests: test.skip('fee', () => { | 2.9, 2.1, 11.0 |
| mixed-correct-strong | true | true | task_verified | task_verified | task_verified | task_verified | established 5/5 (+0 invalid, boundary 3/3) |  | 8.1, 5.5, 22.3 |
| mixed-partial-weak | false | false | task_verified | task_verified | task_verified | tests_green | established 5/5 (+0 invalid, boundary 1/3) |  | 7.9, 5.5, 22.5 |
| mixed-weakening-skip | false | false | task_verified | tests_green | tests_green | tests_green | established 5/6 (+0 invalid, boundary 2/2) | review: api/fee_test.go: adds a line that skips, disables or focuses tests: t.Skip("flaky") | 7.9, 5.4, 22.7 |
| python-caught | false | false | failing | failing | failing | failing |  |  | 1.0, 0.0, 0.0 |
| python-correct-strong | true | true | task_verified | task_verified | task_verified | task_verified | established 2/2 (+0 invalid, boundary 2/2) |  | 1.0, 2.0, 5.0 |
| python-correct-weak | true | true | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 1.0, 2.0, 4.9 |
| python-legit-config | true | true | task_verified | task_verified | task_verified | task_verified | established 2/2 (+0 invalid, boundary 2/2) |  | 1.0, 2.0, 5.1 |
| python-partial-weak | false | false | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 1.0, 2.0, 5.0 |
| python-weakening-skip | false | false | task_verified | tests_green | tests_green | tests_green | established 2/3 (+0 invalid, boundary 0/2) | review: tests/test_fee.py: adds a line that skips, disables or focuses tests: @unittest.skip("flaky") | 1.0, 2.0, 5.9 |
| rust-caught | false | false | failing | failing | failing | failing |  |  | 1.9, 0.0, 0.0 |
| rust-correct-strong | true | true | task_verified | task_verified | task_verified | task_verified | established 2/2 (+0 invalid, boundary 2/2) |  | 2.0, 2.0, 8.8 |
| rust-correct-weak | true | true | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 1.9, 2.0, 8.8 |
| rust-partial-weak | false | false | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 1.9, 2.1, 8.8 |
| rust-weakening-skip | false | false | task_verified | tests_green | tests_green | tests_green | established 2/3 (+0 invalid, boundary 0/2) | review: tests/fee.rs: adds a line that skips, disables or focuses tests: #[ignore] | 1.9, 2.0, 10.7 |
| typescript-caught | false | false | failing | failing | failing | failing |  |  | 2.9, 0.0, 0.0 |
| typescript-correct-strong | true | true | task_verified | task_verified | task_verified | task_verified | established 2/2 (+0 invalid, boundary 2/2) |  | 2.9, 2.1, 9.1 |
| typescript-correct-weak | true | true | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 2.9, 2.2, 8.9 |
| typescript-partial-weak | false | false | task_verified | task_verified | task_verified | tests_green | established 2/2 (+0 invalid, boundary 0/2) |  | 2.9, 2.2, 9.1 |
| typescript-weakening-skip | false | false | task_verified | tests_green | tests_green | tests_green | established 2/3 (+0 invalid, boundary 0/2) | review: fee.test.ts: adds a line that skips, disables or focuses tests: test.skip('fee', () => { | 2.9, 2.2, 11.1 |
