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
5. Build artifacts from the tagged commit, with a clean tree:
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

   Upload the six binaries, the six SBOMs, the licenses archive and
   `SHA256SUMS`, with `docs/releases/VERSION.md` as the notes (absolute
   links), as a pre-release while the version has a pre-release suffix.
6. Secret-scan the full history: `gitleaks git --redact .`
7. Check DCO sign-off on every commit, including the root commit:
   `scripts/check-dco.sh --root HEAD`
8. **Maintainer:** tag the release and draft the GitHub release from the
   `dist/` artifacts. The name (`renaming.md`), the security contact
   (`SECURITY.md`, `CODE_OF_CONDUCT.md`, private vulnerability reporting)
   and the repository are already set up.

## Versioning

Semantic versioning. Releases before 1.0 may break the CLI and config. Each
breaking change is called out in `CHANGELOG.md`.
