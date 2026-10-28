//go:build windows

package sandbox

import (
	"os/exec"

	"github.com/akynte/boundedcode/internal/proctree"
)

// ownGroup makes a cancel or timeout end the command's whole process tree.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = proctree.NewGroupAttr()
	cmd.Cancel = func() error {
		proctree.KillTree(cmd.Process.Pid)
		return nil
	}
}
