# Fresh-clone Quick Start test (2026-10-05)

A new user was simulated: a clean `git clone` of the publication candidate
into an empty directory, with an empty `HOME` (no prior config, state,
tools, models or caches). The model weights were linked from an existing
download instead of re-downloading 22 GB. The Docker daemon was the
machine's own (`DOCKER_HOST` set, since Docker Desktop's context lives in
the real home).

| Step (README Quick Start) | Result |
|---|---|
| `make build` | ok |
| `boundedcode doctor` before `init` | 3 required failures with actionable hints: llama-server missing (build script / `--llama-server`), model missing (names file and source), container engine unreachable; codebase-memory-mcp, gitleaks, sandbox image reported as warnings with install hints |
| `scripts/install-deps.sh ~/.local/bin` | ok (gitleaks 8.30.1, codebase-memory-mcp 0.11.0, checksum-verified) |
| `scripts/build-llama-cpp.sh` | ok: llama.cpp v0.5.0 (commit 7fe450e) with CUDA, same version as the validation |
| `scripts/fetch-model.sh … main …` | refused: "revision must be a full 40-character commit sha" (download itself not repeated) |
| `init` with `--llama-server`/`--llama-bench` (managed mode) | ok; `doctor`: llama-server, CUDA device, model all ok |
| `init --external-url` (used for the smoke task, to share the running server) | ok |
| `sandbox build --dir adapters/openhands` | ok |
| `doctor` after setup | all required checks ok; `uv` warning (optional) |
| `workspace create`, `workspace add`, `index` | ok |
| `task create "…" -c "go test ./... passes" --run` on a small Go repository with a real bug (blank name → `Hello, !`) | **task_verified** in 2 attempts, 3 min 34 s, local only: correct fix; the evidence gate asked for a test, and the agent added cases that fail on the base |
| `task diff`, `task status` | ok; branch `agent/<id>` created, nothing pushed |

Missing-dependency behaviour at run time (not only in `doctor`):

* `index` without codebase-memory-mcp fails naming the missing executable (no install hint).
* `task run` with Docker unreachable fails with "adapter exited before ready … (see …/adapter.log)"; the cause is in the log, not the message.
* Both are reported clearly by `doctor`; improving the run-time messages is left for after the alpha (product code is frozen for the validated build).

No undocumented dependency was needed beyond the README prerequisites.
