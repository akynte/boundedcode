# Trail study

For each commit, files suggested from the commits before it are compared with the files it changed outside tests (that existed before it). Recall is over every commit's changed files; precision over the files suggested.

## At request time

| Repository | Commits | Named: recall | Named: precision | Named + 5 co-changed: recall | Co-changed files that were changed | 10 most changed: recall | precision |
|---|---|---|---|---|---|---|---|
| cobra | 200 | 9% | 83% | 13% | 7% | 50% | 9% |

## At edit time

After each changed file, up to 3 files changed with it in at least *support* earlier commits and in at least *confidence* of the commits that changed it.

| Repository | Support | Confidence | Suggestions | Right | Multi-file commits with a right note | Single-file commits with a note |
|---|---|---|---|---|---|---|
| cobra | 2 | 0.3 | 361 | 36% | 58% (of 66) | 45% |
| cobra | 3 | 0.5 | 108 | 49% | 26% (of 66) | 17% |
| cobra | 5 | 0.7 | 20 | 70% | 12% (of 66) | 2% |

