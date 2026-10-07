# Model catalog research (2026-10-07)

Candidates for the hardware-aware model choice in set-up
([multiplatform-plan.md](../development/multiplatform-plan.md), Phase 12).

**Sources.** Repository, revision, license and gating from
`https://huggingface.co/api/models/<repo>`; file size and sha256 (the LFS
oid) from `https://huggingface.co/api/models/<repo>/tree/<revision>?recursive=1&expand=1`;
architecture, context length and benchmark claims from each model card
(`README.md`, `config.json`) and the GGUF metadata in the HF API. Nothing was
run locally. **Only Qwen3.6-35B-A3B has been measured by BoundedCode**; every
other row rests on vendor claims and must pass `bench infra` and the
engineering suite before it ships as a non-experimental profile.

All rows below: not gated, a single GGUF file, and a chat template that
handles `tools` (checked in the GGUF's embedded template).

## Candidates

| Tier | Model | GGUF repo @ revision | File | Bytes | sha256 | License |
|---|---|---|---|---|---|---|
| Tiny (CPU, 8 GB RAM) | Qwen3.5-4B | `unsloth/Qwen3.5-4B-GGUF` @ `e87f176479d0855a907a41277aca2f8ee7a09523` | `Qwen3.5-4B-Q4_K_M.gguf` | 2,740,937,888 | `00fe7986ff5f6b463e62455821146049db6f9313603938a70800d1fb69ef11a4` | Apache-2.0 |
| Tiny | granite-4.2-3b | `ibm-granite/granite-4.2-3b-GGUF` @ `c40945d71cd90f249a56985e8155551a9188dc30` | `granite-4.2-3b-Q4_K_M.gguf` | 2,244,011,552 | `e0406663965846ae22a403456eb826ccce5f450840491f71952f18a7cb78e7d5` | Apache-2.0 |
| Small (6–8 GB VRAM, 16 GB unified) | Qwen3.5-9B | `unsloth/Qwen3.5-9B-GGUF` @ `3885219b6810b007914f3a7950a8d1b469d598a5` | `Qwen3.5-9B-Q4_K_M.gguf` | 5,680,522,464 | `03b74727a860a56338e042c4420bb3f04b2fec5734175f4cb9fa853daf52b7e8` | Apache-2.0 |
| Small | gemma-4-12B-it | `unsloth/gemma-4-12b-it-GGUF` @ `fc034cfff751157913579611efad8462ac1be606` | `gemma-4-12b-it-Q4_K_M.gguf` | 7,121,861,440 | `0a270ec9fe6b34f4a0d33992b6135117b484ebc4766ab76b51d4ae8c457e4c42` | Apache-2.0 |
| Medium (12–16 GB VRAM, 32 GB) | gpt-oss-20b | `ggml-org/gpt-oss-20b-GGUF` @ `ef9b12f2ff56c69cf32153a02784e7a3c88bf524` | `gpt-oss-20b-MXFP4.gguf` | 12,109,566,624 | `27cd6c432c7672cb812a92f611cf3ba7bbc35928262bb1e1253ff4ee6ae35901` | Apache-2.0 + usage policy |
| Medium | Devstral-Small-2-24B-Instruct-2512 | `unsloth/Devstral-Small-2-24B-Instruct-2512-GGUF` @ `6e458b8add42681bfd023de5eab93637694aaf82` | `Devstral-Small-2-24B-Instruct-2512-Q4_K_M.gguf` | 14,334,446,752 | `d14ba9edee1bb4c4996a726deb81e49ae81800a3216f0774634238c380aee496` | Apache-2.0 (base repo; the unsloth repo is tagged `other`) |
| Large (default) | Qwen3.6-35B-A3B | `unsloth/Qwen3.6-35B-A3B-GGUF` @ `a483e9e6cbd595906af30beda3187c2663a1118c` | `Qwen3.6-35B-A3B-UD-Q4_K_M.gguf` | 22,134,528,992 | `ac0e2c1189e055faa36eff361580e79c5bd6f8e76bffb4ce547f167d53e31a61` | Apache-2.0 |
| Large (24 GB+ VRAM, 32–64 GB Mac) | Qwen3.8-27B | `unsloth/Qwen3.8-27B-GGUF` @ `4ca720788d1e01f1bff70c033e0d0028fd02e502` | `Qwen3.8-27B-UD-Q4_K_M.gguf` | 16,464,440,224 | `322e194ff79741c7baa497c240f677f54b201b0efab44ca8e50f122b39123482` | Apache-2.0 |

Vendor claims, for orientation only (different harnesses; not comparable
across rows): Qwen3.5-4B BFCL-V4 50.3, TAU2 79.9; granite-4.2-3b BFCL v4 52.4;
Qwen3.5-9B BFCL-V4 66.1, TAU2 79.1; gemma-4-12B-it Tau2 69.0; gpt-oss-20b
"runs within 16 GB of memory", native function calling; Devstral Small 2
SWE-bench Verified 68.0 (the card targets a 24 GB GPU or a 32 GB Mac);
Qwen3.8-27B SWE-bench Pro 61.7.

Notes:

- Memory need is file size plus KV cache plus compute buffers. KV figures in
  the research notes are estimates from `config.json`, not measurements.
  Dense models with many full-attention layers (granite, Devstral) need much
  more KV at long context than the hybrid-attention Qwen3.5/3.8 models.
- Devstral Small 2 needs llama.cpp changes from PR #17945 (merged
  2025-12-12); no card states a minimum llama.cpp version, and loading on the
  pinned llama.cpp v0.5.0 is unverified for every row except the default.
- Gemma 4 is under the Apache License 2.0
  (https://ai.google.dev/gemma/docs/gemma_4_license), not the earlier Gemma
  terms. Several base repositories have no LICENSE file in the repo tree
  (Gemma 4, Granite 4.2, Devstral Small 2); the license-matrix step must use
  the vendor's published license text.
- Considered and not recommended for these tiers: Qwen3.8-Flash-Next (177B,
  custom license), GLM-5.3-Flash (320B), gpt-oss-120b (63 GB file),
  NVIDIA-Nemotron-3.5-Lightning-30B-A3B (OpenMDW-1.1, needs review),
  North-Mini-Code-1.0 (Apache-2.0 in card metadata but no license file;
  benchmarks only as an image), GLM-4.7-Flash (MIT; older).

## Prebuilt llama.cpp

The latest llama.cpp release on 2026-10-07 is v0.6.0 (build b11429,
commit `d81235049384534c167caea52b85a694f6103d14`); BoundedCode pins v0.5.0
(build b11146). Build b11429 publishes binaries for macOS (arm64, x64),
Windows (CPU, Vulkan, CUDA 12.4 and 13.4, ROCm, SYCL; x64 and arm64) and
Ubuntu (CPU, Vulkan, CUDA 12.8 and 13.4, ROCm). There is no SHA256SUMS file:
the GitHub API reports a sha256 `digest` per asset, and the release links
build attestations. Phase 12/14/15 pins per-platform assets by that digest.
