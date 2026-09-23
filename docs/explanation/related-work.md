# Related work and differentiation

Research snapshot: **2026-09-21**. This is a conceptual comparison of documented
designs, not a head-to-head benchmark or an exhaustive priority search.

## What already exists

| Prior work | Relevant established capability | What BoundedCode is trying to add |
|---|---|---|
| [Aider repository maps](https://aider.chat/docs/repomap.html) | Graph-ranked, token-budgeted repository maps | Compiler/SCIP evidence, impact obligations and phase-specific context in a supervised task state machine |
| [Aider lint/test repair](https://aider.chat/docs/usage/lint-test.html) | Automatic checks and repair after edits | Candidate identity, persisted evidence, exact write grants and explicit finalization gates |
| [OpenHands local models](https://docs.openhands.dev/openhands/usage/llms/local-llms) | Local model serving as part of an engineering agent | A specific ternary runtime and 8 GB reference, rather than endpoint compatibility alone |
| [mini-SWE-agent environments](https://mini-swe-agent.com/latest/advanced/environments/) and [local models](https://mini-swe-agent.com/v2/models/local_models/) | Local/container execution and local inference configuration | A larger deterministic supervisor that owns localization, impact, planning constraints and evidence |
| [local-agent-arena](https://github.com/jimenezcarrero/local-agent-arena) | Memory-tiered local agent evaluation, including an 8 GB Jetson tier | A concrete product workflow; the arena also illustrates why low-memory agent work is not an exclusive claim |

A Jetson's shared memory is not the same resource configuration as a discrete
8 GB RTX GPU. Conversely, an agent process using little host RAM while calling a
hosted model says nothing about fitting its inference into 8 GB of VRAM.

Repository maps, tests, sandboxes, repair loops and local-model endpoints are
established techniques. BoundedCode should not present their mere presence
as novel, or imply that competing tools cannot run on small GPUs.

## The defensible distinction

BoundedCode's engineering focus is the combination:

**An 8 GB reference inference configuration + deterministic repository analysis +
bounded native agent phases + candidate-linked verification and recovery.**

The testable hypothesis is that reducing the problem presented to the generator
can make a constrained local stack useful on real repository changes. This
differs from a weight-loading demonstration: task completion also depends on
context quality, tool reliability, compiler/test environments, retry cost and
false acceptance.

This is an architectural distinction, not a measured superiority result. The
current Bonsai evidence establishes synthetic inference on the reference
machine. Earlier local task evaluations used a different model and did not
establish a graph-driven success advantage.
[Evidence](../benchmarks/results/README.md).

## Jev and novelty

TypeSafe's [Jev announcement](https://typesafe.ai/blog/introducing-system-one-models-and-jev)
is dated September 15, six days before this review. The code already contains
typed, redacted judgment calls with fallbacks and authority tiers. That is a
timely integration, not evidence of being the first integration.

- **Verified priority/novelty:** none established by this research.
- **Potentially unusual combination:** low-VRAM ternary inference, rich
  deterministic orchestration and separately tiered probabilistic judgments.
  This is an assessment, not a proven research novelty claim.
- **Interesting differentiation:** explicit hardware/runtime identity,
  consumer-impact evidence, persisted failure recovery and honest false-accept
  accounting.
- **Not established:** first/only 8 GB engineering agent, frontier-agent parity,
  Jev correctness gains, lower total generation cost, or better task results
  than the systems above.

The most valuable next comparison is a fixed Bonsai/runtime/hardware task study:
same tasks and budgets, with and without supervision/graph, scored by independent
acceptance tests. Add optional Jev and the local embedding control only after
their input parity, served model identity and disclosure are recorded.
