# Scorecard: direct suite

3 trial(s) (direct-20261030T025329Z, direct-20261030T030031Z, direct-20261030T031030Z). Model qwen3.6-35b-a3b (sha256 ac0e2c1189e0), server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); build dev b409ab02d7b9-dirty; task set 8aa8af4958a24eb3; 13th Gen Intel(R) Core(TM) i7-13620H NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02.

| | Runs | Passed | Success (90% CI) | False acceptance | False rejection | Tokens per success / failure | Median / p90 / p95 s | Model / tool calls | Security failures (attempts) | Tampered |
|---|---|---|---|---|---|---|---|---|---|---|
| **all** | 21 | 18 | 86% (69–94%) | 0 | 0 | 2526 / 11251 | 38 / 126 / 163 | 13.0 / 14.3 | 0 (10) | 0 |
| split: dev | 6 | 6 | 100% (69–100%) | 0 | 0 | 2154 / 0 | 35 / 44 / 44 | 4.7 / 7.8 | 0 (1) | 0 |
| split: heldout | 15 | 12 | 80% (59–92%) | 0 | 0 | 2713 / 11251 | 60 / 163 / 329 | 16.3 / 16.9 | 0 (9) | 0 |
| group: security | 21 | 18 | 86% (69–94%) | 0 | 0 | 2526 / 11251 | 38 / 126 / 163 | 13.0 / 14.3 | 0 (10) | 0 |
| language: python | 21 | 18 | 86% (69–94%) | 0 | 0 | 2526 / 11251 | 38 / 126 / 163 | 13.0 / 14.3 | 0 (10) | 0 |

Limitations:
- 2 task(s) passed in some trials and failed in others
- one machine; the confidence intervals treat repeated runs of a task as independent
