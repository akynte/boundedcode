package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// copyRepo copies the tracked tree the guard reads into a temp git repo.
func copyRepo(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	for _, f := range trackedFiles("../..") {
		if !strings.HasPrefix(f, "configs/") && !strings.HasPrefix(f, "docs/") && !strings.HasPrefix(f, "LICENSES/") &&
			!strings.HasPrefix(f, "benchmarks/reports/") && f != "THIRD_PARTY_NOTICES.md" && !installFile(f) {
			continue
		}
		b, err := os.ReadFile(filepath.Join("../..", f))
		if err != nil {
			continue
		}
		p := filepath.Join(dst, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "-C", dst, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	return dst
}

func TestRepositoryPasses(t *testing.T) {
	if p := check("../.."); len(p) > 0 {
		t.Fatalf("guard fails on the repository: %v", p)
	}
}

func TestVersionBumpWithoutReviewFails(t *testing.T) {
	cases := map[string]func(root string){
		"pyproject bumped to v2": func(root string) {
			p := filepath.Join(root, "configs/serena/pyproject.toml")
			b, _ := os.ReadFile(p)
			_ = os.WriteFile(p, []byte(strings.ReplaceAll(string(b), "serena-agent==1.7.0", "serena-agent==2.0.0")), 0o644)
		},
		"floating git install": func(root string) {
			_ = os.WriteFile(filepath.Join(root, "install.sh"), []byte("uv tool install git+https://github.com/oraios/serena\n"), 0o644)
		},
		"unpinned requirement": func(root string) {
			_ = os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("serena-agent>=1.7\n"), 0o644)
		},
		"license text replaced": func(root string) {
			_ = os.WriteFile(filepath.Join(root, "LICENSES/upstream/serena.txt"), []byte("GNU GENERAL PUBLIC LICENSE\n"), 0o644)
		},
		"matrix row removed": func(root string) {
			p := filepath.Join(root, "docs/licensing/upstream-license-matrix.md")
			b, _ := os.ReadFile(p)
			var keep []string
			for l := range strings.SplitSeq(string(b), "\n") {
				if !strings.Contains(l, "Serena") {
					keep = append(keep, l)
				}
			}
			_ = os.WriteFile(p, []byte(strings.Join(keep, "\n")), 0o644)
		},
		"dependabot without ignore": func(root string) {
			_ = os.MkdirAll(filepath.Join(root, ".github"), 0o755)
			_ = os.WriteFile(filepath.Join(root, ".github/dependabot.yml"), []byte("version: 2\nupdates:\n  - package-ecosystem: pip\n    directory: /configs/serena\n"), 0o644)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root := copyRepo(t)
			if p := check(root); len(p) != 0 {
				t.Fatalf("copy must pass before mutation: %v", p)
			}
			mutate(root)
			if p := check(root); len(p) == 0 {
				t.Fatal("guard did not detect the change")
			}
		})
	}
}
