package benchmark

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A gomod: source missing from the module cache is downloaded (here from a
// local file proxy, offline), instead of failing the task.
func TestGoModSourceIsDownloaded(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	proxy := t.TempDir()
	// example.com/fixture requires example.com/dep: verification builds the
	// fixture as the main module, so the dependency must be cached as well.
	publish := func(path, ver string, files map[string]string) {
		v := filepath.Join(proxy, filepath.FromSlash(path), "@v")
		if err := os.MkdirAll(v, 0o755); err != nil {
			t.Fatal(err)
		}
		write := func(name, content string) {
			if err := os.WriteFile(filepath.Join(v, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("list", ver+"\n")
		write(ver+".info", `{"Version":"`+ver+`","Time":"2026-01-01T00:00:00Z"}`)
		write(ver+".mod", files["go.mod"])
		f, err := os.Create(filepath.Join(v, ver+".zip"))
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		for name, content := range files {
			w, err := z.Create(path + "@" + ver + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(content))
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	publish("example.com/dep", "v0.1.0", map[string]string{"go.mod": "module example.com/dep\n\ngo 1.22\n", "dep.go": "package dep\n\nconst N = 1\n"})
	publish("example.com/fixture", "v1.2.3", map[string]string{
		"go.mod":     "module example.com/fixture\n\ngo 1.22\n\nrequire example.com/dep v0.1.0\n",
		"fixture.go": "package fixture\n\nimport \"example.com/dep\"\n\nconst Answer = 41 + dep.N\n",
	})

	cache := t.TempDir()
	t.Setenv("GOMODCACHE", cache)
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", "")
	t.Cleanup(func() { _ = exec.Command("go", "clean", "-modcache").Run() }) // the cache is read-only

	dir, err := sourceDir(context.Background(), "gomod:example.com/fixture@v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "fixture.go")); err != nil || !strings.Contains(string(b), "Answer = 41") {
		t.Fatalf("downloaded module: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(cache, "example.com", "dep@v0.1.0", "dep.go")); err != nil {
		t.Fatalf("the module's dependency was not downloaded: %v", err)
	}
	// Built offline as the main module, from a writable copy (as a task's
	// checkout is), the way verification builds it.
	work := filepath.Join(t.TempDir(), "fixture")
	if out, err := exec.Command("cp", "-r", dir, work).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	_ = exec.Command("chmod", "-R", "u+w", work).Run()
	build := exec.Command("go", "build", "./...")
	build.Dir = work
	build.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("offline build: %v\n%s", err, out)
	}
	if _, err := sourceDir(context.Background(), "gomod:example.com/missing@v1.0.0"); err == nil || !strings.Contains(err.Error(), "could not be downloaded") {
		t.Fatalf("missing module: %v", err)
	}
}
