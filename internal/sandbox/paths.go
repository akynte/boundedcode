package sandbox

import (
	"path/filepath"
	"runtime"
	"strings"
)

// ContainerPath is where the sandbox mounts a host path. On Linux and macOS
// it is the same path (identity mounts keep absolute paths valid on both
// sides). Windows paths cannot exist in a Linux container, so C:\Users\a\b
// is mounted at /host/c/Users/a/b (and \\server\share\x at
// /host/unc/server/share/x).
func ContainerPath(p string) string { return ContainerPathFor(runtime.GOOS, p) }

// ContainerPathFor is ContainerPath for a given host OS.
func ContainerPathFor(goos, p string) string { return containerPathFor(goos, p) }

func containerPathFor(goos, p string) string {
	if goos != "windows" || p == "" {
		return p
	}
	slash := strings.ReplaceAll(p, `\`, "/")
	switch {
	case len(slash) >= 2 && slash[1] == ':' && isLetter(slash[0]):
		rest := strings.TrimRight(slash[2:], "/")
		if rest == "" || rest[0] != '/' {
			rest = "/" + rest
		}
		return strings.TrimRight("/host/"+strings.ToLower(slash[:1])+rest, "/")
	case strings.HasPrefix(slash, "//"):
		return "/host/unc/" + strings.TrimLeft(slash, "/")
	}
	return slash
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// hostPathValue reports whether an environment value is a host path that
// must be translated (an absolute Windows path).
func hostPathValue(v string) bool {
	return runtime.GOOS == "windows" && filepath.IsAbs(v)
}
