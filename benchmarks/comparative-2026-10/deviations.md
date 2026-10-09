# Deviations from the frozen protocol

This file lists every change made after the freeze commit (`d4ce733`), with
its reason and its effect on results. Changes made before the freeze (slot
replacement, command adaptations, preparation fixes) are recorded in
`manifest.json` and in protocol.md section 3.

## D1. Environment-only verification configs (found by the pilot)

**What happened.** The declared pilot pair (`gin-gonic__gin-3741`, results
in `results/pilot/`) showed that BoundedCode's verification fails on checks
that already fail on the untouched base in the offline sandbox. For gin,
`TestRunTLS` reads certificate files that BoundedCode's secret masking hides.
- Every BoundedCode attempt therefore failed verification for a reason
  unrelated to the task.
- The model then edited those unrelated tests to skip, and the gate
  accepted the result.

The earlier validations avoided this by committing, into each task's base,
an environment-only `.boundedcode/verification.yaml`: BoundedCode's preset
minus the checks that fail on the base offline. The protocol omitted that
step. That was a tooling fault in the protocol, not a property of the tasks.

**Change.**
1. For each frozen task, BoundedCode's full gate was run on the untouched
   base, in the evaluation's sandbox and caches (`basecheck/`, output
   `results/basecheck-preset.json`).
2. Every failure was diagnosed on the base (`basediag.sh`).
3. `env_configs.py` derived each task's config: the preset, minus what fails
   or times out on the base, excluding single tests where the runner allows
   it. The configs, with their justifications, are in `env-configs/`.
4. Each config was validated: with it committed, BoundedCode's full gate
   passes on every base (`results/basecheck-with-env-config.json`; the
   configs were corrected twice in this step, for gin's other
   certificate-reading tests and immutable-js' build-before-test order).
5. Each config is committed into the task's base
   (`screen_freeze.py apply-configs`); `frozen-tasks.json` records the new
   spec digests next to the frozen ones.
6. Gate 1 was re-run on the configured specs; all eight are still valid
   (`results/gate1-frozen-with-env-config.json`).

No exclusion was chosen from an agent run or a reference patch. The hidden
acceptance commands are unchanged. Both systems see the same repository: the
baseline's agent can read the file, as BoundedCode's can.

| Task | Change from the preset |
|---|---|
| gin-gonic__gin-3820 | `go test` skips `TestRunTLS` and `TestPusher` (masked certificate files) |
| gin-gonic__gin-4003 | `go test` skips `TestRunTLS`, `TestPusher` and `TestRunQUIC` (masked certificate files) |
| briannesbitt__carbon-3103 | PHPUnit excludes `testStartAndEndFallback` (date-dependent) and `testCreateFromPartialFormat` (2 errors) |
| sharkdp__bat-2393 | `cargo test` skips `no_args_doesnt_break` (aborts the test binary under Rust 1.99) |
| tokio-rs__axum-691 | `cargo test` skips axum-debug's `ui` trybuild test (Rust 1.99 diagnostics) |
| immutable-js__immutable-js-2006 | The test script's network steps (`dtslint` downloads, `build:stats`) are dropped; type-check (Flow) and jest remain; the build runs without `build:stats` |
| phpoffice__phpspreadsheet-3463 | The PHPUnit stage is removed: the full suite times out (20 min) on the base. **No test stage remains.** |
| fluent__fluentd-3616 | The `rake test` stage is removed: it fails on the base and is unstable offline (four base runs: 14+3, 2, 0 and 56+962 failures and errors). **No test stage remains.** |

**Effect on results.**
- On phpspreadsheet-3463 and fluentd-3616, BoundedCode has no test stage,
  so its behavioural-evidence check cannot verify a change. The best
  possible outcome there is `tests_green`.
- The primary endpoint (hidden acceptance) is unaffected.
- Both tasks stay in the set (the protocol does not allow replacing them),
  and the report shows them separately where it matters.

**Pilot.** The pilot ran before this fix, so its BoundedCode result reflects
the missing config. It is published, and it is excluded from the analysis,
as declared.
