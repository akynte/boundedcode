//go:build !linux

package execbox

func platformBackends() []backend { return nil }

func probeBackend(backend) string { return "not supported on this operating system" }
