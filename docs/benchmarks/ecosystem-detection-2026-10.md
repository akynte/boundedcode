# Project detection in any language (2026-10-27)

Direct mode's project map used to read build files at the repository root
only, and only Go's `_test.go` counted as test code for history notes. The
map now comes from `internal/ecosystem`: every project in the top four
directory levels, of 27 kinds (build systems and package formats), with its package manager,
build and test commands, workspace members folded under their root, the
tools that are not installed, and the CI configuration. Test files are
classified by each language's conventions (`ecosystem.IsTest`). No model is
involved in either.

## Method

- **Unit tests** (`go test ./internal/ecosystem`): a polyglot monorepo
  fixture (Go, a pnpm workspace, Python with uv, a Cargo workspace, Maven
  modules with a wrapper, a Gradle build, a .NET solution, Ruby, PHP,
  Elixir, Terraform, a Makefile), package managers and test runners for
  Node, Python, PHP, Dart and Flutter, Deno, CMake and just, the depth,
  entry and project limits, a virtual environment, a symbolic link out of
  the tree, and test-file names in Go, Python, JavaScript and TypeScript,
  the JVM languages, Rust, Ruby, PHP, .NET, Swift, Elixir, Dart, C and
  C++, and Bats.
- **Real repositories**: shallow clones of 12 public repositories chosen
  to cover the ecosystems, plus BoundedCode itself and golang.org/x/tools
  v0.50.0 from the module cache. Detection was run on each and its output
  compared by hand with the repositories' manifests. No command it
  reports was run: this machine has Go, Node and Python only, so most
  commands could not be checked here, and they were not run on project
  code that has not been reviewed.
- **Regression**: the held-out trail study (x/tools, cobra, cli/cli, 200
  commits each) was run again with the new test-file classifier.

## Real repositories

| Repository | Commit | Found | Time |
|---|---|---|---|
| GoogleCloudPlatform/microservices-demo | `38e7348eb` | 14 projects (12 listed, 2 more): Go modules, Node packages, Python services, a Gradle service with its wrapper, a .NET solution (2 member projects), Terraform, a Helm chart | 1.1 ms |
| vitejs/vite | `cce559ab1` | pnpm workspace root with its build, test, typecheck and lint scripts; 265 member packages | 10–17 ms |
| pallets/flask | `d086db856` | uv project (`uv run pytest`), three example projects, the docs Makefile | 0.4–0.7 ms |
| BurntSushi/ripgrep | `6892bc2b7` | Rust workspace, 10 member crates | 0.6 ms |
| spring-projects/spring-petclinic | `500158f73` | both its Maven and Gradle builds, with their wrappers | 0.1 ms |
| square/okhttp | `a3156ec85` | Gradle build with wrapper, 35 member projects; the separate Maven test project | 1.1 ms |
| jbogard/MediatR | `fb83d852b` | .NET solution, 15 member projects | 0.3 ms |
| sinatra/sinatra | `cb22afd79` | three Ruby projects: `rake test` at the root, `rspec` in the two with a `spec/` directory | 0.3 ms |
| guzzle/guzzle | `7d9b1f430` | PHP project (`vendor/bin/phpunit`), the Makefile's targets | 0.3–0.4 ms |
| phoenixframework/phoenix | `50642688a` | Elixir project and Node package at the root, the installer and integration-test Elixir projects, the assets package | 0.7 ms |
| libuv/libuv | `338ac6ac0` | CMake and Autotools builds at the root | 0.4 ms |
| dart-lang/http | `23495b011` | 24 Dart and Flutter packages (12 listed) | 0.9 ms |
| BoundedCode | working tree | the Go module, the Makefile, two uv projects, and the benchmark fixtures' Go modules and Node packages | 2.5 ms |
| golang.org/x/tools v0.50.0 | module cache | the Go module | 1.4 ms |

Times are one run each on the reference laptop, with other work running
during some of them.

Two mistakes showed up in this check and were fixed before the commit:
Guzzle's `vendor-bin/` tool directories were reported as PHP projects (the
directory is now skipped), and Vite's `playground/env` package was missed
because `env` was skipped as a virtual-environment name (a virtual
environment is now recognised by its `pyvenv.cfg`).

## Regression: history notes

Rerun with the new classifier, the held-out trail study gives the same
JSON and Markdown as the recorded reports
(`benchmarks/reports/20261026T221045Z-trail-heldout-*`), apart from the
timestamps, for all three repositories. The Go rule is unchanged, so the
measured history-note results stand. For other languages, test files are
now left out of history notes as Go's are. That part has not been
measured: no non-Go repository has been studied.

## Limits

- **Depth.** Projects deeper than four levels below the root are not
  found. The walk also stops after 20,000 directory entries.
- **Skipped directories.** Dependency and output directories (`vendor`,
  `node_modules`, `target`, `dist`, `build`, `out`, `bin`, `obj`, `deps`,
  `testdata`, and others) are skipped by name, so a real project inside a
  directory with one of these names is missed.
- **Commands are inferred from manifests and not run.** A project may need
  set-up first, such as installing dependencies, `cmake` configure options
  or a `.env` file, and a CI workflow may use different commands.
  Detection says which tool is missing; it does not say that a command
  works.
- **Workspace members** are counted, not checked against the workspace's
  globs: every nested project of the same build under a workspace root is
  counted as a member.
- **Test-file conventions** differ by project. A project that keeps tests
  elsewhere, or names non-test code `*Test.java`, is classified wrongly.
