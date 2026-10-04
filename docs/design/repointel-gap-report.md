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
| Cross-repo HTTP | ◐ idiom-dependent | conventional variant: literal TS `fetch("http://svc-a:8080/v1/payments")` + Go `http.HandleFunc("/v1/payments")` gives `HTTP_CALLS` and `CROSS_HTTP_CALLS` ✅ | ❌ Go 1.22 method patterns (`"POST /v1/payments"`) produce **no Route** ❌; URLs built from constants or template interpolation are not resolved |
| Kafka / async | ❌ no (corrected 2026-10-03) | The `ASYNC_CALLS` edge first read as "consumer detected" is a **false positive**: it comes from `sarama.NewConsumerGroup(brokers, groupID, cfg)` and treats the **consumer-group ID** as the topic | ❌ The topic in `ConsumerGroup.Consume(ctx, []string{"t"}, h)` is not extracted. ❌ `SyncProducer.SendMessage(&ProducerMessage{Topic: "t"})` is not detected (the `Topic` field is not in the composite-field whitelist, and the method call on a sarama-typed variable is not classified). So there is **no producer→consumer link** (`cross_async_calls: 0`). |
| Go stdlib HTTP client | ❌ no (found 2026-10-03) | `http.Get/Post/NewRequest(..., "http://host/path")` produce no `HTTP_CALLS`; the URL is extracted, but classification of external calls uses the raw callee text (`http.Get`), which never matches the `net/http` pattern | Go callers are never linked cross-repo |
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

## Re-verification (2026-10-03, before reporting upstream)

Every gap was re-checked with minimal two-repository reproductions and
controls, against both the v0.11.0 release and `main` @ `96c3f41c` built
from source (identical results). Root causes were confirmed with an
instrumented build. This corrected one earlier finding (the "consumer
detected" result was a false positive) and found one more gap (Go stdlib
HTTP clients). The four confirmed defects, with reproductions and drafted
reports, are summarized in
[upstream-reports.md](upstream-reports.md). The Helm/env and Terraform
items are feature requests, not defects, and are not reported. An observed
`name`-override anomaly could not be reproduced deterministically, so it is
not reported either.

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
