# Judgment Validation Campaign — data

See [`docs/explanation/judgment-validation.md`](../../docs/explanation/judgment-validation.md)
for the full explanation: why this exists, the dataset format, the dev/held-out
rule, the arms, the metrics, and how to reproduce a result. This file is a map
of what is where.

```
evals/judgment/
  policy.yaml     promotion policy: one entry per registered site, generated
                  by `bcode judgment policy init` and then hand-edited. Every
                  numeric threshold starts unconfigured (requires_selection:
                  true) until an operator sets a real, evidence-based value.
  datasets/       one <site>.jsonl per site, loaded by `bcode judgment eval
                  <site>`. See judgeval.Case for the schema.
  fixtures/       hand-authored source files a mutation generator or a
                  fixture case is built from.
  annotations/    human-label JSONL files for cases whose ground truth needs
                  a person (real diffs, real failures) rather than a
                  deterministic rule — see judgeval.Annotation.
  results/        generated, not committed (see .gitignore). Reproduced from
                  a dataset + manifest + repo revision.
  manifests/      generated, not committed. Every result has one.
```

## What exists today

`datasets/verification_integrity.jsonl` is the one seeded dataset: 27
mutation-derived cases (design instruction §12), generated deterministically
from `internal/doctor/budget_test.go` by every rule in
`judgeval.AllMutationRules`. All 27 are labeled `weakening` — a mutation
generator can only honestly produce the weakening side of the M4 dataset (see
`internal/judgeval/mutation.go`'s package comment); the companion
"legitimate update" and "harmless refactor" cases design instruction §11 asks
for need a human annotator reading a real diff and its objective, which has
not been done yet. A dataset with only positive-class cases is degenerate for
Brier scoring by construction (`ledger.Population.Degenerate` /
`judgeval.BrierResult.Degenerate` both already catch this) — this is stated
here so it is not mistaken for a bug.

`policy.yaml` has every threshold unconfigured. No site is, or can currently
be, eligible for promotion — see `bcode judgment eligibility <site>`.

No other site has a dataset yet. `bcode judgment eval <site>` says so rather
than fabricating one.

## Regenerating the seed dataset

```go
cases := judgeval.GenerateMutationCases(
    "internal/doctor/budget_test.go", sourceText,
    "fix the request-budget check", "1", judgeval.SplitDev)
for i := range cases {
    if i%3 == 0 {
        cases[i].Split = judgeval.SplitHeldout
    }
}
judgeval.SaveDataset("evals/judgment/datasets/verification_integrity.jsonl", cases)
```

This is deterministic: the same source text and rule set always produce the
same case IDs and the same split assignment (see
`TestGenerateMutationCasesIsDeterministic`).
