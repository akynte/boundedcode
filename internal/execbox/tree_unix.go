//go:build !windows

package execbox

import (
	"os/exec"
	"syscall"
)

// tree is the command's process group: a cancel or the end of the command
// kills every process in it. A process that leaves the group (setsid) is
// beyond it; the bwrap backend's pid namespace covers that case.
type tree struct{ pgid int }

func newTree(c *exec.Cmd) *tree {
	t := &tree{}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		t.kill()
		return nil
	}
	return t
}

func (t *tree) started(c *exec.Cmd) { t.pgid = c.Process.Pid }

func (t *tree) kill() {
	if t.pgid > 0 {
		_ = syscall.Kill(-t.pgid, syscall.SIGKILL)
	}
}

func (t *tree) close() {}
