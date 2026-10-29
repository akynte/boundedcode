# Retrieval

Before the model's first call for a message, BoundedCode looks up the
places in the repository that the message (and any failure output collected
so far) points at. It sends them with the message as a short ranked list.
A small local model otherwise spends its first turns on `grep` and `read`.
At 35 tokens per second, each of those turns costs seconds and context.

Retrieval works in any language and in polyglot repositories, with or
without a language server. It never builds or runs the project, and it
keeps no index. A retrieval walks the checkout once within fixed budgets.
The strategies the message calls for then share one content scan.

This page is the reference for what retrieval does, when, and with what
limits. The measurements are in
[the retrieval benchmark](../benchmarks/retrieval-2026-10.md).

## Where it runs

| Mode | When | What the model gets |
|---|---|---|
| Direct | Every message (`direct.retrieval`) | A block "Likely relevant places, ranked", stored with the message. The block sits in the stable prefix, so it does not break the prompt cache on later turns. |
| Bounded | Every retry pack after a failed verification | Under "RELEVANT CODE": the lines around each location the failures point at (frames, compiler and test-runner locations, the code failing tests exercise), per repository. The request's own names are still routed through Serena and the code graph (ADR-0008). |

A block looks like this:

```
Likely relevant places, ranked (found by stack trace, test to impl, symbols; verify before relying on them):
- jobs/inventory/stock.py:31 (reorder): stack frame /srv/app/inventory/stock.py:31
- jobs/inventory/test_stock.py:4: test test_stock.py
- jobs/inventory/cli.py:9 (main): stack frame /srv/app/inventory/cli.py:9
- already read in this conversation, unchanged: jobs/inventory/models.py
```

## Strategies

The selection is deterministic. A strategy runs when the message or the
evidence holds what it needs, and when the environment can serve it.

