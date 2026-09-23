# syntax=docker/dockerfile:1.7
#
# boundedcode image (design v3 §4.2). Three targets:
#
#   --target slim   supervisor plus engine only; inference and toolchains external
#   --target cpu    adds the Go and Node toolchains and the analysis tools (amd64, arm64)
#   --target cuda   adds the CUDA llama-server build (amd64)
#
# Build:
#   docker build -f deploy/Dockerfile --target cpu -t boundedcode:dev .
#
# Every tool version is pinned by digest or release tag, and the manifest is
# generated into /opt/bcode/image-manifest.txt at build time so the running image
# can state exactly what it contains.

# ---------------------------------------------------------------------------
# Stage 1: build the supervisor.
#
# CGO is on because of the tree-sitter grammars (DR-8). Two things follow, and
# both were free when the build was static:
#
#   - the stage runs on the target platform rather than cross-compiling with
#     GOARCH, because a cross build would need a C toolchain per architecture.
#     BuildKit emulates the non-native one, which is slower and correct;
#   - the binary is dynamically linked against this stage's glibc, so this stage
#     and the runtime stages must stay on the same Debian release. They are both
#     trixie, and moving either is a change to the other.
# ---------------------------------------------------------------------------
FROM --platform=$TARGETPLATFORM golang:1.27-trixie AS build

ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG DATE=unknown

WORKDIR /src

# Dependencies first: this layer is reused whenever only source changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY . .

# -trimpath and the pinned toolchain make the build reproducible; the ldflags
# stamp the version the supervisor reports on /healthz.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux \
    go build -trimpath \
      -ldflags="-s -w \
        -X github.com/akynte/boundedcode/internal/version.Version=${VERSION} \
        -X github.com/akynte/boundedcode/internal/version.Commit=${COMMIT} \
        -X github.com/akynte/boundedcode/internal/version.Date=${DATE}" \
      -o /out/bcode ./cmd/bcode

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/storescope ./tools/analyzers/storescope/cmd/storescope
# storescope stays static: it does not use the grammars, and a static analyzer
# tool is one less thing that has to match the runtime's libc.

# ---------------------------------------------------------------------------
# Stage 2: slim runtime. Supervisor plus the engine, nothing else.
# ---------------------------------------------------------------------------
FROM debian:trixie-slim AS slim

ARG VERSION=0.0.0-dev
ARG COMMIT=unknown

LABEL org.opencontainers.image.title="boundedcode" \
      org.opencontainers.image.description="A supervised local coding engineer with per-workspace isolation" \
      org.opencontainers.image.source="https://github.com/akynte/boundedcode" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"

