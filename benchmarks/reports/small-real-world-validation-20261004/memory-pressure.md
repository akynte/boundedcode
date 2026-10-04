# Memory-pressure interruption (2026-10-04)

## What happened

At about 13:50 local time the frozen 8-task run was stopped, 5 tasks in
(`vuejs__core-11899` had just started: the model was being ensured, no
task had been created). The stop came from the Claude Code session that
drove the benchmark: its background-shell reaper kills background commands
"because the system was critically low on memory". It is not a kernel OOM
kill as far as we can tell (see below). Results of the 5 finished tasks were
already persisted; the report JSON is written after every task.

## Evidence collected afterwards (13:52-14:05)

| Source | Finding |
|---|---|
| Kernel log (`dmesg`, `journalctl -k`) | Not readable by this user (not in `adm`/`systemd-journal`): **no evidence either way** about a kernel OOM kill. |
| User journal 13:40-13:54 | No OOM, memory-pressure, oomd or earlyoom entries. Only Docker Desktop backend chatter. |
| systemd-oomd, earlyoom | Both inactive. |
| PSI `/proc/pressure/memory` | `some total=17.96 s`, `full total=17.65 s` since boot: memory stalls did happen at some point; avg10/60/300 were 0 when read. |
| Docker | Docker **Desktop** (LinuxKit VM in qemu). The VM is configured with `MemoryMiB: 64028` (all host RAM) and `SwapMiB: 4096`; qemu runs with `-m 64028`. Host `/home` is shared through `virtiofsd --cache=auto`. The VM had been stopped by Docker Desktop's resource saver after the run ended and restarted on demand during this check (RSS 2.3 GiB 21 s after boot). |
| llama-server | 289 MiB RSS when read (idle sleep). While serving, the model is about 19-20 GiB resident (status.md, idle-sleep measurement). |
| Serena / language servers / codebase-memory-mcp | None left running from the benchmark. |
| Containers | None running. Each sandbox container is capped at 8 GiB (`sandbox.memory`). |
| `~/.cache/boundedcode/bench-work` | 15 GiB on disk, 115 directories (screening and task scratch; disk, not RAM). |
| Other host load | Other Claude Code sessions, a browser and desktop applications (a few GiB in total when read). |

## Interpretation

The evidence is **insufficient to name a root cause**. What it does show:

* Nothing in the stack bounds total memory: the Docker VM may grow to all of
  host RAM, while llama.cpp keeps about 20 GiB resident on the host. Guest
  memory a VM has touched (including guest page cache for virtiofs files:
  large repositories, Go build caches, `node_modules`) is not necessarily
  returned to the host until the VM stops.
* The interruption occurred when the largest JavaScript task (vuejs/core,
  1.2 M source tokens plus 478 MB of `node_modules`) was being set up,
  after five tasks that included the two largest Go repositories.

Plausible but unconfirmed: cumulative Docker VM growth plus llama.cpp
residency plus indexing of the next repository. Ruled out as the direct
trigger only for processes still running afterwards.

## Product finding

BoundedCode has per-container memory limits but no whole-machine memory
budget: it does not account for the container engine's VM, the model
server's residency and repository indexing together, and does not check
available memory before starting a task. On a 64 GiB machine running a
20 GiB model, this was enough to end a multi-hour benchmark.

## Changes to how the remaining runs are executed (environment only)

* One task per invocation; nothing else runs in parallel.
* A sampler (`memsample.sh`) logs every 15 s: available memory, qemu (Docker
  VM) RSS, llama-server RSS, per-container usage and the top process.
* After each task: metrics extracted, essentials archived, `bench-work`
  removed, memory checked before the next task.
