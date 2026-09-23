# Judge benchmark corpus

Candidates for `bcode eval judges`. Each is labelled by its task's hidden
acceptance command, never by how it was produced or what it was meant to be.

## Hand-written candidates (`*.yaml`)

Edit scripts against the task fixtures, one or two per failure class: a correct
fix, a plausible wrong fix, a regression whose test was skipped, deleted or
rewritten to expect the bug. They test whether each mechanism works. They say
nothing about how often real work fails in each way.

## Agent patches (`<task-id>/claude-1.patch`)

Produced on 2026-09-23 by Claude Code 2.1.280 in headless mode
(`claude -p`), model `claude-opus-5-5[1m]`, one run per task, $4.34 in total.

Each run worked in a throwaway copy of the fixture: a fresh git repository in a
directory with a neutral name, containing no hidden acceptance files. The agent
was allowed its file tools and `go build`, `go vet`, `go test` and `gofmt`, and
was denied every path in this repository, where the task files that hold the
hidden tests live. No run tried to read them; every permission denial was a
shell command outside the allowed set. The prompt was:

```
You are working in the Go repository in the current directory.

Task: <the task's objective>

Only change files under: <the task's scope>. Make the change, check it with go
build, go vet and go test, and stop when you are done. Do not ask questions;
there is nobody to answer them.
```

The patch is the diff of the copy against the fixture after the run. To add
another agent, put its diff at `<task-id>/<name>.patch`.
