# Release Process

**Nothing in this process is run without explicit maintainer approval.**
Publishing the repository, tags, releases or packages is external and
irreversible.

## Checklist

1. `make check` is green, and CI is green on the release commit.
2. Re-verify upstream pins and licenses (`docs/licensing/policy.md`). Update
   the matrix, `THIRD_PARTY_NOTICES.md` and `LICENSES/`, then run
   `go run ./scripts/licensecheck -check-notices
   THIRD_PARTY_NOTICES.md,docs/licensing/upstream-license-matrix.md` and
   `scripts/pylicensecheck.py` in both Python environments (see the policy).
3. Re-run benchmarks on the reference machine, idle and on AC power:
   * `boundedcode bench infra …`
   * `boundedcode bench tasks`

   Commit the reports to `benchmarks/reports/`, and update README numbers
   only from those reports.
4. Update `docs/development/status.md` and the release notes in
   `CHANGELOG.md`.
5. Write `docs/releases/vX.Y.Z.md` (the release notes; relative links are
   fine, the release workflow points them at the tag) and check the build
   locally from a clean tree:
   ```bash
   VERSION=vX.Y.Z make dist   # binaries + SBOMs + licenses archive + checksums
   ```
   The version comes from `git describe` when `VERSION` is unset; a
   `-dirty` suffix means the tree has uncommitted changes and must not be
   released. Contents of `dist/`:
   * `boundedcode-{linux,darwin}-{amd64,arm64}` and
     `boundedcode-windows-{amd64,arm64}.exe`: static binaries (CGO
     disabled); `install.sh` and `install.ps1` download these names
   * `SBOM-<os>-<arch>.spdx.json`: one SBOM per binary (the linked modules
     differ per OS)
   * `boundedcode-VERSION-licenses.tar.gz`: `LICENSE`, `NOTICE`,
     `THIRD_PARTY_NOTICES.md` and `LICENSES/` (Go modules of every release
     target and upstream components), with neutral ownership and the
     commit time, so it is reproducible
   * `SHA256SUMS`: checksums of the files above; the installers verify the
     binary against it
   * `LICENSE`, `NOTICE`, `THIRD_PARTY_NOTICES.md`, `LICENSES/` (unpacked
     copies, not uploaded)

   The release itself is built in CI, not from this local `dist/` (step 8).
6. Secret-scan the full history: `gitleaks git --redact .`
7. Check DCO sign-off on every commit, including the root commit:
   `scripts/check-dco.sh --root HEAD`
8. **Maintainer:** tag the commit on `main` and push the tag:
   ```bash
   git tag -a vX.Y.Z -m "BoundedCode vX.Y.Z" && git push origin vX.Y.Z
   ```
   The `release` workflow (`.github/workflows/release.yml`) then:
   * checks that the tag is on `main` and has release notes;
   * runs the tests and `make dist`, and fails if the build changes the tree;
   * attests every file (SLSA build provenance, signed through Sigstore);
   * creates a **draft** release with the six binaries, the six SBOMs, the
     licenses archive and `SHA256SUMS`, marked pre-release when the version
     has a suffix.
9. **Maintainer:** review the draft (assets, notes, the workflow run) and
   publish it. Nothing is public until then. Optionally confirm that the
   CI build matches your local one: `diff dist/SHA256SUMS <(gh release
   download vX.Y.Z -p SHA256SUMS -O -)`.

## Verifying a release

Anyone can check a downloaded binary, and rebuild a release to compare:

```bash
# Checksum of a download (the installers do this automatically):
sha256sum -c --ignore-missing SHA256SUMS

# Build provenance (releases built by the release workflow, from
# v0.1.0-alpha.5): proves the file was built by this repository's workflow
# from the tagged commit.
gh attestation verify boundedcode-linux-amd64 -R akynte/boundedcode

# Rebuild from the tag with the Go version in that tag's go.mod:
git clone https://github.com/akynte/boundedcode.git && cd boundedcode
git checkout vX.Y.Z
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) VERSION=vX.Y.Z make dist
cd dist && sha256sum -c /path/to/downloaded/SHA256SUMS
```

`make dist` builds with `-trimpath` and without cgo. It packs the licenses
archive with the commit time and neutral ownership. Since this change, it
also gives the SBOMs the commit time (`SOURCE_DATE_EPOCH`).

**Measured on 2026-10-10.** v0.1.0-alpha.4 was rebuilt from its tag with
Go 1.27.1:
- All six binaries and the licenses archive matched the published
  checksums.
- The six SBOMs did not. They differed only in their creation time and
  document namespace, which the generator then took from the clock. Later
  releases take both from the commit, so they reproduce too.

**Limits.**
- `SHA256SUMS` is published in the same release as the binaries, so it
  detects corrupted or altered downloads, but not a release replaced by
  someone with access to the repository.
- Releases up to v0.1.0-alpha.4 were built locally and have no attestation.
  Later releases are built and attested by the release workflow; an
  attestation shows which workflow and commit built a file, so a release
  replaced by hand fails `gh attestation verify`.
- The SBOMs list the modules compiled into each binary. Pinned external
  components (llama.cpp, the OpenHands SDK, codebase-memory-mcp, gitleaks,
  Serena) are listed as runtime or optional dependencies, not as contents
  of the binary.

## Versioning

Semantic versioning. Releases before 1.0 may break the CLI and config. Each
breaking change is called out in `CHANGELOG.md`.
