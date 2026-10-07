# Upstream reports for codebase-memory-mcp

Status: **filed** on 2026-10-04 as
[#2537](https://github.com/DeusData/codebase-memory-mcp/issues/2537),
[#2538](https://github.com/DeusData/codebase-memory-mcp/issues/2538),
[#2539](https://github.com/DeusData/codebase-memory-mcp/issues/2539) and
[#2540](https://github.com/DeusData/codebase-memory-mcp/issues/2540); all
four were still open on 2026-10-07, with v0.11.0 still the latest release.
These four defects were confirmed on v0.11.0 and on `main` @ `96c3f41c`.
BoundedCode's own analyzers already cover all four cases
([cross-service-analysis.md](cross-service-analysis.md)).

| Issue | Defect | Severity for cross-service analysis | Root cause (verified) |
|---|---|---|---|
| [#2537](https://github.com/DeusData/codebase-memory-mcp/issues/2537) | Go 1.22 `ServeMux` patterns (`"POST /v1/x"`, `"GET host/x"`) produce no Route | high for modern Go services | route paths must start with `/` (`is_route_path_shaped` and 3 other checks) |
| [#2538](https://github.com/DeusData/codebase-memory-mcp/issues/2538) | Go `net/http` client calls never produce `HTTP_CALLS` | high: Go callers are never linked | external-call classification uses the raw callee text `http.Get`; the import map at that point is empty, so `net/http` never matches |
| [#2539](https://github.com/DeusData/codebase-memory-mcp/issues/2539) | sarama: consumer-group ID recorded as a topic (false edge); `Consume` / `ProducerMessage{Topic}` topics missed | high: wrong edges, no links | first string arg of `NewConsumerGroup` is the group ID; `Topic` is missing from the composite-field whitelist; calls on sarama-typed variables are not classified |
| [#2540](https://github.com/DeusData/codebase-memory-mcp/issues/2540) | TS `` fetch(`${BASE}/v1/x`) `` produces no `HTTP_CALLS` | medium | template flattened to `"{}/v1/x"`, which the URL gate rejects |

The reproduction script and full drafts are kept outside the repository
(maintainer's machine). Each draft uses minimal dummy code, the exact
commands, actual versus expected output, a passing control variant, and
source references at `96c3f41c`.
