# Two-task baseline: plain OpenHands vs. BoundedCode, same local model

Baseline: one OpenHands session (same SDK 1.51.0, same sandbox image, same
Qwen3.6-35B-A3B UD-Q4_K_M on the same llama.cpp server and laptop, same
150-iteration cap and condenser settings, same offline sandbox, same task
text and base commit) with no context packs, code graph, Serena,
verification loop, retries or frontier escalation (`bench tasks
--baseline`). Acceptance is the same hidden dataset check. Tasks were fixed
in the manifest before any run. BoundedCode values are the original frozen
runs (build 0217d96).

| | gin-gonic__gin-3227: BoundedCode | gin: baseline | caddyserver__caddy-6288: BoundedCode | caddy: baseline |
|---|---|---|---|---|
| Verified result (hidden tests) | **failed** | **failed** | **failed** | **failed** |
| Same failing test | `TestRedirectTrailingSlash` | `TestRedirectTrailingSlash` | `TestCaddyfileAdaptToJSON` (`expression_quotes`) | `TestCaddyfileAdaptToJSON` |
| Self-verified by BoundedCode | yes (false pass) | n/a | yes (false pass) | n/a |
| Wall-clock | 4,953 s | 285 s | 1,797 s | 835 s |
| Attempts | 3 | 1 | 1 | 1 |
| Frontier calls | 1 (Z2, Codex) | 0 | 0 | 0 |
| Local model calls | 167 | 35 | 52 | 52 |
| Local prompt tokens | 4.46 M | 1.02 M | 1.96 M | 1.76 M |
| Source context tokens: context packs | 10,968 | 0 | 1,195 | 0 |
| Source context tokens: tool output (upper bound) | 44,127 | 17,945 | 42,302 | 35,695 |
| Human intervention | ENVIRONMENT_ONLY (repo verification config) | NONE | ENVIRONMENT_ONLY (repo verification config) | NONE |

Both systems made the same kind of change in the wrong place for gin
(`gin.go` instead of `tree.go`) and both left the dataset's
`expression_quotes` case unsolved for caddy.

**On these two tasks BoundedCode did not improve the local model's result.**
It spent more time and tokens (retries, verification, one frontier call) to
reach the same failures, and its verification gate passed changes that the
hidden tests reject. Two tasks are far too few to generalize in either
direction.
