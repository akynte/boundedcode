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
| [Serena](https://github.com/oraios/serena) (PyPI `serena-agent`), optional | `v1.7.0` (`serena-agent==1.7.0`) | `949a27ef1e5fda1a6e7b561e777bcece345c6ffd` | MIT, (c) 2025 Oraios AI | `16017e50…b1c195b0c` | MIT | ✅ Verified 2026-10-03: the tag resolves to the commit; the PyPI wheel (sha256 `6dbf1459…c76e891`) carries the same LICENSE and all 213 packaged files are byte-identical to `src/` at the commit. ⚠ `main` (`2.0.0.dev0`) is **GPL-3.0-or-later** for the application (SolidLSP stays MIT); that change is not retroactive. Pinned, manual upgrades only (ADR-0008, `scripts/serenaguard`). |
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
* We do **not** depend on an MCP SDK. codebase-memory-mcp and Serena are
  driven over MCP stdio by our own minimal client (`internal/jsonrpc`; see
  ADR-0006 and ADR-0008).

## Serena environment

Serena is installed by `boundedcode serena setup` into a uv environment from
the embedded lock [`configs/serena/uv.lock`](../../configs/serena/uv.lock)
(exact versions and hashes). It is not redistributed. The environment is
classified by `scripts/pylicensecheck.py`; the result is
[`configs/serena/THIRD_PARTY.md`](../../configs/serena/THIRD_PARTY.md).

Findings (2026-10-03):

| Package | Pulled in by | License | Decision |
|---|---|---|---|
| `pystray` 0.19.5, `python-xlib` 0.33 | `serena-agent` (system-tray icon) | LGPL-3.0, LGPL-2.0-or-later | **Excluded** with a uv `override-dependencies` entry. Serena imports pystray lazily, only from the dashboard/GUI code, which we always disable. `pillow` (MIT-CMU), which pystray brought in and Serena's dashboard module imports at load time, is listed directly. The integration tests pass in this environment. |
| `dotenv` 0.9.9 | `serena-agent` | no PyPI license metadata | MIT per github.com/pedroburon/dotenv (GitHub license API); a shim over `python-dotenv` (BSD-3-Clause). |
| `certifi`, `pathspec`, `tqdm` | `requests`, `serena-agent` | MPL-2.0 (tqdm: MPL-2.0 AND MIT) | Same decision as for the adapter: used unmodified as separate packages. |
| `anthropic` 0.117.0 | `serena-agent` | MIT | Present because Serena depends on it (optional API-based token counting). Our generated Serena config sets `token_count_estimator: CHAR_COUNT` and the scrubbed environment carries no `ANTHROPIC_*` variables, so no paid API is ever called. |

No GPL, AGPL or SSPL packages are present. The Serena application itself is
MIT at the pinned release.

## Python adapter dependencies

The adapter is not part of the Go binary. It is installed from PyPI into a
uv-managed environment or a locally built container image. This project does
not redistribute it or publish an image. Its direct dependencies are
`openhands-sdk` and `openhands-tools` (MIT, see above). The transitive tree
(about 190 distributions) is classified by `scripts/pylicensecheck.py`, and
the result is recorded in
[`adapters/openhands/python/THIRD_PARTY.md`](../../adapters/openhands/python/THIRD_PARTY.md).

Findings (2026-10-03):

| Package | Pulled in by | License | Decision |
|---|---|---|---|
| `lmnr-claude-code-proxy` 0.1.24 | `openhands-sdk` → `lmnr` (Laminar tracing) | **LicenseRef-Proprietary** ("© LMNR AI, Inc. All rights reserved", subject to Laminar ToS) | **Excluded** through a uv `override-dependencies` entry. It is only imported by Laminar's Claude-agent instrumentation, which we never enable. The adapter integration test passes without it. |
| `lmnr` 0.7.64 | `openhands-sdk` | Apache-2.0 | Kept. Tracing activates only when `LMNR_PROJECT_API_KEY` or `OTEL_*` endpoints are set. The sandbox strips those variables and runs the container with `--network none`. |
| `certifi` | `httpx`/`requests` | MPL-2.0 | Manual review: used unmodified as a separate package (file-level copyleft). Accepted for the adapter environment. |
| `tqdm` | `huggingface-hub` and others | MPL-2.0 AND MIT | Same as certifi. |
| `func_timeout` 4.3.5 | `openhands-tools` | LGPLv2 | Manual review: used unmodified as a separately installed library and never vendored. Accepted for the adapter environment. Revisit before shipping any bundled image. |
| `agent-client-protocol`, `openhands-sdk`, `openhands-tools` | | no PyPI license metadata | Upstream repositories verified: OpenHands is MIT (above), and agent-client-protocol is Apache-2.0 (github.com/agentclientprotocol/python-sdk, checked via the GitHub license API). |

No GPL, AGPL or SSPL packages are present.
