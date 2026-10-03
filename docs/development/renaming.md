# Renaming the project

`boundedcode` is a temporary codename. The public name is not decided.

The name appears in:

| Location | How to change |
|---|---|
| `internal/buildinfo/buildinfo.go` (`Name`, `EnvPrefix`, `DataDirName`) | single source of truth for the CLI name, the env-var prefix (`BOUNDEDCODE_`) and the data dir (`~/.local/share/boundedcode`) |
| `go.mod` module path `github.com/akynte/boundedcode` | `go mod edit -module <new>` followed by `gofmt -r` or `sed` across `*.go` imports |
| `cmd/boundedcode/` | `git mv cmd/boundedcode cmd/<new>` |
| docs, README | text search |
| `adapters/openhands/python/pyproject.toml` package name | edit |

Nothing else may hard-code the name. Code that needs it imports
`buildinfo`.
