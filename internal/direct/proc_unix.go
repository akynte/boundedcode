//go:build !windows

package direct

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the command in its own process group so a timeout
// kills the whole tree.
func setProcessGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
}
