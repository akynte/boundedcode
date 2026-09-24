//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package bench

import (
	"context"
	"os/exec"
	"syscall"
)

// runProcess starts cmd in its own process group and reaps the complete group
// on every exit path. A shell that backgrounds a compiler must not leave that
// compiler behind merely because the shell exited before the deadline.
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
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	if err := cmd.Start(); err != nil {
		return false, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// The group may still contain descendants after the direct child has
		// exited. Kill it before returning, then preserve the external timeout
		// classification if the deadline fired in the same scheduling window.
		killProcessGroup(cmd)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return true, ctxErr
		}
		return true, err
	case <-ctx.Done():
		killProcessGroup(cmd)
		<-done
		return true, ctx.Err()
	}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Setpgid is set before Start, so the negative pid addresses the complete
	// process group. Fall back to the direct process for a runner that
	// replaced or rejected SysProcAttr after this helper ran.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
