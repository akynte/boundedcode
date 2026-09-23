package oracle_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/oracle"
)

func writeSuite(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadReadsEveryFileSortedByID(t *testing.T) {
	dir := writeSuite(t, map[string]string{
		"b.yaml":    "checks:\n  - id: zeta\n    argv: [\"true\"]\n",
		"a.yml":     "checks:\n  - id: alpha\n    argv: [\"true\"]\n    timeout_seconds: 5\n",
		"notes.txt": "not a check file",
	})
	s, err := oracle.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Checks) != 2 || s.Checks[0].ID != "alpha" || s.Checks[1].ID != "zeta" {
		t.Fatalf("checks = %+v", s.Checks)
	}
	if len(s.Digest) != 64 {
		t.Errorf("digest = %q", s.Digest)
	}
	if s.Checks[0].Timeout().Seconds() != 5 || s.Checks[1].Timeout() != oracle.DefaultTimeout {
		t.Error("timeouts are not what the files say")
	}
}

func TestDigestFollowsContent(t *testing.T) {
	a, err := oracle.Load(writeSuite(t, map[string]string{"c.yaml": "checks:\n  - id: x\n    argv: [\"true\"]\n"}))
	if err != nil {
		t.Fatal(err)
	}
	same, err := oracle.Load(writeSuite(t, map[string]string{"other.yaml": "checks:\n  - id: x\n    argv: [\"true\"]\n"}))
	if err != nil {
		t.Fatal(err)
	}
	changed, err := oracle.Load(writeSuite(t, map[string]string{"c.yaml": "checks:\n  - id: x\n    argv: [\"false\"]\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != same.Digest {
		t.Error("the digest must depend on the checks, not the file names")
	}
	if a.Digest == changed.Digest {
		t.Error("a changed check must change the digest")
	}
}

func TestLoadRefusesWhatCannotBeJudgedBy(t *testing.T) {
	cases := map[string]map[string]string{
		"no checks":     {"c.yaml": "checks: []\n"},
		"no id":         {"c.yaml": "checks:\n  - argv: [\"true\"]\n"},
		"no argv":       {"c.yaml": "checks:\n  - id: x\n"},
		"unknown field": {"c.yaml": "checks:\n  - id: x\n    argv: [\"true\"]\n    comand: [\"true\"]\n"},
		"duplicate":     {"a.yaml": "checks:\n  - id: x\n    argv: [\"true\"]\n", "b.yaml": "checks:\n  - id: x\n    argv: [\"true\"]\n"},
		"escaping file": {"c.yaml": "checks:\n  - id: x\n    argv: [\"true\"]\n    files:\n      ../outside.go: x\n"},
		"absolute file": {"c.yaml": "checks:\n  - id: x\n    argv: [\"true\"]\n    files:\n      /etc/x: x\n"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := oracle.Load(writeSuite(t, files)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	if _, err := oracle.Load(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing directory must be an error, not an empty suite")
	}
	if _, err := oracle.Load("relative/dir"); err == nil {
		t.Error("a relative directory must be refused")
	}
}

func TestAppliesAndTampered(t *testing.T) {
	c := oracle.Check{
		ID: "x", Argv: []string{"true"},
		AppliesTo:     []string{"services/auth/**"},
		MustNotChange: []string{"services/auth/refresh_test.go"},
	}
	if c.Applies([]string{"README.md"}) {
		t.Error("a change outside applies_to must not require the check")
	}
	if !c.Applies([]string{"README.md", "services/auth/refresh.go"}) {
		t.Error("a change inside applies_to must require the check")
	}
	if got := c.Tampered([]string{"services/auth/refresh.go", "services/auth/refresh_test.go"}); len(got) != 1 {
		t.Errorf("tampered = %v", got)
	}
	if !(oracle.Check{ID: "y", Argv: []string{"true"}}).Applies(nil) {
		t.Error("a check with no applies_to applies to every change")
	}
}

func TestOutside(t *testing.T) {
	repo := t.TempDir()
	inside := filepath.Join(repo, ".oracle")
	if err := os.Mkdir(inside, 0o750); err != nil {
		t.Fatal(err)
	}
	sibling := repo + "-oracle"
	if err := os.Mkdir(sibling, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sibling) })

	if ok, err := oracle.Outside(inside, repo); err != nil || ok {
		t.Errorf("a directory inside the repository reported outside (%v, %v)", ok, err)
	}
	if ok, err := oracle.Outside(repo, repo); err != nil || ok {
		t.Errorf("the repository itself reported outside (%v, %v)", ok, err)
	}
	if ok, err := oracle.Outside(sibling, repo); err != nil || !ok {
		t.Errorf("a sibling sharing the repository's name as a prefix reported inside (%v, %v)", ok, err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(inside, link); err != nil {
		t.Fatal(err)
	}
	if ok, _ := oracle.Outside(link, repo); ok {
		t.Error("a symlink into the repository must count as inside")
	}
	if !strings.HasPrefix(sibling, repo) {
		t.Fatal("test setup: the sibling must share the repository path as a string prefix")
	}
}
