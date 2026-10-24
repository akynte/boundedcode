//go:build windows

package direct

import "os/exec"

func setProcessGroup(c *exec.Cmd) {}
