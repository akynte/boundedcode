# Scorecard: verify suite

1 trial(s) (verify-20261029T223649Z). Model – (sha256 –), server –; build –; task set –; –.

| | Cases | Correct changes | Accepted | False acceptance (rate) | False rejection (rate) | Precision | Recall |
|---|---|---|---|---|---|---|---|
| **all** | 71 | 23 | 39 | 16 (33%) | 0 (0%) | 0.59 | 1.00 |
| split: dev | 36 | 15 | 22 | 7 (33%) | 0 (0%) | 0.68 | 1.00 |
| split: heldout | 35 | 8 | 17 | 9 (33%) | 0 (0%) | 0.47 | 1.00 |
| group: verification | 71 | 23 | 39 | 16 (33%) | 0 (0%) | 0.59 | 1.00 |
| language: c | 7 | 2 | 5 | 3 (60%) | 0 (0%) | 0.40 | 1.00 |
| language: cpp | 7 | 2 | 5 | 3 (60%) | 0 (0%) | 0.40 | 1.00 |
| language: go | 9 | 3 | 5 | 2 (33%) | 0 (0%) | 0.60 | 1.00 |
| language: java | 6 | 2 | 3 | 1 (25%) | 0 (0%) | 0.67 | 1.00 |
| language: javascript | 11 | 4 | 6 | 2 (29%) | 0 (0%) | 0.67 | 1.00 |
| language: php | 7 | 2 | 4 | 2 (40%) | 0 (0%) | 0.50 | 1.00 |
| language: python | 8 | 3 | 4 | 1 (20%) | 0 (0%) | 0.75 | 1.00 |
| language: ruby | 7 | 2 | 3 | 1 (20%) | 0 (0%) | 0.67 | 1.00 |
| language: rust | 6 | 2 | 3 | 1 (25%) | 0 (0%) | 0.67 | 1.00 |
| language: typescript | 6 | 2 | 3 | 1 (25%) | 0 (0%) | 0.67 | 1.00 |

Limitations:
- verification alone: the changes are fixed in advance and no model ran
- one machine; the confidence intervals treat repeated runs of a task as independent
