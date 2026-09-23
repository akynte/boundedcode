package recipe_test

import (
	"github.com/akynte/boundedcode/internal/recipe"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestMonorepoPresetsCoverEveryModule(t *testing.T) {
	root := t.TempDir()
	// node_modules beside each manifest: discovery emits npm presets only for
	// an installed one, because it never installs dependencies and `npm run`
	// against an empty tree exits 127 on the first missing binary.
	for _, path := range []string{"ui/node_modules/.keep", "ui/apps/web/node_modules/.keep"} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"a/go.mod", "b/go.mod", "rust/Cargo.toml", "ui/package.json", "ui/apps/web/package.json"} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		body := ""
		if filepath.Ext(path) == ".json" {
			body = `{"packageManager":"pnpm@9.12.3","scripts":{"build":"build","test":"test","typecheck":"tsc"}}`
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	presets, err := recipe.DiscoverPresets(root, recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, p := range presets {
		counts[p.Dir]++
	}
	if counts["a"] != 4 || counts["b"] != 4 || counts["rust"] != 4 || counts["ui"] != 3 || counts["ui/apps/web"] != 0 {
		t.Fatalf("wrong discovery: %+v", counts)
	}
	results := make([]recipe.Result, 0, len(presets))
	for _, p := range presets {
		results = append(results, recipe.Result{Recipe: p.Name, Kind: p.Kind, Status: recipe.Pass, Candidate: "code"})
	}
	if ok, _ := recipe.CheckPresets(presets, results, "code"); !ok {
		t.Fatal("complete evidence refused")
	}
	if ok, _ := recipe.CheckPresets(presets, results[1:], "code"); ok {
		t.Fatal("another module's pass hid missing evidence")
	}
}

func TestPresetConfigCannotEscapeAndRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		"version=1\n[[presets]]\nname='bad'\nkind='test'\nargv=['go','test']\ndir='../outside'\ntimeout_seconds=10\n",
		"version=1\nunknown=true\n",
	} {
		if err := os.WriteFile(filepath.Join(root, ".agent/verify.toml"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := recipe.DiscoverPresets(root, recipe.Standard); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}

func TestGoJSONKeepsIndividualBaselineTests(t *testing.T) {
	status, summary := recipe.GoTestJSON(1, "{\"Action\":\"pass\",\"Package\":\"p\",\"Test\":\"TestGood\"}\n{\"Action\":\"fail\",\"Package\":\"p\",\"Test\":\"TestBad\"}\n", "")
	if status != recipe.Fail || summary.Tests["p/TestGood"] != recipe.Pass || summary.Tests["p/TestBad"] != recipe.Fail {
		t.Fatalf("lost tests: %+v", summary)
	}
}

// A Python repository must be verifiable, and it must be verified with the
// tools it actually declares.
//
// The three imported SWE-bench tasks are what made this necessary: discovery
// knew go.mod, Cargo.toml and package.json, so sympy and pytest produced no
// presets at all and every task in them stopped at INTAKE with "no
// verification presets".
func TestPythonPresetsFollowTheProjectsOwnConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		want   []string
		absent []string
	}{
		{
			name: "pytest declared in pytest.ini, nothing else configured",
			files: map[string]string{
				"setup.py":   "from setuptools import setup\nsetup()\n",
				"pytest.ini": "[pytest]\ntestpaths = tests\n",
			},
			want:   []string{".: python compile", ".: pytest"},
			absent: []string{".: mypy", ".: black", ".: ruff", ".: unittest"},
		},
		{
			name: "pyproject naming ruff, black and mypy",
			files: map[string]string{
				"pyproject.toml": "[tool.pytest.ini_options]\naddopts = '-ra'\n" +
					"[tool.ruff]\nline-length = 100\n[tool.black]\n[tool.mypy]\nstrict = true\n",
			},
			want: []string{".: python compile", ".: pytest", ".: ruff", ".: black", ".: mypy"},
		},
		{
			name: "a tests directory and no pytest configuration falls back to the standard library",
			files: map[string]string{
				"setup.cfg":           "[metadata]\nname = thing\n",
				"tests/test_thing.py": "def test(): pass\n",
			},
			want:   []string{".: python compile", ".: unittest"},
			absent: []string{".: pytest"},
		},
		{
			name: "flake8 in setup.cfg",
			files: map[string]string{
				"setup.cfg": "[metadata]\nname = thing\n[flake8]\nmax-line-length = 99\n",
			},
			want:   []string{".: python compile", ".: flake8"},
			absent: []string{".: ruff"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for path, body := range tc.files {
				full := filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			presets, err := recipe.DiscoverPresets(root, recipe.Standard)
			if err != nil {
				t.Fatalf("discovery: %v", err)
			}
			got := map[string]bool{}
			for _, p := range presets {
				got[p.Name] = true
				if err := p.Validate(root); err != nil {
					t.Errorf("%s does not validate: %v", p.Name, err)
				}
			}
			for _, name := range tc.want {
				if !got[name] {
					t.Errorf("missing %q; discovered %v", name, keysOf(got))
				}
			}
			for _, name := range tc.absent {
				if got[name] {
					t.Errorf("%q was emitted for a project that does not configure it", name)
				}
			}
		})
	}
}

// A manifest whose dependencies are not installed describes commands that
// cannot run, and a preset that cannot run stops every task in the repository
// before it starts. Django is the real case: a Python project carrying a
// package.json for its admin JavaScript.
func TestUninstalledNodeManifestYieldsNoPresets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"),
		[]byte(`{"scripts":{"test":"grunt test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "setup.py"),
		[]byte("from setuptools import setup\nsetup()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	presets, err := recipe.DiscoverPresets(root, recipe.Standard)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	for _, p := range presets {
		if p.Argv[0] == "npm" || p.Argv[0] == "pnpm" || p.Argv[0] == "yarn" {
			t.Errorf("%s was emitted with no node_modules beside the manifest; it would "+
				"exit 127 and be reported as a broken environment", p.Name)
		}
	}
	if len(presets) == 0 {
		t.Error("the Python side of the project produced nothing either, so the repository " +
			"is unverifiable")
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
