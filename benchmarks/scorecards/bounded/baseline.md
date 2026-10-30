# Scorecard: bounded suite

2 trial(s) (suite-20261030T013550Z, suite-20261030T015417Z). Model qwen3.6-35b-a3b (sha256 ac0e2c1189e0), server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); build dev b409ab02d7b9-dirty; task set ab9b01c719753acd; 13th Gen Intel(R) Core(TM) i7-13620H NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02.

| | Runs | Passed | Success (90% CI) | False acceptance | False rejection | Tokens per success / failure | Median / p90 / p95 s | Model / tool calls | Security failures (attempts) | Tampered |
|---|---|---|---|---|---|---|---|---|---|---|
| **all** | 8 | 6 | 75% (46–91%) | 1 | 0 | 20070 / 107596 | 139 / 1524 / 1524 | 25.6 / 26.1 | 0 (0) | 0 |
| split: heldout | 8 | 6 | 75% (46–91%) | 1 | 0 | 20070 / 107596 | 139 / 1524 / 1524 | 25.6 / 26.1 | 0 (0) | 0 |
| group: everyday | 8 | 6 | 75% (46–91%) | 1 | 0 | 20070 / 107596 | 139 / 1524 / 1524 | 25.6 / 26.1 | 0 (0) | 0 |
| language: c | 2 | 1 | 50% (12–88%) | 1 | 0 | 44287 / 31354 | 423 / 722 / 722 | 26.5 / 28.5 | 0 (0) | 0 |
| language: cpp | 2 | 1 | 50% (12–88%) | 0 | 0 | 17407 / 183838 | 158 / 1524 / 1524 | 52.0 / 49.5 | 0 (0) | 0 |
| language: php | 2 | 2 | 100% (42–100%) | 0 | 0 | 15189 / 0 | 116 / 139 / 139 | 13.0 / 15.0 | 0 (0) | 0 |
| language: ruby | 2 | 2 | 100% (42–100%) | 0 | 0 | 14173 / 0 | 111 / 124 / 124 | 11.0 / 11.5 | 0 (0) | 0 |

Limitations:
- 2 trial(s): fewer than three, so run-to-run noise is barely estimated
- 2 task(s) passed in some trials and failed in others
- 2 task run(s) skipped (a toolchain is missing)
- one machine; the confidence intervals treat repeated runs of a task as independent
