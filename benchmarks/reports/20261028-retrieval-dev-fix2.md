# Retrieval benchmark (2026-10-28)

90 instances, block budget 1200 tokens, 99 s. Gold: the existing code files each fix changed. Recall and precision are per instance, then averaged.

## Development split (90 instances)

| Arm | File recall | File precision | Gold first | Gold in top 3 | Any gold | Nothing found | Symbols right / listed | Tokens | p50 ms | p95 ms |
|---|---|---|---|---|---|---|---|---|---|---|
| basic | 28% | 12% | 20% | 30% | 32% | 36% | 20 / 110 | 122 | 196 | 1632 |
| adaptive | 52% | 18% | 39% | 51% | 60% | 0% | 26 / 298 | 202 | 16 | 126 |
| basic+tests | 29% | 10% | 20% | 30% | 33% | 23% | 20 / 125 | 135 | 220 | 3000 |
| adaptive+tests | 57% | 16% | 44% | 59% | 64% | 0% | 25 / 356 | 225 | 21 | 180 |

By category (file recall / precision):

| category | n | basic | adaptive | basic+tests | adaptive+tests |
|---|---|---|---|---|---|
| located-error | 8 | 12% / 4% | 50% / 17% | 12% / 4% | 62% / 23% |
| names | 64 | 33% / 14% | 49% / 18% | 34% / 12% | 54% / 15% |
| prose | 10 | 0% / 0% | 40% / 15% | 0% / 0% | 48% / 14% |
| trace | 8 | 38% / 15% | 92% / 23% | 38% / 12% | 92% / 18% |

By language (file recall / precision):

| language | n | basic | adaptive | basic+tests | adaptive+tests |
|---|---|---|---|---|---|
| c | 4 | 25% / 8% | 25% / 25% | 25% / 8% | 25% / 25% |
| cpp | 6 | 0% / 0% | 42% / 20% | 0% / 0% | 58% / 16% |
| go | 11 | 27% / 8% | 50% / 18% | 27% / 8% | 55% / 15% |
| java | 7 | 43% / 19% | 62% / 21% | 43% / 17% | 76% / 16% |
| javascript | 6 | 25% / 7% | 25% / 11% | 25% / 7% | 25% / 11% |
| php | 6 | 42% / 13% | 54% / 14% | 42% / 13% | 54% / 14% |
| python | 22 | 32% / 10% | 64% / 25% | 32% / 7% | 68% / 20% |
| ruby | 11 | 23% / 13% | 45% / 13% | 23% / 6% | 45% / 13% |
| rust | 12 | 36% / 27% | 74% / 16% | 36% / 27% | 71% / 15% |
| typescript | 5 | 0% / 0% | 20% / 3% | 20% / 7% | 47% / 9% |

By repository (file recall / precision):

| repository | n | basic | adaptive | basic+tests | adaptive+tests |
|---|---|---|---|---|---|
| multi-language | 23 | 20% / 6% | 40% / 12% | 24% / 7% | 49% / 11% |
| single-language | 67 | 30% / 14% | 56% / 20% | 30% / 11% | 60% / 17% |

Paired with basic (per instance; ties dropped; two-sided sign test):

| Arm | Recall better / worse | p | Precision better / worse | p |
|---|---|---|---|---|
| adaptive vs basic | 28 / 1 | 0.000 | 36 / 15 | 0.005 |
| adaptive+tests vs basic+tests | 30 / 1 | 0.000 | 40 / 15 | 0.001 |

