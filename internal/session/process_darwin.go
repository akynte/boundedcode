//go:build darwin

package session

import "os/exec"

// Darwin has no Linux parent-death signal attribute. The launcher still uses
// process groups and explicit cleanup; this platform-specific hook keeps the
// shared lifecycle code buildable without pretending the kernel guarantee
// exists there.
func SetParentDeathSignal(_ *exec.Cmd) {}
