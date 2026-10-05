# Benchmark reports

Raw JSON plus a Markdown summary per run. Each file records the model,
runtime build, machine snapshot, options and run conditions.

| Report | What |
|---|---|
| `20261003T152755Z-infra-qwen3.6-35b-a3b` | Full sweep: feasibility search over `n_cpu_moe` for 64K/128K ctx × ubatch 512/1024/2048, 10-min sustained thermal run. **Ran concurrently with development load**: use it for feasibility and ranking, not absolute speeds. Its sustained section ran without heavy concurrent load. |
| `20261003T161953Z-infra-qwen3.6-35b-a3b` | Finalists re-measured on an idle machine with cold model loads (page cache evicted). These are the reference numbers. |
| `*-tasks-*` | Engineering suite (hidden acceptance checks), see `benchmarks/tasks/`. |
| `small-real-world-validation-20261004/` | 8-task validation on public SWE-bench Multilingual / Multi-SWE-bench tasks: manifest, screening, frozen and post-fix runs, two-task baseline, memory-pressure evidence. Report: [docs/benchmarks/small-real-world-validation-2026-10.md](../../docs/benchmarks/small-real-world-validation-2026-10.md). |
