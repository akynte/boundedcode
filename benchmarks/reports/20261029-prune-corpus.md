# Pruning benchmark

42 cases; budgets [2048 4096 8192 16384] bytes.

| Set | Strategy | Budget | Cases with all evidence | Evidence kept | Tokens (estimate) | Reduction | Median ms | Model prompt tokens | Errors |
|---|---|---|---|---|---|---|---|---|---|
| dev | clip | 2048 | 19 / 23 | 63 / 71 | 184485 → 12704 | 93% | 0.0 | 0 | 0 |
| dev | clip | 4096 | 21 / 23 | 69 / 71 | 184485 → 21228 | 88% | 0.0 | 0 | 0 |
| dev | clip | 8192 | 22 / 23 | 70 / 71 | 184485 → 35511 | 81% | 0.0 | 0 | 0 |
| dev | clip | 16384 | 23 / 23 | 71 / 71 | 184485 → 57569 | 69% | 0.0 | 0 | 0 |
| dev | prune | 2048 | 23 / 23 | 71 / 71 | 184485 → 10754 | 94% | 3.1 | 0 | 0 |
| dev | prune | 4096 | 23 / 23 | 71 / 71 | 184485 → 18376 | 90% | 3.0 | 0 | 0 |
| dev | prune | 8192 | 23 / 23 | 71 / 71 | 184485 → 28525 | 85% | 1.0 | 0 | 0 |
| dev | prune | 16384 | 23 / 23 | 71 / 71 | 184485 → 46904 | 75% | 1.0 | 0 | 0 |
| heldout | clip | 2048 | 12 / 19 | 52 / 64 | 76379 → 10361 | 86% | 0.0 | 0 | 0 |
| heldout | clip | 4096 | 19 / 19 | 64 / 64 | 76379 → 15841 | 79% | 0.0 | 0 | 0 |
| heldout | clip | 8192 | 19 / 19 | 64 / 64 | 76379 → 22241 | 71% | 0.0 | 0 | 0 |
| heldout | clip | 16384 | 19 / 19 | 64 / 64 | 76379 → 35038 | 54% | 0.0 | 0 | 0 |
| heldout | prune | 2048 | 17 / 19 | 62 / 64 | 76379 → 8857 | 88% | 1.1 | 0 | 0 |
| heldout | prune | 4096 | 19 / 19 | 64 / 64 | 76379 → 14095 | 82% | 0.5 | 0 | 0 |
| heldout | prune | 8192 | 19 / 19 | 64 / 64 | 76379 → 18564 | 76% | 0.5 | 0 | 0 |
| heldout | prune | 16384 | 19 / 19 | 64 / 64 | 76379 → 27517 | 64% | 0.5 | 0 | 0 |

Evidence lost (case: strategy budget: patterns missing):

- pytest-verbose (dev): clip 2048: `test_inventory\.py:\d+`
- pytest-verbose (dev): clip 4096: `test_inventory\.py:\d+`
- pytest-verbose (dev): clip 8192: `test_inventory\.py:\d+`
- python-traceback (dev): clip 2048: `run\.py", line \d+`
- node-test (dev): clip 2048: `discount applies`, `fail 1`
- mvn-test-downloads (dev): clip 2048: `expected: <100> but was: <90>`, `OrdersTest\.java:6`, `Tests run: 2, Failures: 1`, `BUILD FAILURE`
- mvn-test-downloads (dev): clip 4096: `OrdersTest\.java:6`
- go-testify-diff (heldout): clip 2048: `Error Trace:.*orders_test\.go:41`
- ruff-check (heldout): clip 2048: `billing\.py:17:5: F841`, `billing\.py:7:9: E741`
- unittest-verbose (heldout): clip 2048: `ERROR: test_no_discount`
- jest-failures (heldout): clip 2048: `price\.test\.js:42`, `Received: "\$12\.3"`
- cargo-test-two-failures (heldout): clip 2048: `panicked at src/lib\.rs:9:32`, `left: 9`
- cargo-test-two-failures (heldout): prune 2048: `left: 9`
- gradle-test-failure (heldout): clip 2048: `Stock\.java:7`, `2 tests completed, 1 failed`
- gradle-test-failure (heldout): prune 2048: `Stock\.java:7`
- mvn-compile-error (heldout): clip 2048: `COMPILATION ERROR`, `BUILD FAILURE`
