# Upstream License Matrix

Last verified: **2026-10-04** (rows not touched on that date: 2026-10-03).
Each license was read from the LICENSE file at the pinned tag or revision, not
only from GitHub's SPDX field. Where a repository has no LICENSE file at the
pinned revision, the row says so and names the metadata the license comes
from. License texts
live in [`LICENSES/`](../../LICENSES). Re-verify on every upgrade (see
[policy](policy.md)).

## Runtime components (external processes, not redistributed)

| Component | Pinned | Commit / revision | License (file text) | LICENSE sha256 | Expected | Status |
|---|---|---|---|---|---|---|
| [llama.cpp](https://github.com/ggml-org/llama.cpp) | `v0.5.0` | `7fe450e19305b828c199d602c23a8337aaa1f03b` | MIT, (c) 2023-2026 The ggml authors | `94f29bbe…f0f1d010d` | MIT | ✅ |
| [OpenHands Software Agent SDK](https://github.com/OpenHands/software-agent-sdk) (`openhands-sdk`, `openhands-tools` on PyPI) | `v1.51.0` | `a955aa5d3188d4b0a44ad7eb4e5c4bba6e6238d9` | MIT, (c) 2026 OpenHands contributors | `14a9b631…59cc5ce86` | MIT | ✅ ⚠ PyPI wheels carry **no** license metadata or file. The MIT grant comes from the repo LICENSE, which we ship in `LICENSES/upstream/`. |
| [codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp) | `v0.11.0` | `8972ea69c6ad94b1ef1d4ffbf0a92d78d2db1798` | MIT, (c) 2025 DeusData | `1f58f991…a152146bb` | MIT | ✅ |
| [gitleaks](https://github.com/gitleaks/gitleaks) | `v8.30.1` | `83d9cd684c87d95d656c1458ef04895a7f1cbd8e` | MIT, (c) 2019 Zachary Rice | `e3884b25…62ebc6` | MIT | ✅ |
| [Serena](https://github.com/oraios/serena) (PyPI `serena-agent==1.7.0`), optional | `v1.7.0` | `949a27ef1e5fda1a6e7b561e777bcece345c6ffd` | MIT, (c) 2025 Oraios AI | `16017e50…b1c195b0c` | MIT | ✅ Verified 2026-10-03: the tag resolves to the commit; the PyPI wheel (sha256 `6dbf1459…c76e891`) carries the same LICENSE and all 213 packaged files are byte-identical to `src/` at the commit. ⚠ `main` (`2.0.0.dev0`) is **GPL-3.0-or-later** for the application (SolidLSP stays MIT); that change is not retroactive. Pinned, manual upgrades only (ADR-0008, `scripts/serenaguard`). |
| [Codex CLI](https://github.com/openai/codex) | user-installed; tested with `codex-cli 0.156.1` (tag `rust-v0.156.1`) | `b412ff32c417f855c2b2d1581b77058eed87c84b` | Apache-2.0, (c) 2025 OpenAI | `d17f227e…d8dc` | n/a | ✅ optional frontier provider. Not installed or distributed by this project. The LICENSE is byte-identical at `rust-v0.160.0` (`a956835d…`). |

## Models (downloaded by the user, never redistributed)

| Model | Base model / revision | License | LICENSE sha256 | Status |
|---|---|---|---|---|
| Qwen3.6-35B-A3B | `Qwen/Qwen3.6-35B-A3B` @ `995ad96e` | Apache-2.0, (c) 2026 Alibaba Cloud | `50cbab8a…d99b86b32` | ✅ default candidate |
| Qwen3-Coder-Next | `Qwen/Qwen3-Coder-Next` @ `a7fbcb5c0e12d62a448eaa0e260346bf5dcc0feb` | Apache-2.0 | `cfc7749b…bc523d30` | ✅ candidate (Q4 GGUF is ~48 GB). The repository has **no LICENSE file** at this revision; the license is from the model-card metadata (`license: apache-2.0`, HF API). `LICENSES/upstream/qwen3-coder-next.txt` is the unmodified Apache-2.0 text from apache.org. |
| Laguna XS 2.1 | `poolside/Laguna-XS-2.1` @ `c5f36269` | **OpenMDW-1.1** (not on the SPDX list, so `LicenseRef-OpenMDW-1.1`) | `8a0c5e23…62d88` | ⚠ **manual review**: a custom permissive model license with a patent-termination clause. Allowed for local benchmarking. Maintainer review is needed before it could become a documented default. |

The profiles in `configs/models/` pin the GGUF files below by commit
(`source.revision`). `scripts/fetch-model.sh` requires that revision and
checks the download against the LFS sha256 the hub publishes for it. The
file hashes were also checked against local downloads on 2026-10-04.

| Profile | GGUF repository @ revision | File | File sha256 (LFS) | License (card metadata) |
|---|---|---|---|---|
| `qwen3.6-35b-a3b` | `unsloth/Qwen3.6-35B-A3B-GGUF` @ `a483e9e6cbd595906af30beda3187c2663a1118c` | `Qwen3.6-35B-A3B-UD-Q4_K_M.gguf` | `ac0e2c11…5bd6f8e76…e31a61` | apache-2.0 (no LICENSE file in the repository) |
| `qwen3-coder-next` | `unsloth/Qwen3-Coder-Next-GGUF` @ `ce09c67b53bc8739eef83fe67b2f5d293c270632` | `Qwen3-Coder-Next-UD-IQ3_XXS.gguf` | `00a9bf4f…9c0ade4` | apache-2.0 (no LICENSE file in the repository) |
| `laguna-xs-2.1` | `poolside/Laguna-XS-2.1-GGUF` @ `1a37c0a5fb8c7a18e6106decb6be6327d1b63fa6` | `Laguna-XS-2.1-Q4_K_M.gguf` | `1ac70791…287f6cb` | openmdw-1.1; `LICENSE.md` at this revision is byte-identical to `LICENSES/upstream/laguna-xs-2.1-OpenMDW-1.1.txt` |

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
| modernc.org/mathutil | v1.7.1 | BSD-3-Clause |
| modernc.org/memory | v1.12.1 | BSD-3-Clause |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause |
| github.com/dustin/go-humanize | v1.0.1 | MIT |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129 (pseudo-version `v0.0.0-20230129092748-24d4a6f8daec`) | BSD-3-Clause |
| golang.org/x/sys | v0.48.0 | BSD-3-Clause |
| golang.org/x/term | v0.46.0 | BSD-3-Clause |
| github.com/charmbracelet/bubbletea | v1.3.10 | MIT |
| github.com/charmbracelet/bubbles | v1.0.0 | MIT |
| github.com/charmbracelet/lipgloss | v1.1.0 | MIT |
| github.com/charmbracelet/colorprofile | v0.4.1 | MIT |
| github.com/charmbracelet/x/ansi | v0.11.6 | MIT |
| github.com/charmbracelet/x/cellbuf | v0.0.15 | MIT |
| github.com/charmbracelet/x/term | v0.2.2 | MIT |
| github.com/muesli/termenv | v0.16.0 | MIT |
| github.com/muesli/ansi | v0.0.0-20230316100256-276c6243b2f6 | MIT |
| github.com/muesli/cancelreader | v0.2.2 | MIT |
| github.com/lucasb-eyer/go-colorful | v1.3.0 | MIT |
| github.com/xo/terminfo | v0.0.0-20220910002029-abceb7e1c41e | MIT |
| github.com/aymanbagabas/go-osc52/v2 | v2.0.1 | MIT |
| github.com/atotto/clipboard | v0.1.4 | BSD-3-Clause |
| github.com/clipperhouse/displaywidth | v0.9.0 | MIT |
| github.com/clipperhouse/stringish | v0.1.1 | MIT |
| github.com/clipperhouse/uax29/v2 | v2.5.0 | MIT |
| github.com/rivo/uniseg | v0.4.7 | MIT |
| github.com/mattn/go-runewidth | v0.0.19 | MIT |
| github.com/mattn/go-isatty | v0.0.24 | MIT |
| github.com/anthropics/anthropic-sdk-go | v1.79.0 | MIT |
| github.com/tidwall/gjson | v1.18.0 | MIT |
| github.com/tidwall/sjson | v1.2.5 | MIT |
| github.com/tidwall/match | v1.1.1 | MIT |
| github.com/tidwall/pretty | v1.2.1 | MIT |
| github.com/buger/jsonparser | v1.1.2 | MIT |
| github.com/invopop/jsonschema | v0.14.0 | MIT |
| github.com/bahlo/generic-list-go | v0.2.0 | BSD-3-Clause |
| github.com/pb33f/ordered-map/v2 | v2.3.1 | Apache-2.0 |
| github.com/standard-webhooks/standard-webhooks/libraries | v0.0.1 | MIT |
| go.yaml.in/yaml/v4 | v4.0.0-rc.2 | MIT AND Apache-2.0 |
| golang.org/x/sync | v0.23.0 | BSD-3-Clause |
| github.com/zalando/go-keyring | v0.2.8 | MIT |
| github.com/godbus/dbus/v5 | v5.2.2 | BSD-2-Clause |

CI runs `go run ./scripts/licensecheck -check-notices
THIRD_PARTY_NOTICES.md,docs/licensing/upstream-license-matrix.md`, which
fails when a linked module and its version are missing from this table or
from the notices, and `make licenses` followed by `git diff --exit-code
LICENSES/`, which fails when `LICENSES/go/` is stale.

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
| `pystray` 0.19.5, `python-xlib` 0.33 | Serena (system-tray icon) | LGPL-3.0, LGPL-2.0-or-later | **Excluded** with a uv `override-dependencies` entry. Serena imports pystray lazily, only from the dashboard/GUI code, which we always disable. `pillow` (MIT-CMU), which pystray brought in and Serena's dashboard module imports at load time, is listed directly. The integration tests pass in this environment. |
| `dotenv` 0.9.9 | Serena | no PyPI license metadata | MIT per github.com/pedroburon/dotenv (GitHub license API); a shim over `python-dotenv` (BSD-3-Clause). |
| `certifi`, `pathspec`, `tqdm` | `requests`, Serena | MPL-2.0 (tqdm: MPL-2.0 AND MIT) | Same decision as for the adapter: used unmodified as separate packages. |
| `regex` 2026.2.28 | Serena, `tiktoken` | Apache-2.0 AND CNRI-Python | Reviewed 2026-10-04: CNRI-Python is a permissive, OSI-approved license (the classifier reports it as UNKNOWN because it is not on the allowed list). Accepted. |
| `anthropic` 0.117.0 | Serena | MIT | Present because Serena depends on it (optional API-based token counting). Our generated Serena config sets `token_count_estimator: CHAR_COUNT` and the scrubbed environment carries no `ANTHROPIC_*` variables, so no paid API is ever called. |

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
| `orjson` 3.12.0 | `openhands-sdk` → `lmnr` | MPL-2.0 AND (Apache-2.0 OR MIT) | Same as certifi: orjson states that it contains source under MPL-2.0 as well as Apache-2.0/MIT; it is used unmodified as a separately installed package. Reviewed 2026-10-04. Earlier versions of `scripts/pylicensecheck.py` let the `OR` short-circuit the whole expression and reported this as `ok`; every `AND` conjunct must now pass. |
| `regex` 2026.9.29 | `tiktoken` | Apache-2.0 AND CNRI-Python | Reviewed 2026-10-04: CNRI-Python is permissive (OSI-approved). Accepted. |
| `func_timeout` 4.3.5 | `openhands-tools` | LGPLv2 | Manual review: used unmodified as a separately installed library and never vendored. Accepted for the adapter environment. Revisit before shipping any bundled image. |
| `agent-client-protocol`, `openhands-sdk`, `openhands-tools` | | no PyPI license metadata | Upstream repositories verified: OpenHands is MIT (above), and agent-client-protocol is Apache-2.0 (github.com/agentclientprotocol/python-sdk, checked via the GitHub license API). |

No GPL, AGPL or SSPL packages are present in the adapter environment.

CI installs both environments from their locks (`uv sync --frozen`), runs
`scripts/pylicensecheck.py` (it fails on a denied license) and fails if the
regenerated `THIRD_PARTY.md` differs from the committed one, so a new
review-class package shows up in review.

## Sandbox image

`adapters/openhands/Dockerfile` describes the agent sandbox image. It is
built locally by `boundedcode sandbox build` and is never published or
distributed by this project. Besides the adapter environment above, it
contains Debian packages (`git`, `gcc`, `make`, `libc6-dev`, `patch`, `less`,
`procps`, `file`, `ripgrep`), the Go toolchain, Node.js and uv, from base
images pinned by digest. Several of the Debian tools are GPL-licensed. That is
acceptable under the [policy](policy.md): they are separate programs, used
unmodified inside an image the user builds on their own machine, and nothing
links them into BoundedCode. If the image were ever distributed, it would need
its own license review and source offer for the GPL packages.
