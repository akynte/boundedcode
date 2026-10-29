//go:build !windows

package execbox

import (
	"os/exec"
	"sync/atomic"
	"syscall"
)

// tree is the command's process group: a cancel or the end of the command
// kills every process in it. A process that leaves the group (setsid) is
// beyond it; the bwrap backend's pid namespace covers that case. pgid is
// set once the command starts and read by the cancel, which exec runs on
// its own goroutine.
type tree struct{ pgid atomic.Int64 }

func newTree(c *exec.Cmd) *tree {
	t := &tree{}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		t.kill()
		return nil
	}
	return t
}

func (t *tree) started(c *exec.Cmd) { t.pgid.Store(int64(c.Process.Pid)) }

func (t *tree) kill() {
	if pgid := int(t.pgid.Load()); pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

func (t *tree) close() {}
