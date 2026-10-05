# Contributing

Thanks for your interest. BoundedCode is a public alpha: interfaces and
configuration may change, and reports from real machines and repositories
are among the most useful contributions.

## Ground rules

* **Developer Certificate of Origin.** Every commit must be signed off
  (`git commit -s`), which certifies the [DCO](DCO). CI rejects commits
  without a `Signed-off-by:` trailer that matches the author. There is no CLA.
* **License.** Contributions are accepted under [Apache-2.0](LICENSE).
* **No copied upstream code** without prior discussion. Code derived from
  another project needs:
  - a provenance header (see [docs/licensing/policy.md](docs/licensing/policy.md));
  - an entry in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

  GPL, AGPL, SSPL and source-available code are not accepted in the core.
  Upstream tools (llama.cpp, the OpenHands SDK, codebase-memory-mcp, Serena)
  are integrated as separate processes or libraries, not vendored.
* **No model weights, credentials or proprietary source** in commits,
  fixtures or test logs.
* **Evidence over claims.** Performance or quality claims in docs need a
  reproducible benchmark under `benchmarks/`.

## Development setup

Requirements:
- Go (see `go.mod`) and Git;
- for the integration paths: Docker, `uv` and llama.cpp.

See [docs/development/guide.md](docs/development/guide.md) and
[docs/usage/getting-started.md](docs/usage/getting-started.md).

```bash
make build     # ./bin/boundedcode
make check     # gofmt, go vet, go test, go test -race, golangci-lint, LICENSES/go
```

The same gates as CI:

```bash
cd adapters/openhands/python && uv run --frozen pytest -q            # Python adapter
go run ./scripts/serenaguard                                         # Serena stays on MIT v1.7.0
go run ./scripts/licensecheck -check-notices THIRD_PARTY_NOTICES.md,docs/licensing/upstream-license-matrix.md
scripts/check-dco.sh origin/main HEAD
gitleaks git --redact --no-banner .
```

Sandbox and security tests run against the real container image when
`BC_TEST_DOCKER_IMAGE` is set (build it with `boundedcode sandbox build`):

```bash
BC_TEST_DOCKER_IMAGE=boundedcode-openhands:local go test -count=1 ./...
```

Tests that need a GPU, a model, a live Codex account or experimental
infrastructure are skipped by default (environment variables or build tags).

## Security-sensitive changes

Changes to any of the following need tests that try to break them:
- the sandbox, masks and mounts (`internal/sandbox`);
- command and path policy (`internal/policy`);
- git worktree handling;
- verification integrity (`internal/verify`);
- frontier packet sanitization (`internal/frontier`).

Add a case to the adversarial suites and describe the threat in the PR. Never
weaken a test or an exclusion to make CI green. Report vulnerabilities
privately ([SECURITY.md](SECURITY.md)), not in issues or PRs.

## Adding an agent adapter

Adapters speak the JSON-RPC protocol in
[ADR-0004](docs/architecture/adr/0004-adapter-protocol.md). See
`adapters/openhands` for the reference implementation and
`internal/agent/scripted` for a test double. An adapter must:
- run inside the sandbox;
- route model calls through the control plane's tunnel;
- support resume and condensation.

## Adding a model profile

Add a YAML file to `configs/models/` with:
- the upstream repository, file and pinned revision (a commit, not a branch);
- the license;
- server settings measured with `boundedcode bench infra`.

Verify the license on the model page and record it in the
[upstream license matrix](docs/licensing/upstream-license-matrix.md). Never
commit weights. Reports of how a model behaves on your hardware are welcome
as issues ("Model compatibility").

## Benchmarks and validation

- **Keep history.** Historical results (including failures) are never
  rewritten or deleted.
- **New run or method → new report.** A new run or a methodology change gets
  a new report directory and document. Say what changed and why.
- **Development is not validation.** Development reruns on tasks used to
  drive fixes must be labelled as development evidence, not as independent
  validation.
- **No unmeasured numbers.** Numeric claims in docs must link to the raw
  report.

## Pull requests

* Keep changes small and focused, and include tests.
* Update docs and the status markers (*implemented*, *experimental*,
  *planned*) when behavior changes.
* Architectural changes need an ADR in `docs/architecture/adr/`.
* New dependencies need a license check and an entry in the notices.
