//go:build linux

package session

import (
	"os/exec"
	"syscall"
)

// SetParentDeathSignal asks the kernel to signal a child if the launcher dies
// unexpectedly. Normal cleanup still uses the explicit ordered path; this is
// the crash-safety net that prevents a supervisor from surviving a SIGKILL of
// its parent and keeping a model resident.
func SetParentDeathSignal(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
