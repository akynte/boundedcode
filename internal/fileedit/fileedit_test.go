package fileedit

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReplace(t *testing.T) {
	for _, c := range []struct {
		name, src string
		e         Edit
		want      string
		n         int
		err       string
	}{
		{"exact", "a b a", Edit{Old: "b", New: "c"}, "a c a", 1, ""},
		{"twice", "a b a", Edit{Old: "a", New: "c"}, "", 0, "occurs 2 times"},
		{"all", "a b a", Edit{Old: "a", New: "c", All: true}, "c b c", 2, ""},
		{"missing", "a b a", Edit{Old: "z", New: "c"}, "", 0, "not found"},
		{"empty", "a", Edit{Old: "", New: "c"}, "", 0, "empty"},
		{"indentation", "func f() {\n\treturn 1\n}\n", Edit{Old: "    return 1", New: "    return 2"}, "func f() {\n\treturn 2\n}\n", 1, ""},
	} {
		got, n, err := Replace(c.src, c.e)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err %v, want %q", c.name, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want || n != c.n {
			t.Errorf("%s: %q %d %v, want %q %d", c.name, got, n, err, c.want, c.n)
		}
	}
}

// Changes to several files apply together; when one edit fails, no file
// is written.
func TestApplyIsAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.txt", "alpha\n")
	b := write(t, dir, "b.txt", "beta\n")
	_, err := Apply([]Change{
		{Path: a, Edits: []Edit{{Old: "alpha", New: "ALPHA"}}},
		{Path: b, Edits: []Edit{{Old: "gamma", New: "GAMMA"}}},
	})
	var ee *EditError
	if !errors.As(err, &ee) || ee.Path != b || ee.Index != 0 || !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	if read(t, a) != "alpha\n" || read(t, b) != "beta\n" {
		t.Fatal("a file was written although an edit failed")
	}
	ps, err := Apply([]Change{
		{Path: a, Edits: []Edit{{Old: "alpha", New: "ALPHA"}}},
		{Path: b, Edits: []Edit{{Old: "beta", New: "BETA"}}},
		{Path: a, Edits: []Edit{{Old: "ALPHA", New: "Alpha"}}}, // follows the first change to a
	})
	if err != nil || len(ps) != 2 {
		t.Fatalf("%v %d", err, len(ps))
	}
	if read(t, a) != "Alpha\n" || read(t, b) != "BETA\n" {
		t.Fatalf("a=%q b=%q", read(t, a), read(t, b))
	}
}

// A file that is not what the caller last saw is not changed.
func TestConflict(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.txt", "one\n")
	seen := blobOf([]byte("one\n"))
	if err := os.WriteFile(a, []byte("one, changed by someone else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "two\n"
	_, err := Apply([]Change{{Path: a, Content: &content, Expect: seen}})
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Path != a || ce.Got == "" {
		t.Fatalf("err %v", err)
	}
	if read(t, a) != "one, changed by someone else\n" {
		t.Fatal("overwritten")
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply([]Change{{Path: a, Edits: []Edit{{Old: "one", New: "two"}}, Expect: seen}}); !errors.As(err, &ce) || ce.Got != "" {
		t.Fatalf("deleted: err %v", err)
	}
	// The content the caller saw is accepted.
	write(t, dir, "a.txt", "one\n")
	if _, err := Apply([]Change{{Path: a, Content: &content, Expect: seen}}); err != nil || read(t, a) != "two\n" {
		t.Fatalf("err %v", err)
	}
}

// When a later file cannot be written, the earlier ones are restored, and
// a file the change created is removed.
func TestCommitRollsBack(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("read-only directories")
	}
	dir := t.TempDir()
	a := write(t, dir, "a.txt", "alpha\n")
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	c := write(t, locked, "c.txt", "gamma\n")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(c, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	newFile := filepath.Join(dir, "new.txt")
	body := "new\n"
	_, err := Apply([]Change{
		{Path: a, Edits: []Edit{{Old: "alpha", New: "ALPHA"}}},
		{Path: newFile, Content: &body},
		{Path: c, Edits: []Edit{{Old: "gamma", New: "GAMMA"}}},
	})
	if err == nil {
		t.Fatal("no error")
	}
	if read(t, a) != "alpha\n" {
		t.Fatalf("a not restored: %q", read(t, a))
	}
	if _, err := os.Stat(newFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the created file was not removed")
	}
	if read(t, c) != "gamma\n" {
		t.Fatal("c changed")
	}
}

// An atomic write keeps the file's permissions and leaves no temporary
// file behind; a missing directory is created.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "run.sh", "echo 1\n")
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	ps, err := Prepare([]Change{{Path: p, Edits: []Edit{{Old: "1", New: "2"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := Commit(ps); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || read(t, p) != "echo 2\n" {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	if err := WriteAtomic(filepath.Join(dir, "sub", "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("left %s", e.Name())
		}
	}
}
