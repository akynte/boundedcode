# Known limitations

BoundedCode is pre-1.0. The architecture and enforcement tests are real; broad
effectiveness of the current 8 GB stack still needs stronger evidence.

## Task effectiveness

The September 21 Bonsai run measures synthetic inference throughput. Historical
task experiments use Qwen3.6-35B-A3B and older harness revisions. The September 17
graph ablation found no statistically detectable quality benefit and recorded
false acceptances in both arms. It must not be republished as a Bonsai success
rate. [Evidence index](../benchmarks/results/README.md).

The committed Evidence Suite v1 configuration uses a hosted generator. Its
prepared tasks and reports are evaluation infrastructure, not proof that the
local reference succeeds on those tasks.

## Inference and resources

- Reasoning can exhaust the output budget before producing a usable answer.
  Structured requests may then fail or require correction. Tasks can take minutes.
- Runtime memory exceeds the model file size. The reference has 64 GB host RAM;
  a lower-RAM full workflow has not been validated here.
- One-slot serving and process-local serialization do not prevent unrelated
  programs or concurrent CLI processes from competing for VRAM.
- `max_concurrent_tasks` is not enforced by a live memory admission scheduler.
- Saved-slot files are scoped/cleaned, but the ordinary client has no explicit
  slot save/restore workflow or external-server cache purge.
- Built-in benchmark RAM excludes external processes; its TTFT labels currently
  contain request duration; GPU sampling can miss peaks.
  [Resource details](8gb-runtime.md).

## Installation and platforms

The reference requires Prism's llama.cpp fork for PTQ1_0. The shipped CUDA image
uses stock llama.cpp, so it is not a turnkey Bonsai image. The host
[`bcode setup`](../how-to/install.md) TUI can discover a trusted runtime or
build the pinned Prism revision after confirmation, but it cannot turn the
stock image into the reference runtime. Other hardware and container layouts
still need separate validation.

Linux/NVIDIA is the measured path. CPU-only, Apple Silicon, integrated GPUs,
ROCm, Windows and other 8 GB cards need separate validation. Toolchains,
dependencies, language sidecars and container images must be provisioned before
network-restricted verification.

## Verification and security

Checks run in a fresh, disposable snapshot of the candidate, not the task
worktree, but the snapshot is writable, not immutable, and runs under the same
user and sandbox as the supervisor. Tests may be incomplete or altered by a
patch. Hidden acceptance checks are kept from the model's context, but the
candidate's code runs beside them in the snapshot and can read them, and each
reported rejection tells the model which checks failed, up to the feedback
budget.

Which tests reach a change is decided statically: the graph as of the base
commit and a same-package scan by name. Calls through function values,
reflection or generated code are missed, and only Go is examined.

The evidence chain is signed with a key the supervisor's user can read. It
detects edits to the ledger by anything else, and truncation only when checked
against the commit's `Evidence-Head`. It is not an attestation from an
independent party. Model review is fallible. Optional Jev taint
annotations do not directly change a recipe's pass status.

Host mode has no outer container boundary. Landlock port restrictions do not
provide complete network containment; MPTCP, non-TCP traffic and the ephemeral
TCP allowance limit the claim. Shared container mounts are not per-task
isolation. Namespace availability varies.
[Security boundaries](trust-boundaries.md).

The HTTP API has no authentication. Keep it on loopback. Prompt-injection fences
label repository text; they do not make the model immune to persuasion.
Editor/MCP workflows can edit the current checkout and differ from native task
worktrees.

## Repository coverage

Go and TypeScript coverage depend on compiler/type-checker availability.
TypeScript without its sidecar does not produce a resolved call graph.
Python SCIP reports missing or partial coverage; duck-typed dynamic dispatch can
remain unresolved. Rust syntax extraction is not a full semantic index.

Dynamic SQL, computed routes, unresolved imports, and unrendered Helm templates
can leave gaps. A missing graph edge means “not discovered.” Large repositories
also consume CPU time and host RAM during indexing and language-server work.

## Jev experiments

Jev is a hosted API, disabled by default. No result establishes that the pinned
model improves this project's task outcomes, reduces generation cost, or is
calibrated on these judgment sites. Default logged sites are observations;
promotion requires explicit operator configuration and supporting evidence.
The seeded test-integrity dataset is one-sided, not sufficient for calibration.
[Validation status](judgment-validation.md).

## Persistence and operation

SQLite state requires a suitable local filesystem. Schema downgrades are not
supported: back up before upgrading. Recovery inspects uncertain actions;
it cannot reconstruct evidence that was never recorded. Browser verification
requires repository-supplied tooling; browsers are not bundled.
