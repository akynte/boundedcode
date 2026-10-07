package serena

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/repointel"
)

// These tests drive the real Serena v1.7.0. They run when it is installed
// (BOUNDEDCODE_SERENA, or the install made by `boundedcode serena setup`)
// and are skipped otherwise.

func realSerena(t *testing.T) string {
	t.Helper()
	exe := os.Getenv("BOUNDEDCODE_SERENA")
	if exe == "" {
		if p, err := config.DefaultPaths(); err == nil {
			exe = DefaultExecutable(p.Data)
		}
	}
	if _, err := os.Stat(exe); err != nil {
		t.Skip("serena not installed (run `boundedcode serena setup`, or set BOUNDEDCODE_SERENA)")
	}
	if testing.Short() {
		t.Skip("starts language servers")
	}
	return exe
}

// fixtureRepo copies benchmarks/fixtures/symbol-nav/<name> into a fresh git
// repository and returns its path.
func fixtureRepo(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(file), "..", "..", "..", "benchmarks", "fixtures", "symbol-nav", name)
	dst := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("cp", "-r", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	git(t, dst, "init", "-q", "-b", "main")
	git(t, dst, "add", "-A")
	git(t, dst, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "init")
	return dst
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

func newRealManager(t *testing.T, exe string) *Manager {
	t.Helper()
	m := &Manager{Executable: exe, Root: filepath.Join(t.TempDir(), "serena"), LogDir: t.TempDir(),
		MaxInstances: 2, CallTimeout: 60 * time.Second, StartupTimeout: 120 * time.Second}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func names(syms []repointel.Symbol) []string {
	var out []string
	for _, s := range syms {
		out = append(out, s.NamePath+"@"+s.File)
	}
	slices.Sort(out)
	return out
}

// one returns the single symbol of a FindSymbol result.
func one(t *testing.T) func([]repointel.Symbol, error) repointel.Symbol {
	return func(syms []repointel.Symbol, err error) repointel.Symbol {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if len(syms) != 1 {
			t.Fatalf("want one symbol, got %v", names(syms))
		}
		return syms[0]
	}
}

func TestRealVersion(t *testing.T) {
	exe := realSerena(t)
	inst, err := Detect(context.Background(), exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(inst); err != nil {
		t.Fatalf("%+v: %v", inst, err)
	}
}

func TestGoNavigation(t *testing.T) {
	exe := realSerena(t)
	repo := fixtureRepo(t, "billing-service")
	nav := &Navigator{M: newRealManager(t, exe)}
	ctx := context.Background()

	cp := one(t)(nav.FindSymbol(ctx, repo, "CreatePayment", repointel.FindOptions{IncludeBody: true}))
	if cp.File != "internal/payment/service.go" || cp.StartLine != 31 || !strings.Contains(cp.Body, "s.repo.Insert(ctx, p)") {
		t.Fatalf("CreatePayment: %+v", cp)
	}
	refs, err := nav.References(ctx, repo, cp)
	if err != nil {
		t.Fatal(err)
	}
	var where []string
	for _, r := range refs {
		where = append(where, r.File+":"+r.Symbol)
	}
	slices.Sort(where)
	if !slices.Equal(where, []string{"internal/api/handler.go:Create", "internal/payment/service_test.go:TestCreatePayment",
		"internal/payment/service_test.go:TestCreatePayment"}) {
		t.Fatalf("references of CreatePayment: %v", where)
	}

	repo1 := one(t)(nav.FindSymbol(ctx, repo, "PaymentRepository", repointel.FindOptions{}))
	impls, err := nav.Implementations(ctx, repo, repo1)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(impls); !slices.Equal(got, []string{"MemoryRepository@internal/payment/memory.go", "PostgresRepository@internal/payment/postgres.go"}) {
		t.Fatalf("implementations: %v", got)
	}

	// Insert on the interface is called by CreatePayment only; the audit
	// package's same-named method must not appear.
	ins := one(t)(nav.FindSymbol(ctx, repo, "PaymentRepository/Insert", repointel.FindOptions{}))
	refs, err = nav.References(ctx, repo, ins)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].File != "internal/payment/service.go" || refs[0].Symbol != "CreatePayment" || refs[0].Line != 36 {
		t.Fatalf("references of PaymentRepository.Insert: %+v", refs)
	}
	if strings.Contains(git(t, repo, "status", "--porcelain", "--ignored"), ".serena") {
		t.Fatal("serena wrote into the repository")
	}
}

func TestTypeScriptNavigation(t *testing.T) {
	exe := realSerena(t)
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	repo := fixtureRepo(t, "web-checkout")
	m := newRealManager(t, exe)
	if err := InstallLanguageServers(context.Background(), m, []string{"typescript"}); err != nil {
		t.Fatal(err)
	}
	nav := &Navigator{M: m}
	ctx := context.Background()

	req := one(t)(nav.FindSymbol(ctx, repo, "PaymentRequest", repointel.FindOptions{}))
	if req.Kind != "Interface" || req.File != "src/types/payment.ts" {
		t.Fatalf("PaymentRequest: %+v", req)
	}
	refs, err := nav.References(ctx, repo, req)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]bool{}
	for _, r := range refs {
		files[r.File] = true
	}
	for _, f := range []string{"src/services/paymentService.ts", "src/api/checkoutController.ts", "src/gateways/cardGateway.ts"} {
		if !files[f] {
			t.Errorf("PaymentRequest not referenced from %s: %+v", f, refs)
		}
	}
	gw := one(t)(nav.FindSymbol(ctx, repo, "PaymentGateway", repointel.FindOptions{}))
	impls, err := nav.Implementations(ctx, repo, gw)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(impls); !slices.Equal(got, []string{"CardGateway@src/gateways/cardGateway.ts", "MockGateway@src/gateways/mockGateway.ts"}) {
		t.Fatalf("implementations: %v", got)
	}
	sp := one(t)(nav.FindSymbol(ctx, repo, "submitPayment", repointel.FindOptions{IncludeBody: true}))
	refs, err = nav.References(ctx, repo, sp)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range refs {
		found = found || (r.File == "src/api/checkoutController.ts" && strings.HasPrefix(r.Symbol, "CheckoutController/handle"))
	}
	if !found {
		t.Fatalf("submitPayment not referenced from CheckoutController.handle: %+v", refs)
	}
}

const replicaRepo = `package payment

import "context"

// ReplicaRepository forwards to a primary repository.
type ReplicaRepository struct{ Primary PaymentRepository }

func (r ReplicaRepository) Insert(ctx context.Context, p Payment) error { return r.Primary.Insert(ctx, p) }

func (r ReplicaRepository) Get(ctx context.Context, id string) (Payment, error) { return r.Primary.Get(ctx, id) }
`

// TestWorktreeIsolation: the instance for a task worktree sees the task's
// edits (including ones made after it started), and the primary checkout's
// instance does not.
func TestWorktreeIsolation(t *testing.T) {
	exe := realSerena(t)
	repo := fixtureRepo(t, "billing-service")
	wt := filepath.Join(t.TempDir(), "TASK-1842", "billing-service")
	git(t, repo, "worktree", "add", "-q", "-b", "agent/TASK-1842", wt)
	if err := os.WriteFile(filepath.Join(wt, "internal/payment/replica.go"), []byte(replicaRepo), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newRealManager(t, exe)
	nav := &Navigator{M: m}
	ctx := context.Background()
	implsAt := func(root string) []string {
		iface := one(t)(nav.FindSymbol(ctx, root, "PaymentRepository", repointel.FindOptions{}))
		impls, err := nav.Implementations(ctx, root, iface)
		if err != nil {
			t.Fatal(err)
		}
		return names(impls)
	}
	if got := implsAt(wt); len(got) != 3 || !slices.Contains(got, "ReplicaRepository@internal/payment/replica.go") {
		t.Fatalf("worktree implementations: %v", got)
	}
	if got := implsAt(repo); len(got) != 2 {
		t.Fatalf("primary checkout must not see the task's edit: %v", got)
	}
	if n := len(m.Instances()); n != 2 {
		t.Fatalf("want one instance per checkout, got %d", n)
	}

	// Edit after the instance started: the next query must not be stale.
	if err := os.WriteFile(filepath.Join(wt, "internal/payment/archive.go"),
		[]byte(strings.ReplaceAll(replicaRepo, "ReplicaRepository", "ArchiveRepository")), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := implsAt(wt); len(got) != 4 {
		t.Fatalf("stale result after a live edit: %v", got)
	}
	// Renaming a method in the worktree: find_symbol sees the new name.
	b, _ := os.ReadFile(filepath.Join(wt, "internal/payment/service.go"))
	if err := os.WriteFile(filepath.Join(wt, "internal/payment/service.go"),
		[]byte(strings.ReplaceAll(string(b), "func (s *Service) CreatePayment(", "func (s *Service) CreatePaymentV2(")), 0o644); err != nil {
		t.Fatal(err)
	}
	one(t)(nav.FindSymbol(ctx, wt, "CreatePaymentV2", repointel.FindOptions{}))
	if st := git(t, wt, "status", "--porcelain", "--ignored"); strings.Contains(st, ".serena") {
		t.Fatalf("serena wrote into the worktree: %s", st)
	}
}

// TestRepoConfigIgnored: a repository cannot make Serena run commands via
// its own .serena/project.yml (activation_command).
func TestRepoConfigIgnored(t *testing.T) {
	exe := realSerena(t)
	repo := fixtureRepo(t, "billing-service")
	marker := filepath.Join(t.TempDir(), "pwned")
	evil := "project_name: evil\nlanguage_servers: [go]\nactivation_command: touch " + marker + "\n"
	if err := os.MkdirAll(filepath.Join(repo, ".serena"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".serena", "project.yml"), []byte(evil), 0o644); err != nil {
		t.Fatal(err)
	}
	nav := &Navigator{M: newRealManager(t, exe)}
	one(t)(nav.FindSymbol(context.Background(), repo, "CreatePayment", repointel.FindOptions{}))
	time.Sleep(time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("repository activation_command was executed")
	}
}

// TestMultiRepoSelection: each repository's questions go to its own
// instance, also when the pool is smaller than the number of repositories.
func TestMultiRepoSelection(t *testing.T) {
	exe := realSerena(t)
	billing := fixtureRepo(t, "billing-service")
	other := fixtureRepo(t, "billing-service")
	// Make the second repo distinguishable.
	b, _ := os.ReadFile(filepath.Join(other, "internal/payment/service.go"))
	_ = os.WriteFile(filepath.Join(other, "internal/payment/service.go"), []byte(strings.ReplaceAll(string(b), "CreatePayment(", "CreateLedgerEntry(")), 0o644)
	m := newRealManager(t, exe)
	m.MaxInstances = 1
	nav := &Navigator{M: m}
	ctx := context.Background()
	for range 2 {
		if s, err := nav.FindSymbol(ctx, billing, "CreatePayment", repointel.FindOptions{}); err != nil || len(s) != 1 {
			t.Fatalf("billing: %v %v", s, err)
		}
		if s, err := nav.FindSymbol(ctx, other, "CreatePayment", repointel.FindOptions{}); err != nil || len(s) != 0 {
			t.Fatalf("other repo answered from the wrong instance: %v %v", names(s), err)
		}
		if s, err := nav.FindSymbol(ctx, other, "CreateLedgerEntry", repointel.FindOptions{}); err != nil || len(s) != 1 {
			t.Fatalf("other: %v %v", s, err)
		}
	}
	if st := m.Stats(); st.Evictions < 2 || len(m.Instances()) != 1 {
		t.Fatalf("pool limit not enforced: %+v, %d instances", st, len(m.Instances()))
	}
}

// TestCrashRecovery: a killed instance makes the in-flight question fail
// (callers fall back) and the next question restarts it.
func TestCrashRecovery(t *testing.T) {
	exe := realSerena(t)
	repo := fixtureRepo(t, "billing-service")
	m := newRealManager(t, exe)
	nav := &Navigator{M: m}
	ctx := context.Background()
	one(t)(nav.FindSymbol(ctx, repo, "CreatePayment", repointel.FindOptions{}))
	pid := m.Instances()[0].PID
	killProcessGroup(pid)
	time.Sleep(300 * time.Millisecond)
	if err := m.Health(ctx, repo); err == nil {
		t.Fatal("health check passed on a killed instance")
	}
	one(t)(nav.FindSymbol(ctx, repo, "CreatePayment", repointel.FindOptions{}))
	if st := m.Stats(); st.Restarts != 1 {
		t.Fatalf("stats: %+v", st)
	}
	if newPID := m.Instances()[0].PID; newPID == pid {
		t.Fatal("instance was not restarted")
	}
}

// TestCancellationLeavesNoProcesses: cancelling a task's context stops its
// instance, including the language server, and Close leaves nothing behind.
func TestCancellationLeavesNoProcesses(t *testing.T) {
	exe := realSerena(t)
	repo := fixtureRepo(t, "billing-service")
	m := newRealManager(t, exe)
	nav := &Navigator{M: m}
	one(t)(nav.FindSymbol(context.Background(), repo, "CreatePayment", repointel.FindOptions{}))
	tag := instanceTag(instanceKey(canonical(repo)))
	if n := len(taggedProcesses(func(s string) bool { return s == tag })); n < 2 {
		t.Fatalf("expected serena plus gopls tagged %s, found %d", tag, n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := nav.FindSymbol(ctx, repo, "PaymentRepository", repointel.FindOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	waitTagGone(t, tag)

	one(t)(nav.FindSymbol(context.Background(), repo, "CreatePayment", repointel.FindOptions{}))
	_ = m.Close()
	waitTagGone(t, tag)
}

func waitTagGone(t *testing.T, tag string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(taggedProcesses(func(s string) bool { return s == tag })) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("processes tagged %s still running: %v", tag, taggedProcesses(func(s string) bool { return s == tag }))
}

// TestResumeAfterRestart: a new control plane (fresh Manager, same instance
// root) serves the same worktree again, also after the worktree directory
// was deleted and recreated from its branch.
func TestResumeAfterRestart(t *testing.T) {
	exe := realSerena(t)
	repo := fixtureRepo(t, "billing-service")
	wt := filepath.Join(t.TempDir(), "TASK-7", "billing-service")
	git(t, repo, "worktree", "add", "-q", "-b", "agent/TASK-7", wt)
	if err := os.WriteFile(filepath.Join(wt, "internal/payment/replica.go"), []byte(replicaRepo), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, wt, "add", "-A")
	git(t, wt, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "attempt 1")
	root := filepath.Join(t.TempDir(), "serena")
	query := func() []string {
		m := &Manager{Executable: exe, Root: root, LogDir: t.TempDir(), CallTimeout: time.Minute, StartupTimeout: 2 * time.Minute}
		defer m.Close()
		nav := &Navigator{M: m}
		iface := one(t)(nav.FindSymbol(context.Background(), wt, "PaymentRepository", repointel.FindOptions{}))
		impls, err := nav.Implementations(context.Background(), wt, iface)
		if err != nil {
			t.Fatal(err)
		}
		return names(impls)
	}
	if got := query(); len(got) != 3 {
		t.Fatalf("before restart: %v", got)
	}
	if got := query(); len(got) != 3 { // "daemon restart"
		t.Fatalf("after restart: %v", got)
	}
	// Worktree recreation from the task branch (as the runner does on resume).
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "worktree", "prune")
	git(t, repo, "worktree", "add", "-q", wt, "agent/TASK-7")
	if got := query(); len(got) != 3 {
		t.Fatalf("after worktree recreation: %v", got)
	}
}
