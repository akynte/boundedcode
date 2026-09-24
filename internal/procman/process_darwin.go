//go:build darwin

package procman

import "os/exec"

func setParentDeathSignal(_ *exec.Cmd) {}
