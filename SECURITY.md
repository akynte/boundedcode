# Security Policy

## Supported versions

BoundedCode is a public alpha. Security fixes go to `main` and the latest
alpha release; older alpha releases are not patched.

## Reporting a vulnerability

Please do **not** open a public issue, pull request or discussion for a
security problem.

* **Preferred:** GitHub private vulnerability reporting. Open the
  [Security tab](https://github.com/akynte/boundedcode/security) and choose
  **Report a vulnerability**
  ([direct link](https://github.com/akynte/boundedcode/security/advisories/new)).
* **Alternative** (no GitHub account): email **ali@aliakbari.dev** with
  "BoundedCode security" in the subject.

Please include the affected version or commit, reproduction steps, impact,
and any suggested fix. Do not include real credentials or other people's
data.

What to expect:

* acknowledgement within **7 days**;
* an initial assessment, and a fix plan or a reasoned decline, within
  **30 days**;
* coordinated disclosure: we publish a GitHub security advisory once a fix
  is available, credit you if you wish, and ask that you not disclose
  publicly before then or 90 days after your report, whichever comes
  first.

## Security model

BoundedCode runs an LLM agent that executes commands and edits code. We
assume the model can be wrong or adversarially steered (for example by prompt
injection in repository content). The design limits what a misbehaving agent
can reach:

| Control | Mechanism | Status |
|---|---|---|
| Execution sandbox | Agent tools run in a container. Only the task worktree is mounted read-write, and there is no host `$HOME`, SSH agent or credentials. | see [docs/design/sandbox.md](docs/design/sandbox.md) |
| Network | The container runs with `--network none`. LLM traffic is tunnelled through the control plane over stdio. | see sandbox doc |
| Secret files | `.env*`, `secrets/`, key material, cloud credentials and kubeconfigs are masked inside the sandbox and denied by path policy. | see sandbox doc |
| Dangerous commands | A deterministic policy in Go (`internal/policy`) checks verification commands. It blocks push, force-push, hard resets, protected-branch merges, `terraform apply/destroy`, mutating `kubectl`/`helm`/cloud CLI calls (any context), publishing and `curl \| sh`, after normalizing global flags and `sh -c` bodies. The agent's own commands run inside the network-less container and are not filtered. | [sandbox doc](docs/design/sandbox.md) control 7 |
| Verification integrity | Verification config and presets are read from the task's base commit, so the agent cannot weaken its own gate. Changes to `.boundedcode/`, CI workflows or CODEOWNERS fail verification. | sandbox doc controls 10–11 |
| Git | Work happens on `agent/<task-id>` worktrees. Nothing is pushed automatically. Before host git touches a worktree, its `.git` pointer, admin dir and `HEAD` are verified. | sandbox doc control 8 |
| Host-side reads | Context packs never follow symlinks out of a worktree or read secret paths, and are redacted. | sandbox doc control 14 |
| Host-side tools | codebase-memory-mcp, Serena (optional) with its language servers, ripgrep and gitleaks run on the host against agent-written files. They do not execute repository code or follow symlinks. | sandbox doc residual risk 9 |
| Model provider API keys | Stored in the OS credential store, or an owner-only file where none exists; never in `config.yaml`, on command lines, in logs or packets (exact-value redaction). Only the host-side gateway adds a key to a request; the agent sandbox keeps `--network none` and sees only the stdio tunnel. Vendor environment variables (`ANTHROPIC_API_KEY`, ...) are not read. With a cloud provider, the agent's conversation, including repository content, is sent to that provider. | [ADR-0010](docs/architecture/adr/0010-cloud-model-providers.md) |
| Frontier credentials | Codex/ChatGPT credentials stay on the host and are never mounted into, or exported to, agent environments. Contained `codex exec` runs in its own container that holds the Codex credential directory and has network access (it must reach OpenAI), with only the packet and an empty workdir inside. API keys are refused. | [ADR-0009](docs/architecture/adr/0009-frontier-escalation.md) |
| Secret scanning | gitleaks scans task diffs; the full verification gate fails if it is not installed. | |
| Logging | Audit events, adapter logs and context packs are redacted. Full source and prompts are not logged by default. | |

Residual risks are listed in [docs/design/sandbox.md](docs/design/sandbox.md).
Agent-level permission prompts are **not** treated as a security boundary.

## Production credentials

Never run boundedcode in an environment that holds production cloud
credentials, production kubeconfigs or deploy keys. The tool does not need
them, and verification stages that would use them (`terraform plan`
against real backends, for example) must be configured explicitly.

## Supply chain

* Upstream components are pinned by version, tag or commit (llama.cpp's
  build script verifies the commit; codebase-memory-mcp and gitleaks are
  checked by sha256; Serena by version, wheel hash and LICENSE hash; Codex
  is installed by the user and only its version is recorded), and their
  licenses are verified per version
  ([docs/licensing/upstream-license-matrix.md](docs/licensing/upstream-license-matrix.md)).
* Go dependencies are pinned in `go.sum`, Python environments in `uv.lock`
  files used with `--frozen`. CI runs a pinned `govulncheck` and license
  checks for Go and both Python environments. There is no Python
  vulnerability scan yet.
* Model weights are never redistributed. Users download them from the
  original publisher.
