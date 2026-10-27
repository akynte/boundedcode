# Engineering benchmark `suite-20261027T203021Z`

model: `qwen3.6-35b-a3b` · started 2026-10-27 20:30Z · finished 2026-10-27 20:36Z

| metric | value |
|---|---|
| tasks | 1 |
| verified (hidden checks) | 1 (100%) |
| local-only completion rate | 100% |
| frontier escalation rate | 0% |
| verified tasks / hour | 9.48 |
| attempts / successful task | 2.00 |
| wall-clock / task | 380 s |
| processed local tokens / task (uncached prompt + generated) | 46452 |
| self-verified but hidden checks failed | 0 |

| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |
|---|---|---|---|---|---|---|---|---|---|
| go-large-iface-method | interface-change-large-repo | true | completed | false | 2 | 46452 | 0 | 380 |  |

Repository intelligence (context packs and agent tool use):

| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |
|---|---|---|---|---|---|---|---|---|---|---|
| go-large-iface-method | 2 | 7037 | 3183 | 0 (0) | 0 | 20 | 259 | 0/10 | 0 | 27 |

Where the time went, and why a task failed:

| task | language | model calls | model s | prefill s | decode s | verification runs (passed) | verification s | cache invalidations | peak sandbox MiB | peak server MiB | failure | stalls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| go-large-iface-method | go | 19 | 219 | 76 | 142 | 4 (4) | 32 | 1 | 1238 | 29070 | verification missed | not recorded |

Environment: build dev 25aafe6fab96; linux/amd64; 13th Gen Intel(R) Core(TM) i7-13620H (16 threads, 62741 MiB); GPU NVIDIA GeForce RTX 4060 Laptop GPU, 8188 MiB, 595.99.02; server llama.cpp 0.5.0-dev (build 11146, commit 7fe450e19); model Qwen3.6-35B-A3B-UD-Q4_K_M.gguf (sha256 ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61); tasks 99dd33b0a5b0a9a9.
