//go:build windows

package sandbox

import "os"

// fileOwner has no uid on Windows.
func fileOwner(os.FileInfo) (int, bool) { return 0, false }
