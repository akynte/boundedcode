package contextplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/task"
)

// TestReadConfinedRejectsPlantedLinks: an agent plants symlinks in its
// worktree and makes a failing test print them as file:line, hoping the
// host copies the targets into the context pack.
func TestReadConfinedRejectsPlantedLinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "id_ed25519")
	_ = os.WriteFile(outside, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, ".env"), []byte("DB_PASSWORD=hunter2\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "ok.go"), []byte("package x\n\nfunc A() {}\n"), 0o644)
	_ = os.Symlink(outside, filepath.Join(root, "key.go"))
	_ = os.Symlink(filepath.Join(root, ".env"), filepath.Join(root, "env.go"))
	_ = os.Symlink("ok.go", filepath.Join(root, "alias.go"))

	for _, rel := range []string{"key.go", "env.go", ".env", "../x.go", "/etc/passwd"} {
		if b, err := ReadConfined(root, rel); err == nil {
			t.Errorf("%s was read: %q", rel, b)
		}
	}
	if _, err := ReadConfined(root, "alias.go"); err != nil {
		t.Errorf("in-tree symlink to a source file rejected: %v", err)
	}
	wts := []task.Worktree{{RepoName: "r", Path: root}}
	if s := readAround(wts, "r", "key.go", 1, 3); s != "" {
		t.Fatalf("readAround followed a planted link: %q", s)
	}
	if s := readAround(wts, "r", "ok.go", 3, 1); !strings.Contains(s, "func A") {
		t.Fatalf("readAround(ok.go) = %q", s)
	}
}

func TestRenderRedactsAndCapPatch(t *testing.T) {
	awsKey := "AKIA" + "ABCDEFGHIJKLMNOP" // split so secret scanners don't flag the fixture
	p := Pack{Sections: []Section{{Title: "T", Body: "token sk-proj-abcdefghijklmnopqrstuvwxyz0123 and " + awsKey}}}
	out := p.Render()
	if strings.Contains(out, "abcdefghijklmnopqrstuvwxyz0123") || strings.Contains(out, awsKey) {
		t.Fatalf("pack not redacted: %s", out)
	}
	big := "diff --git a/big.go b/big.go\n" + strings.Repeat("+x\n", 400)
	sec := "diff --git a/.env b/.env\n+DB_PASSWORD=hunter2\n"
	got := capPatch(big+sec, 50)
	if strings.Contains(got, "hunter2") || !strings.Contains(got, "secret path .env omitted") {
		t.Fatalf("secret diff not omitted: %s", got)
	}
	if strings.Count(got, "+x") > 50 || !strings.Contains(got, "more lines of big.go omitted") {
		t.Fatalf("large file not capped")
	}
}
