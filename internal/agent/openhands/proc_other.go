//go:build !linux

package openhands

import "os/exec"

// hostProcessGroup is a no-op off Linux; cancellation kills only the adapter.
func hostProcessGroup(*exec.Cmd) {}
