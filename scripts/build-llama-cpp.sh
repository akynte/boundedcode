#!/usr/bin/env bash
# Build a pinned llama.cpp release with CUDA. llama.cpp is MIT-licensed and is
# used as an external runtime; nothing is vendored into this repository.
#
# usage: scripts/build-llama-cpp.sh [TAG] [PREFIX]
#   TAG     llama.cpp release tag (default: pinned value below)
#   PREFIX  install root (default: ${XDG_DATA_HOME:-~/.local/share}/boundedcode/runtimes/llama.cpp/TAG)
# env: CUDA_ARCH (default: autodetected compute capability, e.g. 89), JOBS
set -euo pipefail

PINNED_TAG="v0.5.0" # keep in sync with docs/architecture/upstream-components.md
# Commit the pinned tag resolves to (git ls-remote ... 'refs/tags/v0.5.0^{}').
# A tag can be moved upstream; building the pinned tag at any other commit fails.
PINNED_COMMIT="7fe450e19305b828c199d602c23a8337aaa1f03b"
TAG="${1:-$PINNED_TAG}"
DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
PREFIX="${2:-$DATA_HOME/boundedcode/runtimes/llama.cpp/$TAG}"
SRC="${PREFIX}.src"
JOBS="${JOBS:-$(nproc)}"

if [[ -z "${CUDA_ARCH:-}" ]] && command -v nvidia-smi >/dev/null; then
  CUDA_ARCH="$(nvidia-smi --query-gpu=compute_cap --format=csv,noheader | head -1 | tr -d '.[:space:]')"
fi
CUDA_FLAGS=()
if [[ -n "${CUDA_ARCH:-}" ]]; then
  export PATH="/usr/local/cuda/bin:$PATH"
  command -v nvcc >/dev/null || { echo "nvcc not found (install the CUDA toolkit)" >&2; exit 1; }
  CUDA_FLAGS=(-DGGML_CUDA=ON "-DCMAKE_CUDA_ARCHITECTURES=${CUDA_ARCH}")
  echo "building with CUDA for sm_${CUDA_ARCH}"
else
  echo "no NVIDIA GPU detected: CPU-only build"
fi

if [[ ! -d "$SRC/.git" ]]; then
  git clone --depth 1 --branch "$TAG" https://github.com/ggml-org/llama.cpp "$SRC"
fi
git -C "$SRC" fetch --depth 1 origin "refs/tags/$TAG:refs/tags/$TAG" 2>/dev/null || true
git -C "$SRC" checkout -q "$TAG"
COMMIT="$(git -C "$SRC" rev-parse HEAD)"
if [[ "$TAG" == "$PINNED_TAG" && "$COMMIT" != "$PINNED_COMMIT" ]]; then
  echo "llama.cpp $TAG resolved to $COMMIT, expected pinned commit $PINNED_COMMIT; refusing to build" >&2
  echo "(re-verify the tag upstream and update PINNED_COMMIT and the license matrix deliberately)" >&2
  exit 1
fi

cmake -S "$SRC" -B "$SRC/build" -DCMAKE_BUILD_TYPE=Release \
  -DLLAMA_CURL=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF \
  -DGGML_NATIVE=ON "${CUDA_FLAGS[@]}" \
  -DCMAKE_INSTALL_PREFIX="$PREFIX" -DCMAKE_BUILD_RPATH='$ORIGIN'
cmake --build "$SRC/build" --config Release -j "$JOBS" --target llama-server llama-bench
# Copy only what we built: the two tools plus the shared libraries they load.
# The binaries look for libraries next to themselves (RPATH $ORIGIN), and the
# runtime also sets LD_LIBRARY_PATH to the binary directory.
rm -rf "$PREFIX/bin" "$PREFIX/lib"
mkdir -p "$PREFIX/bin"
cp -a "$SRC/build/bin/llama-server" "$SRC/build/bin/llama-bench" "$SRC"/build/bin/*.so* "$PREFIX/bin/"
cp -f "$SRC/LICENSE" "$PREFIX/LICENSE"
cat > "$PREFIX/BUILD_INFO" <<INFO
repo=https://github.com/ggml-org/llama.cpp
tag=$TAG
commit=$COMMIT
cuda_arch=${CUDA_ARCH:-none}
built_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
INFO
"$PREFIX/bin/llama-server" --version 2>&1 | tail -2
echo "installed to $PREFIX"
echo "configure: boundedcode init --llama-server $PREFIX/bin/llama-server --llama-bench $PREFIX/bin/llama-bench"
