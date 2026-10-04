package cbm

import "syscall"

// procAttr puts the MCP server in its own process group (so close can kill
// anything it spawned) and kills it if the control plane dies.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

// killGroup kills the process group led by pid.
func killGroup(pid int) { _ = syscall.Kill(-pid, syscall.SIGKILL) }
