package compat

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/xservice"
)

// The fixture is a three-repository shop (benchmarks/fixtures/contract-break):
// protos (shared .proto and generated Go code), payments (the gRPC server)
// and checkout (a client). payments and checkout vendor protos, so their own
// tests build against the vendored copy.
const fixture = "../../benchmarks/fixtures/contract-break"

type env struct {
	t     *testing.T
	ctx   context.Context
	root  string
	db    *store.Store
	gate  *Gate
	repos map[string]*Repo // name -> repo with worktree
	order []string
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitops.Run(context.Background(), dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// newEnv copies the fixture's repositories into git repositories, creates a
// task worktree for each (as the runner does) and a gate on sandbox none.
func newEnv(t *testing.T, names ...string) *env { t.Helper(); return newEnvP(t, nil, names...) }

// newEnvP is newEnv with prep run on each repository's files before its
// first commit.
func newEnvP(t *testing.T, prep func(name, dir string), names ...string) *env {
	t.Helper()
	return newEnvFrom(t, fixture, prep, names...)
}

// newEnvFrom is newEnvP for another fixture directory.
func newEnvFrom(t *testing.T, fixture string, prep func(name, dir string), names ...string) *env {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("needs the go toolchain")
	}
	if len(names) == 0 {
		names = []string{"checkout", "payments", "protos"}
	}
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	e := &env{t: t, ctx: ctx, root: root, db: s, repos: map[string]*Repo{}}
	for _, n := range names {
		src := filepath.Join(root, "repos", n)
		if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("cp", "-r", filepath.Join(fixture, n), src).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
		if prep != nil {
			prep(n, src)
		}
		git(t, src, "init", "-q", "-b", "main")
		git(t, src, "add", "-A")
		git(t, src, "commit", "-qm", "init")
		base := git(t, src, "rev-parse", "HEAD")
		wt := filepath.Join(root, "work", n)
		if err := gitops.EnsureWorktree(ctx, src, wt, "agent/t1", base); err != nil {
			t.Fatal(err)
		}
		e.repos[n] = &Repo{Name: n, Worktree: wt, Source: src, Base: base}
		e.order = append(e.order, n)
	}
	log := slog.New(slog.DiscardHandler)
	rec := telemetry.New(s.DB, log)
	cache := filepath.Join(root, "cache")
	e.gate = &Gate{DB: s.DB, Rec: rec, Log: log, Scratch: t.TempDir(),
		Verify: &verify.Engine{Sandbox: sandbox.None{}, CacheDir: cache, DB: s.DB, Rec: rec}}
	return e
}

// edit rewrites a file of a repository's worktree (old must occur).
func (e *env) edit(repo, file, old, new string) {
	e.t.Helper()
	p := filepath.Join(e.repos[repo].Worktree, file)
	b, err := os.ReadFile(p)
	if err != nil {
		e.t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		e.t.Fatalf("%s/%s does not contain %q", repo, file, old)
	}
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(b), old, new)), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// commit checkpoints every worktree, like the runner after an attempt.
func (e *env) commit(msg string) {
	e.t.Helper()
	for _, n := range e.order {
		if _, err := gitops.CommitAll(e.ctx, e.repos[n].Worktree, msg); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *env) list() []Repo {
	var out []Repo
	for _, n := range e.order {
		out = append(out, *e.repos[n])
	}
	return out
}

func (e *env) evaluate() Report {
	e.t.Helper()
	rep, err := e.gate.Evaluate(e.ctx, "t1", e.list(), nil)
	e.t.Log("\n" + strings.Join(rep.Lines(), "\n"))
	if err != nil {
		e.t.Fatalf("evaluate: %v\n%s", err, strings.Join(rep.Lines(), "\n"))
	}
	return rep
}

// countingSandbox counts the commands the gate runs.
type countingSandbox struct {
	sandbox.None
	argv []string
}

func (c *countingSandbox) Command(ctx context.Context, s sandbox.Spec) (*exec.Cmd, error) {
	c.argv = append(c.argv, strings.Join(s.Argv, " "))
	return c.None.Command(ctx, s)
}

func (c *countingSandbox) tests() int {
	n := 0
	for _, a := range c.argv {
		if strings.HasPrefix(a, "go test") {
			n++
		}
	}
	return n
}

func byKind(rep Report) map[string][]LinkResult {
	out := map[string][]LinkResult{}
	for _, l := range rep.Links {
		out[l.Kind+" "+l.From.Repo+"->"+l.To.Repo] = append(out[l.Kind+" "+l.From.Repo+"->"+l.To.Repo], l)
	}
	return out
}

// want asserts one link's result.
func want(t *testing.T, rep Report, key string, res Result, reason string) LinkResult {
	t.Helper()
	ls := byKind(rep)[key]
	if len(ls) != 1 {
		t.Fatalf("%s: %d links, want 1\n%s", key, len(ls), strings.Join(rep.Lines(), "\n"))
	}
	if ls[0].Result != res || !strings.Contains(ls[0].Reason, reason) {
		t.Fatalf("%s = %s (%s), want %s containing %q\n%s", key, ls[0].Result, ls[0].Reason, res, reason, strings.Join(rep.Lines(), "\n"))
	}
	return ls[0]
}

// addIdempotencyKey is a compatible contract change: a field added to
// ChargeRequest, the generated code regenerated, and the server using it.
func addIdempotencyKey(e *env) {
	e.edit("protos", "payments/v1/payments.proto", "  string currency = 3;\n}", "  string currency = 3;\n  string idempotency_key = 4;\n}")
	gen := func(repo, file string) {
		e.edit(repo, file, "\tCurrency    string\n}", "\tCurrency    string\n\tIdempotencyKey string\n}\n\nfunc (x *ChargeRequest) GetIdempotencyKey() string {\n\tif x != nil {\n\t\treturn x.IdempotencyKey\n\t}\n\treturn \"\"\n}")
	}
	gen("protos", "payments/v1/payments.pb.go")
	gen("payments", "vendor/example.com/shop/protos/payments/v1/payments.pb.go")
	e.edit("payments", "internal/server/server.go", "\ts.next++\n", "\tif id, ok := s.byKey[req.GetIdempotencyKey()]; ok && req.GetIdempotencyKey() != \"\" {\n\t\treturn &paymentsv1.ChargeResponse{PaymentId: id, Status: paymentsv1.Status_STATUS_CAPTURED}, nil\n\t}\n\ts.next++\n")
	e.edit("payments", "internal/server/server.go", "\ts.captured[id] = req.GetAmountCents()\n", "\ts.captured[id] = req.GetAmountCents()\n\tif s.byKey == nil {\n\t\ts.byKey = map[string]string{}\n\t}\n\ts.byKey[req.GetIdempotencyKey()] = id\n")
	e.edit("payments", "internal/server/server.go", "\tcaptured map[string]int64 // payment id -> amount in cents\n", "\tcaptured map[string]int64 // payment id -> amount in cents\n\tbyKey    map[string]string\n")
	e.edit("payments", "internal/server/server_test.go", "func TestChargeDeclines", `func TestChargeIsIdempotent(t *testing.T) {
	s := New()
	a, _ := s.Charge(context.Background(), &paymentsv1.ChargeRequest{OrderId: "o1", AmountCents: 5, Currency: "EUR", IdempotencyKey: "k"})
	b, _ := s.Charge(context.Background(), &paymentsv1.ChargeRequest{OrderId: "o1", AmountCents: 5, Currency: "EUR", IdempotencyKey: "k"})
	if a.GetPaymentId() != b.GetPaymentId() {
		t.Fatalf("%s != %s", a.GetPaymentId(), b.GetPaymentId())
	}
}

func TestChargeDeclines`)
}

// renameAmount is a breaking contract change: ChargeRequest.amount_cents
// renamed (same number) and regenerated; payments is updated, checkout is
// not. checkout's own tests still pass: they build against its vendored copy.
func renameAmount(e *env) {
	e.edit("protos", "payments/v1/payments.proto", "int64 amount_cents = 2;", "int64 amount_minor = 2;")
	for _, f := range []string{"protos/payments/v1/payments.pb.go", "payments/vendor/example.com/shop/protos/payments/v1/payments.pb.go"} {
		repo, file, _ := strings.Cut(f, "/")
		e.edit(repo, file, "AmountCents", "AmountMinor")
	}
	e.edit("payments", "internal/server/server.go", "AmountCents", "AmountMinor")
	e.edit("payments", "internal/server/server_test.go", "AmountCents", "AmountMinor")
}

func TestGateCompatibleChange(t *testing.T) {
	e := newEnv(t)
	addIdempotencyKey(e)
	e.commit("attempt 1")
	rep := e.evaluate()
	if rep.State() != "compatible" || len(rep.Links) != 5 || !rep.Clear() {
		t.Fatalf("state %s, %d links\n%s", rep.State(), len(rep.Links), strings.Join(rep.Lines(), "\n"))
	}
	l := want(t, rep, "grpc checkout->payments", Compatible, "exercised by passing checks")
	if len(l.Checks) < 2 || l.From.Commit == "" || l.To.Commit == "" || l.Commits["protos"] == "" {
		t.Fatalf("evidence incomplete: %+v", l)
	}
	for _, c := range l.Checks {
		if c.Purpose == "candidate" && (c.Composition["protos"] != e.head("protos") || !strings.Contains(c.Command, "-coverpkg=")) {
			t.Fatalf("candidate check not tied to the candidate commits: %+v", c)
		}
	}
	if !strings.Contains(strings.Join(l.Notes, " "), "idempotency_key") {
		t.Fatalf("notes %q", l.Notes)
	}
}

func (e *env) head(repo string) string { return git(e.t, e.repos[repo].Worktree, "rev-parse", "HEAD") }

// TestBreakMissedBySingleRepositoryTests is the fixture's point: every
// repository's own verification passes, and the gate finds the break.
func TestBreakMissedBySingleRepositoryTests(t *testing.T) {
	e := newEnv(t)
	renameAmount(e)
	e.commit("attempt 1")
	for _, n := range e.order {
		r := e.repos[n]
		res, err := e.gate.Verify.Run(e.ctx, verify.RepoTarget{Name: n, Worktree: r.Worktree, Base: r.Base, TaskID: "t1", Source: r.Source}, verify.Targeted)
		if err != nil || !res.Passed {
			t.Fatalf("single-repository verification of %s: passed=%v err=%v %+v", n, res.Passed, err, res.Failures())
		}
	}
	rep := e.evaluate()
	if rep.State() != "broken" || rep.Clear() {
		t.Fatalf("state %s\n%s", rep.State(), strings.Join(rep.Lines(), "\n"))
	}
	l := want(t, rep, "grpc_def checkout->protos", Broken, "pass with protos's base commit")
	var cand, ctl bool
	for _, c := range l.Checks {
		cand = cand || c.Purpose == "candidate" && c.Status == "fail" && strings.Contains(c.Output, "AmountCents")
		ctl = ctl || c.Purpose == "control" && c.Status == "pass" && c.Composition["protos"] == e.repos["protos"].Base
	}
	if !cand || !ctl {
		t.Fatalf("checks %+v", l.Checks)
	}
	want(t, rep, "grpc checkout->payments", Broken, "")
	want(t, rep, "proto checkout->protos", Broken, "")
	// payments was updated with the definition: its checks pass, but the
	// rename breaks anything built from the old definition.
	p := want(t, rep, "grpc_def payments->protos", Untested, "breaking for code built from the base definition")
	if !strings.Contains(strings.Join(p.Breaking, " "), "renamed amount_cents -> amount_minor") {
		t.Fatalf("breaking %q", p.Breaking)
	}
	if !strings.Contains(rep.FailureSummary(), "checkout") {
		t.Fatal(rep.FailureSummary())
	}
}

// TestGateUntested: links the gate cannot establish are untested, with what
// evidence is missing.
func TestGateUntested(t *testing.T) {
	t.Run("checks never exercise the call", func(t *testing.T) {
		// checkout's only remaining test does not call ChargeOrder.
		e := newEnvP(t, func(name, dir string) {
			if name == "checkout" {
				p := filepath.Join(dir, "internal/pay/client_test.go")
				b, _ := os.ReadFile(p)
				s := string(b)
				i, j := strings.Index(s, "func TestChargeOrderSendsTheTotal"), strings.Index(s, "func TestFormatTotal")
				if err := os.WriteFile(p, []byte(s[:i]+s[j:]+"\nvar _ = (*fakeConn).Invoke\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
		addIdempotencyKey(e)
		e.commit("attempt 1")
		rep := e.evaluate()
		want(t, rep, "grpc_def checkout->protos", Untested, "never exercise the client side: internal/pay/client.go:23-23 never ran")
		want(t, rep, "grpc checkout->payments", Untested, "never exercise the client side")
		want(t, rep, "grpc_def payments->protos", Compatible, "")
		if rep.Clear() || rep.State() != "untested" {
			t.Fatalf("state %s", rep.State())
		}
	})
	t.Run("missing dependency", func(t *testing.T) {
		// The generated package is not a module of any task repository.
		e := newEnvP(t, func(name, dir string) {
			if name == "protos" {
				_ = os.Remove(filepath.Join(dir, "go.mod"))
			}
		})
		addIdempotencyKey(e)
		e.commit("attempt 1")
		rep := e.evaluate()
		want(t, rep, "grpc_def checkout->protos", Untested, "missing dependency: the generated Go package example.com/shop/protos/payments/v1 is not provided by any task repository")
	})
	t.Run("definition changed but not regenerated", func(t *testing.T) {
		e := newEnv(t)
		e.edit("protos", "payments/v1/payments.proto", "  string currency = 3;\n}", "  string currency = 3;\n  string note = 4;\n}")
		e.commit("attempt 1")
		rep := e.evaluate()
		want(t, rep, "proto checkout->protos", Untested, "did not: regenerate it")
	})
	t.Run("dependent outside the task", func(t *testing.T) {
		e := newEnv(t, "payments", "protos")
		var others []xservice.Endpoint
		eps, _, err := xservice.Scan("checkout", filepath.Join(fixture, "checkout"), xservice.ScanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		others = append(others, eps...)
		addIdempotencyKeyNoCheckout(e)
		e.commit("attempt 1")
		rep, err := e.gate.Evaluate(e.ctx, "t1", e.list(), others)
		if err != nil {
			t.Fatal(err)
		}
		l := want(t, rep, "grpc_def checkout->protos", Untested, "checkout is not a task repository")
		if l.From.Commit != "" {
			t.Fatalf("an indexed side has no commit: %+v", l.From)
		}
	})
	t.Run("unsupported language", func(t *testing.T) {
		e := newEnvP(t, func(name, dir string) {
			if name == "checkout" {
				_ = os.WriteFile(filepath.Join(dir, "refunds.py"), []byte("from payments.v1 import payments_pb2_grpc\n\n"+
					"def refund(channel, pid):\n    stub = payments_pb2_grpc.PaymentServiceStub(channel)\n    return stub.Refund(pid)\n"), 0o644)
			}
		})
		addIdempotencyKey(e)
		e.commit("attempt 1")
		rep := e.evaluate()
		found := false
		for _, l := range rep.Links {
			if strings.HasSuffix(l.From.File, ".py") {
				found = true
				if l.Result != Untested || !strings.Contains(l.Reason, "Python code: the gate checks Go sides only") {
					t.Fatalf("python side: %s %s", l.Result, l.Reason)
				}
			}
		}
		if !found {
			t.Fatalf("no Python link\n%s", strings.Join(rep.Lines(), "\n"))
		}
	})
}

// addIdempotencyKeyNoCheckout is addIdempotencyKey for a task without checkout.
func addIdempotencyKeyNoCheckout(e *env) { addIdempotencyKey(e) }

// TestGateUnrelatedChange: a comment in the .proto and a change to another
// function of the client's file affect no link.
func TestGateUnrelatedChange(t *testing.T) {
	e := newEnv(t)
	e.edit("protos", "payments/v1/payments.proto", "// PaymentService captures", "// PaymentService (v1) captures")
	e.edit("checkout", "internal/pay/client.go", "// FormatTotal renders an order total for receipts.", "// FormatTotal renders an order total for receipts and e-mails.")
	e.commit("attempt 1")
	rep := e.evaluate()
	if len(rep.Links) != 0 || len(rep.Unaffected) == 0 || !rep.Clear() || rep.State() != "none" {
		t.Fatalf("state %s, %d links, %d unaffected\n%s", rep.State(), len(rep.Links), len(rep.Unaffected), strings.Join(rep.Lines(), "\n"))
	}
	// The same file, the function holding the call: affected.
	e.edit("checkout", "internal/pay/client.go", `fmt.Errorf("charge order %s: %w"`, `fmt.Errorf("charge %s: %w"`)
	e.commit("attempt 2")
	rep = e.evaluate()
	want(t, rep, "grpc_def checkout->protos", Compatible, "")
}

// TestGateResume: an evaluation interrupted after its first recorded link
// resumes from the ledger: recorded links are not re-run, the rest are.
func TestGateResume(t *testing.T) {
	e := newEnv(t)
	cs := &countingSandbox{}
	e.gate.Verify.Sandbox = cs
	renameAmount(e)
	e.commit("attempt 1")
	// The process is killed (here: its context cancelled) right after the
	// first link is recorded.
	ctx, kill := context.WithCancel(e.ctx)
	e.gate.afterLink = func(LinkResult) error { kill(); return nil }
	if _, err := e.gate.Evaluate(ctx, "t1", e.list(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	first := cs.tests()
	rep, ok, err := Load(e.ctx, e.db.DB, "t1", nil)
	// Links whose checks had completed are recorded; the rest are not.
	recorded := len(rep.Links)
	if err != nil || !ok || !rep.Incomplete || recorded == 0 || recorded >= 5 {
		t.Fatalf("after the interruption: ok=%v err=%v incomplete=%v links=%d", ok, err, rep.Incomplete, recorded)
	}
	e.gate.afterLink = nil
	cs.argv = nil
	rep = e.evaluate()
	if rep.Reused != recorded || len(rep.Links) != 5 || rep.State() != "broken" {
		t.Fatalf("reused %d, %d links, state %s", rep.Reused, len(rep.Links), rep.State())
	}
	// The first run ran checkout's group (candidate and control); the
	// resumed run must still run payments' group but not recompute
	// checkout's recorded link from scratch, so it runs fewer tests in total
	// than a fresh evaluation would.
	fresh := newEnv(t)
	fcs := &countingSandbox{}
	fresh.gate.Verify.Sandbox = fcs
	renameAmount(fresh)
	fresh.commit("attempt 1")
	fresh.evaluate()
	t.Logf("go test runs: interrupted %d, resumed %d, fresh %d", first, cs.tests(), fcs.tests())
	if first == 0 || cs.tests() == 0 || cs.tests() >= fcs.tests() {
		t.Fatalf("tests: interrupted %d, resumed %d, fresh %d", first, cs.tests(), fcs.tests())
	}
	cur := map[string]string{}
	for _, n := range e.order {
		cur[n] = CommitRange(e.repos[n].Base, e.head(n))
	}
	loaded, _, err := Load(e.ctx, e.db.DB, "t1", cur)
	if err != nil || loaded.Stale() || loaded.State() != "broken" || len(loaded.Links) != 5 {
		t.Fatalf("loaded: stale %v state %s links %d err %v", loaded.Stale(), loaded.State(), len(loaded.Links), err)
	}
}

// TestGateStaleEvidence: results are tied to commits; a new commit in a
// repository a result depends on makes it stale, and the next evaluation
// re-runs it instead of reusing it.
func TestGateStaleEvidence(t *testing.T) {
	e := newEnv(t)
	addIdempotencyKey(e)
	e.commit("attempt 1")
	e.evaluate()
	current := func() map[string]string {
		cur := map[string]string{}
		for _, n := range e.order {
			cur[n] = CommitRange(e.repos[n].Base, e.head(n))
		}
		return cur
	}
	rep, _, _ := Load(e.ctx, e.db.DB, "t1", current())
	if rep.Stale() || rep.State() != "compatible" {
		t.Fatalf("fresh results: stale %v state %s", rep.Stale(), rep.State())
	}
	// A later attempt renames the field: every recorded result depends on
	// protos, so all are stale.
	renameAmount(e)
	e.commit("attempt 2")
	rep, _, _ = Load(e.ctx, e.db.DB, "t1", current())
	if !rep.Stale() || rep.State() != "compatible" {
		t.Fatalf("after a new commit: stale %v", rep.Stale())
	}
	for _, l := range rep.Links {
		if !l.Stale {
			t.Fatalf("not stale: %s %s", l.Kind, l.Contract)
		}
	}
	rep = e.evaluate()
	if rep.Reused != 0 || rep.State() != "broken" {
		t.Fatalf("re-evaluation reused %d, state %s", rep.Reused, rep.State())
	}
	// An unrelated repository's new commit does not invalidate a link that
	// does not depend on it... every link here involves protos, so only a
	// commit-identical evaluation reuses everything.
	rep = e.evaluate()
	if rep.Reused != 5 {
		t.Fatalf("same commits: reused %d", rep.Reused)
	}
}

// TestGateBypassAttempts: agent-writable files cannot make a broken link pass.
func TestGateBypassAttempts(t *testing.T) {
	t.Run("verification config edited", func(t *testing.T) {
		e := newEnv(t)
		renameAmount(e)
		if err := os.MkdirAll(filepath.Join(e.repos["checkout"].Worktree, ".boundedcode"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(e.repos["checkout"].Worktree, ".boundedcode/verification.yaml"),
			[]byte("version: 1\nstages:\n  - name: test\n    run: [\"true\"]\n    tests: true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.commit("attempt 1")
		rep := e.evaluate()
		l := want(t, rep, "grpc_def checkout->protos", Broken, "")
		if !strings.Contains(l.Checks[1].Command, "go test") {
			t.Fatalf("the base config's stage was not used: %+v", l.Checks)
		}
	})
	t.Run("replace directive to a stale copy", func(t *testing.T) {
		e := newEnv(t)
		renameAmount(e)
		wt := e.repos["checkout"].Worktree
		if out, err := exec.Command("cp", "-r", filepath.Join(wt, "vendor/example.com/shop/protos"), filepath.Join(wt, "stale")).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
		if err := os.WriteFile(filepath.Join(wt, "stale/go.mod"), []byte("module example.com/shop/protos\n\ngo 1.22\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.edit("checkout", "go.mod", "require example.com/shop/protos v0.1.0", "require example.com/shop/protos v0.1.0\n\nreplace example.com/shop/protos => ./stale")
		e.edit("checkout", "vendor/modules.txt", "## explicit; go 1.22\n", "## explicit; go 1.22\n# example.com/shop/protos => ./stale\n")
		e.commit("attempt 1")
		rep := e.evaluate()
		want(t, rep, "grpc_def checkout->protos", Broken, "")
	})
	t.Run("consumer switched to a private copy of the generated code", func(t *testing.T) {
		e := newEnv(t)
		renameAmount(e)
		wt := e.repos["checkout"].Worktree
		if out, err := exec.Command("cp", "-r", filepath.Join(wt, "vendor/example.com/shop/protos/payments/v1"), filepath.Join(wt, "internal/paymentsv1")).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
		e.edit("checkout", "internal/pay/client.go", `paymentsv1 "example.com/shop/protos/payments/v1"`, `paymentsv1 "example.com/shop/checkout/internal/paymentsv1"`)
		e.edit("checkout", "internal/pay/client_test.go", `paymentsv1 "example.com/shop/protos/payments/v1"`, `paymentsv1 "example.com/shop/checkout/internal/paymentsv1"`)
		e.commit("attempt 1")
		rep := e.evaluate()
		if rep.Clear() || rep.Count(Compatible) == len(rep.Links) {
			t.Fatalf("bypass accepted\n%s", strings.Join(rep.Lines(), "\n"))
		}
		for _, l := range rep.Links {
			if l.From.Repo == "checkout" && l.Result == Compatible {
				t.Fatalf("checkout link compatible: %s %s", l.Kind, l.Reason)
			}
		}
	})
}

// The OpenAPI fixture: api holds openapi.json; orders serves two of its
// operations and has a contract test that reads the spec from the sibling
// checkout (and skips without it, so orders' own verification passes alone).
const openAPIFixture = "testdata/openapi"

func TestGateOpenAPI(t *testing.T) {
	t.Run("operation changed: the contract test reads it", func(t *testing.T) {
		e := newEnvFrom(t, openAPIFixture, nil, "api", "orders")
		e.edit("api", "openapi.json", `"total_cents": {"type": "integer"}`, `"total_cents": {"type": "integer"}, "currency": {"type": "string"}`)
		e.commit("attempt 1")
		rep := e.evaluate()
		l := want(t, rep, "openapi_impl orders->api", Compatible, "its checks fail when GET /v1/orders/{} is removed from openapi.json")
		var ko bool
		for _, c := range l.Checks {
			ko = ko || c.Purpose == "knockout" && c.Status == "fail"
		}
		if !ko || len(rep.Unaffected) != 1 {
			t.Fatalf("knock-out not recorded, or POST /v1/orders not unaffected: %+v", rep)
		}
	})
	t.Run("operation removed while still served", func(t *testing.T) {
		e := newEnvFrom(t, openAPIFixture, nil, "api", "orders")
		e.edit("api", "openapi.json", `,
    "/v1/orders": {
      "post": {
        "operationId": "createOrder",
        "responses": {"201": {"description": "created"}}
      }
    }`, "")
		e.commit("attempt 1")
		rep := e.evaluate()
		l := want(t, rep, "openapi_impl orders->api", Broken, "no longer defines POST /v1/orders, which orders still uses")
		if l.Change != ChangeRemoved || len(l.Checks) != 0 {
			t.Fatalf("%+v", l)
		}
	})
	t.Run("the test reads the spec but does not depend on the operation", func(t *testing.T) {
		e := newEnvFrom(t, openAPIFixture, func(name, dir string) {
			if name == "orders" {
				p := filepath.Join(dir, "internal/httpapi/contract_test.go")
				b, _ := os.ReadFile(p)
				s := strings.Replace(string(b), "for _, r := range Routes() {", "for _, r := range Routes()[:0] {", 1)
				if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}, "api", "orders")
		e.edit("api", "openapi.json", `"total_cents": {"type": "integer"}`, `"total_cents": {"type": "number"}`)
		e.commit("attempt 1")
		rep := e.evaluate()
		want(t, rep, "openapi_impl orders->api", Untested, "still pass with GET /v1/orders/{} removed")
	})
}

// TestGateMovedCode: moving the client into another file is one changed
// link at head, not an extra untested "removed" link.
func TestGateMovedCode(t *testing.T) {
	e := newEnv(t)
	wt := e.repos["checkout"].Worktree
	if err := os.Rename(filepath.Join(wt, "internal/pay/client.go"), filepath.Join(wt, "internal/pay/payments_client.go")); err != nil {
		t.Fatal(err)
	}
	e.commit("attempt 1")
	rep := e.evaluate()
	for _, l := range rep.Links {
		if l.Change == ChangeRemoved {
			t.Fatalf("moved code reported removed: %s %s", l.Kind, l.Reason)
		}
	}
	want(t, rep, "grpc_def checkout->protos", Compatible, "")
}

// TestKnockOutRunsAreKeyedPerOperation: two operations of one spec read by
// the same tests get their own knock-out runs, never each other's record.
func TestKnockOutRunsAreKeyedPerOperation(t *testing.T) {
	e := newEnvFrom(t, openAPIFixture, nil, "api", "orders")
	cs := &countingSandbox{}
	e.gate.Verify.Sandbox = cs
	e.edit("api", "openapi.json", `"responses": {"201": {"description": "created"}}`, `"responses": {"201": {"description": "created order"}}`)
	e.edit("api", "openapi.json", `"total_cents": {"type": "integer"}`, `"total_cents": {"type": "integer", "minimum": 0}`)
	e.commit("attempt 1")
	rep := e.evaluate()
	if len(rep.Links) != 2 || rep.Count(Compatible) != 2 {
		t.Fatalf("%s", strings.Join(rep.Lines(), "\n"))
	}
	// One candidate run, and one knock-out run per operation.
	if n := cs.tests(); n != 3 {
		t.Fatalf("go test runs = %d, want 3: %q", n, cs.argv)
	}
}

// cancellingSandbox cancels the evaluation when the n-th go test starts,
// as a kill during a check would.
type cancellingSandbox struct {
	countingSandbox
	n      int
	cancel context.CancelFunc
}

func (c *cancellingSandbox) Command(ctx context.Context, s sandbox.Spec) (*exec.Cmd, error) {
	cmd, err := c.countingSandbox.Command(ctx, s)
	if c.tests() == c.n {
		c.cancel()
	}
	return cmd, err
}

// TestGateKilledDuringACheckRecordsNothingFromIt: a check interrupted by
// cancellation leaves no result behind for its links (no "could not run"
// verdict to be reused on resume).
func TestGateKilledDuringACheckRecordsNothingFromIt(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(e.ctx)
	e.gate.Verify.Sandbox = &cancellingSandbox{n: 1, cancel: cancel}
	addIdempotencyKey(e)
	e.commit("attempt 1")
	if _, err := e.gate.Evaluate(ctx, "t1", e.list(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	rep, _, _ := Load(e.ctx, e.db.DB, "t1", nil)
	for _, l := range rep.Links {
		t.Fatalf("recorded after a cancelled check: %s %s: %s", l.Kind, l.Result, l.Reason)
	}
	e.gate.Verify.Sandbox = sandbox.None{}
	if rep := e.evaluate(); rep.State() != "compatible" || rep.Reused != 0 {
		t.Fatalf("resumed: %s, reused %d", rep.State(), rep.Reused)
	}
}

func TestFixtureLinks(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	var eps []xservice.Endpoint
	for _, n := range []string{"checkout", "payments", "protos"} {
		e, _, err := xservice.Scan(n, filepath.Join(fixture, n), xservice.ScanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		eps = append(eps, e...)
	}
	for _, l := range xservice.LinkAll(eps, xservice.LinkOptions{}) {
		t.Log(l.String())
	}
}
