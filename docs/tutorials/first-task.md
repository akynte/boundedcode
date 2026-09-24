# Your first local coding task

Run a bounded change against a disposable copy of the included payment service.
This exercise demonstrates setup and the task lifecycle; its outcome depends on
the model and is not a benchmark result.

Complete [`bcode setup`](../how-to/install.md) first. The TUI prepares the local
runtime; you do not need to keep a model server running in another terminal.

## Prepare a disposable repository

From the BoundedCode checkout:

```bash
export BC_SOURCE="$PWD"
export BC_DEMO_DIR="$(mktemp -d /tmp/bc-payment-demo.XXXXXX)"
cp -R "$BC_SOURCE/examples/go-microservice/." "$BC_DEMO_DIR/"
cd "$BC_DEMO_DIR"
git init -b main
git config user.name "BoundedCode demo"
git config user.email "demo@example.invalid"
bcode workspace init --name payment-demo
git add .
git commit -m "Initialize payment service demo"
bcode index
bcode graph impact Refund --change behaviour
```

The example uses the standard library and local packages; no dependency download
is needed. On other repositories, provision dependencies before restricted
verification starts. `bcode workspace init` pins the repository identity in
`.bc/workspace.yaml`.

## Ask for a specific behavior

The included `Refund` method checks payment state and the upper amount bound,
but does not reject a nonpositive amount. Give the model a concrete requirement:

```bash
bcode task create \
  --title "Reject refund amounts less than or equal to zero before changing a payment. Add regression tests for zero, negative, and a valid positive amount." \
  --scope "internal/service/**" --verify standard
```

Copy the task ID printed by the command:

```bash
bcode task run <task-id> --diff
```

BoundedCode establishes the baseline, localizes the code, constructs a plan,
edits its task worktree, executes checks, and reviews the candidate. It may
retry after a failure. A blocked task is a useful result to inspect; do not
assume that every run will finish successfully.

## Read the evidence and approve deliberately

```bash
bcode task list
bcode task journal <task-id>
bcode gate list
bcode gate show <gate-id>
```

Review the diff, the checks that actually ran, and any concerns. A test result
must describe the candidate under review. When satisfied:

```bash
bcode gate approve <gate-id> --note "Reviewed the amount guard and regression tests"
bcode task retry <task-id>
```

The retry resumes persisted state. An unchanged approved candidate can finalize;
a changed one needs new evidence/approval. Success commits on a branch named
for the task, leaving your original checkout in place.

```bash
git worktree list
git branch --list 'bc/*'
```

Inspect the reported task branch with ordinary git tools before cherry-picking
its commit into your branch. Never infer the branch or commit from example
output: use the actual result.

## Use the deterministic tools independently

Indexing and impact queries work without a model. Verification of an existing Go
change also needs no inference:

```bash
bcode graph search "Refund"
bcode task verify --verify standard
```

`task verify` checks the current checkout's committed and uncommitted state in
a separate worktree. It does not run an autonomous fix.

For a more detailed graph walkthrough see
[Index a Go service](index-a-go-service.md).
For an editor session see [OpenCode and MCP](../how-to/use-with-opencode.md).
