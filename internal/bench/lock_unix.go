//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package bench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func withOutputLock(ctx context.Context, root string, fn func() error) error {
	release, err := acquireOutputLock(ctx, root)
	if err != nil {
		return err
	}
	return errors.Join(fn(), release())
}

// acquireOutputLock takes an advisory, process-scoped lock for one benchmark
// result tree. flock locks are released by the kernel if a process is killed,
// so an interrupted run cannot leave a permanent "busy" marker. The lock is
// held for the complete schedule, not just an individual JSON write, which
// prevents two processes from materializing the same run directory at once.
func acquireOutputLock(ctx context.Context, root string) (func() error, error) {
	if root == "" {
		return nil, errors.New("bench: output root is empty")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(root, ".bench.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600) //nolint:gosec // operator-selected output path
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() error {
				unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				closeErr := file.Close()
				return errors.Join(unlockErr, closeErr)
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("bench: lock output %s: %w", root, err)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
