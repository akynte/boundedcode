# Retrieval benchmark (2026-10-28)

90 instances, block budget 1200 tokens, 90 s. Gold: the existing code files each fix changed. Recall and precision are per instance, then averaged.

## Development split (90 instances)

| Arm | File recall | File precision | Gold first | Gold in top 3 | Any gold | Nothing found | Symbols right / listed | Tokens | p50 ms | p95 ms |
|---|---|---|---|---|---|---|---|---|---|---|
| basic | 28% | 12% | 20% | 30% | 32% | 36% | 15 / 96 | 122 | 191 | 1611 |
| adaptive | 51% | 18% | 37% | 50% | 58% | 0% | 22 / 296 | 198 | 17 | 142 |
| adaptive-discovery | 45% | 19% | 34% | 48% | 50% | 12% | 22 / 303 | 161 | 18 | 124 |
| adaptive-modules | 51% | 19% | 34% | 49% | 58% | 0% | 22 / 295 | 195 | 16 | 132 |
| adaptive-docs_config | 51% | 19% | 38% | 51% | 58% | 0% | 22 / 297 | 197 | 16 | 126 |
| adaptive-contracts | 50% | 19% | 37% | 50% | 58% | 0% | 21 / 278 | 194 | 15 | 131 |
| adaptive-lexical | 48% | 18% | 36% | 49% | 56% | 0% | 19 / 257 | 195 | 15 | 135 |
| adaptive-graph | 51% | 18% | 37% | 50% | 58% | 0% | 22 / 296 | 198 | 16 | 138 |
| adaptive-paths | 50% | 17% | 33% | 48% | 58% | 0% | 22 / 294 | 201 | 17 | 131 |
| adaptive-symbols | 42% | 20% | 30% | 39% | 50% | 0% | 8 / 105 | 178 | 13 | 132 |
| adaptive+tests | 56% | 17% | 44% | 60% | 63% | 0% | 21 / 361 | 224 | 20 | 179 |
| adaptive-test_to_impl+tests | 51% | 18% | 36% | 49% | 58% | 0% | 22 / 286 | 195 | 15 | 132 |
| adaptive-stack_trace | 48% | 16% | 33% | 46% | 56% | 0% | 19 / 293 | 197 | 16 | 129 |
| adaptive-build_error | 51% | 18% | 38% | 50% | 58% | 0% | 22 / 299 | 199 | 16 | 131 |

By category (file recall / precision):

| category | n | basic | adaptive | adaptive-discovery | adaptive-modules | adaptive-docs_config | adaptive-contracts | adaptive-lexical | adaptive-graph | adaptive-paths | adaptive-symbols | adaptive+tests | adaptive-test_to_impl+tests | adaptive-stack_trace | adaptive-build_error |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| located-error | 8 | 12% / 4% | 50% / 17% | 50% / 17% | 50% / 17% | 50% / 17% | 50% / 17% | 38% / 7% | 50% / 17% | 50% / 17% | 50% / 18% | 62% / 23% | 50% / 17% | 50% / 17% | 50% / 16% |
| names | 64 | 33% / 14% | 47% / 18% | 44% / 19% | 47% / 19% | 47% / 18% | 47% / 19% | 46% / 18% | 47% / 18% | 46% / 16% | 36% / 20% | 52% / 15% | 47% / 18% | 47% / 18% | 47% / 18% |
| prose | 10 | 0% / 0% | 40% / 10% | 10% / 5% | 40% / 6% | 40% / 15% | 40% / 10% | 40% / 10% | 40% / 10% | 40% / 10% | 40% / 10% | 48% / 12% | 40% / 10% | 40% / 10% | 40% / 10% |
| trace | 8 | 38% / 15% | 92% / 32% | 92% / 32% | 92% / 35% | 92% / 32% | 92% / 32% | 92% / 35% | 92% / 32% | 92% / 32% | 92% / 40% | 92% / 27% | 92% / 32% | 67% / 9% | 92% / 32% |

By language (file recall / precision):

