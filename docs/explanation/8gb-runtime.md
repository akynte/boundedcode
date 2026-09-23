# Engineering around 8 GB of VRAM

The target is a complete local coding workflow on an 8 GB consumer GPU.
The current reference machine is an RTX 4060 Laptop GPU with 8,188 MiB and
64 GB of installed system RAM. The GPU capacity is a design constraint; it is
not the whole machine's memory requirement.

## Where resources go

| Work | Resource | Mechanism actually implemented |
|---|---|---|
| Generate plans, patches and review | GPU + runtime host memory | Bonsai PTQ1_0, every layer on the GPU; 32K context and one slot; resident for the whole task |
| Locate code and consumers | CPU/RAM/disk | FTS5, analyzers, graph traversal, SCIP import, selective source reads |
| Preserve task knowledge | RAM/disk | SQLite workflow state, transcript, read evidence, compaction cards |
| Verify | CPU/RAM/disk | Repository compiler, tests and analyzers in sandboxed child processes |
| Optional embedding control | CPU/RAM | 0.6B Q8_0 service, GPU layers set to zero; evaluation path |
| Required Jev judgments | Network/API | Typed questions; no local weights, no VRAM |

## Weight fit is only the beginning

Bonsai's 5.95 GB file does not imply 5.95 GB of runtime VRAM. The server also
allocates KV state and compute buffers; graphics applications may already own
part of the card. The reference requests all eligible layers on GPU, flash
attention, Q8_0 KV, a 2048 batch and 512 microbatch.
[Exact profile](https://github.com/akynte/boundedcode/blob/main/profiles/bonsai-2-27b-8gb-cuda.yaml).

The working context is 32,768 tokens, regardless of the model's larger native
window. The profile caps ordinary retrieval packets at 12,000 tokens and reserves
output room. Structured phases have their own total/output/reasoning budgets:

| Phase | Context budget | Output allowance | Requested reasoning allowance |
|---|---:|---:|---:|
| LOCALIZE | 26,000 | 8,192 | 4,096 |
| PLAN | 28,000 | 10,240 | 5,120 |
| EDIT | 28,000 | 8,192 | 4,096 |
| REVIEW | 30,000 | 10,240 | 5,120 |

Token estimation and request admission live in `contextpack` and `task`.
A reasoning allowance is a request to the server, not a proven hard limit on the
model's internal reasoning. Long reasoning can consume the output cap and leave
no final structured answer.

The older MoE profile keeps experts in host RAM and attention on GPU. That is a
different resource tradeoff. **Bonsai is dense and does not use CPU MoE offload.**

## Spend less context on each question

Localization proceeds through map, signature and body views. Retrieval combines
lexical anchors with bounded graph expansion and required consumers/contracts.
Large tool output is summarized at its source and retained separately as
evidence. The graph is incomplete; the model can still search and request
targeted reads.

A phase has a frozen prefix, an append-only evidence log, and a changing tail.
Preserving the prefix can reuse the server's prompt cache. Crossing a bounded
context boundary creates a new state card from recorded facts, edits, checks
and failed actions. This avoids relying only on the model's recollection.

This is an implemented prompt layout, not a guarantee of cache hits on every
call. The HTTP client records reported cache counts. Historical
[Qwen prefix-reuse measurements](../benchmarks/results/README.md#phase-cache-baseline-2026-09-18)
show the mechanism; they are not a Bonsai task-speedup result.

The store creates saved-slot directories and clears their files when switching
workspaces. The normal inference client does **not** save/restore llama.cpp
slots or clear a shared external server's live KV state. Do not infer those
guarantees from the directory names.

## Serialize inference and respect ownership

External llama.cpp providers serialize calls to the same endpoint **within one
Go process**. Optional managed providers additionally share one owned process
slot: stop the old process before starting the next; refuse an occupied external
port; verify the executable hash; wait for health; time out rather than load a
second process after failed shutdown.

Embedded inference is a supervised child of `bcode api`. External inference,
used by the walkthrough, remains operator-owned. There is no automatic unload
after each phase.

The reference configuration uses one generator for every role, so it loads once
and stays resident; nothing is swapped between phases. A configuration that
routes a role to a second generator shares the same slot and pays a model load
at each phase that changes model.

These mechanisms do not coordinate unrelated `bcode` processes or other GPU
programs. `max_concurrent_tasks` is profile metadata; there is no runtime
scheduler enforcing it against live free VRAM. Ledger leases protect worktrees,
not the GPU. Run one native task/inference workload at a time on the reference
card.

## Hardware requirements and confidence

| Item | Reference / expectation |
|---|---|
| OS | Linux x86-64 reference; Linux-specific task sandboxing |
| GPU | RTX 4060 Laptop, 8,188 MiB verified; other 8 GB cards need measurement |
| Driver/runtime | Reference driver 595.84; Prism built with CUDA 13.3 for compute capability 8.9 |
| CPU | Reference Intel i7-13620H, 16 logical CPUs; indexing and builds can be substantial |
| RAM | 64 GB reference. 32 GB may fit smaller workloads but is not a validated full-workflow minimum |
| Disk | Exact model 5.95 GB; plan at least 20 GB free for source/runtime/model/basic caches, plus repositories and task images (estimate, not measured minimum) |
| Go | 1.27.1 toolchain, cgo and C compiler |
| Optional tooling | Node 22 for TypeScript/Python semantic sidecars; Docker for container layouts/evaluation runtimes |

Laptop power limits, cooling, CPU load and shared display use affect throughput.
An 8 GB GPU with little free memory is not equivalent to an idle one. CUDA
compatibility must match the chosen runtime build; the observed driver is not a
claimed minimum driver version.

CPU-only, Apple Silicon, integrated GPUs, ROCm and Windows are not validated by
the current Bonsai reference measurement. Profiles for some alternatives remain
in the source tree; their existence is not a support matrix.

## Measure honestly

```bash
bcode models bench --iterations 3 --prompt-tokens 2000 --output-tokens 256 --json
```

The built-in benchmark is useful for relative throughput, with important limits:

- Its `ttft_*` fields currently contain **whole non-streaming request duration**,
  not time to first token.
- `peak_vram_mb` samples total GPU memory **after** each request; it can miss
  peaks and includes unrelated GPU users. Values come from MiB-based nvidia-smi.
- `peak_ram_mb` is the `bcode` benchmark process's RSS; it excludes the external
  model server, sidecars and verification children.
- `--write` generates a heuristic profile. It does not preserve the Bonsai
  profile's tuned phase budgets and all runtime settings. Inspect and merge
  measurements before changing the active profile.

See [the current measurement](../benchmarks/results/2026-09-21-bonsai-runtime.md)
for exact commands, hashes and scope. End-to-end task peaks, cold startup and
representative Bonsai task success remain to be measured.
