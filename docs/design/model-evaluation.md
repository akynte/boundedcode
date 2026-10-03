# Model Evaluation (Phase 8)

Date: 2026-10-03. Machine: reference laptop (i7-13620H, RTX 4060 Laptop
8 GB, 64 GB DDR5), llama.cpp v0.5.0, OpenHands SDK 1.51.0 in the container
sandbox, frontier **disabled**. Each model's server flags come from its own
`bench infra` sweep, not guesses.

## Infrastructure (128K ctx, ubatch 2048, q8_0 KV, cold load)

| Model | Quant / size | min `n_cpu_moe` (64K / 128K) | Chosen | Decode t/s @2K / @19K / @58K | Prompt t/s @19K | Load |
|---|---|---|---|---|---|---|
| Qwen3.6-35B-A3B | UD-Q4_K_M / 22.1 GB | 31 / 33 | 35 | 39.4 / 35.9 / 30.2 | 777 | 9.0 s |
| Laguna XS 2.1 | Q4_K_M / 20.3 GB | 32 / 36 | 36 | ~38 / ~36 / ~31 | ~750 | 7.5 s |
| Qwen3-Coder-Next | UD-IQ3_XXS / 28.5 GB | 41 / 43 | 43 | 26.5 / 24.9 / 21.4 | 604 | ≈8 s |

Reports: `benchmarks/reports/*-infra-*`. Qwen3-Coder-Next at Q4 (≈48 GB)
does not fit alongside a desktop session in 64 GB RAM. The 3-bit quant is
the largest practical one, which handicaps its quality.

## Engineering suite (11 tasks, hidden acceptance checks, local only)

| Model | Verified | Verified tasks/hour | Wall s/task | Attempts/success | Generated tokens/task | Processed tokens/task | Self-verified but hidden-failed |
|---|---|---|---|---|---|---|---|
| **Qwen3.6-35B-A3B** | 10/11 | **18.2** | **180** | 1.00 | 3.3K | 23K | 1 |
| **Laguna XS 2.1** | **11/11** | 7.0 | 517 | 1.27 | 10.6K | 69K | 0 |
| Qwen3-Coder-Next (IQ3) | 10/11 | 9.8 | 333 | 1.00 | 4.8K | 26K | 1 |

The only discriminating task is `cross-service-idempotency`. It needs a
race-free idempotency implementation, checked by concurrent same-key
requests under `-race`. Qwen3.6 and Coder-Next both left the
check-then-act race. Laguna avoided it on the first run.

Repeat runs of that task: see the table below, which is filled in from
`benchmarks/reports/*-repeat-xsvc-*`.

| Model | Suite run | Repeat 1 | Repeat 2 | Repeat 3 | **Total** | Wall s (repeats) |
|---|---|---|---|---|---|---|
| Qwen3.6-35B-A3B | ✗ | ✗ | ✗ | ✓ | **1/4** | 1022 / 863 / 667 |
| Laguna XS 2.1 | ✓ | ✓ | ✓ | ✓ | **4/4** | 838 / 1122 / 1008 |

On the hard task, Laguna's per-task time is similar to Qwen3.6's: both
spend 11–19 minutes. The 2.6× speed gap appears on routine tasks.

## Reading the results

* On this suite all three models solve routine Go, SQL, Docker, K8s,
  Terraform and TypeScript changes locally on the first attempt.
* The hard, concurrency-sensitive cross-service change separates them.
  Frontier review (Z3) fixed it for Qwen3.6 (Phase 7).
* Laguna generates about 3× more tokens (longer reasoning). That is the
  main reason it is about 2.6× slower per verified task.
* **Limitations.** Eleven synthetic tasks on small repos, with one full run
  per model plus repeats of the discriminating task. This is enough to rank
  infrastructure and catch gross differences. It is not enough to claim a
  general success rate. Larger real-repository tasks are the next
  benchmark priority.

## Decision

See [ADR-0007](../architecture/adr/0007-default-model.md).
