# Sandbox image for agent execution: the OpenHands adapter plus common
# toolchains used by verification. Built locally (`boundedcode sandbox build`);
# this project does not publish it. Base images are pinned by digest.
FROM golang:1.27-trixie@sha256:3b77fc618ec235a1ab412de7737f120dd507c57e8d87de4cbb7994fb94275ed5 AS go
FROM node:24-trixie-slim@sha256:8ec5d7557396cfe32d21c3f9c13072355ceab22b584578ca4bb28af31120cffe AS node
FROM ghcr.io/astral-sh/uv:0.12.18@sha256:3adc3706091ce7c2fe595e669628caedd6d951551b92b258b7e7dbe06d9440bc AS uv
FROM rust:1.99-trixie@sha256:6ff07edce8775d0f64be7aba9197229407301bddf2054d62c27b541a6238a181 AS rust
FROM eclipse-temurin:11-jdk@sha256:8db2bcf62ae171d1247c331db0410d8bc6347e7493bdafe104bb9a33d59c1291 AS jdk11
FROM eclipse-temurin:17-jdk@sha256:5d6042fb8cdc14d614e4e421f52e3211fc5aeadddce44cbf9de8ed37c791f824 AS jdk17
FROM eclipse-temurin:21-jdk@sha256:3e3c176ffed168beb42c607be9bc1639b466cf00261a0fb04425562c9d0c5c2b AS jdk21
FROM maven:3.9-eclipse-temurin-21@sha256:99e61abcff91a9b1333463bd8451fb18495d6eba9250ac66a338b518f8278320 AS maven
FROM gradle:9-jdk21@sha256:4debe478645a3ad9208f3403ac021859fdf4d1a819d59970d75309b098f28f76 AS gradle

FROM python:3.13-slim-trixie@sha256:bb2988715db2cf7ace7b53f38f3cffbef7c7046a656bee66245eb0ed386e2e81
# Toolchains for the languages verification has presets for (see
# internal/verify/presets.go): C/C++ (CMake, Meson, Make), Ruby and PHP from
# Debian; Go, Node, Rust, Java (11, 17, 21; Maven, Gradle) from pinned images.
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ripgrep ca-certificates make gcc g++ libc6-dev patch procps less file \
      cmake meson ninja-build pkg-config autoconf automake libtool \
      ruby ruby-dev ruby-bundler \
      php-cli php-xml php-mbstring php-curl php-intl php-zip php-sqlite3 php-bcmath php-gd composer \
 && rm -rf /var/lib/apt/lists/*
COPY --from=go /usr/local/go /usr/local/go
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
COPY --from=uv /uv /usr/local/bin/uv
COPY --from=rust /usr/local/rustup /usr/local/rustup
COPY --from=rust /usr/local/cargo/bin /usr/local/cargo/bin
COPY --from=jdk11 /opt/java/openjdk /opt/jdk/11
COPY --from=jdk17 /opt/java/openjdk /opt/jdk/17
COPY --from=jdk21 /opt/java/openjdk /opt/jdk/21
COPY --from=maven /usr/share/maven /opt/maven
COPY --from=gradle /opt/gradle /opt/gradle
RUN ln -s /opt/maven/bin/mvn /usr/local/bin/mvn && ln -s /opt/gradle/bin/gradle /usr/local/bin/gradle

WORKDIR /opt/adapter
COPY python/pyproject.toml python/uv.lock python/README.md* ./
COPY python/src ./src
ENV UV_PROJECT_ENVIRONMENT=/opt/adapter/.venv UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy
RUN uv sync --frozen --no-dev --no-editable && rm -rf /root/.cache

# Go caches live under HOME (a tmpfs in the sandbox) so nothing persists
# between tasks unless explicitly mounted.
# Package caches likewise (CARGO_HOME, the Maven local repository and
# GRADLE_USER_HOME are set per task by verification and the agent runtime).
ENV PATH=/opt/adapter/.venv/bin:/usr/local/go/bin:/usr/local/cargo/bin:$PATH \
    GOTOOLCHAIN=local GOPROXY=off \
    RUSTUP_HOME=/usr/local/rustup CARGO_NET_OFFLINE=true \
    JAVA_HOME=/opt/jdk/21 JDK11_HOME=/opt/jdk/11 JDK17_HOME=/opt/jdk/17 JDK21_HOME=/opt/jdk/21 \
    OPENHANDS_SUPPRESS_BANNER=1 PYTHONUNBUFFERED=1
# Git refuses repositories owned by another uid unless marked safe; the
# worktree is owned by the invoking user, who is also the container user.
RUN git config --system --add safe.directory '*' || true
ENTRYPOINT []
CMD ["bc-openhands-adapter"]
