package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/agent/scripted"
	"github.com/akynte/boundedcode/internal/compat"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
)

// setupShop registers the contract-break fixture's three repositories
// (protos, payments, checkout) in a workspace. prep runs on each
// repository's files before its first commit.
func setupShop(t *testing.T, prep func(name, dir string)) (*store.Store, workspace.Workspace, string) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("needs go toolchain")
	}
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ws := workspace.Store{DB: s.DB}
	w, _ := ws.Create(ctx, "shop")
	for _, name := range []string{"protos", "payments", "checkout"} {
		dir := filepath.Join(root, "repos", name)
		_ = os.MkdirAll(filepath.Dir(dir), 0o755)
		if out, err := exec.Command("cp", "-r", "../../benchmarks/fixtures/contract-break/"+name, dir).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
		if prep != nil {
			prep(name, dir)
		}
		for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i"}} {
			if _, err := gitops.Run(ctx, dir, a...); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ws.AddRepo(ctx, w, dir, name); err != nil {
			t.Fatal(err)
		}
	}
	return s, w, root
}

func replaceAllIn(path, old, new string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), old) {
		return os.ErrNotExist
	}
	return os.WriteFile(path, []byte(strings.ReplaceAll(string(b), old, new)), 0o644)
}

// declineUnsupportedCurrency changes payments' Charge (the server side of
// the checkout -> payments gRPC link) with a test that fails on the base.
func declineUnsupportedCurrency(ws, _ string) (string, error) {
	srv := filepath.Join(ws, "payments/internal/server/server.go")
	if err := replaceAllIn(srv, `	if req.GetAmountCents() <= 0 || req.GetCurrency() == "" {`,
		`	if req.GetAmountCents() <= 0 || !supported[req.GetCurrency()] {`); err != nil {
		return "", err
	}
	if err := replaceAllIn(srv, "// New returns an empty server.", "// supported are the currencies payments can capture.\nvar supported = map[string]bool{\"EUR\": true, \"USD\": true}\n\n// New returns an empty server."); err != nil {
		return "", err
	}
	return "declined unsupported currencies", os.WriteFile(filepath.Join(ws, "payments/internal/server/currency_test.go"), []byte(`package server

import (
	"context"
	"testing"

	paymentsv1 "example.com/shop/protos/payments/v1"
)

func TestChargeDeclinesUnsupportedCurrency(t *testing.T) {
	resp, err := New().Charge(context.Background(), &paymentsv1.ChargeRequest{OrderId: "o3", AmountCents: 100, Currency: "XTS"})
	if err != nil || resp.GetStatus() != paymentsv1.Status_STATUS_DECLINED {
		t.Fatalf("charge = %+v, %v", resp, err)
	}
}
`), 0o644)
}

// contractCheckNoop answers the runner's existing contract-check round
// (payments changed, its client checkout did not): no change needed.
func contractCheckNoop(_, msg string) (string, error) {
	if !strings.Contains(msg, "Contract check") {
		return "", os.ErrInvalid
	}
	return "checkout is unaffected", nil
}

