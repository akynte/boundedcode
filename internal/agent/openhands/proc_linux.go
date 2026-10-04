package openhands

import (
	"os/exec"
	"syscall"
)

// hostProcessGroup puts an unsandboxed adapter (and everything it spawns) in
// its own process group, killed as a unit on cancel, and has the kernel kill
// it if this process dies first.
func hostProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
