# BoundedCode documentation

A local software-engineering agent built around an **8 GB VRAM budget**.
The Go supervisor supplies repository evidence, scoped execution and verification
around local inference: a ternary Bonsai model for the structured phases and
a second generator for editing.

## Start here

1. [Install the exact reference stack](how-to/install.md).
2. [Run a first task in a disposable repository](tutorials/first-task.md).
3. [Understand the architecture](explanation/architecture.md).
4. [Read the hardware evidence and limits](explanation/8gb-runtime.md).

## Understand the system

- [Reference model stack](reference/model-stack.md): Bonsai and the EDIT
  generator, optional hosted Jev,
  and the experimental CPU embedding control.
- [Repository intelligence](explanation/repository-intelligence.md): language
  analysis, graph evidence, retrieval and context budgets.
- [Verification](explanation/verification.md): checks, repair, review and gates.
- [Why small/local models can work here](explanation/why-small-models.md):
  capability through decomposition, without a claim of frontier-model parity.
- [Trust boundaries](explanation/trust-boundaries.md) and
  [isolation](explanation/isolation-model.md): protections and deployment limits.
- [Jev judgments](explanation/judgments.md): narrow semantic decisions, required
- [What leaves the machine](explanation/judgment-data-flow.md): the data flow to Jev
  egress, deterministic fallbacks and unproven domain calibration.
- [Known limitations](explanation/known-limitations.md) and
  [related work](explanation/related-work.md).

## Operate and contribute

Use the [CLI](reference/cli.md), [configuration](reference/configuration.md),
[storage layout](reference/storage-layout.md), [troubleshooting](how-to/troubleshooting.md)
and [recovery](explanation/crash-recovery.md) references.
The [OpenCode bridge](how-to/use-with-opencode.md) is optional.

[Contributing](https://github.com/akynte/boundedcode/blob/main/CONTRIBUTING.md) maps packages to responsibilities and checks.
[ADRs](adr/README.md) preserve design history; they do not override current code.
[Measurements](benchmarks/results/README.md) distinguish current inference
throughput from historical task evaluations.

[Presentation recommendations](maintainers/github-presentation.md) contain
repository metadata and a reproducible demo plan.

Only command blocks explicitly marked `<!-- test:run -->` execute in
`make docs-test`. GPU downloads and live coding examples require separate
manual validation.