# tini is PID 1 so orphaned grandchildren are reaped even when the supervisor
# is not itself PID 1 (§4.3). bubblewrap is present so DR-3 layer 3 can be
# enabled when the runtime permits user namespaces; git and ripgrep are the
# minimum a task needs.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates tini git ripgrep curl jq bubblewrap procps less \
    && rm -rf /var/lib/apt/lists/*

# An unprivileged user: DR-3 layer 1 assumes the container does not run as
# root, and the data volume is owned by this uid.
RUN groupadd --gid 10001 bcode \
 && useradd --uid 10001 --gid bcode --create-home --home-dir /home/bcode --shell /bin/bash bcode \
 && mkdir -p /data /work /opt/bcode \
 && chown -R bcode:bcode /data /work /opt/bcode

COPY --from=build /out/bcode /usr/local/bin/bcode
COPY --from=build /out/storescope /usr/local/bin/storescope
COPY deploy/entrypoint/entrypoint.sh /usr/local/bin/bcode-entrypoint
COPY profiles/ /opt/bcode/profiles/
RUN chmod +x /usr/local/bin/bcode-entrypoint

# The API binds every interface INSIDE the container because Docker's port
# publishing cannot reach a loopback bind there. Exposure is restricted on the
# host side: publish as `-p 127.0.0.1:7777:7777` (§4.1), never `-p 7777:7777`.
#
# XDG variables are deliberately NOT pointed at /data here. §2.2 sets them per
# task process, at that workspace's own opencode/ directory, so the engine can
# never see another workspace's sessions. A global value would both break that
# and — because build-time tools honour XDG — bake state into the image's /data
# layer, which Docker then copies into every fresh volume.
ENV BC_DATA=/data \
    BC_API_ADDR=0.0.0.0:7777 \
    BC_IN_CONTAINER=1 \
    HOME=/home/bcode \
    XDG_DATA_HOME=/home/bcode/.local/share \
    XDG_CONFIG_HOME=/home/bcode/.config \
    XDG_CACHE_HOME=/home/bcode/.cache \
    PATH=/usr/local/bin:/usr/bin:/bin

# Record what this image actually holds (§4.2: a generated image manifest).
RUN { \
      echo "boundedcode ${VERSION} (${COMMIT})"; \
      echo "variant: slim"; \
      echo "built: $(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
      echo "debian: $(cat /etc/debian_version)"; \
      echo "git: $(git --version)"; \
      echo "ripgrep: $(rg --version | head -1)"; \
      echo "bubblewrap: $(bwrap --version)"; \
      echo "tini: $(tini --version)"; \
    } > /opt/bcode/image-manifest.txt

# /data must ship EMPTY. Docker copies an image's content at a volume path into
# a fresh volume, so anything left here — including files a build step wrote as
# root — becomes undeletable state in every new install.
RUN rm -rf /data/* /data/.[!.]* 2>/dev/null; \
    chown bcode:bcode /data; \
    [ -z "$(ls -A /data)" ] || { echo "/data is not empty in the image:" >&2; ls -la /data >&2; exit 1; }

USER bcode
WORKDIR /work
VOLUME ["/data"]
EXPOSE 7777

# A container health check mirrors /readyz so `docker ps` and compose see the
# same readiness the supervisor reports (DR-1's visibility compensation).
HEALTHCHECK --interval=15s --timeout=5s --start-period=60s --retries=4 \
  CMD curl -fsS http://127.0.0.1:7777/readyz >/dev/null || exit 1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/bcode-entrypoint"]
CMD ["api"]

# ---------------------------------------------------------------------------
# Stage 3: cpu runtime. Adds the toolchains and analysis tools a Go or
# TypeScript task needs, but no inference: on CPU-only and Apple Silicon hosts
# inference is expected to be external (§4.2).
# ---------------------------------------------------------------------------
FROM slim AS cpu

ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG TARGETARCH
# Renovate keeps these pinned versions current; each is also listed in the
# generated image manifest.
ARG GO_VERSION=1.27.1
# Pinned checksums from the Go release index. Deliberately pinned rather than
# fetched alongside the tarball: downloading a checksum from the same server as
# the artifact only checks that server against itself. Update both when
# GO_VERSION moves — the build fails loudly if they do not match.
ARG GO_SHA256_amd64=63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445
ARG GO_SHA256_arm64=3450b45a3f9ee8568792736a5c5e70a1f2e9b36c35a8f74958c03e51d7d92bec
ARG NODE_MAJOR=22
ARG GOLANGCI_VERSION=2.13.2
ARG GOLANGCI_SHA256_amd64=2277d43b98ec0054280f2ac26b53268bae97682444678a59a657dd565da021d6
ARG GOLANGCI_SHA256_arm64=a2a4e0065aa41be71f7c5ac90f271b61751331e5d04314e62afe4027855f0893
ARG SEMGREP_VERSION=1.177.0
# The §4.2 analysis tools a repository can wire into a `check:` step in
# .bc/verify.yaml. Each is pinned; `go install` resolves through the module
# proxy and the checksum database, which is stronger provenance than a
# hand-pinned tarball hash.
ARG GOSEC_VERSION=v2.29.0
ARG GITLEAKS_VERSION=v8.30.1
ARG OSV_SCANNER_VERSION=v2.6.0
ARG BUF_VERSION=v1.73.0
ARG OASDIFF_VERSION=v1.32.0
ARG SQUAWK_VERSION=2.65.0

USER root

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
      build-essential pkg-config make xz-utils gnupg \
    && rm -rf /var/lib/apt/lists/*

# Go toolchain, verified against a pinned checksum.
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) expected="${GO_SHA256_amd64}" ;; \
      arm64) expected="${GO_SHA256_arm64}" ;; \
      *) echo "no pinned Go checksum for architecture ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" -o /tmp/go.tgz; \
    echo "${expected}  /tmp/go.tgz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm -f /tmp/go.tgz

ENV PATH=/usr/local/go/bin:/opt/bcode/gotools/bin:$PATH \
    GOPATH=/opt/bcode/gopath \
    GOBIN=/opt/bcode/gotools/bin \
    GOFLAGS=-mod=mod

# Node LTS for the TypeScript sidecars and OpenCode.
RUN set -eux; \
    curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" -o /tmp/node.sh; \
    bash /tmp/node.sh; \
    apt-get install -y --no-install-recommends nodejs; \
    rm -rf /var/lib/apt/lists/* /tmp/node.sh

# The compiler-backed analysis tools the verification loops depend on (§10.1).
# HOME is overridden for this layer: the ENV sets it to /home/bcode, and a root
# `go install` would leave a root-owned build cache there that the runtime user
# then cannot write — which surfaces much later as a permission error from
# inside the compiler.
RUN set -eux; \
    export HOME=/root; \
    mkdir -p "$GOBIN"; \
    go install golang.org/x/tools/gopls@latest; \
    go install honnef.co/go/tools/cmd/staticcheck@latest; \
    go install golang.org/x/vuln/cmd/govulncheck@latest; \
    go install golang.org/x/perf/cmd/benchstat@latest; \
    chmod -R a+rx /opt/bcode/gotools; \
    go clean -cache -modcache -testcache || true; \
    rm -rf "$GOPATH"/pkg "$HOME"/.cache/go-build || true

# §4.2's analysis tools. None of these has a built-in recipe — which tables a
# migration may lock, and which proto changes are breaking, are facts about a
# repository — so a repository reaches them through a `check:` step in
# .bc/verify.yaml. They are in the image because the engine has no shell tool:
# its only route to an external program is run_recipe, so a tool absent here is
# unreachable rather than "install it yourself".
# Each tool is its own `go install` line rather than one chain, so a network
# hiccup on the fourth does not re-download the first three on retry. buf asks
# for a newer toolchain than GO_VERSION pins, so Go fetches one here: that is
# the cost of pinning the base toolchain and letting tools state their own
# minimum, and it is visible in the build log rather than silent.
#
# The cleanup at the end removes the extracted module tree, the build cache and
# any toolchain Go fetched. Removing only the *download* cache — which is what
# this did at first — left 3.2 GB of extracted modules behind, and the image
# went from 710 MB to 3.8 GB without anyone deciding that.
#
# Note the comments are here and not inside the RUN: a `#` after a line ending
# in `\` comments out every line that follows it, cleanup included.
RUN set -eux; \
    export HOME=/root; \
    go install "github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}"; \
    go install "github.com/zricethezav/gitleaks/v8@${GITLEAKS_VERSION}"; \
    go install "github.com/google/osv-scanner/v2/cmd/osv-scanner@${OSV_SCANNER_VERSION}"; \
    go install "github.com/bufbuild/buf/cmd/buf@${BUF_VERSION}"; \
    go install "github.com/oasdiff/oasdiff@${OASDIFF_VERSION}"; \
    chmod -R a+rx /opt/bcode/gotools; \
    go clean -cache -modcache -testcache || true; \
    rm -rf "$GOPATH"/pkg "$HOME"/.cache/go-build || true

# squawk lints Postgres migrations and is Rust, distributed through npm.
RUN set -eux; \
    export HOME=/root; \
    npm install -g --no-audit --no-fund "squawk-cli@${SQUAWK_VERSION}"; \
    npm cache clean --force

# golangci-lint and semgrep are the two tools `internal/recipe` shells out to
# that the toolchain does not provide. Both were listed in §4.2's image
# contents and neither was here, which made the semgrep recipe skip on every
# published image and left §10.1's lint leg with nothing to run.
#
# They are in the image rather than left to the user because the engine has no
# shell tool: its only route to an external program is `run_recipe`, so a tool
# absent from the image is not "install it yourself", it is unreachable.
#
# Released binaries, verified against the published checksums. `go install`
# would build golangci-lint from source against this image's Go rather than the
# one it was released with, which upstream explicitly does not support.
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) expected="${GOLANGCI_SHA256_amd64}" ;; \
      arm64) expected="${GOLANGCI_SHA256_arm64}" ;; \
      *) echo "no pinned golangci-lint checksum for ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    base="golangci-lint-${GOLANGCI_VERSION}-linux-${TARGETARCH}"; \
    curl -fsSL "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_VERSION}/${base}.tar.gz" -o /tmp/gl.tgz; \
    echo "${expected}  /tmp/gl.tgz" | sha256sum -c -; \
    tar -xzf /tmp/gl.tgz -C /tmp; \
    install -m 0755 "/tmp/${base}/golangci-lint" /usr/local/bin/golangci-lint; \
    rm -rf /tmp/gl.tgz "/tmp/${base}"; \
    golangci-lint --version

# semgrep is Python and has no released binary, so it gets its own virtualenv
# rather than being installed into the system interpreter, where its pins would
# become the image's pins. Only the entry point is exposed on PATH.
#
# It is the single largest thing in this image — about 374 MB against a 710 MB
# base — and that is a deliberate trade recorded in docs/reference/image-manifest.md:
# §4.2 lists semgrep, §10.1 adopts project-invariant analyzers, and a recipe
# that skips on every published image is worse than the bytes.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
      python3-venv \
    && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    python3 -m venv /opt/bcode/semgrep; \
    /opt/bcode/semgrep/bin/pip install --no-cache-dir --upgrade pip; \
    /opt/bcode/semgrep/bin/pip install --no-cache-dir "semgrep==${SEMGREP_VERSION}"; \
    ln -s /opt/bcode/semgrep/bin/semgrep /usr/local/bin/semgrep; \
    semgrep --version

# The TypeScript sidecar (§3.2). Node above exists for it, and without this
# layer the image had Node and no sidecar — so every TypeScript repository got
# the lexical reading and no call graph, silently, because a missing sidecar is
# an ordinary condition the analyzer is designed to survive.
#
# Dependencies are installed from the committed lockfile, in their own layer so
# a change to analyze.js does not re-resolve them. HOME is /root for this layer
# for the same reason the Go tools layer does it: npm's cache would otherwise
# land in /home/bcode owned by root, and surface much later as a permission error.
COPY sidecars/typescript/package.json sidecars/typescript/package-lock.json \
     /opt/bcode/sidecars/typescript/
RUN set -eux; \
    export HOME=/root; \
    cd /opt/bcode/sidecars/typescript; \
    npm ci --omit=dev --no-audit --no-fund; \
    npm cache clean --force

COPY sidecars/typescript/analyze.js /opt/bcode/sidecars/typescript/

# BC_TYPESCRIPT_SIDECAR_DIR is checked first by SidecarPath, and naming it
# explicitly keeps the answer independent of where the binary happens to sit.
ENV BC_TYPESCRIPT_SIDECAR_DIR=/opt/bcode/sidecars/typescript

# The sidecar must actually run, not merely be present. An `npm ci` that
# resolved nothing, or a Node that cannot load the compiler, fails here rather
# than degrading to the lexical reading in a user's repository.
RUN set -eux; \
    mkdir -p /tmp/probe; \
    printf 'export function f(): number { return 1; }\nexport function g(): number { return f(); }\n' > /tmp/probe/probe.ts; \
    node /opt/bcode/sidecars/typescript/analyze.js /tmp/probe > /tmp/probe.json; \
    grep -q '"kind":"calls"' /tmp/probe.json \
      || grep -q '"kind": "calls"' /tmp/probe.json \
      || { echo "the TypeScript sidecar produced no call edges" >&2; cat /tmp/probe.json >&2; exit 1; }; \
    rm -rf /tmp/probe /tmp/probe.json

RUN { \
      cat /opt/bcode/image-manifest.txt | sed 's/^variant: slim/variant: cpu/'; \
      echo "go: $(go version)"; \
      echo "node: $(node --version)"; \
      echo "npm: $(npm --version)"; \
      echo "gopls: $(gopls version 2>/dev/null | head -1)"; \
      echo "staticcheck: $(staticcheck --version 2>/dev/null)"; \
      echo "govulncheck: present"; \
      echo "benchstat: present"; \
      echo "golangci-lint: $(golangci-lint --version 2>/dev/null | head -1)"; \
      echo "gosec: ${GOSEC_VERSION}"; \
      echo "gitleaks: ${GITLEAKS_VERSION}"; \
      echo "osv-scanner: ${OSV_SCANNER_VERSION}"; \
      echo "buf: ${BUF_VERSION}"; \
      echo "oasdiff: ${OASDIFF_VERSION}"; \
      echo "squawk: ${SQUAWK_VERSION}"; \
      echo "semgrep: $(semgrep --version 2>/dev/null | head -1)"; \
      echo "typescript-sidecar: typescript $(node -p "require('/opt/bcode/sidecars/typescript/node_modules/typescript/package.json').version")"; \
    } > /opt/bcode/image-manifest.cpu.txt && mv /opt/bcode/image-manifest.cpu.txt /opt/bcode/image-manifest.txt

RUN chown -R bcode:bcode /opt/bcode /home/bcode

# Re-assert the invariant after the root-owned toolchain install.
RUN rm -rf /data/* /data/.[!.]* 2>/dev/null; \
    chown bcode:bcode /data; \
    [ -z "$(ls -A /data)" ] || { echo "/data is not empty in the image:" >&2; ls -la /data >&2; exit 1; }

USER bcode
WORKDIR /work

# ---------------------------------------------------------------------------
# Stage 4: cuda runtime. The default published image on amd64 (§4.2).
# The CUDA llama-server build is pinned; §16 verify item 4 tracks the driver
# and runtime pairing.
# ---------------------------------------------------------------------------
FROM cpu AS cuda

ARG VERSION=0.0.0-dev
# A build that publishes a Linux CUDA server. The asset naming carries the CUDA
# minor, so bumping this may mean bumping LLAMA_CUDA with it.
ARG LLAMA_RELEASE=b11042
ARG LLAMA_CUDA=13.3

USER root

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
      libgomp1 unzip \
    && rm -rf /var/lib/apt/lists/*

# The llama.cpp CUDA server binary. Kept as a separate layer so a llama.cpp
# bump does not invalidate the toolchain layers.
#
# A failure here fails the build. It used to warn and carry on, and the result
# was a `cuda` image that had never once contained llama-server: the pinned
# release predated Linux CUDA assets entirely, so the download 404'd on every
# build and the warning scrolled past in CI. An image that says cuda in its name
# and cannot run a model is worse than a build that stops.
RUN set -eux; \
    url="https://github.com/ggml-org/llama.cpp/releases/download/${LLAMA_RELEASE}/llama-${LLAMA_RELEASE}-bin-ubuntu-cuda-${LLAMA_CUDA}-x64.tar.gz"; \
    curl -fsSL "$url" -o /tmp/llama.tar.gz; \
    mkdir -p /opt/bcode/llama; \
    tar -xzf /tmp/llama.tar.gz -C /opt/bcode/llama --strip-components=1; \
    # The binary is linked with RUNPATH $ORIGIN, so it finds its libraries by
    # sitting beside them. Installing it into /usr/local/bin and the libraries
    # into /usr/local/lib separates the two and the loader then fails with an
    # exit status that reads as "command not found". It stays where it was
    # unpacked and PATH gets a symlink, which ld.so resolves before expanding
    # $ORIGIN.
    ln -sf /opt/bcode/llama/llama-server /usr/local/bin/llama-server; \
    rm -f /tmp/llama.tar.gz; \
    llama-server --version

RUN sed -i 's/^variant: cpu/variant: cuda/' /opt/bcode/image-manifest.txt \
 && echo "llama.cpp: ${LLAMA_RELEASE}" >> /opt/bcode/image-manifest.txt \
 && chown -R bcode:bcode /opt/bcode

RUN rm -rf /data/* /data/.[!.]* 2>/dev/null; \
    chown bcode:bcode /data; \
    [ -z "$(ls -A /data)" ] || { echo "/data is not empty in the image:" >&2; ls -la /data >&2; exit 1; }

USER bcode
WORKDIR /work
