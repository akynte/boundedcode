// Package buildinfo is the single source of truth for the project's name and
// version. See docs/development/renaming.md.
package buildinfo

import "runtime/debug"

const (
	// ProductName is the human-facing project name.
	ProductName = "BoundedCode"
	// Name is the CLI binary name (lowercase).
	Name = "boundedcode"
	// EnvPrefix prefixes every environment variable the tool reads.
	EnvPrefix = "BOUNDEDCODE_"
	// DataDirName is the directory name used under XDG base directories.
	DataDirName = "boundedcode"
)

// Version is set at link time with -ldflags "-X .../buildinfo.Version=v0.1.0".
var Version = "dev"

// Commit returns the VCS revision embedded by the Go toolchain, if any.
func Commit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev == "" {
		return ""
	}
	return rev + dirty
}
