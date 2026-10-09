# Onboarding validation (2026-10-09)

This page records what a first external user's installation and first task
were tested on, and how. Each result names the environment, the command and
what was observed. "Not executed" means exactly that: those tests are written
but have not run yet.

Code revision: `main` after `bd9de5b`, with the changes listed in
[Changes](#changes). The published release at the time was v0.1.0-alpha.4.

## Platform compatibility matrix

| Platform | Install (`install.sh` / `install.ps1`) | First-run checks (`setup --check`, `setup --only config`, `doctor`, cloud-provider path) | First task, sandboxed | Full flow with a real model | Status claimed |
|---|---|---|---|---|---|
| Linux x86-64, Debian 13 host (reference machine) | **Passed** (release from this commit; published alpha.4; source build) | **Passed** | **Passed** (scripted model, Docker sandbox) | **Passed earlier** (README demo; validations in `docs/benchmarks/`) | Validated |
| Linux x86-64, Debian 13 container (clean, non-root) | **Passed** | **Passed** | Not executed | Not executed | Covered by "Linux x86-64" |
| Linux x86-64, Ubuntu 24.04 container (clean, non-root) | **Passed** | **Passed** | Not executed | Not executed | Covered by "Linux x86-64" |
| Linux x86-64, Fedora 42 container (clean, non-root) | **Passed** | **Passed** | Not executed | Not executed | Covered by "Linux x86-64" |
| Linux arm64, Debian 13 container under QEMU emulation | **Passed** | **Passed** | Not executed | Not executed | Installer and first run only |
| macOS (Apple Silicon, Intel) | CI job added (`smoke.yml`, `macos-latest`); **not executed yet** | CI job added; **not executed yet** | Not executed | Not executed | Experimental |
| Windows 10/11 x64 | Failure handling and download/checksum logic **passed** under PowerShell 7.4.6 **on Linux**; CI job added (`windows-latest`); **not executed on Windows** | CI job added; **not executed yet** | Not executed | Not executed | Experimental |

The first-task smoke test uses a scripted model, so it checks the plumbing
and the verification gate, not model quality. "Full flow with a real model"
is the evidence in the README's results and demo.

## Environments

| Environment | Details |
|---|---|
| Host | Debian GNU/Linux 13 (trixie), kernel 7.1.13, x86-64. Lenovo LOQ 15IRH8 (i7-13620H, 64 GB RAM, RTX 4060 8 GB). |
| Host tools | bash 5.2.37, git 2.47.3, Go 1.27.1, Python 3.13.5 |
| Container engine | Docker Desktop: client 29.8.2, server 29.8.0 (linux/amd64); QEMU emulation for linux/arm64 |
| Sandbox image | `boundedcode-openhands:local` (`sha256:9eb682a03b3c…`, built 2026-10-08), current for this sandbox definition according to `setup --check` |
| Distribution images (pulled 2026-10-09) | `debian:13@sha256:913f6706df59…`, `ubuntu:24.04@sha256:534baea6a22c…`, `fedora:42@sha256:99e203b80b1c…` |
| PowerShell | PowerShell 7.4.6 for Linux (official tarball), used only to exercise `install.ps1`'s control flow |

The host runs are isolated:
- each uses a throw-away `HOME` or `BOUNDEDCODE_HOME`;
- `BOUNDEDCODE_SECRETS=file` keeps the system keychain out of it;
- no credentials are used. The scripted model server's "API key" is a
  placeholder that only that local server sees.

## Tests, commands and results

### 1. Installer and first run: `scripts/smoke/install.sh`

**What it does:**
- It builds a binary from this checkout and lays it out like a release:
  `TAG/boundedcode-OS-ARCH` and `TAG/SHA256SUMS`.
- It runs `scripts/install.sh` against that directory through
  `BC_DOWNLOAD_BASE=file://…`, as an isolated user.

**What it checks:**
- **Install:** a fresh install reports checksum verification, links `bcode`,
  prints a PATH hint and `bcode version` works.
- **Re-run:** it is idempotent and leaves no staging or temporary files.
- **Tampered release:** it is refused with "checksum mismatch", and the
  installed binary is unchanged.
- **Unrelated `bcode`:** an unrelated `bcode` in the target directory is
  refused and left intact. `BC_FORCE=1` replaces it.
- **`setup --check`:** it exits non-zero and lists `[todo] Configuration`.
- **`setup --only config`:** it writes `config.yaml` with mode 600.
- **Cloud alternative:** the llama.cpp and model steps name the cloud
  alternative (`provider use NAME`).
- **`doctor`:** it runs, and fails only for what is not installed.
- **Cloud provider:** after `provider use openai-compatible`, the llama.cpp
  and model steps report "not needed".
- **Uninstall:** removing the two files leaves the bin directory empty.

**Results:**

| Run | Command | Result |
|---|---|---|
| Host, local release | `scripts/smoke/install.sh` | 12 checks passed (about 22 s) |
| Host, plus pinned tool downloads | `SMOKE_TOOLS=1 scripts/smoke/install.sh` | 13 checks passed. gitleaks 8.30.1 and codebase-memory-mcp 0.11.0 were downloaded from GitHub, sha256-verified and run. |
| Host, published release | `SMOKE_PUBLISHED=1 scripts/smoke/install.sh` | 5 installer checks passed against v0.1.0-alpha.4 from GitHub. |
| Containers, amd64 | `scripts/smoke/install-containers.sh` (debian:13, ubuntu:24.04, fedora:42; non-root user `smoke`) | 12/12 checks passed in each (4 min 45 s in total) |
| Container, arm64 (emulated) | `SMOKE_PLATFORM=linux/arm64 scripts/smoke/install-containers.sh debian:13` | 12/12 checks passed. The installer selected `boundedcode-linux-arm64`. |

**About the published-release run:**
- Running all the checks against alpha.4 showed that its `setup --check`
  exits 0 with steps missing. That is the behaviour this change fixes, so
  published mode now runs only the installer checks.
- The Docker check in isolated homes failed only because Docker Desktop
  keeps its context in `~/.docker`. The script now keeps `DOCKER_CONFIG`.

### 2. Build from source: `install.sh` fallback

```bash
HOME=$W/home BC_BIN_DIR=$W/bin BC_VERSION=main BC_GIT_URL=file:///home/ali/boundedcode bash scripts/install.sh
```

**Result:** `main` has no release binary, so the installer cloned and built
it with `make build` and installed it. `bcode version` printed
`boundedcode bd9de5b bd9de5bf4161`. A shallow clone has no tags, so the
version is the commit hash.

### 3. Interruption and cleanup

| Case | Method | Result |
|---|---|---|
| `install.sh` interrupted during the download | A stand-in `curl` that hangs. `timeout -s INT 2` and `timeout -s TERM 2` | Stopped after 2 s. The temporary directory was removed, and the existing installation was byte-for-byte unchanged. |
| `install.ps1` failing under `irm \| iex` | PowerShell 7.4.6, an unsupported CPU value | The red error was printed and the session stayed alive. `$ErrorActionPreference` and strict mode did not leak. Run as `-File`, it exits 1. |
| `install.ps1` tampered download | `scripts/smoke/install.ps1` | Written, **not executed on Windows** (CI) |
| `setup --only model` interrupted | `timeout -s INT 6` into a 22.1 GB download | The message was "interrupted at 58164028 of 22134528992 bytes (run the fetch again to resume)". The `.part` file was kept for resume, and `setup --check` still reports the model as not downloaded. |
| Disk space before the model download | Code review | The downloader already refuses when free space is less than the remainder plus 512 MiB (`internal/model/download.go`). |

### 4. First task: `scripts/smoke/first-task.sh`

The real CLI, gateway, OpenHands adapter, Docker sandbox and verification
run on the README demo bug:
- `go test ./...` passes on the buggy code;
- `Total` misses the bulk discount at exactly 10 items.

`scripts/smoke/fake_model.py` stands in for the model. It is an
OpenAI-compatible server on 127.0.0.1 that returns scripted tool calls,
selected through `bcode provider use openai-compatible`, so the cloud
provider path is exercised without a cloud account.

```bash
scripts/smoke/first-task.sh "$PWD/bin/boundedcode"
```

| Scenario | Expected | Observed |
|---|---|---|
| 1. Fix plus a test for 10 items | `verification=task_verified`. The diff shows the fix and the test. | **Passed** |
| 2. Fix without a test | BoundedCode asks once for a test, then `verification=tests_green` (UNVERIFIED) | **Passed** |
| User's checkout | Unchanged; results only on `agent/*` branches | **Passed** |

The run took 1 min 31 s for both scenarios, plus set-up and indexing.

Two first runs failed before the harness was correct. Both failures were in
the scripted command, not in BoundedCode. Literal tabs in a heredoc
triggered tab completion in the agent's interactive terminal, and then
shell quoting went wrong. The test content is now passed base64-encoded.

**Documented example, run verbatim.** The "Your first task" block in
[getting started](../usage/getting-started.md#your-first-task) was extracted
and run unchanged, with `bcode` on PATH and the scripted model. It ended
`verification=task_verified attempts=1` in 15 s.

The first attempt failed because `git commit` needs a git identity, and this
machine has none configured globally. The docs now say so.

### 5. Unit tests added or changed

| Test | Covers |
|---|---|
| `TestSetupCheckLocalAndCloud` | `setup --check` fails while steps are missing, names the cloud alternative, and skips the llama.cpp and model steps with a provider |
| `TestLlamaSourceBuildNeedsCUDAToolkit` | With an NVIDIA GPU and no `nvcc`, set-up picks the prebuilt CUDA build instead of a source build |
| `TestContractUsesCloudUpstream` | The task contract uses a cloud provider's upstream. It fails on the previous code with "no model client". |
| `TestWorkspaceFromCurrentRepository` | The workspace is resolved from the current repository; the error text gives the fix |

Full suite: `make fmt`, `go vet ./...`, `golangci-lint run ./...` (0
issues) and `go test -count=1 ./...` all passed.

## Findings and changes

### Defects found and fixed

1. **The task contract was skipped with every cloud provider.**
   - The request-reading calls required a local model client. With a
     cloud provider, the contract and its ambiguity check (default
     `task.ambiguity: ask`) were silently skipped. Only a warning was
     logged: "task contract unavailable … no model client".
   - Found by the first-task smoke test. Fixed in
     `internal/orchestrator/contract.go`.
2. **The llama.cpp build failed without the CUDA toolkit.** On Linux with an
   NVIDIA driver but no CUDA toolkit, `bcode setup` chose the source build,
   which stopped at "nvcc not found". It now downloads the pinned prebuilt
   CUDA 12.8 build (`internal/cli/installers.go`).
3. **`setup --check` always exited 0**, so scripts could not tell a ready
   set-up from an incomplete one.
4. **`install.sh` could overwrite unrelated programs.** It silently replaced
   any `bcode` or `boundedcode` in `~/.local/bin`, including an unrelated
   program of the same name.
5. **The installers wrote the binary in place.** An interrupted copy could
   leave a half-written program.
6. **Installers had no fallback when the GitHub API rate limit was hit**
   (60 unauthenticated requests an hour):
   - `install.sh` fell through to a source build, which needs Go;
   - `install.ps1` failed outright.

   Both now read the releases feed instead.
7. **The PATH hint always named `~/.bashrc`.** macOS uses zsh by default.
8. **The first-time CLI path dead-ended without a workspace.**
   - `task create` in a fresh install failed with "no workspace selected:
     … run `workspace use NAME`", but there was no workspace to use.
   - The error now names the commands that create one.
   - A command run inside a registered repository now uses its workspace.
9. **Local and cloud set-up were not distinguished.** Neither the installers'
   next steps nor `setup` explained the two paths.

### Changes

**Installers:**
- **`scripts/install.sh`:**
  - refuses to replace unrelated programs (`BC_FORCE=1`);
  - identifies its own binary without executing it;
  - replaces the binary atomically;
  - warns when another `bcode` comes first on PATH;
  - gives the PATH line for bash, zsh or fish;
  - falls back to the releases feed;
  - accepts `BC_DOWNLOAD_BASE`;
  - prints the download URLs;
  - lists both model paths in its next steps.
- **`scripts/install.ps1`:**
  - stages files before replacing them;
  - falls back to the releases feed;
  - warns about a shadowing `bcode`;
  - accepts `BC_DOWNLOAD_BASE`;
  - lists both model paths in its next steps.

**Set-up and CLI:**
- `internal/cli/setup.go`: the exit status of `setup --check`, and the
  cloud alternative in the hints.
- `internal/cli/installers.go`: the CUDA-toolkit check.
- `internal/cli/workspace.go`: resolving the workspace from the current
  repository, and actionable errors.
- `internal/orchestrator/contract.go`: contract calls through a cloud
  upstream.

**Smoke tests:**
- `scripts/smoke/install.sh`;
- `scripts/smoke/install-containers.sh`;
- `scripts/smoke/install.ps1`;
- `scripts/smoke/first-task.sh`;
- `scripts/smoke/fake_model.py`;
- `.github/workflows/smoke.yml`:
  - installer jobs on Linux, macOS and Windows for pushes and pull requests
    that touch the installers or the CLI;
  - the first-task job on demand and weekly, because it builds the 5 GB
    sandbox image.

**Docs:**
- the README Quick start: install, choose local or cloud, first task, and
  an evidence-based platform table;
- getting started: the fast path, the platform table, "Your first task" and
  "When a step fails";
- `tui.md` and `configuration.md`: the CUDA fallback, the `--check` exit
  status, and the contract with cloud providers;
- the CHANGELOG.

### Security review of the installers

- **Checksums come from the same place as the binary.** Both installers
  verify the binary against the release's `SHA256SUMS`, which is fetched
  from the same place. This detects corruption and a swapped asset. It
  does not detect a compromise of the release itself, because releases are
  not signed. `BC_DOWNLOAD_BASE` inherits this: use only a mirror you trust.
- **Not executed by the installers:**
  - No step needs root.
  - `install.sh` no longer runs an existing program to identify it.
  - `install.sh` runs the binary it just verified, for `version`.
- **The installers write only:**
  - the target directory (two files);
  - a temporary directory, which is removed on exit, error or interruption.
  - `install.ps1` also appends one entry to the user PATH.
- **Credentials:**
  - None are used or stored by the installers.
  - `bcode provider key set` stores keys in the OS credential store, or an
    owner-only file.
  - The smoke tests use only a placeholder key for a local stand-in server.
- **`bcode setup` downloads:** each one is pinned, sha256-checked, and
  confirmed before it runs (unless `--yes`). It installs under the user's
  directories. The sandbox image is built locally and never pushed.

## Unresolved limitations

1. **macOS and Windows have not been run.** The installer smoke jobs exist
   in CI but have not executed: nothing was pushed. The full flow, with a
   model and the sandbox, has never run on either system. Both remain
   experimental. Several CI unit-test failures on those systems are
   untriaged; see the README's known limitations.
2. **Windows installer.** Under PowerShell on Linux, only the control flow
   and the failure paths of `install.ps1` were exercised; the `.exe` cannot
   run there. `scripts/smoke/install.ps1` has only been parse-checked.
3. **The distribution containers ran the installer and first-run checks
   only.** The sandboxed first task ran on the host, where Docker is.
   Docker-in-Docker was not attempted.
4. **Linux arm64 ran only under emulation,** and only the installer and
   first-run checks. Local inference and the sandbox on arm64 are untested.
5. **Releases are not signed.** Checksum verification does not protect
   against a compromised release.
6. **Most of these fixes are unreleased.** They are on `main`; v0.1.0-alpha.4
   and earlier still have the old behaviour (`setup --check` exit 0, no
   cloud contract, the CUDA failure, unprotected overwrites). Once pushed,
   the installer script changes take effect at once, because users fetch
   `install.sh` from `main`.
7. **The first-task smoke test checks wiring, not the agent.** It cannot
   show that a real model solves the example. The README demo is the
   evidence for that, on the reference machine only.
8. **The CI first-task job is unverified.** It builds the sandbox image on a
   standard runner (about 5 GB). Its run time and disk headroom there have
   not been measured.
9. **Podman** is supported by configuration but was not exercised here.
