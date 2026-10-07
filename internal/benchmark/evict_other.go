//go:build !linux

package benchmark

import (
	"errors"
	"runtime"
)

// EvictFromPageCache has no unprivileged equivalent outside Linux (macOS
// `purge` needs root; Windows has none), so cold-load measurements there
// include whatever the OS still caches.
func EvictFromPageCache(string) error {
	return errors.New("page-cache eviction is not supported on " + runtime.GOOS)
}
