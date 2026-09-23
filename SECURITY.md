# Security policy

## Report privately

Use this repository's GitHub **Security → Report a vulnerability** channel.
Do not publish an exploitable reproduction in a public issue. If the private
reporting option is unavailable, ask the maintainer for a private reporting
channel without disclosing the vulnerability.

Include the affected commit/version, `bcode doctor` output, deployment shape,
kernel, mounts/network settings, and a minimal reproduction with secrets removed.
Pre-1.0 support focuses on the latest revision/release; no long-term support
window is promised.

## Scope

Please report path/symlink escapes, unauthorized tool actions, foreign-workspace
state returned by scoped APIs, bypasses of candidate/gate checks, credential
leaks, unexpected configured-offline service calls, or escapes from the sandbox
actually configured for the run.

The current boundaries and their limits are documented in
[trust boundaries](docs/explanation/trust-boundaries.md) and
[isolation](docs/explanation/isolation-model.md). In particular:

- A host install does not have an outer container boundary.
- Landlock TCP rules do not contain every network protocol or filter HTTP routes.
  The shipped Docker network is not deny-all.
- `offline: true` is not an operating-system firewall.
- Verification runs executable repository code in a writable task worktree,
  not an immutable verification VM.
- Native tasks use separate worktrees; editor/MCP edits can affect the opened
  checkout. Setup alone does not sandbox an editor.
- Tests, model review and prompt-injection fences are not correctness proofs.
- The API and local inference server must remain private; the API has no auth.
- Workspace scoping is not tenant isolation against hostile same-user processes.

A weakness beyond these stated limits still merits discussion; documenting a
limit is not a reason to ignore a practical security improvement.

## Supply chain

The release workflow is configured to generate SBOMs, provenance and signatures.
Check that the specific release actually carries the expected artifacts before
relying on them. [Release verification](docs/how-to/verify-a-release.md) describes
the process. Model weights and the Prism llama.cpp fork are separate dependencies;
the [reference install](docs/how-to/install.md) pins their identity.
