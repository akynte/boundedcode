//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package bench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func withOutputLock(ctx context.Context, root string, fn func() error) error {
	release, err := acquireOutputLock(ctx, root)
	if err != nil {
		return err
	}
	return errors.Join(fn(), release())
}

// acquireOutputLock is a best-effort fallback for platforms without flock.
// Production benchmark runs use the Unix implementation; keeping a lock
// directory here still prevents the common case of two cooperative writers.
func acquireOutputLock(ctx context.Context, root string) (func() error, error) {
	if root == "" {
		return nil, errors.New("bench: output root is empty")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(root, ".bench.lock.d")
	for {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			return func() error { return os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
