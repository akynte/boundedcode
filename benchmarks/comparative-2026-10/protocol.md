# Comparative evaluation protocol: BoundedCode vs. a plain OpenHands agent

Status: **frozen before any comparative run.** The git commit that adds
this file, together with `frozen-tasks.json`, is the freeze point. Every
later change is listed in [`deviations.md`](deviations.md), and none may
alter a recorded result.

## 1. Questions

1. On which tasks does BoundedCode produce a change that passes the
   dataset's hidden acceptance tests?
2. On which tasks does the baseline?
3. On which tasks do both fail, and why?
4. Does behavioural verification add measurable value? This is measured
   as the agreement of BoundedCode's `TASK_VERIFIED` with the hidden
   result, compared with checks-only (`tests_green`). BoundedCode's gate
   is also replayed on the baseline's patches.
5. Can any difference be attributed to BoundedCode's architecture rather
   than to the evaluation set-up?
6. What remains unproven?

The study is not designed to show superiority. With at most 12 paired
tasks, it can only describe what happened on those tasks.

## 2. Systems

| | BoundedCode (system **B**) | Baseline (system **O**) |
|---|---|---|
| What it is | The BoundedCode control plane at the frozen commit: task contract, context packs from repository intelligence, OpenHands agent, verification (build, lint, tests, secret scan, behavioural evidence), retries, ledger | A single session of the OpenHands Software Agent SDK: the request text verbatim, the same tools, nothing else |
| Implementation | `boundedcode bench tasks` | `boundedcode bench tasks --baseline` (`internal/benchmark/baseline.go`) |
| Agent SDK | openhands-sdk / openhands-tools 1.51.0 (`adapters/openhands/python/uv.lock`) | the same |
| Agent tools | terminal, file editor, task tracker | the same |
| Model | Qwen3.6-35B-A3B UD-Q4_K_M (sha256 `ac0e2c11…`), through the same llama.cpp server | the same server |
| Sandbox | `boundedcode-openhands:local` (`sha256:9eb682a0…`), no network, the same mounts | the same |
| Iterations per agent session | 150 | 150 |
| Sessions | up to 6 attempts (retries after failed verification, one request for a test) | 1 |
| Token budget | 4,000,000 processed tokens | none (bounded by the 150 iterations and the time limit) |
| Wall-clock limit per task | 60 min | 60 min |
| Frontier escalation | off | not applicable |
| Ambiguity policy | `task.ambiguity: proceed` (no human answers questions) | not applicable |
| Serena | off | not applicable |

**Why this baseline.** It is publicly available (OpenHands Software Agent
SDK). It uses the same agent loop, tools, model, sandbox and limits as
BoundedCode, so a difference in outcome can be attributed to what
BoundedCode adds around that loop, and not to a different model or agent.

Other public agents were not used:
- SWE-agent, mini-swe-agent, Aider, OpenHands' full application and
  Claude Code / Codex would each change the model or the tool set, or both.
- Several need network access or a paid API.
- Running them on this local model in the same offline sandbox would
  require adaptations that themselves become a confound.

This baseline answers "does the control plane help this agent and model?".
It does not answer "is BoundedCode better than other products?".

**The local model server.** It is the one llama.cpp server already running
on the reference machine:
- llama.cpp v0.5.0, commit `7fe450e1`, CUDA sm_89;
- arguments recorded in `environment.json`: context 131072,
  `--n-cpu-moe 35`, flash attention, q8_0 KV cache;
- sampling temperature 0.6, top-p 0.95, top-k 20, one slot.

Runs are sequential and never concurrent.

## 3. Tasks

**Source.** SWE-bench Multilingual at revision `846e647b`. The parquet file
is pinned by sha256 in `manifest.json`.

**Selection rule** (`select.py`, fixed before screening):
1. **Slots.** There are six slots, defined in `manifest.json`:
   - **go-dev** (gin, prometheus, caddy) is the only slot from BoundedCode's
     development repositories, reported as the stratum **development**;
   - **jsts**, **rust**, **php**, **ruby** and **java** come from
     repositories never used in BoundedCode work, reported as the stratum
     **unseen**.
