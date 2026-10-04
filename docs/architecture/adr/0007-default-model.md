# ADR-0007: Default local model

Status: **accepted** (2026-10-03). Maintainer decision: keep Qwen3.6-35B-A3B
as the default.

## Context
Phase 8 measured three candidates on the reference laptop. The full
results are in [model-evaluation.md](../../design/model-evaluation.md).

* **Success:** Laguna XS 2.1 passed 11/11 tasks. It also passed the hard
  cross-service idempotency task in 4/4 runs, where Qwen3.6-35B-A3B passed
  1/4. Qwen3-Coder-Next (3-bit) scored 10/11 and was slower.
* **Speed:** Qwen3.6 completed 18.2 verified tasks per hour versus
  Laguna's 7.0. Laguna generates about 3× more tokens on routine tasks.
* **License:** Qwen3.6 is Apache-2.0. Laguna uses OpenMDW-1.1, a custom
  model license with a patent-termination clause. It is not on the SPDX
  list.

## Decision
* `default_model: qwen3.6-35b-a3b`. It has the best throughput, a standard
  permissive license, and its one measured weakness (the concurrency-
  sensitive cross-service task) is caught by deterministic verification and
  Z3 frontier review.
* Laguna XS 2.1 and Qwen3-Coder-Next remain supported profiles. Users can
  opt in per task (`-m laguna-xs-2.1`) after reviewing the model license.
  The project does not recommend or redistribute either.

## Consequences
* Hard, concurrency-sensitive changes are more likely to need the Z3
  pre-merge review with Qwen3.6. The frontier escalation rate should be
  tracked per task category.
* **Planned:** a local escalation tier that switches Z1/Z2-flagged tasks to
  a stronger local model before using the frontier. It needs its own
  benchmark, and it is the user's choice because of model licensing.
