# Trail study

For each commit, files suggested from the commits before it are compared with the files it changed outside tests (that existed before it). Recall is over every commit's changed files; precision over the files suggested.

## At request time

| Repository | Commits | Named: recall | Named: precision | Named + 5 co-changed: recall | Co-changed files that were changed | 10 most changed: recall | precision |
|---|---|---|---|---|---|---|---|
| x-tools | 200 | 18% | 46% | 25% | 8% | 11% | 3% |

## At edit time

After each changed file, up to 3 files changed with it in at least *support* earlier commits and in at least *confidence* of the commits that changed it.

| Repository | Support | Confidence | Suggestions | Right | Multi-file commits with a right note | Single-file commits with a note |
|---|---|---|---|---|---|---|
| x-tools | 2 | 0.3 | 621 | 28% | 38% (of 98) | 50% |
| x-tools | 3 | 0.5 | 238 | 44% | 31% (of 98) | 17% |
| x-tools | 5 | 0.7 | 83 | 70% | 17% (of 98) | 5% |

