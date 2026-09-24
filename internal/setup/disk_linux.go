//go:build linux

package setup

import "syscall"

// availableBytes reports free bytes on the filesystem containing path.
func availableBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	if stat.Bavail <= 0 || stat.Bsize <= 0 {
		return 0, nil
	}
	blocks := stat.Bavail
	blockSize := uint64(stat.Bsize)
	if blocks > ^uint64(0)/blockSize {
		return ^uint64(0), nil
	}
	return blocks * blockSize, nil
}
