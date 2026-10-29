//go:build windows

package execbox

import (
	"os/exec"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/akynte/boundedcode/internal/proctree"
)

// tree is a job object the command is assigned to as it starts: ending the
// job ends every process it started, and closing it (KILL_ON_JOB_CLOSE)
// ends what is left. Processes the command starts in the instant before
// the assignment are ended by taskkill's tree walk instead.
type tree struct {
	job windows.Handle
	// pid is set once the command starts and read by the cancel, which
	// exec runs on its own goroutine.
	pid atomic.Int64
}

func newTree(c *exec.Cmd) *tree {
	t := &tree{}
	if job, err := windows.CreateJobObject(nil, nil); err == nil {
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err == nil {
			t.job = job
		} else {
			_ = windows.CloseHandle(job)
		}
	}
	c.SysProcAttr = proctree.NewGroupAttr()
	c.Cancel = func() error {
		t.kill()
		return nil
	}
	return t
}

func (t *tree) started(c *exec.Cmd) {
	pid := c.Process.Pid
	t.pid.Store(int64(pid))
	if t.job == 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(t.job, h)
}

func (t *tree) kill() {
	if t.job != 0 {
		_ = windows.TerminateJobObject(t.job, 1)
	}
	if pid := int(t.pid.Load()); pid > 0 {
		proctree.KillTree(pid)
	}
}

func (t *tree) close() {
	if t.job != 0 {
		_ = windows.CloseHandle(t.job) // KILL_ON_JOB_CLOSE ends what is left
		t.job = 0
	}
}
