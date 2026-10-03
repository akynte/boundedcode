# Sandbox image for agent execution: the OpenHands adapter plus common
# toolchains used by verification. Built locally (`boundedcode sandbox build`);
# this project does not publish it. Base images are pinned by digest.
FROM golang:1.27-trixie@sha256:3b77fc618ec235a1ab412de7737f120dd507c57e8d87de4cbb7994fb94275ed5 AS go
FROM node:24-trixie-slim@sha256:8ec5d7557396cfe32d21c3f9c13072355ceab22b584578ca4bb28af31120cffe AS node
FROM ghcr.io/astral-sh/uv:0.12.18@sha256:3adc3706091ce7c2fe595e669628caedd6d951551b92b258b7e7dbe06d9440bc AS uv

FROM python:3.13-slim-trixie@sha256:bb2988715db2cf7ace7b53f38f3cffbef7c7046a656bee66245eb0ed386e2e81
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ripgrep ca-certificates make gcc libc6-dev patch procps less file \
 && rm -rf /var/lib/apt/lists/*
COPY --from=go /usr/local/go /usr/local/go
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
COPY --from=uv /uv /usr/local/bin/uv

WORKDIR /opt/adapter
COPY python/pyproject.toml python/uv.lock python/README.md* ./
COPY python/src ./src
ENV UV_PROJECT_ENVIRONMENT=/opt/adapter/.venv UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy
RUN uv sync --frozen --no-dev --no-editable && rm -rf /root/.cache

# Go caches live under HOME (a tmpfs in the sandbox) so nothing persists
# between tasks unless explicitly mounted.
ENV PATH=/opt/adapter/.venv/bin:/usr/local/go/bin:$PATH \
    GOTOOLCHAIN=local GOPROXY=off \
    OPENHANDS_SUPPRESS_BANNER=1 PYTHONUNBUFFERED=1
# Git refuses repositories owned by another uid unless marked safe; the
# worktree is owned by the invoking user, who is also the container user.
RUN git config --system --add safe.directory '*' || true
ENTRYPOINT []
CMD ["bc-openhands-adapter"]
