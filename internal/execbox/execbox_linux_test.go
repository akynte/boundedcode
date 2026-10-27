//go:build linux

package execbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	Main()
	os.Exit(m.Run())
}

type fixture struct {
	root, home, outside, state string
	box                        *Box
}

// newFixture lays out a project, a home directory with credentials and a
// file outside both, under /var/tmp: not under /home or /tmp, so each
// protection is tested by its own mechanism.
func newFixture(t *testing.T, backend string) *fixture {
	t.Helper()
	base, err := os.MkdirTemp("/var/tmp", "bcx-")
	if err != nil {
		t.Skip("/var/tmp:", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	f := &fixture{root: filepath.Join(base, "proj"), home: filepath.Join(base, "home"), outside: filepath.Join(base, "outside"),
		state: filepath.Join(base, "state")}
	files := map[string]string{
		f.root + "/main.go":                     "package main\n\nfunc main() { println(\"hi\") }\n",
		f.root + "/go.mod":                      "module example.com/x\n\ngo 1.21\n",
		f.root + "/.env":                        "API_KEY=project-secret\n",
		f.root + "/config/credentials.json":     `{"token":"project-secret"}`,
		f.home + "/.ssh/id_rsa":                 "PRIVATE KEY home-secret\n",
		f.home + "/.aws/credentials":            "[default]\naws_secret_access_key=home-secret\n",
		f.home + "/.cargo/credentials.toml":     "token = \"home-secret\"\n",
		f.home + "/.cargo/registry/cache/x.txt": "cached\n",
		f.home + "/.bashrc":                     "export SECRET=home-secret\n",
		f.outside + "/victim.txt":               "original\n",
	}
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init"}} {
		c := exec.Command("git", args...)
		c.Dir = f.root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	t.Setenv("HOME", f.home)
	t.Setenv("FAKE_API_KEY", "env-secret")
	t.Setenv("GITHUB_TOKEN", "env-secret")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	f.box = New(f.root, Config{Backend: backend, StateDir: f.state, Network: NetProxy})
	if got := f.box.Report().Backend; got != backend {
		t.Skipf("%s not available here: %v", backend, f.box.Report().Missing)
	}
	return f
}

func (f *fixture) run(t *testing.T, script string, hosts ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := f.box.Command(ctx, Spec{Argv: []string{"bash", "-c", script}, Dir: f.root, Hosts: hosts})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err = c.Run()
	return out.String(), err
}

// A PATH entry exposes that directory only, not the directory around it
// (a tool's credentials sit next to its bin).
func TestPathEntriesExposeOnlyThemselves(t *testing.T) {
	f := newFixture(t, "bwrap")
	tool := filepath.Join(f.home, ".sometool")
	if err := os.MkdirAll(filepath.Join(tool, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tool, "auth.json"), []byte(`{"token":"tool-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tool, "bin", "sometool"), []byte("#!/bin/sh\necho tool-ran\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(tool, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	f.box = New(f.root, Config{StateDir: f.state})
	out, _ := f.run(t, "sometool; cat "+filepath.Join(tool, "auth.json"))
	if !strings.Contains(out, "tool-ran") || strings.Contains(out, "tool-secret") {
		t.Fatalf("%s", out)
	}
}

func TestBwrapReport(t *testing.T) {
	f := newFixture(t, "bwrap")
	r := f.box.Report()
	if r.Level != LevelStrong || !strings.Contains(r.Network, "no network") || !strings.Contains(r.Processes, "pid namespace") {
		t.Fatalf("%+v", r)
	}
}

// Credentials and files outside the project cannot be read, however the
// path is reached.
func TestBwrapUnauthorizedReads(t *testing.T) {
	f := newFixture(t, "bwrap")
	for _, script := range []string{
		"cat " + f.home + "/.ssh/id_rsa", "cat ~/.ssh/id_rsa", "cat $HOME/.aws/credentials", "cat " + f.home + "/.bashrc",
		"cat .env", "cat config/credentials.json", "ln -s " + f.home + "/.ssh/id_rsa link && cat link",
		"cat " + f.home + "/.cargo/credentials.toml", "grep -r secret " + f.home + " .",
		"python3 -c \"print(open('" + f.home + "/.ssh/id_rsa').read())\"",
		"cat /proc/1/environ; cat /proc/$PPID/environ", "ls /home /root /run 2>/dev/null; cat /run/user/*/bus 2>&1",
	} {
		out, _ := f.run(t, script)
		if strings.Contains(out, "secret") || strings.Contains(out, "PRIVATE KEY") {
			t.Errorf("%q read a secret: %s", script, out)
		}
	}
	// What the project needs stays readable and writable; caches too.
	out, err := f.run(t, "cat main.go && echo ok > new.txt && cat new.txt && cat ~/.cargo/registry/cache/x.txt && ls /usr/bin/env")
	if err != nil || !strings.Contains(out, "package main") || !strings.Contains(out, "cached") {
		t.Fatalf("ordinary access: %v %s", err, out)
	}
}

// Nothing outside the project, the private /tmp and caches can be written,
// including through a symlink planted in the project.
func TestBwrapWritesAndSymlinkEscapes(t *testing.T) {
	f := newFixture(t, "bwrap")
	victim := filepath.Join(f.outside, "victim.txt")
	for _, script := range []string{
		"echo pwned > " + victim, "ln -s " + victim + " v && echo pwned > v", "ln -s " + f.outside + " d && echo pwned > d/victim.txt",
		"cp main.go " + f.outside + "/copied", "echo pwned >> ~/.bashrc", "touch /usr/local/pwned", "rm -f " + victim,
	} {
		_, _ = f.run(t, script)
	}
	if b, _ := os.ReadFile(victim); string(b) != "original\n" {
		t.Fatalf("victim changed: %q", b)
	}
	if _, err := os.Stat(filepath.Join(f.outside, "copied")); err == nil {
		t.Fatal("wrote outside the project")
	}
	if b, _ := os.ReadFile(filepath.Join(f.home, ".bashrc")); strings.Contains(string(b), "pwned") {
		t.Fatal("changed the home directory")
	}
	// /tmp is private and survives between commands of a session.
	if _, err := f.run(t, "echo x > /tmp/keep"); err != nil {
		t.Fatal(err)
	}
	if out, _ := f.run(t, "cat /tmp/keep"); strings.TrimSpace(out) != "x" {
		t.Fatalf("session /tmp: %q", out)
	}
}

// Git hooks and configuration cannot be changed; ordinary git works.
func TestBwrapGitMetadata(t *testing.T) {
	f := newFixture(t, "bwrap")
	for _, script := range []string{
		"printf '#!/bin/sh\\necho pwned > " + f.outside + "/hook\\n' > .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit",
		"git config core.hooksPath .h", "git config core.fsmonitor 'touch " + f.outside + "/fsmon'", "echo '[core] fsmonitor = x' >> .git/config",
		"mkdir -p .git/info && echo '* filter=x' > .git/info/attributes",
	} {
		_, _ = f.run(t, script)
	}
	if _, err := os.Stat(filepath.Join(f.root, ".git/hooks/pre-commit")); err == nil {
		t.Error("hook planted")
	}
	cfg, _ := os.ReadFile(filepath.Join(f.root, ".git/config"))
	if strings.Contains(string(cfg), "hooksPath") || strings.Contains(string(cfg), "fsmonitor") {
		t.Errorf("git config changed:\n%s", cfg)
	}
	out, err := f.run(t, "echo y >> main.go && git add main.go && git commit -q -m change && git log --oneline | wc -l && git status --short")
	if err != nil || !strings.Contains(out, "2") {
		t.Fatalf("ordinary git: %v %s", err, out)
	}
}

// No network, however it is attempted; allowed hosts only via the proxy.
func TestBwrapNetworkEgress(t *testing.T) {
	f := newFixture(t, "bwrap")
	for _, script := range []string{
		"curl -sS -m 5 https://example.com", "curl -sS -m 5 --noproxy '*' https://1.1.1.1", "exec 3<>/dev/tcp/1.1.1.1/80 && echo CONN''ECTED",
		"python3 -c \"import socket; s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.sendto(b'x', ('1.1.1.1', 53)); print('UDP' + '-SENT')\"",
		"getent hosts example.com && echo RES''OLVED",
	} {
		out, err := f.run(t, script)
		if err == nil || strings.Contains(out, "CONNECTED") || strings.Contains(out, "UDP-SENT") || strings.Contains(out, "RESOLVED") || strings.Contains(out, "<html") {
			t.Errorf("%q reached the network: %v %s", script, err, out)
		}
	}
	// With a host allowed, other hosts are refused by the proxy, and a
	// client that ignores the proxy still has no network.
	out, _ := f.run(t, "curl -s -o /dev/null -w 'connect=%{http_connect}' -m 5 https://not-allowed.example; echo; curl -sS -m 5 --noproxy '*' https://1.1.1.1 || echo direct-failed", "example.com")
	if !strings.Contains(out, "connect=403") || !strings.Contains(out, "direct-failed") {
		t.Fatalf("proxy policy: %s", out)
	}
	// The proxy never reaches this machine or private networks.
	out, _ = f.run(t, "curl -s -m 5 -o /dev/null -w '%{http_code}' --noproxy '' --proxy http://127.0.0.1:3128 http://localhost:1/", "localhost")
	if !strings.Contains(out, "403") {
		t.Fatalf("loopback through the proxy: %s", out)
	}
	if os.Getenv("BOUNDEDCODE_NET_TESTS") == "" {
		t.Log("allowed-host success not tested (set BOUNDEDCODE_NET_TESTS=1, needs the internet)")
		return
	}
	if out, err := f.run(t, "curl -s -m 15 -o /dev/null -w '%{http_code}' https://example.com", "example.com"); err != nil || !strings.Contains(out, "200") {
		t.Fatalf("allowed host: %v %s", err, out)
	}
}

// Secret environment variables and agent sockets are not inherited.
func TestEnvironmentScrubbed(t *testing.T) {
	f := newFixture(t, "bwrap")
	out, err := f.run(t, "env")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"env-secret", "SSH_AUTH_SOCK", "FAKE_API_KEY"} {
		if strings.Contains(out, s) {
			t.Errorf("environment has %s", s)
		}
	}
	if !strings.Contains(out, "PATH=") || !strings.Contains(out, "GIT_AUTHOR_NAME=t") {
		t.Errorf("environment lost what commands need:\n%s", out)
	}
}

func processWith(marker string) bool {
	out, _ := exec.Command("pgrep", "-f", marker).Output()
	return strings.TrimSpace(string(out)) != ""
}

// Background processes end with the command; cancellation and timeouts
// end the whole tree.
func TestBwrapProcessTree(t *testing.T) {
	f := newFixture(t, "bwrap")
	marker := fmt.Sprintf("bcxmark%d", time.Now().UnixNano())
	script := "(exec -a " + marker + "a sleep 300) & setsid bash -c 'exec -a " + marker + "b sleep 300' & nohup bash -c 'exec -a " + marker + "c sleep 300' >/dev/null 2>&1 & disown; echo started"
	if out, err := f.run(t, script); err != nil || !strings.Contains(out, "started") {
		t.Fatalf("%v %s", err, out)
	}
	time.Sleep(300 * time.Millisecond)
	if processWith(marker) {
		t.Fatal("a background process survived the command")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c, err := f.box.Command(ctx, Spec{Argv: []string{"bash", "-c", "(exec -a " + marker + "d sleep 300) & exec -a " + marker + "e sleep 300"}, Dir: f.root})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	cancel()
	_ = c.Wait()
	if time.Since(start) > 5*time.Second {
		t.Fatal("cancel took too long")
	}
	time.Sleep(300 * time.Millisecond)
	if processWith(marker) {
		t.Fatal("a process survived cancellation")
	}
}

// Language-native builds work inside the sandbox without the network.
func TestBwrapOrdinaryBuilds(t *testing.T) {
	f := newFixture(t, "bwrap")
	for _, tool := range [][2]string{{"go", "go build ./... && go vet ./... && echo built"}, {"python3", "python3 -c 'print(\"built\")'"},
		{"node", "node -e 'console.log(\"built\")'"}} {
		if _, err := exec.LookPath(tool[0]); err != nil {
			continue
		}
		if out, err := f.run(t, tool[1]); err != nil || !strings.Contains(out, "built") {
			t.Errorf("%s: %v %s", tool[0], err, out)
		}
	}
}

// Landlock: the home directory and paths outside are confined; what it
// cannot do is reported, not claimed.
func TestLandlock(t *testing.T) {
	f := newFixture(t, "landlock")
	r := f.box.Report()
	if r.Level != LevelPartial || !strings.Contains(r.Filesystem, "NOT protected") || !strings.Contains(r.Network, "not confined") {
		t.Fatalf("report %+v", r)
	}
	for _, script := range []string{"cat " + f.home + "/.ssh/id_rsa", "cat ~/.aws/credentials", "ln -s " + f.home + "/.ssh/id_rsa l && cat l"} {
		if out, _ := f.run(t, script); strings.Contains(out, "secret") || strings.Contains(out, "PRIVATE") {
			t.Errorf("%q: %s", script, out)
		}
	}
	victim := filepath.Join(f.outside, "victim.txt")
	_, _ = f.run(t, "echo pwned > "+victim+"; ln -s "+victim+" v; echo pwned > v")
	if b, _ := os.ReadFile(victim); string(b) != "original\n" {
		t.Fatalf("victim changed: %q", b)
	}
	if out, err := f.run(t, "cat main.go && echo ok > new.txt && ls /usr/bin/env"); err != nil || !strings.Contains(out, "package main") {
		t.Fatalf("ordinary access: %v %s", err, out)
	}
}

func TestNoneAndOffReports(t *testing.T) {
	b := New(t.TempDir(), Config{Mode: "off"})
	if r := b.Report(); r.Level != LevelOff || r.Backend != "off" {
		t.Fatalf("%+v", r)
	}
	b = New(t.TempDir(), Config{Backend: "none"})
	if r := b.Report(); r.Level != LevelNone || !strings.Contains(r.Filesystem, "not confined") {
		t.Fatalf("%+v", r)
	}
}

// Package registries are reachable for dependency installs, other hosts
// are not (needs the internet: BOUNDEDCODE_NET_TESTS=1).
func TestBwrapRegistries(t *testing.T) {
	if os.Getenv("BOUNDEDCODE_NET_TESTS") == "" {
		t.Skip("set BOUNDEDCODE_NET_TESTS=1 (needs the internet)")
	}
	f := newFixture(t, "bwrap")
	f.box = New(f.root, Config{StateDir: f.state, Registries: true})
	run := func(script string, tools ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		c, err := f.box.Command(ctx, Spec{Argv: []string{"bash", "-c", script}, Dir: f.root, Tools: tools})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		err = c.Run()
		return out.String(), err
	}
	checks := [][]string{
		{"go", "GOMODCACHE=/tmp/mc GOFLAGS=-modcacherw go mod download -json golang.org/x/text@v0.14.0 | grep -c '\"Zip\"'", "1"},
	}
	if _, err := exec.LookPath("pip3"); err == nil {
		checks = append(checks, []string{"pip3", "pip3 download --no-deps --no-cache-dir -q -d /tmp/p six==1.16.0 && ls /tmp/p", "six-1.16.0"})
	}
	if _, err := exec.LookPath("npm"); err == nil {
		checks = append(checks, []string{"npm", "npm view --cache /tmp/npmc left-pad@1.3.0 version", "1.3.0"})
	}
	for _, c := range checks {
		if out, err := run(c[1], c[0]); err != nil || !strings.Contains(out, c[2]) {
			t.Errorf("%s: %v %s", c[0], err, out)
		}
	}
	if out, _ := run("curl -s -o /dev/null -w 'connect=%{http_connect}' -m 10 https://example.com", "go"); !strings.Contains(out, "connect=403") {
		t.Errorf("a host outside the registries: %s", out)
	}
}
