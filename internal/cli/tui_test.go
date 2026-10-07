package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/tui"
)

// newTestBackend returns a terminal-UI backend over an isolated home.
func newTestBackend(t *testing.T) *tuiBackend {
	t.Helper()
	t.Setenv("BOUNDEDCODE_HOME", t.TempDir())
	app := &App{Out: io.Discard, Err: io.Discard, Log: newLogger(io.Discard, false)}
	if err := app.load(); err != nil {
		t.Fatal(err)
	}
	be, closeBE, err := newTUIBackend(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeBE()
		app.close()
	})
	return be
}

func mustExec(t *testing.T, be *tuiBackend, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := be.Exec(context.Background(), args, &out); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "orders")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "add", "."},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestTUIBackendDrivesTheCLI(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	be := newTestBackend(t)
	ctx := context.Background()

	if be.Info(ctx).ConfigExists {
		t.Fatal("fresh home reports a config")
	}
	mustExec(t, be, "init")
	in := be.Info(ctx)
	if !in.ConfigExists || in.Err != "" {
		t.Fatalf("after init: %+v", in)
	}

	mustExec(t, be, "workspace", "create", "demo")
	repo := gitRepo(t)
	if out := mustExec(t, be, "workspace", "add", repo); !strings.Contains(out, "added orders") {
		t.Fatalf("workspace add output: %q", out)
	}
	ws, err := be.Workspaces(ctx)
	if err != nil || len(ws) != 1 || !ws[0].Current || len(ws[0].Repos) != 1 {
		t.Fatalf("workspaces = %+v, %v", ws, err)
	}
	if be.Info(ctx).CurrentWorkspace != "demo" {
		t.Fatal("current workspace not reported")
	}

	id, err := be.CreateTask(ctx, tui.CreateTaskRequest{Request: "add a README", Criteria: []string{"README exists"}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := be.Task(ctx, id)
	if err != nil || d.Task.OriginalRequest != "add a README" || len(d.Worktrees) != 1 {
		t.Fatalf("task = %+v, %v", d, err)
	}
	evs, err := be.Events(ctx, id, 0, 100)
	if err != nil || len(evs) == 0 || evs[0].Kind != "task.created" {
		t.Fatalf("events = %+v, %v", evs, err)
	}
	if ds, err := be.Diffs(ctx, id); err != nil || len(ds) != 0 {
		t.Fatalf("diffs of a new task = %+v, %v", ds, err)
	}

	mustExec(t, be, "task", "cancel", id)
	ts, err := be.Tasks(ctx, 10)
	if err != nil || len(ts) != 1 || ts[0].Status != task.StatusCancelled {
		t.Fatalf("tasks = %+v, %v", ts, err)
	}
	if s, err := be.Stats(ctx, 0); err != nil || s.Tasks != 1 {
		t.Fatalf("stats = %+v, %v", s, err)
	}
	mustExec(t, be, "task", "cleanup", id, "--delete-branch")
	if _, err := os.Stat(d.Worktrees[0].Path); !os.IsNotExist(err) {
		t.Fatalf("worktree not removed: %v", err)
	}
}

func TestTUIBackendExecReportsErrors(t *testing.T) {
	be := newTestBackend(t)
	var out bytes.Buffer
	if err := be.Exec(context.Background(), []string{"task", "status", "t-nope"}, &out); err == nil {
		t.Fatal("status of a missing task succeeded")
	}
	if err := be.Exec(context.Background(), []string{"no-such-command"}, &out); err == nil {
		t.Fatal("unknown command succeeded")
	}
}

func TestTUIBackendCommands(t *testing.T) {
	be := newTestBackend(t)
	paths := map[string]bool{}
	for _, c := range be.Commands() {
		paths[c.Path] = true
	}
	for _, want := range []string{"task run", "task create", "bench infra", "intel search", "serena setup", "runtime start", "doctor"} {
		if !paths[want] {
			t.Errorf("missing command %q", want)
		}
	}
	for _, not := range []string{"tui", "help", "completion", "task"} {
		if paths[not] {
			t.Errorf("unexpected command %q", not)
		}
	}
}

func TestTUIBackendRoutesQuestionsToThePrompter(t *testing.T) {
	be := newTestBackend(t)
	var asked []string
	be.SetPrompter(func(_ context.Context, title, body string) bool {
		asked = append(asked, title+": "+body)
		return title == "Confirm"
	})
	a, err := be.fork(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if a.approve == nil || a.confirmFn == nil {
		t.Fatal("hooks not installed")
	}
	if a.approve(context.Background(), frontier.Trigger{Code: "Z2", Reason: "stuck"}, "/p/packet.md", 1200) {
		t.Fatal("approval should follow the prompter's answer")
	}
	if !confirm(a, "Install now? [y/N] ") {
		t.Fatal("confirm should follow the prompter's answer")
	}
	if len(asked) != 2 || !strings.Contains(asked[0], "Frontier escalation Z2") || !strings.Contains(asked[0], "1200 tokens") ||
		asked[1] != "Confirm: Install now?" {
		t.Fatalf("asked = %q", asked)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestChatFlowProjectFollowUpAndApply(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	be := newTestBackend(t)
	ctx := context.Background()
	mustExec(t, be, "init")
	repo := gitRepo(t)
	sub := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Starting in a subdirectory registers the repository once.
	p, err := be.Project(ctx, sub)
	if err != nil || p.Root != repo || p.Workspace != "orders" || !p.Created {
		t.Fatalf("project = %+v, %v", p, err)
	}
	if p2, _ := be.Project(ctx, repo); p2.Workspace != "orders" || p2.Created {
		t.Fatalf("second lookup = %+v", p2)
	}
	if p3, _ := be.Project(ctx, t.TempDir()); p3.Root != "" {
		t.Fatalf("non-repository = %+v", p3)
	}

	// A first task, whose agent commits a change on its branch.
	id, err := be.CreateTask(ctx, tui.CreateTaskRequest{Workspace: "orders", Request: "add a README"})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := be.Task(ctx, id)
	wt := d.Worktrees[0].Path
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, wt, "add", "-A")
	gitOut(t, wt, "-c", "user.name=a", "-c", "user.email=a@a", "commit", "-qm", "readme")

	// A follow-up starts from that branch, not from HEAD.
	id2, err := be.CreateTask(ctx, tui.CreateTaskRequest{Workspace: "orders", Request: "extend the README", FromTask: id})
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := be.Task(ctx, id2)
	if d2.Worktrees[0].BaseCommit != gitOut(t, repo, "rev-parse", "agent/"+id) {
		t.Fatal("follow-up does not start from the previous task's branch")
	}
	if _, err := os.Stat(filepath.Join(d2.Worktrees[0].Path, "README.md")); err != nil {
		t.Fatal("follow-up worktree lacks the previous task's change")
	}

	// Applying needs a completed task, or --force.
	var out bytes.Buffer
	if err := be.Exec(ctx, []string{"task", "apply", id}, &out); err == nil || !strings.Contains(err.Error(), "not a verified merge candidate") {
		t.Fatalf("apply of an unfinished task: %v", err)
	}
	// A dirty checkout is refused and left alone.
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main // edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := be.Exec(ctx, []string{"task", "apply", id, "--force"}, &out); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("apply into a dirty checkout: %v", err)
	}
	gitOut(t, repo, "checkout", "--", "main.go")
	mustExec(t, be, "task", "apply", id, "--force")
	if staged := gitOut(t, repo, "diff", "--cached", "--name-only"); staged != "README.md" {
		t.Fatalf("staged = %q", staged)
	}
	gitOut(t, repo, "reset", "-q", "--hard")
	gitOut(t, repo, "config", "user.name", "Dev")
	gitOut(t, repo, "config", "user.email", "dev@example.com")
	mustExec(t, be, "task", "apply", id, "--force", "--commit")
	if msg := gitOut(t, repo, "log", "-1", "--format=%s%n%b"); !strings.Contains(msg, "add a README") || !strings.Contains(msg, id) {
		t.Fatalf("commit message = %q", msg)
	}
}

func TestSetupCheckAndConfigStep(t *testing.T) {
	be := newTestBackend(t)
	ctx := context.Background()
	steps := be.Setup(ctx)
	names := []string{}
	for _, s := range steps {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "config,tools,inference,model,sandbox" {
		t.Fatalf("steps = %v", names)
	}
	if steps[0].OK {
		t.Fatal("fresh home reports a configuration")
	}
	out := mustExec(t, be, "setup", "--only", "config")
	if !strings.Contains(out, "wrote") || !be.Setup(ctx)[0].OK {
		t.Fatalf("config step: %q", out)
	}
	if !be.Info(ctx).ConfigExists {
		t.Fatal("config not visible after setup")
	}
	// Steps that download or build ask first; declining skips them.
	be.SetPrompter(func(context.Context, string, string) bool { return false })
	var buf bytes.Buffer
	err := be.Exec(ctx, []string{"setup", "--only", "model"}, &buf)
	if err == nil || !strings.Contains(buf.String(), "skipped") {
		t.Fatalf("declined model step: %v\n%s", err, buf.String())
	}
	// A misspelt step is an error, not a silent no-op; --force needs named
	// steps and never rewrites the configuration.
	for args, want := range map[string]string{
		"setup --only tool":            `unknown setup step "tool"`,
		"setup --force":                "--force needs --only",
		"setup --only config --force":  "does not rewrite the configuration",
		"setup --only sandbox,x --yes": `unknown setup step "x"`,
	} {
		buf.Reset()
		err := be.Exec(ctx, strings.Fields(args), &buf)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", args, err, want)
		}
	}
}

func TestEmbeddedSetupAssets(t *testing.T) {
	for _, f := range []string{"scripts/install-deps.sh", "scripts/build-llama-cpp.sh", "scripts/fetch-model.sh"} {
		if _, err := boundedcode.SetupScripts.ReadFile(f); err != nil {
			t.Errorf("%s not embedded: %v", f, err)
		}
	}
	dir := t.TempDir()
	if err := extract(boundedcode.SandboxContext, dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"Dockerfile", "python/pyproject.toml", "python/uv.lock", "python/src/bc_openhands/__init__.py", "python/src/bc_openhands/main.py"} {
		if _, err := os.Stat(filepath.Join(dir, "adapters", "openhands", f)); err != nil {
			t.Errorf("sandbox context lacks %s", f)
		}
	}
}
