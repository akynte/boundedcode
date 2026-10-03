package benchmark

import (
	"os"

	"golang.org/x/sys/unix"
)

// EvictFromPageCache asks the kernel to drop a file's cached pages so the next
// model load is a true cold load from disk. Unprivileged (posix_fadvise
// DONTNEED); best effort — pages mapped by a running process stay resident.
func EvictFromPageCache(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !os.IsPermission(err) {
		// Sync on a read-only fd may fail on some filesystems; ignore.
		_ = err
	}
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED)
}
