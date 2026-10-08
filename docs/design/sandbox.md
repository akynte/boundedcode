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
| 7 | Deterministic command policy blocks push/force-push, hard resets, protected-branch merges, `terraform apply/destroy`, mutating `kubectl`/`helm`/cloud CLIs, publishing, and `curl … \| sh`. Command lines are normalized first (global flags such as `git -C`, `kubectl --context`, `terraform -chdir`; env assignments; wrappers; `sh -c` bodies). It applies to verification commands. The agent's own commands are not filtered: inside the container they cannot reach a remote, credentials or the host. | `policy.CheckCommand` |
| 8 | Host-side git hardening: hooks disabled (`core.hooksPath=/dev/null`), `core.fsmonitor=false`, `--no-ext-diff --no-textconv`, no commit signing. Before any host git runs in a worktree (run, resume, `task verify`, `task diff`, checkpoints) the control plane verifies the `.git` pointer, that the agent-writable admin dir's `commondir` still points at the real (read-only) common dir, that there is no `config.worktree`, and that `HEAD` is the task branch. A redirected `commondir` would otherwise let host `git add` run an agent-defined filter (reproduced in `TestAdminDirTamperingDetected`). | `gitops.CheckTaskWorktree` |
| 9 | Commits happen host-side on `agent/<task-id>` only. `CommitAll` refuses other branches. There are no pushes. | `gitops.CommitAll` |
| 10 | Verification runs in the same image with the worktree mounted and the git dirs read-only. The module cache is mounted read-only, with `GOPROXY=off`. Installed `node_modules` directories of the repository's own checkout (path from the task ledger, never from the worktree) are mounted read-only at the same paths in the worktree, for verification and the agent; they shadow anything the agent put there, and a symlink or file at a mount point fails the stage (or refuses the session). Tool caches inside them (`.cache`, `.vite`, `.vitest`) get a per-run writable tmpfs (512 MiB each, counted against the container memory limit); a missing cache directory is created empty in the checkout, because a mount point cannot be made inside a read-only mount. A JavaScript project that declares a `test`/`lint`/`build` script but has no installed dependencies fails that stage instead of skipping it. The checkout's Python virtual environments (`.venv`/`venv` with a `pyvenv.cfg`), Composer and Bundler `vendor/` directories (with `autoload.php` or `bundle/`; a Go `vendor/` is tracked source and never mounted) and `.bundle/` are mounted the same way; a venv's interpreter is mounted read-only only when it is a self-contained installation outside system directories on a Linux host. The host's Cargo registry, Maven repository and Gradle caches are mounted read-only (never `~/.cargo`, `~/.m2` or `~/.gradle` themselves, which hold credentials; those files are refused as mount sources); tool homes (`CARGO_HOME`, the writable Maven repository in front of the read-only tail, `GRADLE_USER_HOME`), build outputs and copied Gradle wrapper distributions are in a per-task directory, separate for verification and the agent. The agent's sandbox gets the same read-only Go module cache and offline Go settings (`GOPROXY=off`), with a build cache of its own: never verification's, since Go caches test results and an agent-written cache could forge a pass. Verification does not change the candidate: it snapshots the worktree before its stages and afterwards restores tracked files they changed and removes untracked files they created (ignored files are left alone), reporting what it undid as a `side-effects` stage; if it cannot undo them, verification fails. Each task has its own Go build cache. Its config and language presets come from the task's **base commit**, never from the agent-writable worktree; deleting a stage's required file (e.g. `go.mod`) fails the stage. | `verify.LoadConfig`, `verify.Engine.spec`, `sandbox.DependencyMounts`, `sandbox.PackageCaches` |
| 11 | Diff-scope gate: changes touching secret paths, protected paths (`.boundedcode/`, CI workflows, CODEOWNERS, `.gitmodules`), or more than N files fail verification. gitleaks scans the task diff; without gitleaks the full gate fails. | `verify`, `policy.IsProtectedPath` |
| 14 | Host-side reads of agent-written files (context packs, ADRs, cross-service scans) never follow symlinks out of the worktree and never read secret paths; packs are redacted before they reach the model or a frontier packet, and diffs omit secret paths. codebase-memory-mcp and ripgrep do not follow symlinks (verified 2026-10-04). | `contextplan.ReadConfined`, `xservice.Scan` |
| 15 | More secret paths than can be masked (500) refuses to start the agent instead of masking only some. | `policy.FindSecretPaths` |
| 16 | Containers are named and removed on cancellation or timeout (killing `docker run` alone leaves the container running); a stale adapter container is removed before reuse. | `sandbox.Container.Command` |
| 12 | Frontier: packets are redacted, and host paths are rewritten: the work dir, worktrees and checkouts to workspace-relative names, task state, BoundedCode's directories and the Go caches to placeholders, and anything else under the home directory to `$HOME` (plain, JSON-escaped and URL-encoded spellings, symlink-resolved forms, at path boundaries only). A packet that still contains the home directory is not sent: it is kept on the host (0600) and recorded as a `blocked` escalation, which `stats` reports. `codex exec` runs **in a container** with only an empty workdir and the Codex credential dir mounted. Codex's own `read-only` sandbox restricts writes, not reads, so containment is required. API-key auth is refused. | `frontier.Codex`, `frontier.Sanitize`, `frontier.CheckPacket` |
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
| redirect the admin dir's `commondir` to a gitdir with a clean filter; `HEAD` to another branch; add `config.worktree` | rejected; task blocks; filter never runs (`TestAdminDirTamperingDetected`, `TestAdminDirTamperingBlocksTask`) |
| prompt-injected agent rewrites `.boundedcode/verification.yaml`, plants `leak.go -> ~/.ssh/id_ed25519` and a failing test naming `leak.go:2` | config ignored (base commit), attempt rejected for the protected path, key never in a pack (`TestPromptInjectedAgentIsContained`) |
| `git -C . push`, `kubectl --context=prod apply`, `terraform -chdir=x apply`, `rm -r -f /` in a verification stage | denied (`TestCheckCommandBypasses`) |
| cancel a long verification stage | container removed (`TestContainerCancelRemovesContainer`) |
| containerized codex: list host repos / `$HOME` | not visible (only the empty workdir) |

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
   unrestricted within it. The worktree's `package.json` is agent-writable,
   so its `test` script is too: as with editable test files, the stages
   catch mistakes, not an adversarial agent. Only checks the agent cannot
   edit catch that: human review of the merge candidate in normal use, or a
   benchmark's hidden acceptance tests in validation runs.
