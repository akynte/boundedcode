# Project name

The project is **BoundedCode**. Machine-facing identifiers use the lowercase
form `boundedcode`: the CLI binary, the Go module path
`github.com/akynte/boundedcode`, the `BOUNDEDCODE_` environment prefix and
the `~/.local/share/boundedcode` data directories.

All of these are defined in `internal/buildinfo/buildinfo.go` (`ProductName`,
`Name`, `EnvPrefix`, `DataDirName`). Code that needs the name imports
`buildinfo` and never hard-codes it.
