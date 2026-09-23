# Validation status

This replaces an older implementation-stage checklist. Historical answers about
the previous MoE runtime are not validation of the current Bonsai stack.

| Question | Status as of 2026-09-21 |
|---|---|
| Does the current generator serve at 32K context on an 8 GB device? | Measured on one RTX 4060 Laptop; [raw record](../benchmarks/results/2026-09-21-bonsai-runtime.md) |
| Is there a reproducible Bonsai end-to-end task-success benchmark? | Not yet; older task runs used Qwen MoE |
| Is the entire workflow's peak VRAM/RAM known? | No; current GPU samples are post-request and whole-device, RAM field is the client |
| Is the stock Docker image the tested Bonsai distribution? | No; pinned ternary runtime packaging needs validation |
| Are host/container sandbox boundaries universal? | No; kernel and namespace availability vary; see [isolation](../explanation/isolation-model.md) |
| Has Podman/ROCm/CPU-only parity been demonstrated? | Not by current reference evidence |
| Are TypeSafe judgments calibrated for these tasks? | Not established; see [judgment validation](../explanation/judgment-validation.md) |
| Does a vector database power normal retrieval? | No; lexical/graph retrieval is the default, embedding cosine is an evaluation control |
| Are SQLite performance results a container-volume comparison? | No; historical storage results do not establish named-volume versus bind-mount parity |

Future results should disclose the code revision, runtime/model identity, hardware,
sampling method, task set and failures—not just replace an unchecked box with
“works.” See [benchmark methodology](../benchmarks/METHODOLOGY.md).
