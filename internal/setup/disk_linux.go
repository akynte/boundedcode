//go:build linux

package setup

import "syscall"

// availableBytes reports free bytes on the filesystem containing path.
func availableBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
