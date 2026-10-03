# ADR-0007: Default local model

Status: **provisional** (2026-10-03). Awaiting the maintainer's license
decision on OpenMDW-1.1.

## Context
Phase 8 measured three candidates on the reference laptop. The full
results are in [model-evaluation.md](../../design/model-evaluation.md). The
spec ranks candidates by verified task success, then reliability, then
speed.

* **Success:** Laguna XS 2.1 passed 11/11 tasks. It also passed the hard
  cross-service idempotency task in 4/4 runs, where Qwen3.6-35B-A3B passed
  1/4. Qwen3-Coder-Next (3-bit) scored 10/11 and was slower.
* **Speed:** Qwen3.6 completed 18.2 verified tasks per hour versus
  Laguna's 7.0. Laguna generates about 3× more tokens on routine tasks.
* **License:** Qwen3.6 is Apache-2.0. Laguna uses **OpenMDW-1.1**, a custom
  permissive model license with a patent-termination clause. It is not on
  the SPDX list. Under our policy that is "manual review".

## Decision
1. Following the spec's ranking, **Laguna XS 2.1 is the measured best
   model** on this suite.
2. Until the maintainer reviews OpenMDW-1.1, `default_model` stays
   **qwen3.6-35b-a3b**. Changing it is legally consequential, because the
   documentation would steer users to that license.
3. If OpenMDW-1.1 is approved, switch with a config change
   (`default_model: laguna-xs-2.1`). No code changes are needed, because
   models are profiles.
4. Users can opt in now: `boundedcode task create … -m laguna-xs-2.1`.

## Consequences
* With Qwen3.6, the hard concurrency-sensitive task needed frontier review
  (Z3) to pass, as measured in Phase 7. With Laguna it passed locally.
  Switching the default would likely lower the frontier escalation rate, at
  a throughput cost on routine work.
* **Candidate follow-up (planned, not implemented):** a local escalation
  tier, where Z1/Z2-flagged tasks switch to Laguna before going to the
  frontier. Measured cost is a model swap of about 8 s cold load. This is
  justified by the 4/4 versus 1/4 result, but needs its own benchmark.
