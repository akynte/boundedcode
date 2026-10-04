# Upstream reports for codebase-memory-mcp

Status: **drafted, not filed.** Filing is the maintainer's decision. These
four defects were confirmed on v0.11.0 and on `main` @ `96c3f41c`.
BoundedCode's own analyzers already cover all four cases
([cross-service-analysis.md](cross-service-analysis.md)).

| # | Defect | Severity for cross-service analysis | Root cause (verified) |
|---|---|---|---|
| 1 | Go 1.22 `ServeMux` patterns (`"POST /v1/x"`, `"GET host/x"`) produce no Route | high for modern Go services | route paths must start with `/` (`is_route_path_shaped` and 3 other checks) |
| 2 | Go `net/http` client calls never produce `HTTP_CALLS` | high: Go callers are never linked | external-call classification uses the raw callee text `http.Get`; the import map at that point is empty, so `net/http` never matches |
| 3 | sarama: consumer-group ID recorded as a topic (false edge); `Consume` / `ProducerMessage{Topic}` topics missed | high: wrong edges, no links | first string arg of `NewConsumerGroup` is the group ID; `Topic` is missing from the composite-field whitelist; calls on sarama-typed variables are not classified |
| 4 | TS `` fetch(`${BASE}/v1/x`) `` produces no `HTTP_CALLS` | medium | template flattened to `"{}/v1/x"`, which the URL gate rejects |

The reproduction script and full drafts are kept outside the repository
(maintainer's machine). Each draft uses minimal dummy code, the exact
commands, actual versus expected output, a passing control variant, and
source references at `96c3f41c`.
