package serena

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/jsonrpc"
	"github.com/akynte/boundedcode/internal/repointel"
)

// The test binary doubles as a fake Serena (and as the python of its
// "virtual environment") when FAKE_SERENA is set. Behaviour is selected
// with environment variables so each test can build the installation it
// needs: FAKE_VERSION, FAKE_PKG_VERSION, FAKE_LICENSE, FAKE_START (ok|exit|
// wrong-project) and the symbol names HANG and CRASH.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_SERENA") == "1" {
		os.Exit(fakeMain())
	}
	os.Exit(m.Run())
}

func fakeMain() int {
	if filepath.Base(os.Args[0]) == "python" {
		lic := []string{}
		if p := os.Getenv("FAKE_LICENSE"); p != "" {
			lic = append(lic, p)
		}
		b, _ := json.Marshal(map[string]any{"version": os.Getenv("FAKE_PKG_VERSION"), "license": "MIT", "license_files": lic})
		fmt.Println(string(b))
		return 0
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("Serena %s-0123abcd\n", os.Getenv("FAKE_VERSION"))
		return 0
	}
	if len(os.Args) < 2 || os.Args[1] != "start-mcp-server" {
		return 2
	}
	switch os.Getenv("FAKE_START") {
	case "exit":
		fmt.Fprintln(os.Stderr, "fatal: language server failed")
		return 1
	}
	project := ""
	for i, a := range os.Args {
		if a == "--project" && i+1 < len(os.Args) {
			project = os.Args[i+1]
		}
	}
	name := "bc-" + filepath.Base(os.Getenv("SERENA_HOME"))
	if os.Getenv("FAKE_START") == "wrong-project" {
		name = "some-other-project"
	}
	p := jsonrpc.New(os.Stdin, os.Stdout)
	p.Handle("initialize", func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"protocolVersion": "2025-06-18", "serverInfo": map[string]any{"name": "Serena", "version": "1.28.1"}}, nil
	})
	p.Handle("tools/list", func(context.Context, json.RawMessage) (any, error) { return map[string]any{"tools": []any{}}, nil })
	text := func(s string, isErr bool) map[string]any {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}, "isError": isErr}
	}
	p.Handle("tools/call", func(_ context.Context, raw json.RawMessage) (any, error) {
		var req struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(raw, &req)
		switch req.Name {
		case "get_current_config":
			return text("Current configuration:\nSerena version: 1.7.0-0123abcd\nActive project: "+name+"\nLanguage server status: ready\n", false), nil
		case "find_symbol":
			pat, _ := req.Arguments["name_path_pattern"].(string)
			switch pat {
			case "HANG":
				select {}
			case "CRASH":
				os.Exit(3)
			case "MISSING":
				return text("Error executing tool find_symbol: ValueError: No symbol", true), nil
			}
			b, _ := json.Marshal([]map[string]any{{"name_path": pat, "kind": "Function", "relative_path": "main.go",
				"body_location": map[string]int{"start_line": 9, "end_line": 11}, "body": "// root=" + project}})
			return text(string(b), false), nil
		}
		return text("Unknown tool: "+req.Name, true), nil
	})
	p.Serve()
	return 0
}

// fakeInstall creates bin/serena and bin/python pointing at the test binary.
func fakeInstall(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"serena", "python"} {
		if err := os.Symlink(self, filepath.Join(bin, n)); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(bin, "serena")
}

// upstreamLicense is our copy of Serena v1.7.0's LICENSE.
func upstreamLicense(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "LICENSES", "upstream", "serena.txt")
}

func setFake(t *testing.T, kv ...string) {
	t.Helper()
	t.Setenv("FAKE_SERENA", "1")
	for i := 0; i+1 < len(kv); i += 2 {
		t.Setenv(kv[i], kv[i+1])
	}
}

