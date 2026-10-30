# Scorecard: direct suite

1 trial(s) (direct-20261030T005043Z). Model qwen3.6-35b-a3b (sha256 ac0e2c1189e0), server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); build dev b409ab02d7b9-dirty; task set c857abf9d6cf7e53; 13th Gen Intel(R) Core(TM) i7-13620H NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02.

| | Runs | Passed | Success (90% CI) | False acceptance | False rejection | Tokens per success / failure | Median / p90 / p95 s | Model / tool calls | Security failures (attempts) | Tampered |
|---|---|---|---|---|---|---|---|---|---|---|
| **all** | 16 | 13 | 81% (61–92%) | 0 | 0 | 20310 / 20662 | 118 / 550 / 651 | 10.2 / 10.2 | 0 (0) | 0 |
| split: heldout | 16 | 13 | 81% (61–92%) | 0 | 0 | 20310 / 20662 | 118 / 550 / 651 | 10.2 / 10.2 | 0 (0) | 0 |
| group: everyday | 16 | 13 | 81% (61–92%) | 0 | 0 | 20310 / 20662 | 118 / 550 / 651 | 10.2 / 10.2 | 0 (0) | 0 |
| language: c | 2 | 1 | 50% (12–88%) | 0 | 0 | 20107 / 21899 | 118 / 157 / 157 | 8.5 / 9.5 | 0 (0) | 0 |
| language: go | 2 | 1 | 50% (12–88%) | 0 | 0 | 3400 / 30811 | 35 / 651 / 651 | 4.5 / 3.0 | 0 (0) | 0 |
| language: java | 2 | 2 | 100% (42–100%) | 0 | 0 | 57147 / 0 | 44 / 550 / 550 | 22.5 / 23.0 | 0 (0) | 0 |
| language: javascript | 1 | 1 | 100% (27–100%) | 0 | 0 | 12174 / 0 | 145 / 145 / 145 | 10.0 / 9.0 | 0 (0) | 0 |
| language: php | 2 | 2 | 100% (42–100%) | 0 | 0 | 12054 / 0 | 61 / 157 / 157 | 11.5 / 11.5 | 0 (0) | 0 |
| language: python | 2 | 2 | 100% (42–100%) | 0 | 0 | 6873 / 0 | 30 / 56 / 56 | 6.5 / 6.0 | 0 (0) | 0 |
| language: ruby | 2 | 2 | 100% (42–100%) | 0 | 0 | 6095 / 0 | 18 / 137 / 137 | 3.0 / 2.0 | 0 (0) | 0 |
| language: rust | 2 | 2 | 100% (42–100%) | 0 | 0 | 32009 / 0 | 152 / 300 / 300 | 15.5 / 17.5 | 0 (0) | 0 |
| language: typescript | 1 | 0 | 0% (0–73%) | 0 | 0 | 0 / 9276 | 96 / 96 / 96 | 10.0 / 10.0 | 0 (0) | 0 |

Limitations:
- 1 trial(s): fewer than three, so run-to-run noise is barely estimated
- one machine; the confidence intervals treat repeated runs of a task as independent
