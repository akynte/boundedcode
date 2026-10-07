# Plan: model choice, cloud providers, TUI set-up, Linux/macOS/Windows

Status: **proposed** (2026-10-07). Requested by the maintainer to close the
"one machine and one model" limitation and to make BoundedCode usable on
ordinary hardware and by non-technical users. It changes the product
direction: BoundedCode is no longer strictly local-first.

## Requirements (from the maintainer)

1. Qwen3.6-35B-A3B stays the default local model; set-up lets the user pick
   another model, filtered by the machine's hardware.
2. Cloud models are first-class alternatives to a local model:
   OpenAI-compatible APIs (with an API key), Anthropic's Messages API and
   Google's Gemini API.
3. Everything is configurable from the TUI. Users never edit config files.
4. Linux, macOS and **native** Windows, with different RAM and GPUs; set-up
   takes the hardware into account.
5. API keys live in the OS keychain (Secret Service, macOS Keychain, Windows
   Credential Manager), with an owner-only file as the fallback.
6. Model licenses: permissive and open-weight custom licenses (shown with a
   license notice the user accepts).

## Invariants that do not change

- The agent sandbox never sees a credential. Every agent model call already
  goes through the host-side gateway (`inference.Gateway.Forward`) over the
  adapter's stdio JSON-RPC tunnel; the gateway adds the key and translates the
  request. The sandbox keeps `--network none`.
- Keys never appear in `config.yaml`, command lines, audit events, logs,
  context packs or frontier packets. The TUI passes them to the credential
  store directly, not as CLI arguments (job output and `tui.log` echo
  arguments).
- No silent paid use: a cloud provider is used only after the user selects it
  and enters a key; the TUI shows the provider and model in the header, and
  usage is recorded per provider.
- Verification, the gate and the evidence check are provider-independent.
- Frontier escalation (Codex subscription) is unchanged and separate.

## What exists (investigation, 2026-10-07)

- `inference.mode: external` sends OpenAI chat completions to a URL, with no
  auth header. The gateway accepts only `/v1/chat/completions`, overwrites
  `model`, strips `stream`, and reads llama.cpp `timings`. Tool calls are
  native OpenAI `tools`; no streaming.
- llama.cpp-only assumptions: `chat_template_kwargs` in contract and bench
  calls, sampling and reasoning budget as server flags, GGUF-only profiles,
  `ctx_size` from the profile, cache accounting from `timings.cache_n`,
  outage repair only for transport errors in managed mode, budgets and stats
  labelled "local".
- TUI: forms with text, toggle, horizontal choice and checkbox fields; no
  masked input, no vertical single-choice list, no wizard; the prompter can
  only ask yes/no; the only config writer is `init --force`, which resets
  everything. `/model` in chat is not persisted.
- `internal/hw` reads `/proc` and `nvidia-smi` only.
- macOS: does not compile (one Linux-only benchmark function); at run time
  the llama.cpp manager's liveness check reads `/proc` (server reuse and stop
  break), hardware detection is empty, the setup scripts refuse non-Linux,
  the contained Codex mounts a host binary into a Linux container, host
  `node_modules` with native macOS binaries break JS verification, and the
  sandbox's `--cpus 8 --memory 8g` defaults can exceed Docker Desktop's VM.
- Windows: does not compile (process groups and signals in three packages);
  the sandbox mounts every host path at the same path inside a Linux container
  (impossible with `C:\...`), `--user -1:-1`, bash set-up scripts, `.exe`
  discovery, Serena paths and symlinks, frontier path sanitizing, owner-only
  file modes need ACLs, case-insensitive path comparisons.

## Phases

Each phase ends with tests, docs and `make check` green, and is committed
separately. Phases 11–13 are platform-independent and come first, so Linux
users get them early; 14 and 15 port to macOS and Windows.

### Phase 11: providers and credentials

**Done (2026-10-07)**, except the TUI screens, which are Phase 13. See
[ADR-0010](../architecture/adr/0010-cloud-model-providers.md). Not yet done:
live calls against the real APIs (needs keys).

