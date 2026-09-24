//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package bench

import (
	"context"
	"os/exec"
)

// Platforms without a Unix process-group API still get the context-bound
// behavior of exec.CommandContext. The benchmark's supported production
// platforms use process_unix.go, which additionally reaps descendants.
func runProcess(ctx context.Context, cmd *exec.Cmd) error {
	_, err := runProcessStarted(ctx, cmd)
	return err
}

func runProcessStarted(ctx context.Context, cmd *exec.Cmd) (bool, error) {
	if cmd == nil {
		return false, exec.ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := cmd.Start(); err != nil {
		return false, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return true, err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return true, ctx.Err()
	}
}