func TestParseCLIVersion(t *testing.T) {
	for in, want := range map[string]string{
		"Serena 1.7.0-dec97a4a\n":       "1.7.0",
		"Serena 1.7.0-dec97a4a-dirty\n": "1.7.0",
		"Serena 1.7.0\n":                "1.7.0",
		"Serena 2.0.0.dev0\n":           "2.0.0.dev0",
		"warning: x\nSerena 2.1.0\n":    "2.1.0",
	} {
		got, err := ParseCLIVersion(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "serena-agent 1.7.0", "Serena version unknown"} {
		if _, err := ParseCLIVersion(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestLicenseCopyMatchesPin(t *testing.T) {
	inst := Installation{CLIVersion: RequiredVersion, PackageVersion: RequiredVersion}
	b, err := os.ReadFile(upstreamLicense(t))
	if err != nil {
		t.Fatal(err)
	}
	inst.LicenseSHA256 = sha256hex(b)
	if err := Check(inst); err != nil {
		t.Fatalf("LICENSES/upstream/serena.txt is not the pinned v%s license: %v", RequiredVersion, err)
	}
	if !strings.HasPrefix(string(b), "MIT License") {
		t.Fatal("expected the MIT license text")
	}
}

func TestDetectAndCheck(t *testing.T) {
	exe := fakeInstall(t)
	other := filepath.Join(t.TempDir(), "LICENSE")
	_ = os.WriteFile(other, []byte("GNU GENERAL PUBLIC LICENSE Version 3"), 0o644)
	cases := []struct {
		name, cli, pkg, license string
		ok                      bool
		reason                  string
	}{
		{"pinned release", "1.7.0", "1.7.0", upstreamLicense(t), true, ""},
		{"v2 rejected", "2.0.0", "2.0.0", other, false, "GPL-3.0-or-later"},
		{"v2 dev build", "2.0.0.dev0", "2.0.0.dev0", other, false, "pinned to the MIT-licensed Serena v1.7.0"},
		{"older release", "1.6.1", "1.6.1", upstreamLicense(t), false, "pinned"},
		{"metadata disagrees", "1.7.0", "2.0.0", upstreamLicense(t), false, "disagrees"},
		{"license replaced", "1.7.0", "1.7.0", other, false, "does not match the MIT license"},
		{"license missing", "1.7.0", "1.7.0", "", false, "sha256 missing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setFake(t, "FAKE_VERSION", c.cli, "FAKE_PKG_VERSION", c.pkg, "FAKE_LICENSE", c.license)
			inst, err := Detect(context.Background(), exe)
			if err != nil {
				t.Fatal(err)
			}
			err = Check(inst)
			if c.ok != (err == nil) {
				t.Fatalf("%+v: ok=%v err=%v", inst, c.ok, err)
			}
			var ue *UnsupportedError
			if err != nil && (!errors.As(err, &ue) || !strings.Contains(err.Error(), c.reason)) {
				t.Fatalf("error %q does not explain %q", err, c.reason)
			}
		})
	}
}

func TestDetectMissingAndInvalid(t *testing.T) {
	if _, err := Detect(context.Background(), filepath.Join(t.TempDir(), "serena")); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("want ErrNotInstalled, got %v", err)
	}
	// An executable that prints something else is not mistaken for Serena.
	bad := filepath.Join(t.TempDir(), "serena")
	_ = os.WriteFile(bad, []byte("#!/bin/sh\necho 'serena-agent version 1.7.0'\n"), 0o755)
	if _, err := Detect(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "unrecognised") {
		t.Fatalf("want unrecognised-output error, got %v", err)
	}
	// No package metadata next to the executable: unsupported, not assumed.
	inst := Installation{CLIVersion: "1.7.0", Executable: bad}
	if err := Check(inst); err == nil || !strings.Contains(err.Error(), "package metadata") {
		t.Fatalf("want metadata error, got %v", err)
	}
}

func fakeManager(t *testing.T, exe string) *Manager {
	t.Helper()
	m := &Manager{Executable: exe, Root: filepath.Join(t.TempDir(), "serena"), LogDir: t.TempDir(),
		MaxInstances: 2, StartupTimeout: 10 * time.Second, CallTimeout: 2 * time.Second, SkipVersionCheck: true}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func proj(t *testing.T) Project {
	t.Helper()
	return Project{Root: t.TempDir(), Languages: []string{"go"}}
}

func find(t *testing.T, m *Manager, p Project, name string) (string, error) {
	t.Helper()
	out, err := m.Call(context.Background(), p, "find_symbol", map[string]any{"name_path_pattern": name})
	return out, err
}

func TestLifecycle(t *testing.T) {
	setFake(t)
	m := fakeManager(t, fakeInstall(t))
	p := proj(t)
	if err := m.Health(context.Background(), p.Root); err == nil {
		t.Fatal("health must fail before start")
	}
	out, err := find(t, m, p, "Foo")
	if err != nil || !strings.Contains(out, "root="+canonical(p.Root)) {
		t.Fatalf("call: %q %v", out, err)
	}
	if err := m.Health(context.Background(), p.Root); err != nil {
		t.Fatal(err)
	}
	pid := m.Instances()[0].PID
	// Reuse: a second call does not start another process.
	if _, err := find(t, m, p, "Bar"); err != nil || m.Instances()[0].PID != pid || m.Stats().Starts != 1 {
		t.Fatalf("instance not reused: %v %+v", err, m.Stats())
	}
	m.Stop(p.Root)
	if len(m.Instances()) != 0 || alive(pid) {
		t.Fatal("stop left the instance running")
	}
	if _, err := find(t, m, p, "Foo"); err != nil || m.Stats().Starts != 2 {
		t.Fatalf("restart after stop: %v %+v", err, m.Stats())
	}
	// Serena's own config never lands in the project directory.
	if ents, _ := os.ReadDir(p.Root); len(ents) != 0 {
		t.Fatalf("files written into the project: %v", ents)
	}
}

func TestToolErrorKeepsInstance(t *testing.T) {
	setFake(t)
	m := fakeManager(t, fakeInstall(t))
	p := proj(t)
	if _, err := find(t, m, p, "MISSING"); err == nil {
		t.Fatal("want tool error")
	}
	if len(m.Instances()) != 1 {
		t.Fatal("a tool-level error must not stop the instance")
	}
}

func TestTimeoutKillsHungInstance(t *testing.T) {
	setFake(t)
	m := fakeManager(t, fakeInstall(t))
	m.CallTimeout = 300 * time.Millisecond
	p := proj(t)
	if _, err := find(t, m, p, "Foo"); err != nil {
		t.Fatal(err)
	}
	pid := m.Instances()[0].PID
	t0 := time.Now()
	_, err := find(t, m, p, "HANG")
	if !errors.Is(err, repointel.ErrUnavailable) || time.Since(t0) > 2*time.Second {
		t.Fatalf("want bounded ErrUnavailable, got %v after %s", err, time.Since(t0))
	}
	waitDead(t, pid)
	if _, err := find(t, m, p, "Foo"); err != nil {
		t.Fatalf("no recovery after timeout: %v", err)
	}
	if st := m.Stats(); st.Timeouts != 1 || st.Starts != 2 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestCancellationStopsInstance(t *testing.T) {
	setFake(t)
	m := fakeManager(t, fakeInstall(t))
	m.CallTimeout = time.Minute
	p := proj(t)
	if _, err := find(t, m, p, "Foo"); err != nil {
		t.Fatal(err)
	}
	pid := m.Instances()[0].PID
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	t0 := time.Now()
	_, err := m.Call(ctx, p, "find_symbol", map[string]any{"name_path_pattern": "HANG"})
	if !errors.Is(err, context.Canceled) || time.Since(t0) > 2*time.Second {
		t.Fatalf("want prompt context.Canceled, got %v after %s", err, time.Since(t0))
	}
	waitDead(t, pid)
}

func TestCrashDuringCallFallsBackThenRestarts(t *testing.T) {
	setFake(t)
	m := fakeManager(t, fakeInstall(t))
	p := proj(t)
	if _, err := find(t, m, p, "CRASH"); !errors.Is(err, repointel.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if _, err := find(t, m, p, "Foo"); err != nil {
		t.Fatalf("no restart after crash: %v", err)
	}
}

func TestStartFailureBreaker(t *testing.T) {
	setFake(t, "FAKE_START", "exit")
	m := fakeManager(t, fakeInstall(t))
	p := proj(t)
	for range breakerThreshold {
		if _, err := find(t, m, p, "Foo"); !errors.Is(err, repointel.ErrUnavailable) {
			t.Fatalf("want ErrUnavailable, got %v", err)
		}
	}
	t0 := time.Now()
	_, err := find(t, m, p, "Foo")
	if !errors.Is(err, repointel.ErrUnavailable) || !strings.Contains(err.Error(), "retrying after") || time.Since(t0) > 100*time.Millisecond {
		t.Fatalf("breaker did not short-circuit: %v (%s)", err, time.Since(t0))
	}
	if st := m.Stats(); st.Starts != breakerThreshold {
		t.Fatalf("starts while the breaker is open: %+v", st)
	}
}

func TestWrongProjectRejected(t *testing.T) {
	setFake(t, "FAKE_START", "wrong-project")
	m := fakeManager(t, fakeInstall(t))
	if _, err := find(t, m, proj(t), "Foo"); err == nil || !strings.Contains(err.Error(), "serves project") {
		t.Fatalf("want project mismatch, got %v", err)
	}
}

func TestVersionGateBlocksStart(t *testing.T) {
	setFake(t, "FAKE_VERSION", "2.0.0", "FAKE_PKG_VERSION", "2.0.0")
	m := fakeManager(t, fakeInstall(t))
	m.SkipVersionCheck = false
	_, err := find(t, m, proj(t), "Foo")
	var ue *UnsupportedError
	if !errors.Is(err, repointel.ErrUnavailable) || !errors.As(err, &ue) || m.Stats().Starts != 0 {
		t.Fatalf("unsupported version must not start: %v %+v", err, m.Stats())
	}
}

func TestPoolEvictionAndIdleStop(t *testing.T) {
	setFake(t)
	m := fakeManager(t, fakeInstall(t))
	m.MaxInstances = 2
	a, b, c := proj(t), proj(t), proj(t)
	for _, p := range []Project{a, b, c} {
		if out, err := find(t, m, p, "X"); err != nil || !strings.Contains(out, canonical(p.Root)) {
			t.Fatalf("%q %v", out, err)
		}
	}
	if n := len(m.Instances()); n != 2 || m.Stats().Evictions != 1 {
		t.Fatalf("instances=%d stats=%+v", n, m.Stats())
	}
	for _, in := range m.Instances() {
		if in.Root == canonical(a.Root) {
			t.Fatal("least recently used instance was not evicted")
		}
	}
	m2 := fakeManager(t, fakeInstall(t))
	m2.IdleTimeout = 200 * time.Millisecond
	if _, err := find(t, m2, a, "X"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(m2.Instances()) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if len(m2.Instances()) != 0 || m2.Stats().IdleStops != 1 {
		t.Fatalf("idle instance not stopped: %+v", m2.Stats())
	}
}

func TestConcurrentEnsureStartsOnce(t *testing.T) {
	setFake(t)
	m := fakeManager(t, fakeInstall(t))
	p := proj(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			if _, err := find(t, m, p, "X"); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if st := m.Stats(); st.Starts != 1 {
		t.Fatalf("starts: %+v", st)
	}
}

func TestNoLanguagesIsUnavailable(t *testing.T) {
	setFake(t)
	m := fakeManager(t, fakeInstall(t))
	_, err := m.Call(context.Background(), Project{Root: t.TempDir()}, "find_symbol", map[string]any{})
	if !errors.Is(err, repointel.ErrUnavailable) || m.Stats().Starts != 0 {
		t.Fatalf("want ErrUnavailable without starting, got %v", err)
	}
}

func TestDecodeReferences(t *testing.T) {
	out := `{"internal/payment/service.go": {"Method": [{"name_path": "CreatePayment", "body_location": {"start_line": 30, "end_line": 39},
	"content_around_reference": "...  34:\tp := x\n  >  35:\tif err := s.repo.Insert(ctx, p); err != nil {\n...  36:\t\treturn"}]}}`
	refs, err := decodeReferences(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Line != 36 || refs[0].Symbol != "CreatePayment" || refs[0].Kind != "Method" ||
		refs[0].Snippet != "if err := s.repo.Insert(ctx, p); err != nil {" {
		t.Fatalf("%+v", refs)
	}
	if _, err := decodeReferences("The answer is too long (200000 characters)."); err == nil {
		t.Fatal("non-JSON output must be an error")
	}
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatalf("process %d still running", pid)
	}
}

// TestSweepOrphans: language servers left by a control plane that died are
// killed when an instance for the same root starts again.
func TestSweepOrphans(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	key := instanceKey("/some/worktree")
	start := func(owner string) *exec.Cmd {
		c := exec.Command("sleep", "60")
		c.Env = append(os.Environ(), instanceEnv+"="+key+":"+owner)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		return c
	}
	orphan := start("2147483646")            // owner pid that does not exist
	mine := start(strconv.Itoa(os.Getpid())) // owned by a live control plane
	// Start can return before the kernel has set up the new program's
	// environment, so /proc/<pid>/environ may still be empty: wait until
	// both processes are visible (a busy CI runner hits this window).
	deadline := time.Now().Add(5 * time.Second)
	for len(taggedProcesses(func(t string) bool { return strings.HasPrefix(t, key+":") })) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the test processes never showed their environment")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := sweepOrphans(key); n != 1 {
		t.Fatalf("swept %d processes, want 1", n)
	}
	_ = orphan.Wait()
	if !alive(mine.Process.Pid) {
		t.Fatal("a live control plane's process was killed")
	}
}