| Strategy | Runs when | How it finds places |
|---|---|---|
| `stack_trace` | A stack trace: a panic, an exception, a failed test's trace | Frames in the formats of Go, Python, JavaScript/TypeScript, the JVM (Java, Kotlin, Scala, Groovy), Rust, C/C++ (gdb, sanitizers), C#, PHP, Ruby, Swift, Dart and Elixir (`lang.Frames`). They are mapped onto the checkout by the longest matching path suffix, because traces print absolute paths from another machine. A JVM frame's package narrows a bare file name. The innermost frame weighs most. |
| `build_error` | A located diagnostic | The `file:line[:col]` and `file(line,col)` formats most compilers, linters and test runners print, plus each language's own (`lang.Diagnostics`). |
| `test_to_impl` | A failing or named test | The test's definition, then two things. First, the implementation by each ecosystem's naming convention: `cart_test.go`→`cart.go`, `test_cart.py`→`cart.py`, `cart.test.ts`→`cart.ts`, `CartTest.java`→`Cart.java` (the mirrored source tree first), `cart_spec.rb`→`cart.rb`, `CartTests.cs`→`Cart.cs`. Second, the names the test's body calls, where they are defined outside tests (string literals are skipped: they are data). Test ids are understood as Go, pytest, unittest, cargo, PHPUnit, Surefire and Jest print them, and as a generic `FAILED <id>` line. |
| `paths` | A file or directory named | Resolved by path suffix. A bare name shared by several files (`Cargo.toml` in a workspace) counts for less. So does a file a code sample includes or imports (`#include <fmt/printf.h>`). |
| `modules` | A qualified name (`tokio::sync::broadcast`, `django.db.models`, `PhpOffice\PhpSpreadsheet\Calculation`, `com.acme.Orders`) | The longest run of its segments that ends a file's module path. Layout directories (`src/`, `lib/`, `src/main/java/`) and module files (`mod.rs`, `__init__.py`, `index.ts`) are skipped. Tests are not candidates. |
| `lexical` | A quoted error message or a configuration key | A literal search; the place that raises the message comes with its enclosing definition. |
| `symbols` | A code-like name (backticked, a call, CamelCase), or a member written in the prose (`Ledger.Balance`, `ledger.Post`, `Order::total`) | Its definitions, from a language server where one serves the file's language and from definition patterns everywhere else. A member's definition in the file named after its qualifier (`ledger.go`, `inventory.py`) comes first. Its uses are weighted low, and only for names used in at most 40 files. Names that appear only in a code sample count for less. |
| `graph` | A refactor or rename of a named symbol | The uses of the main symbol (a language server's references where available). |
| `contracts` | An HTTP route in the prose (`GET /api/v1/orders`); a route starts a word, so `internal/ledger` holds none | Its definition in OpenAPI, Swagger, AsyncAPI, protobuf or GraphQL files first, then the handlers that register it. |
| `docs_config` | An environment variable in the prose (`SHOP_RATE_LIMIT`) | Where it is read, set and documented. |
| `history` | A commit id the message names ("since commit 3f2a…") | The files that commit changed. It needs a git checkout; otherwise the step reports why it did not run. |
| `discovery` | No frame, test, message, path or name, **or** nothing found so far that is reliable (the best finding outside tests weighs less than 0.5) | The message's words against the file paths, weighted by how rare each word is among the paths and how often the message repeats it. |

Names and messages come from the existing seed ranking (quoted errors
first, then names discussed in prose, configuration keys, and sample-only
identifiers last). Placeholders such as `foo` and `bar` are dropped.

### Measured and dropped

Two things were measured on the development split and not kept. Notes
from these measurements are in the
[benchmark](../benchmarks/retrieval-2026-10.md):
- Documentation found by words, for questions and configuration requests. It never raised recall, and it lowered precision.
- Running discovery on every message. It added one point of recall for about 60 more tokens, so it runs only as the fallback.

## Ranking

Each finding has a weight: the strategy's reliability times the anchor's
specificity.
- **Reliability** is how often that kind of finding points at a place the
  task needs:
  - a stack frame 0.95;
  - a compiler location 0.9;
  - a named path 0.9;
  - a raised message 0.85;
  - a definition 0.8 (0.85 from a language server);
  - an implementation by naming convention 0.75;
  - a module path 0.7–0.85;
  - a route definition 0.7;
  - a test, configuration key or history hit 0.6;
  - references 0.5;
  - a path word 0.35;
  - a use 0.25.
- **Specificity** falls slowly with the number of files the anchor matches. A name defined in one file says more than one defined in twenty.

A file's findings combine as independent evidence, `1 − Π(1 − w)`, with
each anchor counted once. Agreement between strategies therefore raises a
file, and repetition of one does not. Three adjustments follow:
- **Demotions:**
  - test files and test data ×0.6, unless the message is about tests;
  - examples and benchmarks ×0.7;
  - prose ×0.5, unless the message is a question or about configuration.
- **Relative cut:** items below 30% of the best are dropped, and at most 12 are kept. The exception is the best place of each name the prose mentions (up to 3): a request that names a Go struct field and a TypeScript function needs both, though the field has only uses.
- **Order:** score, then path.

## Evidence already collected

- **Files already read:** a file whose content a result still in the
  history shows unchanged is named on one line instead of being listed
  again. A file that changed since it was read is listed normally.
- **Recent command output:** before a follow-up message, the output of the
  last commands of the previous turn (a test run, a build) is retrieval
  evidence too. Its frames and failing tests are located like the message's
  own.
- **The discovery fallback** runs only when the specific strategies found
  nothing reliable.

## Language servers and other providers

BoundedCode does not assume a language server is installed. A navigator
that reports the languages it serves (Serena reports Go, TypeScript and
JavaScript, Python and Rust) is wrapped in `repointel.Composite`, which
routes each symbol question by the file's language:
- Where a server covers a language, it answers.
- Definition patterns from `internal/lang` answer for every other language
  and replace a server that is down or failing.

So a Go service with a TypeScript client and SQL migrations gets each
answer from the best source available for that file, in one result. The
`find_symbol`, `find_references` and `find_implementations` tools use the
same routing. Implementations need a language server; without one, the
tool says so.

## Provenance and staleness

- **Items:** every retrieved item records the commit checked out, the git
  blob id of the file's content as read, the strategies that found it, and
  when. The message stores this record (`Message.Retrieval` in the session
  file).
- **Tool results:** every result that shows a file (`read`, `read_many`,
  `notebook_read`) or changes one (`edit`, `multi_edit`, `write`,
  `notebook_edit`) is stamped with the file's blob id at that moment. Every
  user message records the branch and commit.
- **The check:** before each message is sent, BoundedCode compares these
  records with the checkout and adds a note for:
  - a checkout that moved since the previous message (another branch, a
    pull, a resumed session);
  - files whose content differs from what the conversation last read or
    wrote. The cause can be the agent's own commands (a formatter, a
    generator), the user's edits or a branch switch.

  Each change is reported once.
