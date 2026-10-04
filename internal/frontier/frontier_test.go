package frontier

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/contextplan"
)

func codes(ts []Trigger) string {
	var s []string
	for _, t := range ts {
		s = append(s, string(t.Code))
	}
	return strings.Join(s, ",")
}

func TestEvaluate(t *testing.T) {
	cfg := config.Defaults().Escalation
	cases := []struct {
		name string
		s    Signals
		want string
	}{
		{"routine stays local", Signals{Text: "rename a variable in the logger"}, ""},
		{"keyword alone stays local", Signals{Text: "fix typo in payment docs"}, ""},
		{"payments across services", Signals{Text: "add idempotency to payments", ChangedRepos: 2}, "Z1"},
		{"contract change across repos", Signals{Text: "add field", ChangedRepos: 2, ChangedFiles: []string{"shared-protos/proto/payment.proto"}}, "Z1"},
		{"repeated failure", Signals{Text: "x", ConsecutiveFailures: 2, RepeatedFailure: true}, "Z2"},
		{"threshold failures", Signals{Text: "x", ConsecutiveFailures: 3}, "Z2"},
		{"stuck", Signals{Text: "x", Stuck: true}, "Z2"},
		{"pre-merge auth", Signals{Text: "x", PreMerge: true, ChangedFiles: []string{"svc/internal/auth/token.go"}}, "Z3"},
		{"pre-merge benign", Signals{Text: "x", PreMerge: true, ChangedFiles: []string{"svc/internal/log/log.go"}}, ""},
		{"budget spent", Signals{Text: "x", ConsecutiveFailures: 5, EscalationsUsed: 2, MaxEscalations: 2}, ""},
		{"user overrides budget", Signals{UserRequested: true, EscalationsUsed: 2, MaxEscalations: 2}, "Z4"},
		{"z1 once", Signals{Text: "idempotency for payments", ChangedRepos: 3, AlreadyReviewedZ1: true}, ""},
		{"contract counterpart not updated", Signals{Text: "x", PreMerge: true, ChangedFiles: []string{"svc/internal/events/kafka.go"},
			UnupdatedCounterparts: []string{"topic payments.charged consumed by ledger-service"}}, "Z3"},
		{"counterpart without pre-merge", Signals{Text: "x", UnupdatedCounterparts: []string{"x"}}, ""},
	}
	for _, c := range cases {
		if got := codes(Evaluate(cfg, c.s)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestPacketCompactAndRedacted(t *testing.T) {
	pack := contextplan.Pack{Sections: []contextplan.Section{
		{Key: "task", Title: "TASK", Body: "do x with key sk-proj-abcdefghijklmnopqrstuvwxyz123456"},
		{Key: "rules", Title: "RULES", Body: "internal rules"},
	}}
	p := BuildPacket(Trigger{Z2, "repeated failure"}, pack, DefaultQuestion(Trigger{Code: Z2}), nil, "")
	if strings.Contains(p, "abcdefghijklmnop") || strings.Contains(p, "internal rules") || !strings.Contains(p, "SPECIFIC QUESTION") {
		t.Fatalf("bad packet:\n%s", p)
	}
}

func TestPacketRewritesHostPaths(t *testing.T) {
	pack := contextplan.Pack{Sections: []contextplan.Section{{Key: "code", Title: "CODE",
		Body: "file_path: /home/u/.cache/w/repos/svc/internal/a.go\nworktree /home/u/.local/share/bc/tasks/t1/work/svc/x.go"}}}
	p := BuildPacket(Trigger{Z2, "x"}, pack, "q", PathMap{
		"/home/u/.cache/w/repos/svc": "svc", "/home/u/.local/share/bc/tasks/t1/work": "."}, "/home/u")
	if strings.Contains(p, "/home/u") || !strings.Contains(p, "svc/internal/a.go") || !strings.Contains(p, "./svc/x.go") {
		t.Fatalf("paths not rewritten:\n%s", p)
	}
	if CheckPacket(p, "/home/u") != nil || CheckPacket("see /home/u/secret", "/home/u") == nil {
		t.Fatal("CheckPacket wrong")
	}
}

func TestCodexContainerIsNamedAndRemovedOnCancel(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	c := &Codex{Binary: bin, Container: &CodexContainer{Engine: "docker", Image: "img", UID: 1, GID: 1}}
	cmd, err := c.containerCmd(context.Background(), []string{"exec"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := ""
	for i, a := range cmd.Args {
		if a == "--name" && i+1 < len(cmd.Args) {
			name = cmd.Args[i+1]
		}
	}
	if !strings.HasPrefix(name, "bc-codex-") || cmd.Cancel == nil || cmd.WaitDelay == 0 {
		t.Fatalf("name=%q cancel=%v waitdelay=%v", name, cmd.Cancel != nil, cmd.WaitDelay)
	}
}