4. **Secret detection is pattern-based.** A secret stored under an
   innocuous name (for example `config/prod.yaml`) is not masked. gitleaks
   on the diff catches *new* secrets only. Mitigation: keep production
   secrets out of development checkouts (see SECURITY.md). Source-code
   files (`.go`, `.ts`, `.py`, ...) are not secret by name alone: before
   2026-10-04, grpc-go's `credentials/` package and files such as
   `credentials.go` or `kubeconfig.go` were masked and rejected by the diff
   scope, so tasks there could not be done (found by the ADR-0008 benchmark
   and the small real-world validation). A secret-named directory that holds
   source (such as `credentials/`) is walked file by file: its code is
   visible, its non-code files stay masked. `secrets/` directories and dot
   directories (`.ssh`, `.aws`, ...) stay masked whole, code included.
   Literal secrets inside other code are left to gitleaks on the diff.
5. **Persistence directory.** The OpenHands conversation store is writable
   by the agent. It holds data, not executables, and the control plane
   treats it as untrusted. The task ledger in SQLite is the source of truth
   and is not mounted.
6. **Resource exhaustion** within limits, such as filling the worktree's
   disk. This is bounded by disk quota only.
7. **`sandbox.kind: none`** disables all of the above. It is refused for
   autonomous tasks unless `--unsafe-no-sandbox` is passed.
8. **The frontier container has network access** (it must reach OpenAI) and
   holds the Codex credentials. Only the packet and an empty workdir are
   inside it.
9. **Host-side tools read agent-written worktrees.** codebase-memory-mcp
   (worktree indexing for impact), Serena and its language servers
   (ADR-0008; `gopls` runs `go list`), ripgrep and gitleaks run on the
   host, outside the container, against files the agent wrote. They do not
   execute repository code (toolchain downloads and network are disabled
   for Serena's language servers) and do not follow symlinks, but a parser
   bug in one of them would be reachable by the agent.
10. **Prompt injection steering the work itself.** A malicious repository
   can make the agent write wrong code. The defence is deterministic
   verification plus human review of the merge candidate. Nothing merges
   automatically.
11. **A cloud model provider receives repository content.** With
   `inference.provider` set to a cloud API, the gateway sends the agent's
   conversation (context packs, file contents, tool output) to that provider.
   Secret masking and redaction apply as with a local model; repository code
   does not stay on the machine. The provider's API key is held by the host
   gateway only (ADR-0010).