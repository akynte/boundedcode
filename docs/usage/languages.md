# Languages

BoundedCode is written in Go. The projects it works on can be in any
language. This page covers:
- what works for every file;
- what a language's description adds;
- how build systems and tools are found;
- which ecosystems were validated, and how;
- how to add a language.

Run `bcode capabilities [dir]` to see what BoundedCode finds in a
repository. It lists each language with its check, formatter, linter and
language server, and each project (nested ones too) with its build and
test commands. Every entry is marked *available*, *missing tool* or
*unsupported* / *none known*.

## Every file, any language

Reading, editing, writing, searching (`grep`, `glob`), the shell, git and
sub-agents do not depend on the language. A file in a format BoundedCode
has never heard of is read, edited and searched like any other. Symbol
search (`find_symbol`, `find_references`) works in every described
language from its definition patterns, and finds names as whole words
everywhere else.

## The capability model

Ten concerns, three descriptors, no language-specific code in the agent
loop:

| Concern | Where | What it is |
|---|---|---|
| Generic repository operations | `internal/direct` tools | language-blind |
| Language detection | `internal/lang` `Language` | extensions, file names, `#!` interpreters |
| Project and workspace detection | `internal/ecosystem` `BuildSystem` | marker files in every directory, nested projects, workspaces that aggregate members (Cargo, Gradle, Maven modules, npm workspaces, .NET solutions, Bazel, CMake) |
| Build-system detection | `BuildSystem` | build and test commands, wrappers (`./gradlew`, `./mvnw`) |
| Dependency and package managers | `BuildSystem` | the manager (npm, pnpm, yarn, bun from the lockfile or `packageManager`; pip, uv, poetry, pdm, pipenv), its tools and registry hosts (used by the [sandbox](direct-sandbox.md#network)) |
| Test runners | `BuildSystem`, `Language.Tests` | the test command (pytest or unittest, rspec or rake, `composer test` or phpunit, …), test-file patterns per language |
| Formatting and linting | `Language.Format`, `Language.Lint` | tools, first installed wins; formatters are marked as changing files |
| Language servers and navigation | `Language.LSP`, `Language.Definitions` | Serena where it serves the language, otherwise the text navigator (`internal/repointel`) |
| Diagnostics | `lang.ParseDiagnostics`, `Language.Diagnostics` | `file:line:col: message` from compilers, interpreters, test runners and linters (generic formats plus tsc, MSBuild, rustc, javac, Maven, kotlinc, Python tracebacks, PHP, bash, shellcheck) |
| Verification and mutation | `Language.Check` (per file, after each edit), bounded-mode presets (`internal/verify`) | the cheapest check that says "this file does not parse"; the project's own build and tests |

A repository is never assigned one language. Each directory is examined
for every build system, so a Go module, a Node package and a Rust crate
side by side are three projects. The repository root is not assumed to be
a build root. In bounded mode, a root that runs its own tests also verifies
nested projects in other ecosystems, and nested Go modules.

### Statuses

| Status | Meaning |
|---|---|
| available | the tool is installed on the host (never a binary inside the repository) |
| missing tool | the language has a tool for this; none is installed |
| unsupported / none known | BoundedCode has nothing for this language here, or the project has no such command |

After an edit, the model always learns which of these applies. A
language with no per-file check (TypeScript, Rust, Java, C, …), or whose
tool is missing, is named once per session. "Not checked" is never silent
and never reads as "passed". In bounded-mode verification, a stage that
does not pass has a cause:
- `code`: the project's build or tests failed;
- `missing tool`;
- `timeout`: not verified;
- `environment`.

A task whose verification failed only for missing tools or the
environment is **blocked as incomplete**, not handed back to the agent to
"fix".

## Validation

The fixture is `internal/ecosystem/testdata/polyglot`: a monorepo of 15
projects in 14+ ecosystems.

| Ecosystem | Detected (project, build system, test runner) | Built and tested with native tools | Per-file check after an edit |
|---|---|---|---|
| Go | yes | yes: `go build`, `go test` (host and sandbox image) | parse + `go build`/`go vet` of the package |
| JavaScript | yes | yes: `npm test` (node --test) | `node --check` (not JSX) |
| TypeScript | yes | typecheck needs `node_modules` (not run) | none: the project's typecheck |
| Python | yes | yes: `python3 -m unittest` | `ast.parse` |
| Rust | yes | yes: `cargo build`, `cargo test` (sandbox image) | none: `cargo check` |
| Java | yes (Maven) | yes: `mvn compile`, `mvn test`, with the network for plugins | none: the build |
| Kotlin | yes (Gradle, Kotlin DSL) | yes: `gradle assemble`, `gradle test`, with the network for plugins | none: the build |
| C | yes (CMake) | yes: CMake build, CTest (sandbox image) | none: the build |
| C++ | yes (Make) | yes: `make`, `make test` (sandbox image) | none: the build |
| C# / .NET | yes (solution with 2 projects) | **not run**: no .NET SDK on the test machines; reported as missing tool | none |
| PHP | yes (Composer) | yes: `composer test` (sandbox image) | `php -l` (verified to fail on broken files) |
| Ruby | yes (Bundler, Rake) | yes: `bundle exec rake test` (sandbox image) | `ruby -c` (verified) |
| Swift | yes (SwiftPM) | **not run**: no Swift toolchain; missing tool | none |
| Dart | yes (pub) | **not run**: no Dart SDK; missing tool | none |
| Shell | yes (including `#!` scripts without an extension) | — | `bash -n` |
| SQL | yes (definitions: tables, views, indexes) | — | none: dialects differ |
| Terraform, Dockerfile, Kubernetes YAML, JSON, XML | yes | Terraform: **not run**, missing tool | YAML (every document), JSON, XML in-process; JSON with comments (tsconfig) is not checked as strict JSON |

The tests behind the table:
- `internal/lang` (recognition, definitions in every fixture language,
  tests, census, diagnostics, checks);
- `internal/ecosystem/polyglot_test.go` (detection, capabilities);
- `internal/repointel/textnav_test.go` (symbols in 16 languages);
- `internal/direct/polyglot_test.go` (a session across languages);
- `TestNativeToolchainsInSandboxImage` (`BOUNDEDCODE_DOCKER_TESTS=1`, and
  `BOUNDEDCODE_NET_TESTS=1` for Maven and Gradle). It runs every detected
  command in the sandbox image and requires the missing toolchains to fail
  as "not installed".

## Adding a language

Without changing BoundedCode, add it to your configuration
(`~/.config/boundedcode/config.yaml`):

```yaml
languages:
  - id: nim
    name: Nim
    extensions: [.nim, .nims]
    definitions: ['^proc\s+(?P<name>\w+)', '^type\s+(?P<name>\w+)']
    tests: ['(^|/)tests?/.*\.nim$']
    check: {what: nim check, tools: [nim], args: [check, "{file}"]}
    lsp: [nimlangserver]
build_systems:
  - id: nimble
    kind: Nimble package
    markers: ["*.nimble"]
    languages: [nim]
    tool: nimble
    build: nimble build
    test: nimble test
```

`{file}`, `{dir}`, `{pkg}` (the file's directory relative to `root`) and
`{root}` (the nearest directory with one of `root`'s marker files) are
substituted in check arguments. Only your own configuration can add
languages: a workspace override or a repository cannot. Their check
commands run after every edit.

In the source, a built-in language is one `Language` value in
`internal/lang/builtin.go`, and a build system one `BuildSystem` in
`internal/ecosystem`. Nothing else changes.

## Security

Every tool a language brings runs through the same controls:
- checks run host tools only, never a binary from the repository, in the
  session's [sandbox](direct-sandbox.md) with the command timeout;
- the model's commands pass the [security policy](security-policy.md);
- bounded-mode stages run in the container;
- the text navigator skips the same secret files as `grep` and `read`.

Capability discovery looks tools up on `PATH` and executes nothing.
