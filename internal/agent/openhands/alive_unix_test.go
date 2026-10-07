//go:build unix

package openhands

import "syscall"

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
