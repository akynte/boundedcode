# Upstream License Matrix

Last verified: **2026-10-03**. Each license was read from the LICENSE file at
the pinned tag or revision, not only from GitHub's SPDX field. License texts
live in [`LICENSES/`](../../LICENSES). Re-verify on every upgrade (see
[policy](policy.md)).

## Runtime components (external processes, not redistributed)

| Component | Pinned | Commit / revision | License (file text) | LICENSE sha256 | Expected | Status |
|---|---|---|---|---|---|---|
| [llama.cpp](https://github.com/ggml-org/llama.cpp) | `v0.5.0` | `7fe450e19305b828c199d602c23a8337aaa1f03b` | MIT, (c) 2023-2026 The ggml authors | `94f29bbe…f0f1d010d` | MIT | ✅ |
| [OpenHands Software Agent SDK](https://github.com/OpenHands/software-agent-sdk) (`openhands-sdk`, `openhands-tools` on PyPI) | `v1.51.0` | `a955aa5d3188d4b0a44ad7eb4e5c4bba6e6238d9` | MIT, (c) 2026 OpenHands contributors | `14a9b631…59cc5ce86` | MIT | ✅ ⚠ PyPI wheels carry **no** license metadata or file. The MIT grant comes from the repo LICENSE, which we ship in `LICENSES/upstream/`. |
| [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp) | `v0.11.0` | `8972ea69c6ad94b1ef1d4ffbf0a92d78d2db1798` | MIT, (c) 2025 DeusData | `1f58f991…a152146bb` | MIT | ✅ |
| [gitleaks](https://github.com/gitleaks/gitleaks) | `v8.30.1` | `83d9cd684c87d95d656c1458ef04895a7f1cbd8e` | MIT, (c) 2019 Zachary Rice | `e3884b25…62ebc6` | MIT | ✅ |
| [Codex CLI](https://github.com/openai/codex) | user-installed (verified against `rust-v0.160.0`, `a956835d…`) | | Apache-2.0, (c) 2025 OpenAI | `d17f227e…d8dc` | n/a | ✅ optional frontier provider |

## Models (downloaded by the user, never redistributed)

| Model | Source / revision | License | LICENSE sha256 | Status |
|---|---|---|---|---|
| Qwen3.6-35B-A3B | `Qwen/Qwen3.6-35B-A3B` @ `995ad96e`; GGUF `unsloth/Qwen3.6-35B-A3B-GGUF` @ `a483e9e6` | Apache-2.0, (c) 2026 Alibaba Cloud | `50cbab8a…d99b86b32` | ✅ default candidate |
| Qwen3-Coder-Next | `Qwen/Qwen3-Coder-Next` @ `a7fbcb5c` | Apache-2.0 | `f36668dd…aba59` | ✅ candidate (Q4 GGUF is ~48 GB) |
| Laguna XS 2.1 | `poolside/Laguna-XS-2.1` @ `c5f36269`; GGUF `poolside/Laguna-XS-2.1-GGUF` | **OpenMDW-1.1** (not on the SPDX list, so `LicenseRef-OpenMDW-1.1`) | `8a0c5e23…62d88` | ⚠ **manual review**: a custom permissive model license with a patent-termination clause. Allowed for local benchmarking. Maintainer review is needed before it could become a documented default. |

## Optional or excluded

| Component | Verified state | Decision |
|---|---|---|
| [OpenCode](https://github.com/anomalyco/opencode) (`sst/opencode` redirects here) | `v1.18.34`, MIT | Not integrated. It may become an optional front end later. |
| [Serena](https://github.com/oraios/serena) | **Discrepancy vs. expectation.** The latest *release* `v1.7.0` (`949a27ef`) is **MIT**. On `main`, commit `6707cd9b7e` (2026-09-14) relicensed the *application* to **GPL-3.0-or-later** from v2 (unreleased). SolidLSP stays MIT but is not packaged separately (no PyPI project). | **Excluded from core**, as decided. Future Serena v2 is GPL, so any integration must be an optional, externally installed process with its own license review. No code is copied. |

## Go modules linked into the `boundedcode` binary

These are generated and enforced by `scripts/licensecheck` (`make licenses`).
The texts are in `LICENSES/go/`.

| Module | Version | License |
|---|---|---|
| github.com/spf13/cobra | v1.10.2 | Apache-2.0 |
| github.com/spf13/pflag | v1.0.9 | BSD-3-Clause |
| go.yaml.in/yaml/v3 | v3.0.5 | MIT AND Apache-2.0 (NOTICE: Canonical) |
| modernc.org/sqlite | v1.60.1 | BSD-3-Clause; embedded SQLite is public domain; sqlite-vec MIT |
| modernc.org/libc | v1.77.1 | BSD-3-Clause AND MIT (musl) |
| modernc.org/mathutil, modernc.org/memory | v1.7.1, v1.12.1 | BSD-3-Clause |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause |
| github.com/dustin/go-humanize | v1.0.1 | MIT |
| github.com/remyoudompheng/bigfft | 2023-01-29 | BSD-3-Clause |
| golang.org/x/sys | v0.48.0 | BSD-3-Clause |

Notes:

* `modernc.org/sqlite` ships `LICENSE-3RD-PARTY.md`, which mentions MPL-2.0
  (`hashicorp/golang-lru`). That module is **not linked** into our binary
  (`go list -deps ./cmd/...`). The checker classifies linked modules
  individually and skips aggregated inventory files.
* `github.com/google/licensecheck` (BSD-3-Clause) is used only by the
  build-time checker. It is not linked into the shipped binary.
* We do **not** depend on an MCP SDK. codebase-memory-mcp is driven through
  its one-shot CLI mode (see ADR-0005).

## Python adapter dependencies

The adapter is not part of the Go binary. It is installed into its own
environment or container image, which is built locally and not published.
Its direct dependencies are `openhands-sdk` and `openhands-tools` (MIT, see
above). The transitive tree (litellm, fastmcp, pydantic, …) is checked by
`scripts/pylicensecheck.sh` and the result is recorded in
`adapters/openhands/python/THIRD_PARTY.md`.
