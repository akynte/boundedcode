# Pruning benchmark

42 cases; budgets [4096] bytes.

| Set | Strategy | Budget | Cases with all evidence | Evidence kept | Tokens (estimate) | Reduction | Median ms | Model prompt tokens | Errors |
|---|---|---|---|---|---|---|---|---|---|
| dev | model | 4096 | 22 / 23 | 68 / 71 | 184485 → 70298 | 62% | 5891.8 | 217422 | 0 |
| heldout | model | 4096 | 17 / 19 | 62 / 64 | 76379 → 47286 | 38% | 3797.0 | 106060 | 0 |

Evidence lost (case: strategy budget: patterns missing):

- go-panic (dev): model 4096: `main\.go:10$`, `main\.go:14`, `exit status \d`
- make-c-errors (heldout): model 4096: `main\.c:5:14: warning: format`
- npm-ls-tree (heldout): model 4096: `nodejest@1\.0\.0`
