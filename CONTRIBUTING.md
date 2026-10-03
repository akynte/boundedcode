# Contributing

Thanks for your interest. The project is pre-release, so expect interfaces
to change.

## Ground rules

* **Developer Certificate of Origin.** Every commit must be signed off
  (`git commit -s`), which certifies the [DCO](DCO). CI rejects commits
  without a `Signed-off-by:` trailer that matches the author. There is no CLA.
* **License.** Contributions are accepted under [Apache-2.0](LICENSE).
* **No copied upstream code** without prior discussion. If code is derived
  from another project, it needs a provenance header (see
  [docs/licensing/policy.md](docs/licensing/policy.md)) and an entry in
  [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). GPL, AGPL, SSPL and
  source-available code are not accepted in the core.
* **No model weights, credentials or proprietary source** in commits,
  fixtures or test logs.
* **Evidence over claims.** Performance or quality claims in docs need a
  reproducible benchmark under `benchmarks/`.

## Development

Requirements: Go (see `go.mod`), Git, and optionally Docker, uv and
llama.cpp for the integration paths. See
[docs/development/guide.md](docs/development/guide.md).

```bash
make check     # gofmt, go vet, go test, go test -race, golangci-lint if installed
make build     # ./bin/boundedcode
```

Integration tests that need a GPU, a model or Docker are behind build tags
or environment variables and are skipped by default.

## Pull requests

* Keep changes small and focused, and include tests.
* Update docs and the status markers (*implemented*, *experimental*,
  *planned*) when behavior changes.
* Architectural changes need an ADR in `docs/architecture/adr/`.
