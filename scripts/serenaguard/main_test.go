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

// TestInstallScanBypasses covers install forms an earlier version of the scan
// missed, plus the forms that must keep passing.
func TestInstallScanBypasses(t *testing.T) {
	const ver, commit = "1.7.0", "949a27ef1e5fda1a6e7b561e777bcece345c6ffd"
	flagged := []string{
		"pip install serena-agent",
		"  pip install serena-agent && echo done",
		"uvx --from serena-agent serena start-mcp-server",
		"uv tool install serena-agent==2.0.0",
		"uv tool install serena-agent==1.7.01",
		"uv tool install serena-agent==1.7.0.post1",
		"uv tool install 'serena-agent>=1.7.0'",
		"pip install serena_agent",
		"pip install Serena.Agent~=1.7",
		"uvx serena-agent@latest",
		"uv tool install git+https://github.com/oraios/serena@main",
		"uv tool install git+https://github.com/oraios/serena.git@v2.0.0",
		"uv tool install git+https://github.com/oraios/serena",
		"pip install https://github.com/oraios/serena/archive/refs/heads/main.zip",
		`"serena-agent",`,
	}
	for _, l := range flagged {
		if len(scanLine(l, ver, commit)) == 0 {
			t.Errorf("not flagged: %s", l)
		}
	}
	allowed := []string{
		"pip install serena-agent==1.7.0",
		`    "serena-agent==1.7.0",`,
		"uvx serena-agent@1.7.0",
		"PyPI `serena-agent==1.7.0`.",
		"| ok | serena-agent | 1.7.0 | MIT License |",
		"uv tool install git+https://github.com/oraios/serena@v1.7.0",
		"uv tool install git+https://github.com/oraios/serena@" + commit,
		"pip install https://github.com/oraios/serena/archive/refs/tags/v1.7.0.tar.gz",
		"Serena (https://github.com/oraios/serena) v1.7.0 is MIT.",
	}
	for _, l := range allowed {
		if p := scanLine(l, ver, commit); len(p) != 0 {
			t.Errorf("flagged %q: %v", l, p)
		}
	}
}

// TestInstallScanFiles checks the file-level rules: mid-file matches, Markdown
// scanning, and the allow marker (Markdown only).
func TestInstallScanFiles(t *testing.T) {
	cases := map[string]struct {
		path, body string
		wantFail   bool
	}{
		"mid-file pip install":      {"scripts/setup.sh", "#!/bin/sh\nset -e\npip install serena-agent\necho ok\n", true},
		"uvx --from":                {"scripts/run.sh", "uvx --from serena-agent serena --help\n", true},
		"uv tool install v2":        {"scripts/tool.sh", "uv tool install serena-agent==2.0.0\n", true},
		"git ref main":              {"Dockerfile", "RUN uv pip install git+https://github.com/oraios/serena@main\n", true},
		"markdown install":          {"docs/howto.md", "Run:\n\n```\npip install serena-agent\n```\n", true},
		"marker ignored in scripts": {"scripts/x.sh", "pip install serena-agent # serenaguard:allow\n", true},
		"markdown prose with marker": {"docs/note.md",
			"Never install serena-agent 2.x. <!-- serenaguard:allow -->\n", false},
		"markdown pinned":   {"docs/pinned.md", "uv tool install serena-agent==1.7.0\n", false},
		"go code unscanned": {"internal/x/x.go", "const pkg = \"serena-agent\"\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := copyRepo(t)
			p := filepath.Join(root, c.path)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got := check(root)
			if c.wantFail && len(got) == 0 {
				t.Fatal("guard did not detect the install")
			}
			if !c.wantFail && len(got) != 0 {
				t.Fatalf("unexpected problems: %v", got)
			}
		})
	}
}