- Config: `inference.provider` (`local`, `openai`, `anthropic`, `gemini`,
  `openai-compatible`) with per-provider base URL, model id, context window,
  output limit and optional per-token prices (user-entered; no hard-coded
  prices). Profiles gain `kind: local|cloud`; cloud profiles have no GGUF.
- `internal/secrets`: OS keychain with an owner-only file fallback (ACL on
  Windows); keys referenced by name from config; redaction of stored keys.
- Gateway: per-provider request/response translation for chat completions
  with tools, tool results, system prompts, stop reasons and usage; API-key
  headers; mapping of 401/403 (blocks with "check the API key"), 429 and 5xx
  (bounded retry with `Retry-After`); sampling and reasoning settings sent
  per request for cloud providers; llama-only fields only for llama.cpp.
- Runner, budgets and stats: provider-aware failure handling, token budgets
  per provider, usage and optional cost per provider; "local-only rate"
  reported as before for local runs.
- Docs: product spec (mission), README, SECURITY.md, sandbox and ADR-0009
  wording ("No API keys" applied to the frontier Codex sign-in only), a new
  ADR for cloud providers.
- Verification: unit tests per provider against recorded request/response
  shapes and httptest fakes. **Live calls need real keys, which the
  maintainer must provide or run.**

### Phase 12: hardware detection and model catalog

- `internal/hw` per OS: RAM, CPU, disk space, NVIDIA (nvidia-smi), Apple
  Silicon unified memory, other GPUs where the OS reports them.
- Profiles gain size, sha256, quantization, minimum memory and per-backend
  server settings (CUDA, Metal, CPU); a fit check recommends models for the
  machine and explains why.
- Model downloads in Go (no bash or python): resumable, pinned revision and
  sha256 from the profile, progress events, license notice and acceptance,
  Hugging Face token (keychain) for gated repos.
- `model fetch|use|remove` commands.
- Catalog candidates: see
  [model-catalog-research-2026-10.md](../design/model-catalog-research-2026-10.md).
  Only Qwen3.6-35B-A3B is measured; every new profile ships marked
  *experimental* until `bench infra` and the engineering suite have run on it.

### Phase 13: TUI set-up wizard and settings

- First-run wizard: hardware summary → local or cloud → local: model list with
  fit badges, license notice, download progress → cloud: provider, masked key
  entry, model, connection test → tools and sandbox → done.
- Settings view to change the provider, model or key later; key removal.
- Form additions: masked input, vertical choice list, progress display, and a
  typed settings API for config writes (no `init --force`).

### Phase 14: macOS

- Compile fixes; process management for unix (linux and darwin); liveness via
  sysctl; hardware detection; prebuilt llama.cpp release binaries (Metal) by
  checksum instead of a source build; Docker Desktop resource clamping;
  contained Codex installed in the image instead of mounting a host binary;
  JS dependencies for verification installed inside the Linux sandbox when
  host `node_modules` are not Linux-compatible; Linux build tags for
  container stages; multi-arch base image digests; darwin release artifacts,
  installer and a macOS CI job.

### Phase 15: native Windows

- Process management with Job Objects; Windows liveness; a host→container path
  translation layer for mounts, working directories, environment values and
  container output; worktrees with relative gitdir pointers;
  `core.autocrlf=false` and `core.longpaths=true` in worktrees; a fixed
  sandbox uid; ACL-protected secret file; Go-native installers instead of
  bash; prebuilt llama.cpp (CUDA, Vulkan, CPU) and Windows assets for
  gitleaks and codebase-memory-mcp; Serena paths without symlinks; frontier
  sanitizing of drive-letter paths; SQLite DSN; case-insensitive path
  comparisons; PowerShell installer, `.exe` release and a Windows CI job.

### Phase 16: release

- Release matrix (linux amd64, darwin amd64/arm64, windows amd64); license
  inventory and SBOM per target (Windows and darwin pull extra Go modules);
  docs per platform.

## What can and cannot be verified here

- Unit and integration tests on Linux; compile, vet and unit tests on macOS
  and Windows through GitHub Actions runners.
- **Not here:** real GPUs other than the reference RTX 4060, Apple Silicon
  inference, Windows desktops, and live cloud API calls without keys. The
  "one machine and one model" limitation closes only after runs on other
  hardware; the docs will keep saying so until then.
