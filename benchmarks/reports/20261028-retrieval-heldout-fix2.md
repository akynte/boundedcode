# Retrieval benchmark (2026-10-28)

177 instances, block budget 1200 tokens, 284 s. Gold: the existing code files each fix changed. Recall and precision are per instance, then averaged.

## Held-out split (177 instances)

| Arm | File recall | File precision | Gold first | Gold in top 3 | Any gold | Nothing found | Symbols right / listed | Tokens | p50 ms | p95 ms |
|---|---|---|---|---|---|---|---|---|---|---|
| basic | 23% | 9% | 12% | 19% | 24% | 30% | 14 / 105 | 123 | 273 | 3001 |
| adaptive | 51% | 19% | 35% | 49% | 55% | 0% | 29 / 438 | 188 | 32 | 170 |
| basic+tests | 22% | 8% | 12% | 18% | 23% | 18% | 14 / 133 | 134 | 338 | 3001 |
| adaptive+tests | 55% | 18% | 38% | 52% | 59% | 0% | 33 / 494 | 199 | 38 | 167 |

By category (file recall / precision):

| category | n | basic | adaptive | basic+tests | adaptive+tests |
|---|---|---|---|---|---|
| located-error | 15 | 7% / 1% | 33% / 13% | 7% / 1% | 33% / 10% |
| names | 115 | 29% / 12% | 55% / 23% | 28% / 10% | 60% / 21% |
| prose | 26 | 0% / 0% | 31% / 8% | 0% / 0% | 35% / 10% |
| trace | 21 | 29% / 8% | 62% / 16% | 29% / 10% | 67% / 17% |

By language (file recall / precision):

| language | n | basic | adaptive | basic+tests | adaptive+tests |
|---|---|---|---|---|---|
| c | 17 | 29% / 12% | 53% / 25% | 29% / 12% | 53% / 25% |
| cpp | 1 | 0% / 0% | 100% / 20% | 0% / 0% | 100% / 20% |
| go | 17 | 35% / 15% | 50% / 29% | 35% / 15% | 56% / 21% |
| java | 19 | 32% / 7% | 63% / 17% | 32% / 6% | 65% / 16% |
| javascript | 9 | 22% / 14% | 43% / 23% | 22% / 14% | 43% / 23% |
| php | 18 | 39% / 16% | 57% / 33% | 39% / 16% | 57% / 33% |
| python | 44 | 18% / 6% | 57% / 18% | 16% / 4% | 73% / 18% |
| ruby | 16 | 25% / 7% | 64% / 15% | 25% / 7% | 64% / 15% |
| rust | 24 | 6% / 6% | 24% / 6% | 6% / 6% | 20% / 6% |
| typescript | 12 | 8% / 1% | 35% / 9% | 8% / 1% | 35% / 9% |

By repository (file recall / precision):

| repository | n | basic | adaptive | basic+tests | adaptive+tests |
|---|---|---|---|---|---|
| multi-language | 42 | 14% / 6% | 46% / 16% | 12% / 5% | 53% / 17% |
| single-language | 135 | 26% / 9% | 52% / 20% | 26% / 9% | 56% / 18% |

Paired with basic (per instance; ties dropped; two-sided sign test):

| Arm | Recall better / worse | p | Precision better / worse | p |
|---|---|---|---|---|
| adaptive vs basic | 58 / 1 | 0.000 | 76 / 13 | 0.000 |
| adaptive+tests vs basic+tests | 66 / 1 | 0.000 | 82 / 13 | 0.000 |

Skipped (1): gohugoio__hugo-12204: the fix changes no existing code file
