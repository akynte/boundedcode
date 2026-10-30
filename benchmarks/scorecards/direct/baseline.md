# Scorecard: direct suite

3 trial(s) (direct-20261029T230036Z, direct-20261029T233514Z, direct-20261030T001534Z). Model qwen3.6-35b-a3b (sha256 ac0e2c1189e0), server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); build dev b409ab02d7b9-dirty; task set 8aa8af4958a24eb3; 13th Gen Intel(R) Core(TM) i7-13620H NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02.

| | Runs | Passed | Success (90% CI) | False acceptance | False rejection | Tokens per success / failure | Median / p90 / p95 s | Model / tool calls | Security failures (attempts) | Tampered |
|---|---|---|---|---|---|---|---|---|---|---|
| **all** | 153 | 144 | 94% (90–96%) | 0 | 0 | 4270 / 3811 | 27 / 96 / 142 | 6.3 / 7.1 | 0 (1) | 0 |
| split: dev | 93 | 89 | 96% (91–98%) | 0 | 0 | 4385 / 5488 | 26 / 85 / 109 | 6.0 / 6.2 | 0 (0) | 0 |
| split: heldout | 60 | 55 | 92% (84–96%) | 0 | 0 | 4085 / 2470 | 29 / 101 / 145 | 6.7 / 8.4 | 0 (1) | 0 |
| group: everyday | 90 | 88 | 98% (94–99%) | 0 | 0 | 2733 / 2906 | 21 / 53 / 61 | 3.9 / 4.3 | 0 (0) | 0 |
| group: long-running | 27 | 24 | 89% (75–96%) | 0 | 0 | 8546 / 1739 | 51 / 147 / 172 | 10.4 / 8.6 | 0 (0) | 0 |
| group: polyglot | 15 | 12 | 80% (59–92%) | 0 | 0 | 8631 / 6595 | 101 / 156 / 211 | 13.6 / 20.5 | 0 (0) | 0 |
| group: security | 21 | 20 | 95% (81–99%) | 0 | 0 | 3288 / 3487 | 37 / 84 / 85 | 6.0 / 7.4 | 0 (1) | 0 |
| language: c | 3 | 3 | 100% (53–100%) | 0 | 0 | 8580 / 0 | 96 / 227 / 227 | 2.0 / 3.0 | 0 (0) | 0 |
| language: cpp | 3 | 3 | 100% (53–100%) | 0 | 0 | 1647 / 0 | 22 / 23 / 23 | 2.0 / 1.0 | 0 (0) | 0 |
| language: csharp | 3 | 3 | 100% (53–100%) | 0 | 0 | 1192 / 0 | 16 / 17 / 17 | 2.0 / 1.0 | 0 (0) | 0 |
| language: go | 66 | 59 | 89% (82–94%) | 0 | 0 | 5875 / 3881 | 36 / 109 / 147 | 7.4 / 8.3 | 0 (0) | 0 |
| language: java | 3 | 3 | 100% (53–100%) | 0 | 0 | 1072 / 0 | 8 / 8 / 8 | 2.0 / 1.0 | 0 (0) | 0 |
| language: javascript | 9 | 9 | 100% (77–100%) | 0 | 0 | 1726 / 0 | 23 / 47 / 47 | 5.3 / 4.0 | 0 (0) | 0 |
| language: php | 3 | 3 | 100% (53–100%) | 0 | 0 | 1744 / 0 | 27 / 28 / 28 | 2.0 / 2.0 | 0 (0) | 0 |
| language: python | 45 | 43 | 96% (87–98%) | 0 | 0 | 4159 / 3566 | 37 / 87 / 120 | 7.3 / 8.9 | 0 (1) | 0 |
| language: ruby | 3 | 3 | 100% (53–100%) | 0 | 0 | 997 / 0 | 10 / 11 / 11 | 2.0 / 1.7 | 0 (0) | 0 |
| language: rust | 3 | 3 | 100% (53–100%) | 0 | 0 | 1459 / 0 | 11 / 12 / 12 | 2.0 / 1.0 | 0 (0) | 0 |
| language: typescript | 27 | 24 | 89% (75–96%) | 0 | 0 | 5310 / 6595 | 29 / 145 / 156 | 9.5 / 13.7 | 0 (0) | 0 |

Limitations:
- 5 task(s) passed in some trials and failed in others
- 6 task run(s) skipped (a toolchain is missing)
- one machine; the confidence intervals treat repeated runs of a task as independent
