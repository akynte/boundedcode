# Submitting an evaluation result

Results from other machines, models and task sets are welcome, including
results where BoundedCode does badly. A result is accepted when someone
else can rerun it from what you submit. Whether it favours BoundedCode
plays no part.

## What a result must include

1. **The task set, chosen before any run.**
   - Give the dataset and its revision, the task IDs, and the rule that
     selected them.
   - Say what screening you applied (for example "gate 1: the hidden
     tests fail on the base and pass with the reference patch").
   - A task dropped after you saw its result must still be reported.
2. **Every run.** Include failures, timeouts and runs you consider broken,
   with the reason. Never report only the best run.
3. **Hidden acceptance.** The agent must never see the acceptance tests or
   the reference patch. In `bench tasks` they live in the task's `hidden`
   section and are applied only after the run. Say how your setup ensures
   this if you used anything else.
4. **The raw output.** Attach the JSON and Markdown that
   `boundedcode bench tasks --out FILE.json` writes. Do not attach
   summaries you computed by hand without the files they came from.
5. **The environment:**
   - `boundedcode version`;
   - the model (repository, file and revision, or the cloud model ID);
   - `llama-server --version`;
   - the hardware;
   - the OS and the container engine;
   - your `config.yaml` with credentials removed.
6. **Deviations.** Anything you changed after starting, and why.

## How to run one

- **Your own tasks:** use `boundedcode bench tasks`. A task is one YAML
  file (see `benchmarks/tasks/` for the format): the request, the setup,
  and the hidden acceptance files.
- **Public SWE-bench-style datasets:** reuse the tooling in
  [comparative-2026-10](comparative-2026-10/README.md). It covers selection,
  environment preparation, screening, freezing, runs, a baseline arm and
  analysis.
- **The same task set without BoundedCode:** `bench tasks --baseline` runs
  the plain agent with the same model and sandbox. That is the comparison
  most results need.

Keep the machine otherwise idle while measuring time or speed
(`docs/development/guide.md`, "Benchmark hygiene").

## How to submit

- **A pull request (preferred).**
  - Add a directory `benchmarks/community/YYYY-MM-<short-name>/` holding a
    `README.md`, with the items above, and the raw files.
  - Commits need a DCO sign-off (`git commit -s`), as for any
    contribution.
  - Keep home directories and private repository names out of the files.
- **An issue.** Use the **Evaluation result** form, and attach the raw
  files (zip them if needed).

## What happens next

- **Labelling.** A maintainer checks that the submission is complete and
  labels it `evaluation-result`.
- **Merging.** Submitted numbers are merged as written, labelled as
  external and not reproduced by the maintainers. The maintainers do not
  edit them.
- **Reproduction.** If a maintainer reruns the result, the rerun is added
  as a separate report next to yours, whatever it shows.
- **Linking.** Results are linked from the
  [evaluation overview](../docs/benchmarks/README.md) only when they meet
  the requirements above.
