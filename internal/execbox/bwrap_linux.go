//go:build linux

package execbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// bridgePort is where the bridge listens inside the sandbox's own network
// namespace (nothing else is there to collide with it).
const bridgePort = "3128"

type bwrapBackend struct{}

func (bwrapBackend) name() string { return "bwrap" }

func bwrapPath() (string, error) {
	for _, p := range []string{"/usr/bin/bwrap", "/bin/bwrap", "/usr/local/bin/bwrap"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	p, err := exec.LookPath("bwrap")
	if err != nil {
		return "", errors.New("bubblewrap (bwrap) is not installed")
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("bwrap found only relative to the working directory")
	}
	return p, nil
}

func probeBwrap() string {
	bw, err := bwrapPath()
	if err != nil {
		return err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bw, "--die-with-parent", "--new-session", "--unshare-pid", "--unshare-net", "--unshare-ipc",
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "true")
	var e bytes.Buffer
	c.Stderr = &e
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(e.String())
		if msg == "" {
			msg = err.Error()
		}
		return "bwrap cannot create namespaces here (" + firstLine(msg) + "); unprivileged user namespaces may be disabled"
	}
	return ""
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

func (bwrapBackend) report(b *Box) Report {
	net := "no network; " + b.netDescription()
	if b.cfg.Network == NetHost {
		net = "the host's network, not confined (direct.sandbox.network: host)"
	}
	level := LevelStrong
	if b.cfg.Network == NetHost {
		level = LevelPartial
	}
	return Report{Backend: "bwrap", Level: level,
		Filesystem: "the project is writable; the host is read-only; home directories, /run and other users' files are hidden " +
			"except toolchains (read-only) and package caches; secret files in the project and git hooks and configuration are protected",
		Network:     net,
		Processes:   "own pid namespace: every process ends with the command",
		Credentials: "home directory, agent sockets and secret-looking environment variables are not available"}
}

func (b *Box) netDescription() string {
	switch b.cfg.Network {
	case NetNone:
		return "no hosts allowed (direct.sandbox.network: none)"
	}
	d := "approved hosts through an allowlisting proxy"
	if b.cfg.Registries {
		d += ", and the package registries of the project's ecosystems"
	}
	return d + " (a program that ignores the proxy settings has no network)"
}

func (bwrapBackend) wrap(b *Box, s Spec, ph *proxyHandle, tmp string) ([]string, []string, error) {
	bw, err := bwrapPath()
	if err != nil {
		return nil, nil, err
	}
	hp := b.paths()
	a := []string{bw, "--die-with-parent", "--new-session", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--unshare-cgroup-try"}
	if b.cfg.Network != NetHost {
		a = append(a, "--unshare-net")
	}
	a = append(a, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc")
	for _, h := range append(append([]string{}, hiddenRoots...), hp.home) {
		if h != "" && isDir(h) && h != "/" {
			a = append(a, "--tmpfs", h)
		}
	}
	a = append(a, "--bind", tmp, "/tmp")
	if hp.home != "" {
		a = append(a, "--dir", hp.home)
	}
	for _, p := range append(append([]string{}, hp.pathBinds...), hp.read...) {
		a = append(a, "--ro-bind", p, p)
	}
	for _, p := range hp.write {
		a = append(a, "--bind", p, p)
	}
	a = append(a, "--bind", b.root, b.root)
	for _, p := range hp.gitRW {
		a = append(a, "--bind", p, p)
	}
	for _, p := range hp.gitRO {
		a = append(a, "--ro-bind", p, p)
	}
	visible := append(append(append([]string{b.root}, hp.pathBinds...), hp.read...), hp.write...)
	for _, p := range hp.masks {
		shown := false
		for _, v := range visible {
			shown = shown || under(p, v)
		}
		if !shown {
			continue // hidden already
		}
		if isDir(p) {
			a = append(a, "--tmpfs", p)
		} else {
			a = append(a, "--ro-bind", "/dev/null", p)
		}
	}
	argv := s.Argv
	var env []string
	if ph != nil {
		exe, err := os.Executable()
		if err != nil {
			return nil, nil, err
		}
		exe = realPath(exe)
		a = append(a, "--ro-bind", exe, exe, "--bind", ph.dir, ph.dir)
		argv = append([]string{exe, helperBridge, ph.socket, bridgePort, "--"}, argv...)
		env = proxyEnv("http://127.0.0.1:" + bridgePort)
	}
	if s.Dir != "" {
		a = append(a, "--chdir", s.Dir)
	}
	env = append(env, "TMPDIR=/tmp")
	return append(append(a, "--"), argv...), env, nil
}

// proxyEnv points every common client at the proxy.
func proxyEnv(u string) []string {
	host, port, _ := strings.Cut(strings.TrimPrefix(u, "http://"), ":")
	var env []string
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "npm_config_proxy", "npm_config_https_proxy",
		"YARN_HTTP_PROXY", "YARN_HTTPS_PROXY", "PIP_PROXY"} {
		env = append(env, k+"="+u)
	}
	env = append(env, "NO_PROXY=localhost,127.0.0.1,::1", "no_proxy=localhost,127.0.0.1,::1",
		"JAVA_TOOL_OPTIONS=-Dhttp.proxyHost="+host+" -Dhttp.proxyPort="+port+" -Dhttps.proxyHost="+host+" -Dhttps.proxyPort="+port+
			" -Dhttp.nonProxyHosts=localhost|127.0.0.1"+" "+os.Getenv("JAVA_TOOL_OPTIONS"))
	return env
}
