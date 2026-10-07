// Package pathutil compares host paths the way the host's file system does:
// case-insensitively on macOS and Windows (their default file systems fold
// case), case-sensitively elsewhere.
package pathutil

import (
	"path/filepath"
	"runtime"
	"strings"
)

// FoldsCase reports whether paths on this OS compare without case. macOS
// volumes can be made case-sensitive; folding there errs towards treating
// two spellings as the same path, which is the safe side for the checks
// that use it (a refused mount, a tamper check on read-only paths).
var FoldsCase = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// Equal reports whether two cleaned paths name the same location.
func Equal(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if FoldsCase {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Within reports whether p is dir or inside it.
func Within(p, dir string) bool {
	p, dir = filepath.Clean(p), filepath.Clean(dir)
	if Equal(p, dir) {
		return true
	}
	prefix := dir
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if FoldsCase {
		return len(p) > len(prefix) && strings.EqualFold(p[:len(prefix)], prefix)
	}
	return strings.HasPrefix(p, prefix)
}

// Resolve returns p with symlinks resolved when it exists, else p cleaned
// (macOS temp paths: /var is a link to /private/var).
func Resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
