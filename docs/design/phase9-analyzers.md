# Phase 9: Cross-service analyzers (decision: none yet)

Rule (roadmap): add a custom analyzer only when **real task failures**
prove that the missing knowledge caused them.

Evidence to date:

| Source | Failures | Caused by missing graph knowledge? |
|---|---|---|
| 11-task suite × 3 models (33 runs) | 2 (both `cross-service-idempotency`) | No. The bug was a concurrency race inside one handler, and both repos were in the task. |
| 6 repeat runs of that task | 3 | No (same race) |
| Milestone 1 and Phase 7 runs | 1 | No (same race) |

The Phase 3 gap report found real **capability** gaps in codebase-memory-mcp
v0.11.0:

* no Kafka producer→consumer topic linking;
* no Go 1.22 method-pattern routes;
* no Helm↔EnvVar links.

None of them has yet caused a measured task failure. The fixture tasks name
the affected repositories explicitly, which masks the gaps.

**Decision:** no analyzer is implemented now.

**Next step to collect evidence:** add benchmark tasks where the *agent must
discover* the affected service. An example is "rename the PaymentCharged
field `amount_cents`" given only payment-service, which must find the
ledger consumer. Measure failure with and without impact context. If the
topic linker proves necessary, contributing it upstream (MIT) is preferred
over a local analyzer.