// TestCompatGateCompatibleChangeIsVerified: a server change whose client is
// exercised against it by checkout's checks is TASK_VERIFIED, and the
// report says why.
func TestCompatGateCompatibleChangeIsVerified(t *testing.T) {
	s, w, root := setupShop(t, nil)
	ctx := context.Background()
	rt := &scripted.Runtime{Steps: []scripted.Step{declineUnsupportedCurrency, contractCheckNoop}}
	r := newRunner(s, root, rt, nil)
	r.CrossService = true
	tk, err := r.Create(ctx, w, "Decline charges in currencies payments does not support", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != task.VerificationTaskVerified || len(rt.Messages) != 2 {
		t.Fatalf("status=%s verify=%s messages=%d decisions=%+v", got.Status, got.VerificationState, len(rt.Messages), got.Decisions)
	}
	if !hasDecision(got, "cross-repository compatibility: COMPATIBLE") {
		t.Fatalf("decisions %+v", got.Decisions)
	}
	rep, ok, err := compat.Load(ctx, s.DB, tk.ID, nil)
	if err != nil || !ok || rep.State() != "compatible" {
		t.Fatalf("persisted report: %v %v %s", ok, err, rep.State())
	}
}

// TestCompatGateRetriesBrokenLink: a definition change that breaks a
// repository the agent did not update fails verification although every
// repository's own checks pass; the retry is told which link broke; after
// the fix the breaking definition change still withholds TASK_VERIFIED.
func TestCompatGateRetriesBrokenLink(t *testing.T) {
	s, w, root := setupShop(t, nil)
	ctx := context.Background()
	rename := func(ws, _ string) (string, error) {
		for _, f := range []string{"protos/payments/v1/payments.pb.go", "payments/vendor/example.com/shop/protos/payments/v1/payments.pb.go",
			"payments/internal/server/server.go", "payments/internal/server/server_test.go"} {
			if err := replaceAllIn(filepath.Join(ws, f), "AmountCents", "AmountMinor"); err != nil {
				return "", err
			}
		}
		return "renamed amount_cents", replaceAllIn(filepath.Join(ws, "protos/payments/v1/payments.proto"), "int64 amount_cents = 2;", "int64 amount_minor = 2;")
	}
	fixCheckout := func(ws, msg string) (string, error) {
		if !strings.Contains(msg, "Cross-repository compatibility check failed") || !strings.Contains(msg, "checkout@") ||
			!strings.Contains(msg, "AmountCents") {
			return "", os.ErrInvalid
		}
		for _, f := range []string{"checkout/internal/pay/client.go", "checkout/internal/pay/client_test.go",
			"checkout/vendor/example.com/shop/protos/payments/v1/payments.pb.go"} {
			if err := replaceAllIn(filepath.Join(ws, f), "AmountCents", "AmountMinor"); err != nil {
				return "", err
			}
		}
		return "updated checkout", nil
	}
	noTest := func(string, string) (string, error) { return "the rename has no behavioural test", nil }
	rt := &scripted.Runtime{Steps: []scripted.Step{rename, fixCheckout, noTest}}
	r := newRunner(s, root, rt, nil)
	r.CrossService = true
	tk, err := r.Create(ctx, w, "Rename ChargeRequest.amount_cents to amount_minor", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != task.VerificationTestsGreen || len(rt.Messages) != 3 {
		t.Fatalf("status=%s verify=%s messages=%d decisions=%+v", got.Status, got.VerificationState, len(rt.Messages), got.Decisions)
	}
	strats, _ := r.Ledger.Strategies(ctx, tk.ID)
	if len(strats) == 0 || strats[0].Outcome != "rejected" || !strings.Contains(strats[0].Reason, "cross-repository:") {
		t.Fatalf("first attempt %+v", strats)
	}
	if !hasDecision(got, "cross-repository compatibility: UNTESTED") || !hasDecision(got, "not shown compatible") {
		t.Fatalf("decisions %+v", got.Decisions)
	}
}

// TestCompatGateUntestedWithholdsVerified: a behavioural test exists, but
// checkout's checks never execute its call to Charge: the agent is asked
// once for a test that does, and without one the task is not verified.
func TestCompatGateUntestedWithholdsVerified(t *testing.T) {
	s, w, root := setupShop(t, func(name, dir string) {
		if name == "checkout" {
			p := filepath.Join(dir, "internal/pay/client_test.go")
			b, _ := os.ReadFile(p)
			src := string(b)
			i, j := strings.Index(src, "func TestChargeOrderSendsTheTotal"), strings.Index(src, "func TestFormatTotal")
			_ = os.WriteFile(p, []byte(src[:i]+src[j:]+"\nvar _ = (*fakeConn).Invoke\n"), 0o644)
		}
	})
	ctx := context.Background()
	asked := func(_, msg string) (string, error) {
		if !strings.Contains(msg, "none executes these contract uses") || !strings.Contains(msg, "internal/pay/client.go:23") {
			return "", os.ErrInvalid
		}
		return "not adding tests", nil
	}
	rt := &scripted.Runtime{Steps: []scripted.Step{declineUnsupportedCurrency, contractCheckNoop, asked}}
	r := newRunner(s, root, rt, nil)
	r.CrossService = true
	tk, err := r.Create(ctx, w, "Decline charges in currencies payments does not support", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != task.VerificationTestsGreen || len(rt.Messages) != 3 {
		t.Fatalf("status=%s verify=%s messages=%d decisions=%+v", got.Status, got.VerificationState, len(rt.Messages), got.Decisions)
	}
	if !hasDecision(got, "1 affected cross-repository link(s) are not shown compatible (0 broken, 1 untested) (a test does demonstrate the change)") {
		t.Fatalf("decisions %+v", got.Decisions)
	}
	// With the gate off the same run is TASK_VERIFIED: the gate is what
	// withholds it.
	s2, w2, root2 := setupShop(t, func(name, dir string) {
		if name == "checkout" {
			p := filepath.Join(dir, "internal/pay/client_test.go")
			b, _ := os.ReadFile(p)
			src := string(b)
			i, j := strings.Index(src, "func TestChargeOrderSendsTheTotal"), strings.Index(src, "func TestFormatTotal")
			_ = os.WriteFile(p, []byte(src[:i]+src[j:]+"\nvar _ = (*fakeConn).Invoke\n"), 0o644)
		}
	})
	rt2 := &scripted.Runtime{Steps: []scripted.Step{declineUnsupportedCurrency, contractCheckNoop}}
	r2 := newRunner(s2, root2, rt2, nil)
	r2.CrossService = true
	r2.Cfg.RepoIntel.CompatGate = false
	tk2, _ := r2.Create(ctx, w2, "Decline charges in currencies payments does not support", nil, nil)
	got2, err := r2.Run(ctx, tk2.ID, RunOptions{})
	if err != nil || got2.VerificationState != task.VerificationTaskVerified {
		t.Fatalf("gate off: %v %s", err, got2.VerificationState)
	}
}
