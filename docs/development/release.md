# Release Process

**Nothing in this process is run without explicit maintainer approval.**
Publishing the repository, tags, releases or packages is external and
irreversible.

## Checklist

1. `make check` is green, and CI is green on the release commit.
2. Re-verify upstream pins and licenses (`docs/licensing/policy.md`). Update
   the matrix, `THIRD_PARTY_NOTICES.md` and `LICENSES/`.
3. Re-run benchmarks on the reference machine, idle and on AC power:
   * `boundedcode bench infra …`
   * `boundedcode bench tasks`

   Commit the reports to `benchmarks/reports/`, and update README numbers
   only from those reports.
4. Update `docs/development/status.md` and the release notes in
   `CHANGELOG.md`.
5. Build artifacts:
   ```bash
   VERSION=v0.1.0 make dist   # bin + checksums + SBOM + license files
   ```
   Contents of `dist/`:
   * `boundedcode-linux-amd64`
   * `SHA256SUMS`
   * `SBOM.spdx.json`
   * `LICENSE`
   * `NOTICE`
   * `THIRD_PARTY_NOTICES.md`
   * `LICENSES/`
6. Secret-scan the full history: `gitleaks git --redact .`
7. Check DCO sign-off on every commit: `scripts/check-dco.sh <root> HEAD`
8. **Maintainer:** choose the public name (see `renaming.md`), set the
   security contact in `SECURITY.md` and `CODE_OF_CONDUCT.md`, create the
   GitHub repository, push, tag and draft the release.

## Versioning

Semantic versioning. Releases before 1.0 may break the CLI and config. Each
breaking change is called out in `CHANGELOG.md`.
