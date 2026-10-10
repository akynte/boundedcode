---
title: BoundedCode
summary: >-
  An open-source control plane in Go for AI coding agents. It runs the OpenHands agent with a
  local llama.cpp model inside a network-less container, keeps every task in a crash-safe SQLite
  ledger, and refuses to call a change verified until a test it adds fails on the original code
  and passes with the change. It was evaluated against the same agent without it, and that
  negative result is published.
hardest_part: >-
  Deciding when an agent's change is actually verified. Green tests proved nothing: in the first
  validation, 4 of 8 "verified" results failed the hidden acceptance tests. The gate now rebuilds
  the base commit with the change's tests, checks against a pristine control, and requires each
  test to pass on the change. It does this without letting the agent touch its own checks.
category: AI developer tooling · Go control plane
problem: >-
  Coding agents overflow context on large repositories and report success because tests are
  green, even when no test exercises their change.
context: personal-lab
repo: akynte/boundedcode
source: boundedcode
period: Oct 2026 — public alpha
role: Sole designer and maintainer
stack: [Go, SQLite, Docker, llama.cpp, OpenHands SDK, gitleaks, Sigstore, GitHub Actions]
evidence:
  - boundedcode-fail-before-pass-after
  - boundedcode-crash-resume
  - boundedcode-task-isolation
  - boundedcode-sandbox-hardening
  - boundedcode-prompt-injection-containment
  - boundedcode-secret-handling
  - boundedcode-bounded-context
  - boundedcode-escalation-policy
order: 20
updated: "2026-10-10"
---

## Problem

Coding agents fail in two predictable ways on real repositories:
- **They overflow context.** A whole repository does not fit a local
  model and is expensive on a frontier model.
- **They mistake green tests for done.** They report success because the
  suite passes, even when no test exercises the change.

The second failure was measured, not assumed. On 8 public SWE-bench-style
tasks, the first build's gate accepted any change whose checks passed.
**4 of 8 results it called verified then failed the datasets' hidden
acceptance tests.** The existing tests already passed on the untouched
code, and two of the patches changed nothing that mattered.

## Context

BoundedCode is a control plane around existing tools; it does not replace
them:
- **llama.cpp** for local inference;
- **the OpenHands agent SDK** for the agent loop;
- **a code-graph index** and **language servers** for retrieval;
- **gitleaks** for secret scanning.

They run as separate processes or pinned dependencies, never as forks.
BoundedCode's own parts are:
- the task ledger;
- the context planner;
- the sandbox and command policy;
- the verification engine with behavioural evidence;
- the cross-repository compatibility gate.

It is a public alpha for one engineer running long tasks on their own
machine.

## Constraints

- **One laptop:** an 8 GB consumer GPU and 64 GB of RAM. The default model
  decodes at 39 tokens/s with a short prompt and 24 tokens/s near its
  116 K-token limit, so context must be small and deliberate.
- **Untrusted code:** the agent's code is untrusted, and text in the
  repository can prompt-inject it.
- **Durable tasks:** tasks must survive Ctrl-C, crashes and reboots
  without losing work.
- **No action on the user's behalf:** nothing is pushed, merged or
  released for the user.

## Architecture

::diagram{name="boundedcode-loop"}

**Context packs.**
- Each attempt starts from a budgeted context pack: 24 K tokens in ten
  prioritised sections.
- On the held-out tasks, a 1.8 M-token repository needed under 30 K tokens
  of context.

**Agent boundary.**
- The agent runs in a container with no network and no capabilities.
- Its model calls are tunnelled over stdio JSON-RPC to a gateway on the
  host, which meters them and holds any cloud API keys.

**Work and verification.**
- Work lands as commits on a task branch in its own git worktree.
- Verification runs in the sandbox, configured from the **base commit**,
  so the agent cannot edit its own gate.

**Durability.** A SQLite ledger with leases records every attempt, so a
task resumes after a crash.

## Hard problems

**Telling "verified" apart from "green".** The gate builds two trees from
the base commit: one with the change's tests laid over it, and one
pristine control. A test counts as evidence only if it:
1. fails with the change's tests on the base,
2. does not fail in the control,
3. **passes** on the change.

It does not count a test that fails to build on the base (new API proves
nothing about behaviour), or one that is skipped on the change. Without
evidence, a green task ends as `tests_green` and is reported UNVERIFIED.

::evidence{id="boundedcode-fail-before-pass-after"}

