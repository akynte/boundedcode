# Repository intelligence without a second large model

BoundedCode indexes repository structure on CPU and disk, then selects
bounded context for inference. Ordinary retrieval does not require embeddings
or a vector database.

## What is stored

`bcode index` records files, chunks, FTS5 text, symbols and typed relationships in
a workspace-scoped SQLite database. Relationships point from consumer to
consumed; impact analysis traverses them in reverse. Each edge records how it
was obtained: resolved, declared, observed, inferred or unknown.

Every retrieved slice carries workspace/repository/worktree identity, path,
content hash and index version. Guards reject foreign workspace content.
The watcher marks changed scopes dirty; task freshness checks can re-analyze
before using the graph.

## Coverage

| Area | Implementation and limits |
|---|---|
| Go | `go/packages` and type information for declarations, references, calls and interfaces; incomplete builds can reduce coverage |
| TypeScript/JavaScript | Compiler sidecar for resolved relationships; lexical fallback reports weaker evidence and no resolved call graph |
| Python | Optional `scip-python` enrichment and SCIP import; explicit available/partial/unavailable reports; dynamic dispatch can be absent |
| Rust | tree-sitter structure/signatures; not a claim of compiler-resolved references |
| SQL and APIs | SQL schema plus statically recognized database/API consumers; dynamic expressions can remain unresolved |
| Infrastructure | Docker/Compose, Kubernetes, Terraform, protobuf/Avro and build configuration relationships |
| Git history | Observed commit/file relationships; co-change is historical evidence, not proof of a current dependency |

The exact vocabulary is in the [graph schema](../reference/graph-schema.md).
A missing edge means **not discovered**, never proof that no consumer exists.

## How context is selected

The retriever starts with lexical anchors, expands through bounded graph
relationships, and reserves required consumers/contracts where supplied.
Graph candidates are normally budgeted separately from lexical scores because
BM25 and inverse graph depth are not comparable quantities.

LOCALIZE progressively reads a repository map, candidate files, signatures and
targeted bodies. The model can select and refine candidates; it does not receive
every repository file. The supervisor validates paths and budgets around those
selections.

Optional Jev reranking asks a common relevance question across candidates.
If a complete usable result cannot be obtained, deterministic ordering remains.
The embedding-cosine control is an evaluation alternative, not the default
retrieval backend.

## Install semantic tooling

From the BoundedCode checkout:

```bash
npm --prefix sidecars/typescript ci
export BC_TYPESCRIPT_SIDECAR_DIR="$PWD/sidecars/typescript"
npm --prefix sidecars/python ci
```

The Python analyzer's discovery behavior and warnings are implemented in
[its analyzer](https://github.com/akynte/boundedcode/blob/main/internal/analyzers/python/python.go) and
[detection code](https://github.com/akynte/boundedcode/blob/main/internal/analyzers/python/detect.go). Use `bcode index`
output to confirm it found the semantic indexer; an installed package alone
does not prove the current workspace was indexed successfully.

A compiler-produced SCIP file can also be imported explicitly:

```bash
bcode index --scip /absolute/path/to/index.scip
```

This requires a single-repository workspace. Configured LSP servers can supply
live relationships for changed files; they cost host RAM and are optional.

## Query and inspect

```bash
bcode graph stats
bcode graph search "PaymentService"
bcode graph impact Refund --change signature
```

The graph is useful for inspectable repository questions even before a coding
task starts. Its incremental contribution to task quality is a separate claim:
the [historical ablation](../benchmarks/results/2026-09-17-graph-contribution.md)
did not demonstrate a statistically detectable gain.
