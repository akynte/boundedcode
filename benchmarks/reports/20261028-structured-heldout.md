# Structured-output benchmark

Model qwen3.6-35b-a3b (local); 37 instances; 2541 s.

| Call | Arm | Usable | Failures | Median s | Mean output tokens | Tests proposed (named as tests) |
|---|---|---|---|---|---|---|
| contract | prompt | 35 / 37 | nothing_required 2 | 5.0 | 160 |  |
| contract | schema | 35 / 37 | nothing_required 2 | 5.2 | 161 |  |
| grounding | prompt | 9 / 9 |  | 3.1 | 59 |  |
| grounding | schema | 7 / 7 |  | 2.9 | 49 |  |
| counterexample | prompt | 34 / 37 | invalid_json 3 | 27.3 | 1103 | 42 (41) |
| counterexample | schema | 36 / 37 | invalid_json 1 | 23.2 | 1053 | 46 (46) |

Paired (the same instance and call in both arms):

| Call | Pairs | Schema usable, prompt not | Prompt usable, schema not | Two-sided sign test p |
|---|---|---|---|---|
| contract | 37 | 0 | 0 | 1.000 |
| grounding | 5 | 0 | 0 | 1.000 |
| counterexample | 37 | 2 | 0 | 0.500 |