- **`recall`:** when it brings back a result whose file changed since,
  it says so before the content.

## Compaction and recall

When the history is summarised, the replaced messages are kept: in the
session's archive in memory, and in the session file, which is
append-only. The summary ends with an index of the larger tool results it
replaced (`call_12: read internal/x.go (4,120 bytes)`), so `recall` can
bring any of them back. Recall searches the archive too, also after a
session is reloaded from disk. Detail that a summary drops is therefore
never lost, only set aside. What the summary itself holds is in
[Context and session state](context.md).

## Budgets and determinism

| Bound | Default |
|---|---|
| Time for one retrieval | 3 s (results so far are kept; `Truncated` says so) |
| Files listed | 50,000 |
| Bytes read per retrieval | 192 MiB |
| One file | 1 MiB |
| Strings searched per scan | 32 |
| Occurrences examined per string and file | 400 (uses kept: 3 per file, 60 per string) |
| Language-server calls | 4 lookups, 1.5 s each |
| Block size | 1,200 tokens |

Within these bounds a retrieval is deterministic: files are walked in
path order, and ties are broken by path. Only the time budget can cut a
retrieval short, and the result then says so. On the benchmark, a
retrieval took 16–38 ms at the median and under 0.2 s at the 95th
percentile; the slowest took 0.9 s, in a repository of 29,000 files.

## Security

Retrieval reads what `read` and `grep` may read, and nothing else:
- no secret paths (`.env`, keys, credentials: `policy.IsSecretPath`);
- no symlinks;
- no hidden, dependency or build directories;
- no lock files, minified bundles or source maps.

Evidence (command output, verification failures) is untrusted: it only
supplies places and names to look up inside the checkout. Paths that
resolve outside the checkout are dropped. In bounded mode the host reads
the snippets with the same confinement as the rest of the pack
(`ReadConfined`).

## Configuration

```yaml
direct:
  retrieval: adaptive   # adaptive (default) | basic: the earlier grep of the message's names
  seed_pack: true       # false: no retrieval block at all
```

`bcode bench direct --retrieval basic|adaptive` overrides it for a
benchmark run.

## Limits

- **Names, not meaning.** Retrieval finds what the message names, quotes
  or traces, and the files whose paths share its words. It does not
  understand a description. An issue that describes a behaviour without
  naming any code ("the layout jumps when…") gets path-word guesses at
  best. On the benchmark, recall for such requests was about 35–40%.
- **Definition patterns are approximate.** Without a language server,
  a definition is a line matching the language's patterns
  (`internal/lang`). Overloads, macros, generated code and unusual
  formatting can be missed or misread.
- **Module paths are a convention.** A qualified name maps onto a file
  only where the layout follows the language's usual module-to-path rule.
  Re-exports, C++ namespaces and Go import paths that differ from the
  directory are not resolved.
- **Measured coverage.** Rust was the weakest language on the held-out
  split (24% file recall). Its issues often name types re-exported far
  from their definitions, and their fixes are spread across many files.
- **References without a server are textual.** Without a language server,
  `graph` lists the files that use the name as a word. The dataset had too
  few refactor requests to measure it.
