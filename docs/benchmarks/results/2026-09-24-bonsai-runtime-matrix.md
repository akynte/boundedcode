# Bonsai runtime matrix — 2026-09-24

This is the evidence boundary for the current 8 GB RTX 4060 Laptop GPU
machine. `loads` is a runtime allocation result, not a product result. An
OpenCode task result is recorded only when the task reaches a final verified
review; a timeout is a failure.

## Matrix

| Configuration | Physical result | OpenCode product result | Evidence/status |
|---|---:|---:|---|
| 32,768 / q8_0 KV | 7,124–7,134 MiB observed peak in production | **Proven for one representative task** | The strict two-session harness completed a supervised Go fix, fresh-session resume, verification and final review in 254.924 s. It did not trigger compaction, so it is not a long-history result. |
| 65,536 / q8_0 KV | CUDA OOM | Not run | Recurrent-state cache allocation fails; not a viable candidate on this host. |
| 65,536 / q4_0 KV | loads; 7,348 MiB observed, about 542 MiB free | Not run | Too little headroom to call production-safe without a real coding task and peak-memory trace. |
| 131,072 / q4_0 KV | CUDA OOM | Not run | KV allocation fails before a product request can be issued. |
| 262,144 / q4_0 KV | Not run | Not run | Do not infer viability from native model metadata; the smaller q4 allocation already failed. |
| 32,768 / q8_0 + speculative decoding | Not enabled | Not run | The current server was not started with a validated Prism/DSpark draft configuration. |

## Product metric rule

The primary metric is **time to correct completion**, not decoder throughput.
A candidate is not selected because it has more capacity or higher isolated
`llama-bench` numbers. It must complete the same supervised coding task through
OpenCode, produce a final `bc_task_finish` review, preserve the task across a
fresh session, and leave enough memory headroom for the run.

The reproducible harness is:

```text
python3 scripts/bench-opencode-continuation.py \
  --output /tmp/opencode-continuation-report.json \
  --timeout 600
```

The current 420-second two-session run is recorded as a success in
`docs/explanation/opencode-context.md`; it establishes task completion for
32K/q8_0, not capacity for larger contexts. No 64K or larger production claim
is made from the load-only results.

## Decode acceleration

The installed Prism build exposes speculative-decoding mechanisms, but no
production OpenCode run has yet measured accepted speculative tokens,
acceptance ratio, VRAM, TTFT, decode rate and time to correct completion for a
stable draft configuration. Therefore speculative decoding is **not proven**,
and it is not enabled merely because the runtime has the feature.
