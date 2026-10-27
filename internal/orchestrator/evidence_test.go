package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/agent/scripted"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
)

// twoServices sets up payment-service and ledger-service (with the
// unbalanced-posting bug planted) in one workspace.
func twoServices(t *testing.T) (*store.Store, workspace.Workspace, string) {
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
	w, _ := ws.Create(ctx, "pp")
	for _, name := range []string{"payment-service", "ledger-service"} {
		dir := filepath.Join(root, "repos", name)
		_ = os.MkdirAll(filepath.Dir(dir), 0o755)
		if out, err := exec.Command("cp", "-r", "../../benchmarks/fixtures/payment-platform/"+name, dir).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
		if name == "ledger-service" {
			if err := replaceIn(filepath.Join(dir, "internal/consumer/consumer.go"), "AmountCents: -ev.AmountCents", "AmountCents: ev.AmountCents"); err != nil {
				t.Fatal(err)
			}
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

func fixLedger(wsDir string) error {
	if err := addReproTest(wsDir); err != nil {
		return err
	}
	return replaceIn(filepath.Join(wsDir, consumerFile), "AmountCents: ev.AmountCents, Currency: ev.Currency},\n\t)", "AmountCents: -ev.AmountCents, Currency: ev.Currency},\n\t)")
}

// A change to two repositories is verified only when each repository whose
// code changed has a test showing it: the 2026-10-27 baseline's false
// verification, where one service's test verified both.
func TestEvidenceRequiredInEveryChangedRepository(t *testing.T) {
	s, w, root := twoServices(t)
	ctx := context.Background()
	both := func(wsDir, _ string) (string, error) {
		if err := fixLedger(wsDir); err != nil {
			return "", err
		}
		return "fixed the posting; tuned the producer", replaceIn(filepath.Join(wsDir, "payment-service/internal/events/kafka.go"),
			"// PaymentCharged publishes", "// PaymentCharged (retried) publishes")
	}
	noop := func(string, string) (string, error) { return "nothing more to add", nil }
	rt := &scripted.Runtime{Steps: []scripted.Step{both, noop}}
	r := newRunner(s, root, rt, nil)
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting and make the producer retry", []string{"payment-service", "ledger-service"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != task.VerificationTestsGreen {
		t.Fatalf("status=%s verify=%s; messages:\n%s", got.Status, got.VerificationState, strings.Join(rt.Messages, "\n----\n"))
	}
	if len(rt.Messages) < 2 || !strings.Contains(rt.Messages[1], "payment-service: the change adds or modifies no tests") {
		t.Fatalf("the evidence request does not name the repository without a test:\n%s", strings.Join(rt.Messages, "\n----\n"))
	}
	if request, _, _ := strings.Cut(rt.Messages[1], "\n"); strings.Contains(request, "ledger-service") {
		t.Fatalf("the repository with a test is listed as missing one: %s", request)
	}
}

// A repository whose change is documentation only needs no test of its own.
func TestEvidenceNotRequiredForDocumentation(t *testing.T) {
	s, w, root := twoServices(t)
	ctx := context.Background()
	fixAndDocument := func(wsDir, _ string) (string, error) {
		if err := fixLedger(wsDir); err != nil {
			return "", err
		}
		return "fixed the posting; documented it", os.WriteFile(filepath.Join(wsDir, "payment-service/POSTING.md"), []byte("# Postings\n\nCharges post a balanced pair.\n"), 0o644)
	}
	rt := &scripted.Runtime{Steps: []scripted.Step{fixAndDocument}}
	r := newRunner(s, root, rt, nil)
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting and document it", []string{"payment-service", "ledger-service"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != task.VerificationTaskVerified {
		t.Fatalf("status=%s verify=%s; messages:\n%s", got.Status, got.VerificationState, strings.Join(rt.Messages, "\n----\n"))
	}
}
