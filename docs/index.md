# BoundedCode documentation

BoundedCode is a supervised local coding environment built around OpenCode.
The supported user path is deliberately short:

1. install the `bcode` executable;
2. run the [`bcode setup` TUI](how-to/install.md) once;
3. run [`bcode opencode`](how-to/use-with-opencode.md) from any project.

The TUI owns dependency checks, runtime/model configuration, provider wiring,
decision-plane setup, sandbox validation, and initial data preparation. A
normal `bcode opencode` session starts the required services automatically and
cleans them up when OpenCode exits.

## Start here

- [Install and use BoundedCode](how-to/install.md)
- [OpenCode integration and session lifecycle](how-to/use-with-opencode.md)
- [CLI reference](reference/cli.md)
- [Architecture](explanation/architecture.md)
- [Trust boundaries and isolation](explanation/trust-boundaries.md)

## Understand the system

- [Repository intelligence](explanation/repository-intelligence.md)
- [OpenCode context and durable task memory](explanation/opencode-context.md)
- [Verification and evidence](explanation/verification.md)
- [Judgments and what leaves the machine](explanation/judgment-data-flow.md)
- [Reliability, cancellation, and cleanup](explanation/reliability.md)
- [Known limitations](explanation/known-limitations.md)

## Operate and contribute

Use the [configuration reference](reference/configuration.md),
[troubleshooting guide](how-to/troubleshooting.md), [recovery notes](explanation/crash-recovery.md),
and [contribution guide](https://github.com/akynte/boundedcode/blob/main/CONTRIBUTING.md).

Only the TUI-based setup page is the supported installation workflow. Pages
under benchmarks, architecture, and evidence describe evidence and design; they
are not alternate installers.
