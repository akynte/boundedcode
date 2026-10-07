//go:build windows

package cbm

import (
	"syscall"

	"github.com/akynte/boundedcode/internal/proctree"
)

// procAttr starts the MCP server in its own process group. It exits when
// its stdin closes, so it does not outlive the control plane.
func procAttr() *syscall.SysProcAttr { return proctree.NewGroupAttr() }

// killGroup ends the server and what it started.
func killGroup(pid int) { proctree.KillTree(pid) }