**Surviving a SIGKILL mid-turn.** A test kills a real runner process:
1. While the lease is fresh, a second runner is refused.
2. A third runner takes over once the lease is stale and marks the
   orphaned attempt rejected.
3. The killed attempt's edits are kept.
4. The task finishes from a context pack rebuilt from the ledger.

::evidence{id="boundedcode-crash-resume"}

**Containing an agent that has been turned against you.**
- Verification configuration is read from the base commit, and protected
  paths fail the diff check.
- Context reads never follow a planted symlink out of the worktree.
- The command policy normalises wrappers, global flags and `sh -c` bodies,
  so `bash -lc "git -C . push"` is still a push.

::evidence{id="boundedcode-prompt-injection-containment"}

**A sandbox escape found by audit, not by tests.**
- **The attack.** An agent could create a nested git repository in its
  worktree with a malicious clean filter in that repository's config. The
  host's own `git status` would then run the filter **on the host**.
- **How it was found.** A security review reproduced it; the adversarial
  suite had not caught it.
- **The fix.**
  - host git now ignores submodules and refuses a worktree holding a
    nested repository;
  - a related bypass through git's quoted file names was closed in the
    same commit;
  - both have regression tests and shipped in v0.1.0-alpha.5.

::evidence{id="boundedcode-task-isolation"}

## Decisions

- [ADR-0002](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/docs/architecture/adr/0002-embedded-sqlite-state.md): embedded SQLite for control-plane state. One file, transactional, no service to run.
- [ADR-0003](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/docs/architecture/adr/0003-container-sandbox.md): the agent runs in a container sandbox, not on the host. Agent permission prompts are not treated as a boundary.
- [ADR-0004](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/docs/architecture/adr/0004-adapter-protocol.md): JSON-RPC over stdio, with model calls tunnelled to the host. A Unix socket does not reach a Docker Desktop VM, and stdio lets the container keep no network at all.
- [ADR-0006](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/docs/architecture/adr/0006-persistent-mcp-session.md): a persistent session to the code graph instead of one-shot CLI calls. About 12 ms per query instead of about 4 s.
- [ADR-0009](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/docs/architecture/adr/0009-frontier-escalation.md): frontier escalation is an optional exception, triggered by a deterministic policy. No model decides whether to escalate.

## Rejected alternatives

:::callout{kind="trade-off"}
**Accepting green checks** was rejected after it produced 4 of 8 false verifications. The cost of
requiring evidence is real and measured: correct changes that cannot be demonstrated by a test end
UNVERIFIED. In the comparative evaluation, that was 3 of BoundedCode's 8 runs.
:::

:::callout{kind="decision"}
**Forking the upstream agent and indexing tools** was rejected. They run unmodified, as separate
processes or pinned dependencies, so BoundedCode stays a control plane that can follow upstream releases.
:::

:::callout{kind="trade-off"}
**The fastest model, not the most accurate.** Laguna XS 2.1 solved more synthetic tasks, but it was
2.6× slower and its license is not on the SPDX list. Qwen3.6-35B-A3B was chosen, and verification is
relied on to catch its weaknesses.
:::

## Failure model

| Failure | Behaviour | Evidence |
|---|---|---|
| Runner killed mid-task | A lease blocks a concurrent runner; a stale lease is taken over; the attempt resumes from the ledger | [boundedcode-crash-resume](/proof/boundedcode-crash-resume) |
| Change passes tests that do not exercise it | Classified `tests_green` (UNVERIFIED), not `task_verified` | [boundedcode-fail-before-pass-after](/proof/boundedcode-fail-before-pass-after) |
| Agent rewrites the verification config | Ignored: config comes from the base commit; the path is protected | [boundedcode-prompt-injection-containment](/proof/boundedcode-prompt-injection-containment) |
| Git admin directory redirected, or a nested repository planted | The task blocks before host git runs | [boundedcode-task-isolation](/proof/boundedcode-task-isolation) |
| Secret scanner missing | The full gate errors instead of passing silently | [boundedcode-secret-handling](/proof/boundedcode-secret-handling) |
| Unproductive attempt | The strategy governor stops it (token and time limits) and records it; the strategy is not repeated | — |
| Out of attempts, tokens or time | The task is blocked, not failed; its work stays on the task branch | — |

## Security model

**The container:**
- The agent and everything it runs are untrusted.
- The container gets no network, drops all capabilities, runs with
  no-new-privileges, and never mounts the home directory, `~/.ssh`, cloud
  credentials or the Docker socket.
- Secret paths are masked; more than 500 matches refuses the run instead
  of partly masking.

