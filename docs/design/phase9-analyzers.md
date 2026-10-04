# Phase 9: Cross-service analyzers

Rule (roadmap): add a custom analyzer only when evidence shows it is needed,
and prove that it improves outcomes.

## Evidence that motivated it

The [gap report](repointel-gap-report.md) measured that codebase-memory-mcp
v0.11.0 does not link:

* Kafka producers to consumers;
* Go 1.22 method-pattern routes;
* HTTP calls built from constants;
* Helm/env configuration to the code that reads it.

On the `payment-platform` fixture it linked 0 of 7 documented cross-service
relationships.

## What was built

`internal/xservice` ([design](cross-service-analysis.md)) extracts HTTP
routes and calls, topic producers, consumers and provisioning, and env reads
and providers. It links them across repositories and feeds them into:

* context packs;
* a local **contract check** round;
* Z3 pre-merge escalation.

It finds 7/7 fixture links with no extra links.

## Proof of benefit (ablation)

Task `event-field-rename`: rename the `PaymentCharged` event's JSON field
"in payment-service". The request names only the producer. The ledger
consumer in another repository silently breaks unless the agent discovers
it. Model: Qwen3.6-35B-A3B, local only, 3 runs per arm, alternating.
Reports: `benchmarks/reports/*-ablation-xservice-*`.

| `repointel.cross_service` | Hidden checks passed | Our verification passed | Mean wall s | Mean generated tokens |
|---|---|---|---|---|
| **on** | **3/3** | 3/3 | 267 | 4.9K |
| off | 0/3 | 3/3 (**all false passes**) | 154 | 2.3K |

With the analyzers off, every run passed our per-repository verification
while leaving the consumer reading the old field, which would break in
production. With them on, the agent either updated the consumer directly
from the contracts section (1 run) or after the contract-check round
(2 runs).

Cost: about 110 s and 2.6K generated tokens per task. No frontier use in
either arm.

## Limits

* One fixture and one task family, three runs per arm. The effect is
  large and consistent, but more task types (HTTP path change, env rename)
  should be added.
* See the limits section of [cross-service-analysis.md](cross-service-analysis.md).
