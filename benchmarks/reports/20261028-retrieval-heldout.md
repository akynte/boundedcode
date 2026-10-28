# Retrieval benchmark (2026-10-28)

177 instances, block budget 1200 tokens, 285 s. Gold: the existing code files each fix changed. Recall and precision are per instance, then averaged.

## Held-out split (177 instances)

| Arm | File recall | File precision | Gold first | Gold in top 3 | Any gold | Nothing found | Symbols right / listed | Tokens | p50 ms | p95 ms |
|---|---|---|---|---|---|---|---|---|---|---|
| basic | 23% | 9% | 12% | 19% | 24% | 30% | 12 / 95 | 123 | 273 | 3001 |
| adaptive | 50% | 21% | 34% | 47% | 55% | 0% | 30 / 429 | 186 | 32 | 167 |
| basic+tests | 22% | 8% | 12% | 18% | 23% | 18% | 12 / 125 | 133 | 335 | 3001 |
| adaptive+tests | 54% | 19% | 38% | 51% | 58% | 0% | 34 / 488 | 195 | 38 | 169 |

By category (file recall / precision):

| category | n | basic | adaptive | basic+tests | adaptive+tests |
|---|---|---|---|---|---|
| located-error | 15 | 7% / 1% | 33% / 17% | 7% / 1% | 33% / 13% |
| names | 113 | 30% / 12% | 54% / 25% | 29% / 11% | 59% / 22% |
| prose | 28 | 0% / 0% | 35% / 8% | 0% / 0% | 38% / 13% |
| trace | 21 | 29% / 8% | 62% / 18% | 29% / 11% | 67% / 18% |

By language (file recall / precision):

| language | n | basic | adaptive | basic+tests | adaptive+tests |
|---|---|---|---|---|---|
| c | 17 | 29% / 12% | 54% / 25% | 29% / 12% | 54% / 25% |
| cpp | 1 | 0% / 0% | 100% / 22% | 0% / 0% | 100% / 22% |
| go | 17 | 35% / 15% | 50% / 29% | 35% / 15% | 56% / 20% |
| java | 19 | 32% / 7% | 63% / 22% | 32% / 7% | 65% / 19% |
| javascript | 9 | 22% / 14% | 46% / 31% | 22% / 14% | 46% / 31% |
| php | 18 | 39% / 16% | 51% / 27% | 39% / 16% | 51% / 31% |
| python | 44 | 18% / 6% | 57% / 22% | 16% / 4% | 73% / 20% |
| ruby | 16 | 25% / 7% | 64% / 20% | 25% / 7% | 64% / 20% |
| rust | 24 | 6% / 6% | 24% / 6% | 6% / 6% | 20% / 7% |
| typescript | 12 | 8% / 1% | 35% / 9% | 8% / 1% | 33% / 8% |

Paired with basic (per instance; ties dropped; two-sided sign test):

| Arm | Recall better / worse | p | Precision better / worse | p |
|---|---|---|---|---|
| adaptive vs basic | 57 / 1 | 0.000 | 79 / 13 | 0.000 |
| adaptive+tests vs basic+tests | 64 / 1 | 0.000 | 82 / 13 | 0.000 |

Skipped (1): gohugoio__hugo-12204: the fix changes no existing code file
