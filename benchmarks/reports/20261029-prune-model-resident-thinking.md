# Pruning benchmark

42 cases; budgets [4096] bytes.

| Set | Strategy | Budget | Cases with all evidence | Evidence kept | Tokens (estimate) | Reduction | Median ms | Model prompt tokens | Errors |
|---|---|---|---|---|---|---|---|---|---|
| dev | model | 4096 | 21 / 23 | 69 / 71 | 184485 → 19962 | 89% | 24613.6 | 217376 | 21 |
| heldout | model | 4096 | 19 / 19 | 64 / 64 | 76379 → 15841 | 79% | 22530.7 | 106022 | 19 |

Evidence lost (case: strategy budget: patterns missing):

- pytest-verbose (dev): model 4096: `test_inventory\.py:\d+`
- mvn-test-downloads (dev): model 4096: `OrdersTest\.java:6`
