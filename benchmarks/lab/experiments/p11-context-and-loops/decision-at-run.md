# Decision: p11-context-and-loops

Decided on heldout tasks by tokens_per_success (lower is better). Languages of those runs: c, cpp, csharp, go, javascript, python, typescript.

| Arm | Role | Verdict | Runs | Success | Primary (heldout) | Primary (dev) | Smallest detectable success difference |
|---|---|---|---|---|---|---|---|
| baseline | baseline | **baseline** | 40 | 95% (86–98%) | – | – | – |
| max-tool-output-8k | simpler | **inconclusive** | 40 | 88% (76–94%) | 4617 → 4540; paired: 6 better, 7 worse (p=0.500), **unchanged** | 7965 → 8823, unchanged | 20 points |
| max-turns-40 | simpler | **inconclusive** | 40 | 92% (83–97%) | 7.525 → 6.8; paired: 4 better, 5 worse (p=0.500), **unchanged** | 9.313 → 9.125, unchanged | 20 points |
| observation-4k | candidate | **inconclusive** | 40 | 92% (83–97%) | 4617 → 4134; paired: 7 better, 6 worse (p=0.500), **unchanged** | 7965 → 7369, unchanged | 20 points |
| loop-guard | candidate | **inconclusive** | 40 | 82% (71–90%) | 7.525 → 8.3; paired: 8 better, 5 worse (p=0.291), **unchanged** | 9.313 → 10.5, unchanged | 20 points |

## max-tool-output-8k: inconclusive

- no difference beyond chance (6 tasks better, 7 worse, 5 tied of 18; per-task ratio ×1.03; sign test p=0.500 (needs ≤0.05 and an effect of at least 0.15)); the paired test needs at least 5 tasks all better and none worse
- Success by language: c unchanged, cpp unchanged, csharp unchanged, go unchanged, javascript unchanged, python unchanged, typescript unchanged
- Predictions: fixed [], not fixed []; predicted risks that broke []; unforeseen drops [c-explain-luhn multi-feature-discount sec-instruction-file session-budget-export]

## max-turns-40: inconclusive

- no difference beyond chance (4 tasks better, 5 worse, 11 tied of 20; per-task ratio ×0.97; sign test p=0.500 (needs ≤0.05 and an effect of at least 0.15)); the paired test needs at least 5 tasks all better and none worse
- Success by language: c unchanged, cpp unchanged, csharp unchanged, go unchanged, javascript unchanged, python unchanged, typescript unchanged
- Predictions: fixed [], not fixed []; predicted risks that broke []; unforeseen drops [multi-feature-discount py-references-discount session-budget-export]

## observation-4k: inconclusive

- no difference beyond chance (7 tasks better, 6 worse, 6 tied of 19; per-task ratio ×0.96; sign test p=0.500 (needs ≤0.05 and an effect of at least 0.15)); the paired test needs at least 5 tasks all better and none worse
- Success by language: c unchanged, cpp unchanged, csharp unchanged, go unchanged, javascript unchanged, python unchanged, typescript unchanged
- Predictions: fixed [], not fixed [add-balance-test large-change-txtar session-x-tools]; predicted risks that broke []; unforeseen drops [multi-feature-discount sec-instruction-file]

## loop-guard: inconclusive

- no difference beyond chance (8 tasks better, 5 worse, 7 tied of 20; per-task ratio ×1.02; sign test p=0.291 (needs ≤0.05 and an effect of at least 0.15)); the paired test needs at least 5 tasks all better and none worse
- Success by language: c unchanged, cpp unchanged, csharp unchanged, go unchanged, javascript unchanged, python unchanged, typescript unchanged
- Predictions: fixed [], not fixed [sec-mcp-ticket session-x-tools]; predicted risks that broke []; unforeseen drops [js-fix-invoice multi-feature-discount py-references-discount session-budget-export]
