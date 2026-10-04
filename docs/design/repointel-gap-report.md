# Repository Intelligence Gap Report (Phase 3)

Date: 2026-10-03. Upstream: codebase-memory-mcp **v0.11.0**. Machine: the
reference laptop (see README).

Method: we indexed the [`payment-platform`](../../benchmarks/fixtures/payment-platform)
fixture (5 repositories) and ran each capability probe. For every "not
provided" result, we re-tested a **conventional variant** (plain-path
routes, literal URLs, real `github.com/IBM/sarama` imports). That separates
"unsupported idiom" from "unsupported capability". The commands are in
`scripts/repointel-probe.sh`.

## Results

| Capability | Provided? | Evidence | Gap |
|---|---|---|---|
| Exact symbol lookup | ✅ yes | `search_graph` finds `internal/api.CreatePayment` (BM25) | |
| Callers / callees | ✅ yes | `trace_path PaymentCharged`: caller `CreatePayment`, callee `SyncProducer.SendMessage` | |
| Git-diff impact (same repo) | ✅ yes | edit in `events/kafka.go` → `detect_changes` gives seed `PaymentCharged` → impacted `CreatePayment` (`internal/api`) | |
| SQL tables | ◐ partial | `Table` nodes and `WRITES` edges in both Go services | linkage from migration files to readers and writers not verified |
| Environment variables | ◐ partial | `EnvVar` nodes plus `CONFIGURES` edges (3 in payment-service) | no link from Helm `values.yaml` env to `EnvVar` |
| Terraform | ◐ partial | resources indexed as `Class` nodes | no link to the topic name or the DB it configures |
| Protobuf | ◐ partial | rpc indexed as `Route` + `HANDLES` | no link from messages to Go/TS structs |
| Cross-repo HTTP | ◐ idiom-dependent | conventional variant: literal `fetch("http://svc-a:8080/v1/payments")` + `http.HandleFunc("/v1/payments")` gives `HTTP_CALLS` and `CROSS_HTTP_CALLS` ✅ | ❌ Go 1.22 method patterns (`"POST /v1/payments"`) produce **no Route** ❌; URLs built from constants or template interpolation are not resolved |
| Kafka / async | ◐ consumer only | sarama `ConsumerGroup.Consume(..., ["payments.charged"])` gives `ASYNC_CALLS` | ❌ sarama `SyncProducer.SendMessage(&ProducerMessage{Topic: ...})` is not detected, so there is **no producer→consumer link** (`cross_async_calls: 0`). Local wrapper interfaces are not detected. |
| Cross-repo change impact | ❌ no | a diff in payment-service does not reach ledger-service | follows from the async and HTTP gaps above |
| ADR storage | ✅ yes (not yet exercised) | `manage_adr` | |

## Performance

| Operation | Measured |
|---|---|
| `index_repository` (small repo, `full`) | 5.6–6.0 s wall, about 1.3 s CPU, dominated by fixed startup |
| One-shot `cli <tool>` call | about 4.0 s wall, a temporary daemon is started per call |
| Same call with a warm `daemon` | about 1.55 s (client startup alone is about 1.3 s CPU) |
| **Persistent MCP stdio session** | **about 4.2 s once, then ~12 ms per `search_graph`, ~0.26 s per `detect_changes`** |

Decision (ADR-0006): the control plane keeps one persistent MCP session per
command or task. One-shot CLI calls remain the fallback.

## Operational findings

* By default the daemon and MCP server start a **web UI listener on
  127.0.0.1:9749** and enable file watchers. Settings are stored per
  `CBM_CACHE_DIR` (`config.json`, `_config.db`). Our client uses a private
  cache dir and disables `ui_enabled`, `auto_watch` and `watcher_enabled`
  there, so the user's global settings are never touched.
* `XDG_CONFIG_HOME` does **not** isolate settings; `CBM_CACHE_DIR` does.

## Resolution (2026-10-03)

The HTTP, topic, Terraform and env gaps are now closed by BoundedCode's own
analyzers ([cross-service-analysis.md](cross-service-analysis.md)). They
complement codebase-memory-mcp, which still provides the code graph,
search, tracing and same-repo impact. On this fixture they find 7/7
documented cross-service links, against 0/7 linked by codebase-memory-mcp
v0.11.0.

## What this meant for the roadmap (original analysis)

* No custom analyzer is written yet. Phase 9 requires a real task failure
  to justify one. The probes show **where** failures will come from:
  1. a Kafka topic linker (producer `Topic:` constant ↔ consumer
     subscription) for cross-service event impact;
  2. Go 1.22 `ServeMux` method-pattern routes;
  3. Helm/Kustomize env ↔ application `EnvVar` linkage.
* All three are upstream-shaped improvements to an MIT project. Contributing
  fixes upstream is preferable to a parallel analyzer. **No upstream issues
  have been filed**, since that is external publication and needs the
  maintainer's approval.
* For context packs, the planner already gets exact symbols, call
  neighborhoods and same-repo impact cheaply (~12 ms per query).