| language | n | basic | adaptive | adaptive-discovery | adaptive-modules | adaptive-docs_config | adaptive-contracts | adaptive-lexical | adaptive-graph | adaptive-paths | adaptive-symbols | adaptive+tests | adaptive-test_to_impl+tests | adaptive-stack_trace | adaptive-build_error |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| c | 4 | 25% / 8% | 25% / 25% | 25% / 25% | 25% / 25% | 25% / 25% | 25% / 25% | 25% / 2% | 25% / 25% | 25% / 25% | 25% / 25% | 25% / 25% | 25% / 25% | 25% / 25% | 25% / 25% |
| cpp | 6 | 0% / 0% | 42% / 12% | 33% / 11% | 42% / 5% | 42% / 20% | 42% / 12% | 42% / 12% | 42% / 12% | 42% / 12% | 42% / 12% | 58% / 13% | 42% / 12% | 42% / 12% | 42% / 12% |
| go | 11 | 27% / 8% | 50% / 20% | 45% / 17% | 50% / 20% | 50% / 20% | 50% / 20% | 50% / 20% | 50% / 20% | 40% / 15% | 40% / 20% | 55% / 15% | 50% / 20% | 50% / 19% | 50% / 20% |
| java | 7 | 43% / 19% | 57% / 20% | 43% / 18% | 57% / 20% | 57% / 20% | 57% / 20% | 57% / 20% | 57% / 20% | 57% / 20% | 62% / 22% | 76% / 20% | 57% / 20% | 57% / 21% | 57% / 20% |
| javascript | 6 | 25% / 7% | 25% / 11% | 25% / 11% | 25% / 11% | 25% / 11% | 25% / 11% | 25% / 11% | 25% / 11% | 25% / 10% | 17% / 17% | 25% / 11% | 25% / 11% | 25% / 11% | 25% / 11% |
| php | 6 | 42% / 13% | 54% / 14% | 54% / 14% | 54% / 14% | 54% / 14% | 54% / 14% | 54% / 14% | 54% / 14% | 54% / 14% | 54% / 40% | 54% / 14% | 54% / 14% | 54% / 14% | 54% / 14% |
| python | 22 | 32% / 10% | 64% / 27% | 59% / 31% | 64% / 30% | 64% / 27% | 64% / 29% | 59% / 28% | 64% / 27% | 64% / 24% | 41% / 17% | 68% / 20% | 64% / 27% | 55% / 24% | 64% / 27% |
| ruby | 11 | 23% / 13% | 45% / 13% | 32% / 11% | 45% / 13% | 45% / 13% | 45% / 13% | 45% / 14% | 45% / 13% | 45% / 13% | 48% / 20% | 45% / 13% | 45% / 13% | 45% / 11% | 45% / 13% |
| rust | 12 | 36% / 27% | 65% / 19% | 61% / 20% | 65% / 19% | 65% / 19% | 62% / 19% | 65% / 19% | 65% / 19% | 61% / 18% | 53% / 27% | 62% / 18% | 65% / 19% | 65% / 11% | 65% / 19% |
| typescript | 5 | 0% / 0% | 20% / 3% | 20% / 3% | 20% / 3% | 20% / 3% | 20% / 3% | 0% / 0% | 20% / 3% | 40% / 4% | 20% / 3% | 47% / 10% | 20% / 3% | 20% / 3% | 20% / 3% |

Paired with basic (per instance; ties dropped; two-sided sign test):

| Arm | Recall better / worse | p | Precision better / worse | p |
|---|---|---|---|---|
| adaptive vs basic | 26 / 1 | 0.000 | 35 / 15 | 0.007 |
| adaptive-discovery vs basic | 18 / 1 | 0.000 | 29 / 13 | 0.020 |
| adaptive-modules vs basic | 26 / 1 | 0.000 | 35 / 15 | 0.007 |
| adaptive-docs_config vs basic | 26 / 1 | 0.000 | 35 / 15 | 0.007 |
| adaptive-contracts vs basic | 25 / 1 | 0.000 | 36 / 15 | 0.005 |
| adaptive-lexical vs basic | 24 / 1 | 0.000 | 33 / 15 | 0.013 |
| adaptive-graph vs basic | 26 / 1 | 0.000 | 35 / 15 | 0.007 |
| adaptive-paths vs basic | 26 / 2 | 0.000 | 34 / 16 | 0.015 |
| adaptive-symbols vs basic | 26 / 9 | 0.006 | 34 / 16 | 0.015 |
| adaptive+tests vs basic+tests | 0 / 0 | 1.000 | 0 / 0 | 1.000 |
| adaptive-test_to_impl+tests vs basic+tests | 0 / 0 | 1.000 | 0 / 0 | 1.000 |
| adaptive-stack_trace vs basic | 24 / 1 | 0.000 | 33 / 15 | 0.013 |
| adaptive-build_error vs basic | 26 / 1 | 0.000 | 35 / 15 | 0.007 |

