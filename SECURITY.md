# Security Policy

## Supported versions

The project is pre-release. Only the latest commit on `main` receives fixes.
Once releases exist, the latest minor release will be supported.

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

boundedcode runs an LLM agent that executes commands and edits code. We
assume the model can be wrong or adversarially steered (for example by prompt
injection in repository content). The design limits what a misbehaving agent
can reach:

| Control | Mechanism | Status |
|---|---|---|
| Execution sandbox | Agent tools run in a container. Only the task worktree is mounted read-write, and there is no host `$HOME`, SSH agent or credentials. | see [docs/design/sandbox.md](docs/design/sandbox.md) |
| Network | The container runs with `--network none`. LLM traffic is tunnelled through the control plane over stdio. | see sandbox doc |
| Secret files | `.env*`, `secrets/`, key material, cloud credentials and kubeconfigs are masked inside the sandbox and denied by path policy. | see sandbox doc |
| Dangerous commands | A deterministic policy runs in Go and is evaluated before verification commands. It blocks push, force-push, protected-branch resets/merges, `terraform apply/destroy` and `kubectl` against non-local contexts. | see policy doc |
| Git | Work happens on `agent/<task-id>` worktrees. Nothing is pushed automatically. | |
| Frontier credentials | Codex/ChatGPT credentials stay on the host. They are never mounted into, or exported to, agent environments. | |
| Secret scanning | gitleaks runs on task diffs as a verification stage when installed. | |
| Logging | Audit events are redacted. Full source and prompts are not logged by default. | |

Residual risks are listed in [docs/design/sandbox.md](docs/design/sandbox.md).
Agent-level permission prompts are **not** treated as a security boundary.

## Production credentials

Never run boundedcode in an environment that holds production cloud
credentials, production kubeconfigs or deploy keys. The tool does not need
them, and verification stages that would use them (`terraform plan`
against real backends, for example) must be configured explicitly.

## Supply chain

* Upstream components are pinned by tag and commit, and their licenses are
  verified per version
  ([docs/licensing/upstream-license-matrix.md](docs/licensing/upstream-license-matrix.md)).
* Go dependencies are pinned in `go.sum`. CI runs `govulncheck` and a
  license check.
* Model weights are never redistributed. Users download them from the
  original publisher.
