# Infrastructure benchmark `infra-20261003T175952Z`

* model: `qwen3-coder-next` (`Qwen3-Coder-Next-UD-IQ3_XXS.gguf`)
* runtime: llama.cpp `0.5.0-dev (build 1, commit 7fe450e)`
* machine: 13th Gen Intel(R) Core(TM) i7-13620H, 16 threads, 64028 MiB RAM, NVIDIA GeForce RTX 4060 Laptop GPU 8188 MiB (driver 615.71.09, power limit 115 W)
* started: 2026-10-03 17:59Z, finished: 2026-10-03 18:12Z
* decode tokens per request: 128; prompts: [2048 16384 49152] tokens (cold = unique prompt; warm = the conversation continued with the reply plus a new observation)
* model load: cold (model evicted from page cache before each load)
* conditions: idle; AC power

## Feasibility (minimum `n_cpu_moe` that loads)

| ctx | KV type | ubatch | min n_cpu_moe | probes |
|---|---|---|---|---|
| 65536 | q8_0 | 2048 | 41 | [24 36 42 39 40 41] |
| 131072 | q8_0 | 2048 | 43 | [24 36 42 45 43] |

## Candidates

| ctx | KV | ubatch | n_cpu_moe | load s | VRAM used/free MiB | RSS MiB | prompt t/s (median cold) | decode t/s (median) | error |
|---|---|---|---|---|---|---|---|---|---|
| 65536 | q8_0 | 2048 | 41 | 10.5 | 7396 / 792 | 23932 | 623 | 26.0 |  |
| 65536 | q8_0 | 2048 | 43 | 10.5 | 6346 / 1842 | 25043 | 608 | 25.7 |  |
| 131072 | q8_0 | 2048 | 43 | 9.5 | 7510 / 678 | 25302 | 610 | 25.3 |  |
| 131072 | q8_0 | 2048 | 45 | 9.5 | 6462 / 1726 | 26411 | 596 | 24.5 |  |

## Per-prompt detail

| config | prompt tokens | mode | cache_n | prompt ms | prompt t/s | decode t/s |
|---|---|---|---|---|---|---|
| ctx65536/q8_0/ub2048/moe41 | 2371 | cold | 0 | 5382 | 441 | 28.0 |
| ctx65536/q8_0/ub2048/moe41 | 2530 | warm | 2498 | 637 | 50 | 27.1 |
| ctx65536/q8_0/ub2048/moe41 | 19204 | cold | 11 | 30222 | 635 | 25.9 |
| ctx65536/q8_0/ub2048/moe41 | 19363 | warm | 19331 | 645 | 50 | 26.0 |
| ctx65536/q8_0/ub2048/moe41 | 58517 | cold | 11 | 93920 | 623 | 22.1 |
| ctx65536/q8_0/ub2048/moe41 | 58676 | warm | 58644 | 729 | 44 | 22.3 |
| ctx65536/q8_0/ub2048/moe43 | 2371 | cold | 0 | 5544 | 428 | 27.5 |
| ctx65536/q8_0/ub2048/moe43 | 2530 | warm | 2498 | 657 | 49 | 26.3 |
| ctx65536/q8_0/ub2048/moe43 | 19204 | cold | 11 | 31075 | 618 | 25.9 |
| ctx65536/q8_0/ub2048/moe43 | 19363 | warm | 19331 | 676 | 47 | 25.4 |
| ctx65536/q8_0/ub2048/moe43 | 58517 | cold | 11 | 96205 | 608 | 22.0 |
| ctx65536/q8_0/ub2048/moe43 | 58676 | warm | 58644 | 725 | 44 | 21.7 |
| ctx131072/q8_0/ub2048/moe43 | 2371 | cold | 0 | 5480 | 433 | 27.2 |
| ctx131072/q8_0/ub2048/moe43 | 2530 | warm | 2498 | 664 | 48 | 26.9 |
| ctx131072/q8_0/ub2048/moe43 | 19204 | cold | 11 | 30939 | 620 | 25.3 |
| ctx131072/q8_0/ub2048/moe43 | 19363 | warm | 19331 | 677 | 47 | 25.3 |
| ctx131072/q8_0/ub2048/moe43 | 58517 | cold | 11 | 95909 | 610 | 22.0 |
| ctx131072/q8_0/ub2048/moe43 | 58676 | warm | 58644 | 718 | 45 | 22.0 |
| ctx131072/q8_0/ub2048/moe45 | 2371 | cold | 0 | 5651 | 420 | 26.5 |
| ctx131072/q8_0/ub2048/moe45 | 2530 | warm | 2498 | 698 | 46 | 25.5 |
| ctx131072/q8_0/ub2048/moe45 | 19204 | cold | 11 | 31803 | 604 | 24.9 |
| ctx131072/q8_0/ub2048/moe45 | 19363 | warm | 19331 | 686 | 47 | 24.1 |
| ctx131072/q8_0/ub2048/moe45 | 58517 | cold | 11 | 98100 | 596 | 21.4 |
| ctx131072/q8_0/ub2048/moe45 | 58676 | warm | 58644 | 737 | 43 | 21.5 |

## Recommended

ctx=131072, KV=q8_0, ubatch=2048, n_cpu_moe=43: decode 25.3 t/s, prompt 610 t/s, 678 MiB VRAM free.

```
--model $MODELS_DIR/Qwen3-Coder-Next-UD-IQ3_XXS.gguf --alias qwen3-coder-next --host 127.0.0.1 --port 8765 --metrics --no-webui --ctx-size 131072 --n-gpu-layers 999 --n-cpu-moe 43 --batch-size 2048 --ubatch-size 2048 --threads 8 --parallel 1 --flash-attn on --cache-type-k q8_0 --cache-type-v q8_0 --jinja --temp 1 --top-p 0.95 --top-k 40
```
