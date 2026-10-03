# Sandbox and Safety Design (Phase 6)

Status: **implemented** for the Docker-compatible engine; verified by
`internal/sandbox/adversarial_test.go`, `internal/gitops` tamper tests and
`internal/agent/openhands` in-container tests.

## Threat model

The agent is an LLM that executes shell commands chosen from model output.
We assume it can be wrong or adversarially steered, for example by prompt
injection in code, issues or dependencies it reads. The goal is that a
misbehaving agent can damage **only its own task worktree**, which is a Git
branch the control plane can always inspect or discard. It must not be able
to exfiltrate secrets or affect the host.

Agent-level permission prompts (OpenHands confirmation policies) are **not**
part of this boundary.

## Controls

| # | Control | Where |
|---|---|---|
| 1 | Container per agent session: `--rm --init --cap-drop ALL --security-opt no-new-privileges --pids-limit`, memory and CPU limits, runs as the invoking UID/GID (never root) | `sandbox.Container.Args` |
| 2 | `--network none`. The model is reached only through the stdio tunnel to the Go gateway (ADR-0004). | sandbox, adapter |
| 3 | Identity mounts of only: the task work dir (rw), the OpenHands persistence dir (rw), each repo's **git common dir read-only**, and each worktree's own admin dir (rw: HEAD and index) | `openhands.Runtime.Open` |
| 4 | Private tmpfs `$HOME`. The host home, `~/.ssh`, cloud CLIs, kubeconfig, `~/.codex` and the Docker socket are never mounted, and an explicit refusal list is enforced in code. | `forbiddenHostMount` |
| 5 | Secret masking: `.env*`, `secrets/`, keys, kubeconfigs, tfstate/tfvars and service-account JSON inside worktrees are hidden behind empty read-only mounts (tmpfs for directories, an empty bind for files) | `policy.FindSecretPaths`, `sandbox` |
| 6 | Environment: containers get only the variables we set. Host-side runners scrub credential-like variables (`*_TOKEN`, `*SECRET*`, `AWS_*`, `OPENAI_*`, `SSH_AUTH_SOCK`, `KUBECONFIG`, `LMNR_*`, `OTEL_*`, …). | `sandbox.ScrubbedEnv` |
| 7 | Deterministic command policy blocks push/force-push, hard resets, protected-branch merges, `terraform apply/destroy`, mutating `kubectl`/`helm`/cloud CLIs, publishing, and `curl … \| sh`. It applies to configured verification commands. | `policy.CheckCommand` |
| 8 | Host-side git hardening: hooks disabled (`core.hooksPath=/dev/null`), `core.fsmonitor=false`, and `--no-ext-diff --no-textconv`. The worktree `.git` pointer is verified before every host git operation, and a tampered pointer blocks the task. | `gitops.Run`, `gitops.CheckWorktree` |
| 9 | Commits happen host-side on `agent/<task-id>` only. `CommitAll` refuses other branches. There are no pushes. | `gitops.CommitAll` |
| 10 | Verification runs in the same image with the worktree mounted and the git dirs read-only. The module cache is mounted read-only, with `GOPROXY=off`. | `verify.Engine.spec` |
| 11 | Diff-scope gate: changes touching secret paths, or more than N files, fail verification. gitleaks scans the task diff. | `verify` |
| 12 | Frontier credentials stay on the host. `codex exec` runs read-only in an empty directory and receives only a redacted packet. API-key auth is refused. | `frontier.Codex` |
| 13 | Audit events are redacted (`telemetry.Redact`), and prompts and source are not logged by default. | `telemetry` |

## Adversarial tests (all must fail to escape)

| Probe | Result |
|---|---|
| `cat .env` (masked file) | empty file, canary not readable |
| `cat deploy/secrets/key.pem` (masked dir) | not visible |
| read host env canary (`BC_CANARY_API_KEY`) | absent |
| list host `$HOME`, read `~/.ssh/id_ed25519` | not mounted |
| HTTP to 1.1.1.1 | no network |
| access `/var/run/docker.sock` | absent |
| capabilities / privilege | `CapEff` = 0, `NoNewPrivs` = 1, non-root |
| overwrite masked `.env` | host file unchanged |
| plant `.git/hooks/pre-commit`, then host commit | hook not executed |
| redirect worktree `.git` to a crafted gitdir | rejected by `CheckWorktree` |

## Residual risks (known, accepted for now)

1. **Container/VM escape.** A kernel or runtime vulnerability could break
   isolation. Docker Desktop adds a VM boundary, while plain Docker or Podman
   relies on namespaces. Mitigation: keep the engine updated. Rootless Podman
   is supported through `sandbox.engine: podman` but has not been benchmarked.
2. **The worktree is fully writable.** The agent can corrupt or delete its
   own worktree. This is recoverable from checkpoint commits on the task
   branch.
3. **Build scripts run with worktree access.** `go test` or `npm` scripts
   chosen by the agent run inside the sandbox, so they are contained but
   unrestricted within it.
4. **Secret detection is pattern-based.** A secret stored under an
   innocuous name (for example `config/prod.yaml`) is not masked. gitleaks
   on the diff catches *new* secrets only. Mitigation: keep production
   secrets out of development checkouts (see SECURITY.md).
5. **Persistence directory.** The OpenHands conversation store is writable
   by the agent. It holds data, not executables, and the control plane
   treats it as untrusted. The task ledger in SQLite is the source of truth
   and is not mounted.
6. **Resource exhaustion** within limits, such as filling the worktree's
   disk. This is bounded by disk quota only.
7. **`sandbox.kind: none`** disables all of the above. It is refused for
   autonomous tasks unless `--unsafe-no-sandbox` is passed.
8. **Prompt injection steering the work itself.** A malicious repository
   can make the agent write wrong code. The defence is deterministic
   verification plus human review of the merge candidate. Nothing merges
   automatically.
