package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/agent/scripted"
)

// escalateOnce runs a task whose two attempts fail verification, so the
// policy escalates (Z2) once. HOME is the test's root directory, which also
// holds the repository and the runner's state; request receives it.
func escalateOnce(t *testing.T, request func(home string) string, prepare ...func(r *Runner, root string)) (*fakeFrontier, *Runner, string) {
	t.Helper()
	_, w, s, root := setup(t)
	t.Cleanup(func() { s.Close() })
	t.Setenv("HOME", root)
	ctx := context.Background()
	wrongFix := func(ws, _ string) (string, error) {
		return "renamed variable", replaceIn(filepath.Join(ws, consumerFile), "var ev PaymentCharged", "var ev PaymentCharged // decoded event")
	}
	fr := &fakeFrontier{}
	r := newRunner(s, root, &scripted.Runtime{Steps: []scripted.Step{wrongFix, wrongFix}}, fr)
	tk, err := r.Create(ctx, w, request(root), nil, []string{"go test ./... passes"})
	if err != nil {
		t.Fatal(err)
	}
	tk.Budget.MaxAttempts = 2
	if err := r.Ledger.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	for _, f := range prepare {
		f(r, root)
	}
	if _, err := r.Run(ctx, tk.ID, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	return fr, r, root
}

// TestEscalationPacketIsSanitized is the regression test for the
// 2026-10-04 validation (grpc__grpc-go-2744): a Z2 escalation was refused
// because a host path survived packet building, so frontier help silently
// never came. Host paths from any section (here the task text, the
// verification output and the workspace) are rewritten and the packet is
// sent; repository-relative paths survive.
func TestEscalationPacketIsSanitized(t *testing.T) {
	fr, _, home := escalateOnce(t, func(home string) string {
		return "Fix the unbalanced ledger posting in HandlePaymentCharged. Seen in " + home + "/scratch/trace.log:7, " +
			`{"file":"` + strings.ReplaceAll(home, "/", `\/`) + `\/scratch\/out.json"} and file://` + home + "/notes.md."
	})
	if len(fr.asked) != 1 {
		t.Fatalf("expected one escalation to be sent, got %d", len(fr.asked))
	}
	p := fr.asked[0]
	if strings.Contains(p, home) || strings.Contains(p, strings.ReplaceAll(home, "/", `\/`)) {
		t.Fatalf("packet still contains the home directory:\n%s", p)
	}
	for _, want := range []string{"$HOME/scratch/trace.log:7", `$HOME\/scratch\/out.json`, "file://$HOME/notes.md",
		"ledger-service/internal/consumer", "go-test"} {
		if !strings.Contains(p, want) {
			t.Errorf("packet lacks %q:\n%s", want, p)
		}
	}
}

// TestBlockedEscalationIsRecorded: when the fail-closed check refuses a
// packet, nothing is sent, the packet is kept on the host for diagnosis and
// the escalation is recorded as blocked (it used to leave no record).
func TestBlockedEscalationIsRecorded(t *testing.T) {
	old := checkPacket
	checkPacket = func(string, string) error { return errors.New("residue") }
	t.Cleanup(func() { checkPacket = old })
	fr, r, _ := escalateOnce(t, func(string) string { return "Fix the unbalanced ledger posting in HandlePaymentCharged" })
	if len(fr.asked) != 0 {
		t.Fatal("a refused packet was sent")
	}
	var status, path string
	if err := r.DB.QueryRow(`SELECT status, packet_path FROM escalations`).Scan(&status, &path); err != nil {
		t.Fatal(err)
	}
	if status != "blocked" || !strings.HasSuffix(path, "-blocked-packet.md") {
		t.Fatalf("escalation = %s %s", status, path)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("blocked packet not kept privately: %v %v", st, err)
	}
	// The frontier directory is mounted into the provider's networked
	// container; a refused packet must not be readable there.
	if filepath.Base(filepath.Dir(path)) == "frontier" {
		t.Fatalf("blocked packet %s is in the directory the frontier container mounts", path)
	}
}

// TestDataPolicyBlocksEscalation: a repository whose data policy denies
// external models gets no escalation, and the refusal is recorded.
func TestDataPolicyBlocksEscalation(t *testing.T) {
	fr, r, _ := escalateOnce(t, func(string) string { return "Fix the unbalanced ledger posting in HandlePaymentCharged" },
		func(_ *Runner, root string) {
			dir := filepath.Join(root, "repos", "ledger-service", ".boundedcode")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "data-policy.yaml"), []byte("external_models: deny\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		})
	if len(fr.asked) != 0 {
		t.Fatal("a repository that denies external models was escalated")
	}
	var status, outcome string
	if err := r.DB.QueryRow(`SELECT status, outcome FROM escalations`).Scan(&status, &outcome); err != nil {
		t.Fatal(err)
	}
	if status != "blocked" || outcome != "data policy" {
		t.Fatalf("escalation = %s %s", status, outcome)
	}
}

// TestAbstractPacketSent: with frontier.data abstract the packet carries
// the failure but none of the repository's source lines.
func TestAbstractPacketSent(t *testing.T) {
	fr, _, root := escalateOnce(t, func(string) string { return "Fix the unbalanced ledger posting in HandlePaymentCharged" },
		func(r *Runner, _ string) { r.Cfg.Frontier.Data = "abstract" })
	if len(fr.asked) != 1 {
		t.Fatalf("expected one escalation, got %d", len(fr.asked))
	}
	p := fr.asked[0]
	src, err := os.ReadFile(filepath.Join(root, "repos", consumerFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(src), "\n") {
		if l = strings.TrimSpace(l); len(l) >= 16 && strings.Contains(p, l) {
			t.Errorf("abstract packet repeats %q", l)
		}
	}
	if !strings.Contains(p, "content withheld") && !strings.Contains(p, "withheld") {
		t.Errorf("no outline of the change:\n%s", p)
	}
}
