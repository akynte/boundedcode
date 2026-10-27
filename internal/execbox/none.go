package execbox

import "runtime"

// noneBackend runs commands on the host as they are.
type noneBackend struct{ off bool }

func (n noneBackend) name() string {
	if n.off {
		return "off"
	}
	return "none"
}

func (n noneBackend) report(b *Box) Report {
	level := LevelNone
	if n.off {
		level = LevelOff
	}
	procs := "process group ended with the command (a process that starts its own session survives)"
	if runtime.GOOS == "windows" {
		procs = "job object ended with the command"
	}
	creds := "secret-looking environment variables removed; files not protected"
	if n.off {
		creds = "not protected (direct.sandbox.mode: off)"
	}
	return Report{Backend: n.name(), Level: level,
		Filesystem:  "not confined: commands can read and write whatever your user can",
		Network:     "not confined",
		Processes:   procs,
		Credentials: creds}
}

func (noneBackend) wrap(_ *Box, s Spec, _ *proxyHandle, _ string) ([]string, []string, error) {
	return s.Argv, nil, nil
}