2. **Exclusions.** Excluded are:
   - every instance id named anywhere in `benchmarks/reports/` or
     `docs/benchmarks/` (earlier development, validation or screening);
   - instances whose reference patch changes a dependency manifest;
   - instances whose evaluation script has no recognisable test command.
3. **Ranking.** Eligible instances are ranked by
   `sha256("boundedcode-comparative-2026-10-09:" + instance_id)`. The six
   best-ranked per slot are screened.
4. **Gate 1, environment only.** In the offline sandbox, the hidden
   acceptance command must fail on the base and pass with the reference
   patch (`bench tasks --screen-gold`). The first two candidates per slot,
   in rank order, that pass gate 1 are the task set. A slot with fewer is
   not filled from elsewhere.

**No derivability screen.** Unlike the second validation, tasks are not
screened for acceptance tests derivable from the issue. That screen removed
the failure classes BoundedCode had shown, so it is omitted here. Both
systems face the same tasks, unscreened for difficulty or clarity.

**Changes before screening** (from `manifest.json`):
- The original JS/TS slot (axios, vuejs/core) had no eligible instance,
  because all of them had been used before. It was replaced by repositories
  never used: babel, docusaurus, immutable-js and three.js.
- Test commands that call tools absent from the sandbox image were mapped
  to the same binaries:
  - `mvnd` → `mvn`;
  - `yarn jest` / `yarn test` → `node_modules/.bin/jest`.

  Gate 1 confirms that each adapted command still fails on the base and
  passes with the reference patch.

## 4. Isolation and fairness

- **No access to the answers.** Agents never see the hidden test patch, the
  reference patch, the dataset or the screening record:
  - those live under `~/.cache/bc-comparative/tasks`, which is never
    mounted into a sandbox;
  - the hidden test patch is applied only after the agent finishes, with
    the files it touches reset to the base first.
- **No history.** Each repository is checked out at its base commit
  without git history, so no future commit is visible.
- **No later versions in the caches.** The evaluation uses its own
  dependency caches (`GOMODCACHE`, `CARGO_HOME`, a Maven repository),
  filled only with each base commit's dependencies. The machine's shared
  caches cannot contain later versions of the evaluated projects.
- **Same environment for both.** The same request text, base commit,
  dependencies, sandbox and acceptance command are used for both systems.
- **Order.** Systems alternate per task: the parity of
  `sha256(seed + "order:" + id)` decides which runs first. Runs are
  strictly sequential, and the machine runs nothing else heavy.
- **One run per system per task.**
  - **Reruns.** A run is repeated once only after an **infrastructure
    failure**: the harness failed before the agent's first model call
    (for example a Docker error). Both records are kept.
  - **No other reruns** are allowed: a bad result stands.

## 5. Measurements (machine-readable, `results/`)

Per run:
- **Outcome:**
  - hidden acceptance (pass/fail);
  - task status;
  - for B, its verification state (`task_verified`, `tests_green`,
    failing).
- **Effort:**
  - attempts;
  - wall-clock seconds;
  - processed, generated and cached tokens;
  - model calls.
- **Resources:** peak memory of the model server, BoundedCode, the Docker
  VM and containers (`sampler.py`, every 10 s).
- **The agent's patch:** saved before the hidden tests are applied.
- **Cost.** Local inference has no API cost. Energy is not measured.
  Tokens are reported, so a reader can price them for any provider.

After all runs:
- **Gate replay.** Each baseline patch is applied, unchanged, by a
  scripted agent in a fresh BoundedCode task. The hidden acceptance is
  re-run to confirm the replay reproduced the patch. BoundedCode's own gate
  (full verification, one request for a test, which the scripted agent
  declines, then the behavioural-evidence check) records the state it
  would have given the baseline's change.

## 6. Analysis plan (fixed)

1. **Per-task table:** the hidden outcome of B and O, B's verification
   state, the gate-replay state of O's patch, time, tokens and peak memory.
