# Portfolio version of the case study

These files adapt the [engineering case study](../engineering-case-study.md)
to the content model of the portfolio site, aliakbari.dev (repository
`akynte/portfolio`). That repository was **not** modified. Adopting these
files there is a separate change.

| File | For | Target in the portfolio repository |
|---|---|---|
| [boundedcode.md](boundedcode.md) | The case-study page (`work-v1` front matter; Markdown with the site's four directives) | `content/work/boundedcode.md` (replaces the alpha.3 version) |
| [proposed-evidence.yaml](proposed-evidence.yaml) | Three new evidence items: the comparative evaluation, the nested-repository escape fix, release provenance | append the items to `content/sources/boundedcode/evidence.yaml` |

## How to adopt

1. **Move the source lock.** Move `content/sources.lock.json` from
   `v0.1.0-alpha.3` (`be1aa9f`) to `v0.1.0-alpha.5`
   (`5e1bea487b3352aeb381b9616fa89080e5a12e02`). The `ci` workflow passed
   on that commit.
2. **Replace the page and append the evidence.** Replace
   `content/work/boundedcode.md`, and append the proposed evidence items.
3. **Update the environment details.** In the existing evidence items,
   update `environment_details.go` from `1.27.1` to `1.27.2`; alpha.5
   builds with Go 1.27.2.
4. **Optionally, cite the new evidence.**
   - The page's audit section could cite
     `boundedcode-nested-repository-escape-fix` instead of
     `boundedcode-task-isolation`.
   - `content/resume.yaml` could add bullets citing the new items (see
     "Résumé bullets" in the case study).
5. **Validate.** Run the site's checks: `npm run evidence:validate` and
   `npm run evidence:sync`.

## Checks done here (2026-10-10)

- **The page.** Its front matter validates against the portfolio's
  `schemas/work-v1.json`. It uses only:
  - the existing evidence IDs;
  - the `boundedcode-loop` diagram;
  - the allowed callout kinds;
  - no raw HTML.
- **The proposed evidence.** It validates against `schemas/evidence-v1.json`.
  Every implementation, test, benchmark and document path, and every test
  function name, exists at `v0.1.0-alpha.5`.
- **The existing evidence.** All eight existing BoundedCode evidence items
  still resolve at `v0.1.0-alpha.5`, so moving the lock does not break
  them.
- **Pinned links.** Every pinned GitHub link in both documents resolves to
  a file at the tag, and every `#L` anchor is within its file.

**Not done here:**
- The portfolio's own `evidence:validate` and `evidence:sync` were not
  run, because they write into the portfolio repository. They also check
  the CI conclusion for the pinned commit.
- The page was not rendered.
