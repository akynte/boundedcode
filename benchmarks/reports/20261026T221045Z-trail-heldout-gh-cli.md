# Trail study

For each commit, files suggested from the commits before it are compared with the files it changed outside tests (that existed before it). Recall is over every commit's changed files; precision over the files suggested.

## At request time

| Repository | Commits | Named: recall | Named: precision | Named + 5 co-changed: recall | Co-changed files that were changed | 10 most changed: recall | precision |
|---|---|---|---|---|---|---|---|
| gh-cli | 200 | 10% | 45% | 14% | 7% | 7% | 2% |

## At edit time

After each changed file, up to 3 files changed with it in at least *support* earlier commits and in at least *confidence* of the commits that changed it.

| Repository | Support | Confidence | Suggestions | Right | Multi-file commits with a right note | Single-file commits with a note |
|---|---|---|---|---|---|---|
| gh-cli | 2 | 0.3 | 768 | 50% | 61% (of 88) | 72% |
| gh-cli | 3 | 0.5 | 354 | 64% | 48% (of 88) | 30% |
| gh-cli | 5 | 0.7 | 124 | 81% | 24% (of 88) | 7% |

