//go:build !windows

package sandbox

import (
	"os/exec"
	"syscall"
)

// ownGroup runs cmd in its own process group, and a cancel or timeout
// kills the whole group: a stage's children (a test runner's workers, a
// sleep in a script) do not outlive it or hold its output open.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
