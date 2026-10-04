//go:build !linux

package cbm

import "syscall"

// procAttr puts the MCP server in its own process group. Without Pdeathsig,
// a server whose parent died exits when its stdin closes.
func procAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// killGroup kills the process group led by pid.
func killGroup(pid int) { _ = syscall.Kill(-pid, syscall.SIGKILL) }