**The host:**
- Diffs are secret-scanned.
- Escalation packets have host paths rewritten and are refused if any
  remain.

**Residual risks** are published. Secrets committed to git history stay
readable inside the sandbox, and build scripts are agent-writable, so
verification catches mistakes, not an adversary.

::evidence{id="boundedcode-sandbox-hardening"}

::limitations{id="boundedcode-sandbox-hardening"}

## Observability

Every task writes an audit log to the ledger: attempts, strategies,
verification stages, evidence decisions, escalations and recoveries. The
evaluation reports are built from those records.

## Tests and verification

**On every push and pull request:**
- `gofmt`, `go vet`, `go test ./...` and `go test -race ./...`;
- govulncheck and golangci-lint;
- license checks;
- cross-builds for six platforms;
- a gitleaks scan of the full history;
- a DCO check.

**What the check marks don't show.** Skipped tests are listed with their
reasons in the job summary, and failing macOS and Windows tests raise a
warning instead of passing silently. Tests that need the sandbox image run
locally; a weekly CI job for them was added in October 2026.

**Releases** are built from the tag in CI and attested with SLSA build
provenance. An independent local rebuild of v0.1.0-alpha.5 reproduced
every binary and SBOM byte for byte.

## Benchmarks

The evaluations are task-outcome studies on public SWE-bench-style tasks,
judged by the datasets' hidden acceptance tests. Each runs one machine,
one local model and one run per task.

- **Initial validation: 0 of 8, then 1 of 8.** It showed that green checks
  are weak evidence.
- **Held-out validation:** on 6 screened tasks never used in development,
  5 were verified and passed the hidden tests, with no false verification.
- **Comparative evaluation (pre-registered), against the same agent and
  model without BoundedCode:**
  - **Outcome:** 7 of 8 against 6 of 8. **No difference is shown**: the
    one task where they differed changed between two baseline runs.
  - **Cost:** BoundedCode took 2.3× the time.
  - **The gate on the plain agent's patches:** it rejected every one that
    failed the hidden tests, and as many that passed.

The reports, raw records, deviations and failures are in the repository:
[evaluation overview](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/docs/benchmarks/README.md) ·
[comparative report](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/benchmarks/comparative-2026-10/report.md).

## Incidents

No incident replays are published for this project.

## AI assistance and human verification

From the project's README: "I designed the architecture, the threat model, the verification model and the
evaluation protocol, and made the release and scope decisions. Implementation, test runs and first drafts
of the reports were produced with heavy use of AI coding agents, under that design and review." The
validation reports are published unedited, including results that did not support a release.

## Reproduce

```bash
git clone https://github.com/akynte/boundedcode.git && cd boundedcode
git checkout v0.1.0-alpha.5
go test ./...          # what CI runs, minus tests that need the sandbox image
go test -race ./...
gh attestation verify boundedcode-linux-amd64 -R akynte/boundedcode   # a downloaded release binary
```

[A recorded real run](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/docs/assets/evidence-demo/demo.gif)
shows the gate asking for a test and verifying it (with its
[unedited log](https://github.com/akynte/boundedcode/blob/5e1bea487b3352aeb381b9616fa89080e5a12e02/docs/assets/evidence-demo/session.log)).

## Limitations

- **Small sample, no proven advantage.** Validated on small samples,
  8 + 6 + 8 public tasks, each run once, with one model on one machine. It
  shows no proven advantage over the same agent and model without
  BoundedCode, and costs about twice as much.
- **Missed verifications.** Behavioural evidence withholds "verified" from
  correct changes when no test can demonstrate them offline.
- **Platforms.** macOS and Windows are experimental, and some test
  packages fail there.
- **Unmeasured features.** The cross-repository compatibility gate, cloud
  providers and frontier escalation are implemented and tested, but have
  not been measured on real tasks.

## At 10x scale

The single SQLite ledger and per-machine sandbox are deliberate for a
single-user tool. Running many engineers' tasks would first need:
- a shared task store with real leases, such as Postgres with row locks;
- a scheduler for GPU time.

The control plane's state boundaries were drawn so those parts could
change without changing the verification gate.

## Source and evidence

**Repository:** [akynte/boundedcode](https://github.com/akynte/boundedcode)
at release v0.1.0-alpha.5. Every link above points at commit `5e1bea4`.

**Long-form case study:**
[engineering case study](https://github.com/akynte/boundedcode/blob/main/docs/public-launch/engineering-case-study.md).

**Evidence matrix:** [/proof](/proof?capability=security).
