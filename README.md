# BoundedCode

[![ci](https://github.com/akynte/boundedcode/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/akynte/boundedcode/actions/workflows/ci.yml)
[![secret-scan](https://github.com/akynte/boundedcode/actions/workflows/secrets.yml/badge.svg?branch=main)](https://github.com/akynte/boundedcode/actions/workflows/secrets.yml)
[![dco](https://github.com/akynte/boundedcode/actions/workflows/dco.yml/badge.svg?branch=main)](https://github.com/akynte/boundedcode/actions/workflows/dco.yml)
[![release](https://img.shields.io/github/v/release/akynte/boundedcode?include_prereleases&sort=semver&label=release)](https://github.com/akynte/boundedcode/releases)
[![status: public alpha](https://img.shields.io/badge/status-public%20alpha-orange)](docs/releases/v0.1.0-alpha.1.md)
[![license](https://img.shields.io/github/license/akynte/boundedcode)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/akynte/boundedcode)](go.mod)

**Bounded context. Bounded cost. Unbounded codebases.**

BoundedCode is a local-first AI software-engineering platform for
long-running work on large repositories. It keeps model context bounded,
verifies results deterministically, persists task state, and escalates to a
frontier model only when its policy triggers.

> **Status: Public Alpha.** BoundedCode is usable and has been validated
> experimentally on a small independent sample of real software-engineering
> tasks. Commands, configuration and APIs may change, and it has been tested
> on one machine with one model. It is not production-ready. Issue reports,
> compatibility reports and contributions are welcome.

**Why it exists.** Coding agents on large codebases tend to fail in two
ways. They pour the repository into the model's context, which exceeds what a
local model can hold and drives up frontier cost. And they report "done"
because the tests pass, when the tests never exercised the change.

BoundedCode keeps context to small task-specific packs drawn from repository
intelligence. It runs the model locally with llama.cpp, and it accepts a
change only with behavioural evidence: a test that fails without the change
and passes with it. Tasks survive crashes and restarts.

* **Runs locally:** Linux x86-64 with an NVIDIA GPU. The reference machine is
  a laptop with 8 GB of VRAM ([details](#reference-hardware)).
* **Install:** see [Quick start](#quick-start).

## How it fits together

```text
boundedcode CLI (Go control plane)
 ├─ task ledger + audit log (SQLite) ......... persistent state: resume after crash or reboot
 ├─ git worktrees agent/<task-id> ............ isolated work; nothing is pushed or merged automatically
 ├─ context planner .......................... small task-specific packs (a few K tokens)
 │    ├─ codebase-memory-mcp ................. repository breadth: graph, impact, search
 │    ├─ Serena v1.7.0 + language servers .... semantic depth: definitions, references (optional)
 │    └─ cross-service contract analyzers .... HTTP, Kafka-style topics, env, Terraform
 ├─ agent runtime ── JSON-RPC ──> OpenHands SDK adapter, in a network-less container
 │                                  └─ model calls tunnelled back through the control plane
 ├─ inference ──> llama.cpp llama-server ──> local model (tested: Qwen3.6-35B-A3B)
 ├─ verification engine ...................... prove: build, lint, tests, behavioural evidence
 └─ frontier gate (Z1-Z4 policy) ──> Codex CLI with ChatGPT sign-in (optional, off by default)
```

| Component | Role |
|---|---|
| codebase-memory | repository breadth |
| Serena / LSP | semantic depth |
| Context planner | small task-specific packs |
| Model | reason and edit |
| Verification | prove the change |
| Persistent state | resume |

More detail: [system architecture](docs/architecture/system-architecture.md),
[product spec](docs/product-spec.md),
[implementation status](docs/development/status.md).

## Local-first

BoundedCode is designed so that most ordinary work runs on the local model.
The frontier model is a policy-triggered exception, used for:
- repeated failures,
- rejected strategies,
- architectural risk,
- a high-risk review.

In the second independent validation, all five successful tasks completed
without frontier assistance; no escalation was triggered at all. That is the
result of one small sample, not a general local-success rate.

## Validation

Initial validation exposed serious product defects. The frozen build passed
the hidden acceptance tests of **0 of 8** real public tasks, and **1 of 8**
after the first defect fixes
([report](docs/benchmarks/small-real-world-validation-2026-10.md)).

Those failures were then used as a *development corpus*, not as benchmark
evidence. They drove fixes to:
- verification,
- agent tooling,
- resource handling,
- retrieval,
- execution control.

See the [failure-driven pass](docs/benchmarks/failure-driven-engineering-2026-10.md)
and the [targeted pass](docs/benchmarks/targeted-engineering-pass-2026-10.md).

A second independent validation then used six previously unseen public
engineering tasks, selected before execution from SWE-bench Multilingual and
Multi-SWE-bench (Go, JavaScript, TypeScript, an infrastructure tool and a
1.8 M-token repository). Before the set was frozen, each task's acceptance
test was screened for consistency with its issue; 10 of 24 candidates were
rejected. Each task ran once on a frozen build:

- **5/6** satisfied BoundedCode's strict `TASK_VERIFIED` criterion and passed
  hidden acceptance.
- **6/6** hidden acceptance tests passed.
- All five strict successes used **only the local model** (0 frontier calls).
- **0** false verification passes; 0 human code intervention.

In a fresh small validation on 6 previously unseen public engineering tasks
whose hidden acceptance criteria were screened for consistency with the issue
before execution, BoundedCode completed 5/6 successfully, with 5/6 completed
using only the local model.
[Full report](docs/benchmarks/second-independent-validation-2026-10.md).

**Why 5/6 and not 6/6.** The sixth task (Prometheus) was implemented
correctly, and its hidden acceptance test passed. BoundedCode still
classified it UNVERIFIED: its evidence checker did not associate the modified
data-driven test file (`promql/testdata/functions.test`) with the Go test
function that reads it. The official score stays 5/6. Hidden-acceptance
correctness was 6/6. A verifier that withholds "verified" when it cannot
prove the change is working as intended.

> This is a small practical validation sample, not a statistically
> comprehensive benchmark.

## Quick start

The full guide, with prerequisite versions, is
[docs/usage/getting-started.md](docs/usage/getting-started.md).
Prerequisites:
- Linux x86-64
- Go (see `go.mod`)
- Git, ripgrep and Docker
- an NVIDIA GPU, with the CUDA toolkit if you build llama.cpp yourself
- `uv` (only for Serena)

```bash
git clone https://github.com/akynte/boundedcode.git
cd boundedcode
make build                                  # ./bin/boundedcode
./scripts/install-deps.sh ~/.local/bin      # gitleaks + codebase-memory-mcp (checksum-pinned)
./scripts/build-llama-cpp.sh                # pinned llama.cpp v0.5.0 with CUDA

# Download a model yourself (see "Models" below), for example:
./scripts/fetch-model.sh unsloth/Qwen3.6-35B-A3B-GGUF Qwen3.6-35B-A3B-UD-Q4_K_M.gguf \
    a483e9e6cbd595906af30beda3187c2663a1118c ~/models

L=~/.local/share/boundedcode/runtimes/llama.cpp/v0.5.0/bin
./bin/boundedcode init --models-dir ~/models \
    --llama-server $L/llama-server --llama-bench $L/llama-bench \
    --adapter-dir $PWD/adapters/openhands/python
./bin/boundedcode sandbox build --dir adapters/openhands   # agent sandbox image
./bin/boundedcode doctor                                   # checks everything above

./bin/boundedcode workspace create demo
./bin/boundedcode workspace add ~/src/my-service
./bin/boundedcode index
./bin/boundedcode task create "Return 404 instead of 500 for unknown users" \
    -c "go test ./... passes" --run
./bin/boundedcode task diff <id>        # review the agent/<id> branch like a pull request
./bin/boundedcode task resume <id>      # after Ctrl-C, a crash or a reboot
```

If you already run an OpenAI-compatible server (llama.cpp or similar), use
`init --external-url http://127.0.0.1:8080` instead of the llama.cpp flags.

## Models

Model weights are **not** distributed with this project. You download them
from their publisher and are responsible for complying with each model's
license. Profiles in [`configs/models/`](configs/models) give, for each
model:
- the upstream source and pinned revision,
- the file,
- the license,
- the llama.cpp settings.

`scripts/fetch-model.sh` checks the download against the sha256 that the hub
publishes for that revision.

`boundedcode model` inspects the profiles, and `bench infra --apply` tunes
one for your machine.

The validated configuration is **Qwen3.6-35B-A3B, UD-Q4_K_M** (Apache-2.0),
served by llama.cpp v0.5.0. Other profiles exist but were not part of the
validation.

## Frontier escalation is optional

BoundedCode runs fully local without any frontier account: escalation is
**off by default**. If you enable it:

```text
local model first ──> escalation policy (Z1-Z4) ──> frontier only when the policy triggers
```

The supported route is the Codex CLI with a ChatGPT subscription sign-in.
There is also a manual mode that writes the packet to disk for you to answer.
API keys are refused. Packets are sanitized (host paths, secrets) and require
approval unless pre-approved. See
[ADR-0009](docs/architecture/adr/0009-frontier-escalation.md).

## Verification

BoundedCode distinguishes three levels:

| State | Meaning |
|---|---|
| builds | it compiles and lints |
| `tests_green` | the repository's checks pass. Not enough: in the initial validation, patches that changed nothing passed existing tests. |
| `TASK_VERIFIED` | checks pass **and** there is behavioural evidence: a test that the change adds or modifies **fails on the base commit and passes with the change** |

Without that evidence the agent is asked once for a reproduction test.
Otherwise the task ends `tests_green` (UNVERIFIED) and is never presented as
a verified merge candidate. The verification config is read from the base
commit, so the agent cannot weaken its own gate.

This is not formal verification. Known limits:
- data-driven test files read by a test elsewhere are not attributed (the
  Prometheus case above);
- a test that does not compile on the base counts as failing there;
- a test can only prove the reading of a request that the agent chose.

## Security

The agent is treated as potentially wrong or adversarially steered (for
example by prompt injection in repository content):

- The agent's tools run in a container:
  - no network;
  - only the task worktree is writable;
  - no host home, SSH agent or credentials.
- Secret files (`.env*`, keys, cloud credentials, kubeconfigs) are masked
  in the sandbox and denied by path policy.
- Protected paths are guarded: changes to `.boundedcode/`, CI workflows or
  CODEOWNERS fail verification.
- Git metadata is checked: worktree pointers, admin dirs and `HEAD` are
  verified before host git touches them. Nothing is pushed.
- A deterministic command policy blocks push, destructive and deploy
  commands in verification.
- Host-side reads never follow symlinks out of a worktree.
- Frontier packets are sanitized, and the gate fails closed.

Details and residual risks: [SECURITY.md](SECURITY.md) and
[docs/design/sandbox.md](docs/design/sandbox.md). A container is not a
perfect boundary. Running an autonomous coding agent on untrusted repositories
still carries risk. Never run it where production credentials are reachable.

## Known limitations

1. **Small validation sample.** 8 + 6 public tasks, each run once. The second
   set was screened for issue-derivable tests; real requests are not.
2. **One machine and one model.** Other GPUs, platforms and models are
   untested.
3. **Ambiguity detection is imperfect.** The task contract is derived by the
   local model. It has flagged a clear request as ambiguous and misnamed real
   alternatives. With the default `task.ambiguity: ask`, a false positive
   costs a clarification question.
4. **Behavioural evidence misses some test layouts.** These include
   data-driven test files consumed by a test elsewhere.
5. **Frontier escalation was enabled but not exercised** in the second
   validation.
6. **The strategy governor** bounded runaway generation in development runs,
   but did not trigger during the independent validation, so it has no live
   stop on record yet.

Also:
- CLI only;
- verification presets for Go and JavaScript/TypeScript (others need
  `.boundedcode/verification.yaml`);
- cross-service analysis does not cover gRPC, OpenAPI, protobuf or SQL
  contracts;
- run `doctor` first: some run-time errors for missing dependencies (Docker
  unreachable, codebase-memory-mcp missing) name the failure or point to a
  log rather than giving an install hint
  ([fresh-clone test](benchmarks/reports/publication-20261005/fresh-clone-test.md)).

## Reference hardware

**Tested configuration (not a minimum requirement):**

| | |
|---|---|
| Machine | Lenovo LOQ 15IRH8 laptop |
| CPU | Intel Core i7-13620H |
| GPU | NVIDIA RTX 4060 Laptop, 8 GB VRAM |
| RAM | 64 GB DDR5 |
| OS | Debian 13 |
| Model | Qwen3.6-35B-A3B, UD-Q4_K_M, 131 K context, MoE experts partly on CPU |

During validation the model server used up to 29.3 GiB of RAM, and
BoundedCode itself under 70 MiB. No minimum requirement has been measured.
Smaller machines may work with smaller models or contexts, but this is
untested.

Synthetic benchmarks from earlier development (decode speed, an 11-task
engineering suite, kill/resume and cross-service ablations) are in
[benchmarks/reports/](benchmarks/reports/) and
[docs/design/model-evaluation.md](docs/design/model-evaluation.md).

## Contributing, security, license

- [CONTRIBUTING.md](CONTRIBUTING.md): every commit needs a DCO sign-off
  (`git commit -s`).
- [SECURITY.md](SECURITY.md): report vulnerabilities privately.
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) and [GOVERNANCE.md](GOVERNANCE.md).

Apache-2.0 ([LICENSE](LICENSE), [NOTICE](NOTICE)). Third-party components
and their licenses: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and the
[upstream license matrix](docs/licensing/upstream-license-matrix.md).

This project is independent and is not affiliated with, sponsored by, or
endorsed by the upstream projects or vendors it integrates with.
