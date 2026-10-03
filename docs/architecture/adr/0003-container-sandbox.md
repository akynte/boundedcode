# ADR-0003: Container sandbox for agent execution

Status: accepted (2026-10-03). Implementation details are in
[docs/design/sandbox.md](../../design/sandbox.md).

## Context
Agent-level permission prompts are not an isolation boundary. The agent runs
shell commands chosen by an LLM that can be wrong or manipulated through
prompt injection in repository content.

## Decision
* Autonomous tasks run the OpenHands adapter, and therefore every agent tool
  invocation, inside a container. The engine is any Docker-compatible CLI.
  The reference machine runs Docker Desktop, whose containers live in a VM;
  rootless Podman is also supported through `sandbox.engine: podman`.
* Mounts: the task worktree is read-write at `/workspace`. The repository's
  Git common dir is mounted read-write only for what the worktree needs.
  The OpenHands persistence dir is read-write. Nothing else from the host
  is mounted: no `$HOME`, `~/.ssh`, cloud credentials, kubeconfig or Docker
  socket.
* Network: `none`. LLM traffic is tunnelled over stdio (ADR-0004).
* Secret files inside the worktree are masked with empty read-only overlay
  mounts: `.env*`, `secrets/`, keys, kubeconfigs, and `*.pem`/`*.key`.
* The container runs as the invoking UID/GID, with `--cap-drop ALL`,
  `--security-opt no-new-privileges` and memory, CPU and PID limits.
* `sandbox.kind: none` exists for development. Autonomous `task run` refuses
  it unless `--unsafe-no-sandbox` is passed explicitly.

## Consequences
* An image containing Python, the OpenHands SDK and common build toolchains
  must be built locally. It is never published by us.
* Verification commands run in the same image so that results match what the
  agent saw.
* Residual risks, such as kernel/VM escape, malicious build scripts reading
  the mounted worktree, and resource exhaustion inside limits, are documented
  in the sandbox design doc.