2. **Paired counts:** both pass, B only, O only, neither. Also the exact
   two-sided McNemar (binomial) p-value on the discordant pairs, reported
   only for completeness; with n ≤ 12 it cannot establish a difference.
   No rate is extrapolated beyond the tasks run. The strata (development,
   unseen) are reported separately.
3. **Verification value.** Over all patches (B's runs and O's replays):
   - the precision of `TASK_VERIFIED` against the hidden result;
   - the precision of "checks pass" (`tests_green` or `task_verified`);
   - false passes (verified, but hidden fails);
   - false negatives (not verified, but hidden passes).
4. **Failure analysis.** Each failed run gets one primary category,
   assigned by the analyst from the logs, the patch, the issue and the
   reference patch, with a short justification:
   - **ENVIRONMENT:** harness, sandbox or dependencies;
   - **TIMEOUT_OR_BUDGET**;
   - **NO_CHANGE:** the agent changed nothing relevant;
   - **WRONG_LOCATION:** the change is in the wrong place;
   - **INCOMPLETE:** part of the required behaviour;
   - **WRONG_BEHAVIOUR:** the right place, wrong behaviour;
   - **SPEC_GAP:** the hidden test requires details the issue does not
     state;
   - **TOOL_DEFECT:** a BoundedCode or harness defect.

   The categories are judgements, and are labelled as such.
5. **Attribution.** A difference is discussed as possibly architectural
   only when:
   - neither run has an ENVIRONMENT or TOOL_DEFECT category;
   - the patches differ in a way the architecture explains, for example a
     retry after failed verification, or a test requested by the evidence
     gate.

   With one run per task, sampling variation in the model is an
   alternative explanation for every single-task difference, and the
   report says so.

## 7. Frozen task set

Gate 1 results are in [`screening.json`](screening.json). The task set is
in [`frozen-tasks.json`](frozen-tasks.json): 8 tasks, fewer than the 12
planned, because slots are not refilled from elsewhere.

| Slot | Screened | Passed gate 1 | Picked |
|---|---|---|---|
| go-dev | 5 | 5 | gin-gonic__gin-3820, gin-gonic__gin-4003 |
| jsts | 6 | 1 | immutable-js__immutable-js-2006 |
| rust | 6 | 6 | sharkdp__bat-2393, tokio-rs__axum-691 |
| php | 6 | 4 | phpoffice__phpspreadsheet-3463, briannesbitt__carbon-3103 |
| ruby | 6 | 1 | fluent__fluentd-3616 |
| java | 6 | 0 | none |

| Task | Slot | Stratum | Base | Runs first |
|---|---|---|---|---|
| gin-gonic__gin-3820 | go-dev | development | `9f598a31aafb` | BoundedCode |
| gin-gonic__gin-4003 | go-dev | development | `9c081de9cdd1` | BoundedCode |
| immutable-js__immutable-js-2006 | jsts | unseen | `493afba6ec17` | BoundedCode |
| sharkdp__bat-2393 | rust | unseen | `7c847d84b0c3` | BoundedCode |
| tokio-rs__axum-691 | rust | unseen | `d6ce99190b2d` | baseline |
| phpoffice__phpspreadsheet-3463 | php | unseen | `99a7de3812c9` | baseline |
| briannesbitt__carbon-3103 | php | unseen | `d481d8d69a94` | baseline |
| fluent__fluentd-3616 | ruby | unseen | `26c62cddbf23` | baseline |

**Pilot.** One pilot pair, both systems, runs before the frozen runs to
catch tooling faults. It uses `gin-gonic__gin-3741`: a candidate that passed
gate 1 but was not picked (it ranks fourth in its slot). Its results are
published under `results/pilot/` and excluded from the analysis. A tooling
fault found by the pilot is fixed and listed in `deviations.md` before any
frozen run.

## 8. Reproduction

See [README.md](README.md). Re-running the protocol needs:
- the reference model and llama.cpp build;
- Docker with the sandbox image;
- network for preparation only.

Expect different individual outcomes: the model samples at temperature
0.6.
