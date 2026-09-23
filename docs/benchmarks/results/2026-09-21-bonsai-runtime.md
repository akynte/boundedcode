# Bonsai on an 8 GB laptop GPU — 2026-09-21

Three synthetic inference requests on the current Bonsai reference server
measured 264.4 prompt tokens/s and 27.7 generated tokens/s. This is a runtime
measurement, not an end-to-end coding evaluation.

## Provenance

| Field | Observed value |
|---|---|
| BoundedCode | `a3cd20c06c71e0614dd72b5a134ae8771cf0cf79`; runtime source unchanged during documentation work |
| Command | `bcode models bench --iterations 3 --prompt-tokens 2000 --output-tokens 256 --json` |
| GPU | NVIDIA GeForce RTX 4060 Laptop GPU; 8,188 MiB |
| Driver | 595.84 |
| OS / kernel | Ubuntu 26.04 LTS / Linux 7.0.0-31-generic |
| CPU | Intel Core i7-13620H; 16 logical CPUs |
| RAM | 64 GB installed; Linux reported 62,741 MiB usable |
| Runtime | PrismML-Eng/llama.cpp `1a07bfa5f4144274c8f1c9963821dd9d9a51854b` |
| Build | Release, CUDA 13.3, architecture 89 |
| Server binary SHA-256 | `ce5fc7b1346d2b9314d09d4c646533e3fce7c1b7261c5a3a50149b1b3a42ba68` |
| Model | `Ternary-Bonsai-2-27B-PTQ1_0.gguf`; 5,946,648,928 bytes |
| Model SHA-256 | `53107f530aa52eb00912263ab1ee29bd199261c87cd7b4ad4ca1318c1fe33ee3` |

The existing model server was idle before the measurement. It was not restarted.
Other desktop applications were running, as was a CPU-only Qwen embedding
server. No hosted model or Jev call participated.

Server arguments, with the model directory made portable:

```bash
llama-server -m /path/to/Ternary-Bonsai-2-27B-PTQ1_0.gguf \
  -c 32768 --parallel 1 -ngl 99 -fa on -ctk q8_0 -ctv q8_0 \
  -b 2048 -ub 512 -t 8 -tb 16 \
  --temp 1.0 --top-p 0.95 --top-k 20 --min-p 0.0 --jinja \
  --host 127.0.0.1 --port 8080
```

The benchmark overrides temperature to zero and disables thinking for its
synthetic requests. That is different from native coding tasks using the
profile's reasoning settings.

## Results and interpretation

[Raw benchmark JSON](2026-09-21-bonsai-runtime.json).

| Reported field | Value | What it actually establishes |
|---|---:|---|
| Prefill rate | 264.37 tokens/s | Uses server-reported phase timing; warm near-total cache hits are excluded from prefill samples |
| Decode rate | 27.70 tokens/s | Mean of request-level output-token/phase-time ratios |
| Cache reuse | 65.34% | Aggregate reuse across three requests with a shared prefix |
| Highest VRAM sample | 7,691 MiB | Whole GPU, sampled after each request; not a continuous or model-only peak |
| Client RSS sample | 21 MiB | The `bcode` benchmark process only; excludes external inference and tools |
| Request duration fields | 9,658 ms | Both `ttft_p50_ms` and `ttft_p95_ms` report this; they are not TTFT measurements |
| `declared_context` | 0 | Provider capability metadata was unset; server `/props` separately confirmed 32,768 |

A read-only process inspection before the benchmark reported approximately
913 MiB RSS for the Bonsai server and 1.53 GiB for the CPU embedding server.
These instantaneous RSS observations are not full memory footprints: page cache,
mapped weights, CUDA allocations, child processes and peak loading behavior need
separate accounting. The host also had other workloads and used swap, so this
run does not establish a minimum system RAM requirement.

## Reproduce

Follow [the reference installation](../../how-to/install.md), ensure the server
is idle, and run the benchmark command above. Keep raw JSON, model and binary
hashes, server arguments and hardware details together. The public model
revision used for the pinned download is
`6ed5e12bf84b7a63069882c91dd9e9218647d17b`.

No continuous GPU sampling, cold-start timing, coding task, task success rate,
verification overhead or Bonsai graph ablation was measured in this run.
The evidence supports inference on this 8 GB machine, not a universal hardware
guarantee or comparison with cloud agents.
