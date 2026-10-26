package direct

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHistoryNoteNamesCompanions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(p, s string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	// enc.go and dec.go change together six times; misc.go once with
	// enc.go; enc_test.go always (tests are not named).
	for i := range 6 {
		v := strconv.Itoa(i)
		write("codec/enc.go", "package codec\n\nconst E = "+v+"\n")
		write("codec/dec.go", "package codec\n\nconst D = "+v+"\n")
		write("codec/enc_test.go", "package codec\n\nconst T = "+v+"\n")
		if i == 0 {
			write("codec/misc.go", "package codec\n")
		}
		git("add", "-A")
		git("commit", "-qm", "c"+v)
	}
	write("codec/enc.go", "package codec\n\nconst E = 7\n")
	git("commit", "-qam", "enc alone")

	edit := func(p, old, new string) string {
		return r.runTool(context.Background(), toolCallFor(t, "edit", map[string]any{"path": p, "old_string": old, "new_string": new}))
	}
	r.Opts.NoHistoryNotes = true
	if out := edit("codec/enc.go", "E = 7", "E = 70"); strings.Contains(out, "history note") {
		t.Fatalf("notes off: %q", out)
	}
	r.Opts.NoHistoryNotes = false
	out := edit("codec/enc.go", "E = 70", "E = 8")
	if !strings.Contains(out, "history note: in this repository, commits that changed codec/enc.go usually changed these too") ||
		!strings.Contains(out, "- codec/dec.go (6 of 7 commits)") || strings.Contains(out, "misc.go") || strings.Contains(out, "enc_test.go") {
		t.Fatalf("first change: %q", out)
	}
	if out := edit("codec/enc.go", "E = 8", "E = 9"); strings.Contains(out, "history note") {
		t.Fatalf("named twice: %q", out)
	}
	// dec.go's companion, enc.go, was already changed in this session.
	if out := edit("codec/dec.go", "D = 5", "D = 6"); strings.Contains(out, "history note") {
		t.Fatalf("named a file already changed: %q", out)
	}
}

func TestHistoryNoteOff(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not a git repository: no note, no error.
	out := r.runTool(context.Background(), toolCallFor(t, "edit", map[string]any{"path": "a.txt", "old_string": "x", "new_string": "y"}))
	if strings.Contains(out, "history note") || strings.Contains(out, "error") {
		t.Fatalf("outside git: %q", out)
	}
}

func TestParseCoChangeLog(t *testing.T) {
	var big strings.Builder
	for i := range coChangeMaxFiles + 1 {
		big.WriteString("f" + strconv.Itoa(i) + ".go\n")
	}
	commits, of := parseCoChangeLog("\x00\n\na.go\nvendor/x/y.go\nb_test.go\n\x00\n\n" + big.String() + "\x00\n\na.go\nb.go\n")
	if len(commits) != 2 || len(of["a.go"]) != 2 || len(of["b.go"]) != 1 || of["vendor/x/y.go"] != nil || of["b_test.go"] != nil || of["f1.go"] != nil {
		t.Fatalf("commits %v of %v", commits, of)
	}
}
