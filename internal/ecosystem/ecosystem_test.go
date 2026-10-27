package ecosystem

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for f, content := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// installed is a LookPath that finds only the named tools.
func installed(tools ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, t := range tools {
			if t == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func lines(rep Report) map[string]string {
	out := map[string]string{}
	for _, p := range rep.Projects {
		out[p.Dir+" "+p.Kind] = p.Describe()
	}
	return out
}

func TestDetectPolyglotMonorepo(t *testing.T) {
	root := tree(t, map[string]string{
		// A Go service, a pnpm workspace with two packages, a Python
		// service managed by uv, a Cargo workspace with two crates, a Maven
		// multi-module build with a wrapper, a Gradle build, a .NET
		// solution, Ruby, PHP, Elixir, Terraform, a Makefile at the root.
		"Makefile":                               "build:\n\tgo build ./...\ntest: build\n\tgo test ./...\nVAR := x\n.PHONY: test\n",
		"README.md":                              "",
		".github/workflows/ci.yml":               "",
		"services/api/go.mod":                    "module example.com/api\n\ngo 1.22\n",
		"web/package.json":                       `{"name":"web","private":true,"workspaces":["packages/*"],"scripts":{"test":"vitest run","build":"vite build","lint":"eslint ."}}`,
		"web/pnpm-lock.yaml":                     "",
		"web/packages/ui/package.json":           `{"name":"@web/ui","scripts":{"test":"vitest"}}`,
		"web/packages/api/package.json":          `{"name":"@web/api"}`,
		"ml/pyproject.toml":                      "[project]\nname = \"scorer\"\n\n[dependency-groups]\ndev = [\"pytest>=8\"]\n",
		"ml/uv.lock":                             "",
		"engine/Cargo.toml":                      "[workspace]\nmembers = [\"core\", \"cli\"]\n",
		"engine/core/Cargo.toml":                 "[package]\nname = \"core\"\nversion = \"0.1.0\"\n",
		"engine/cli/Cargo.toml":                  "[package]\nname = \"cli\"\nversion = \"0.1.0\"\n",
		"billing/pom.xml":                        "<project><modules><module>model</module></modules></project>",
		"billing/mvnw":                           "#!/bin/sh\n",
		"billing/mvnw.cmd":                       "",
		"billing/model/pom.xml":                  "<project></project>",
		"android/settings.gradle.kts":            "include(\":app\")\n",
		"android/build.gradle.kts":               "",
		"android/app/build.gradle.kts":           "",
		"desktop/Shop.sln":                       "",
		"desktop/Shop/Shop.csproj":               "",
		"desktop/Shop.Tests/Shop.Tests.csproj":   "",
		"admin/Gemfile":                          "source 'https://rubygems.org'\n",
		"admin/spec/cart_spec.rb":                "",
		"cms/composer.json":                      `{"name":"acme/cms","scripts":{"test":"phpunit"}}`,
		"notify/mix.exs":                         "defmodule Notify.MixProject do\n  def project, do: [app: :notify]\nend\n",
		"infra/main.tf":                          "",
		"infra/modules/net/main.tf":              "",
		"web/node_modules/left-pad/package.json": `{"name":"left-pad"}`,
		"services/api/testdata/mod/go.mod":       "module fixture\n",
		"ml/venv/pyvenv.cfg":                     "home = /usr/bin\n",
		"ml/venv/lib/pkg/setup.py":               "",
	})
	rep := Detect(root, Options{MaxProjects: 50, LookPath: installed("go", "pnpm", "uv", "make")})
	got := lines(rep)
	mvn := "./mvnw"
	if runtime.GOOS == "windows" {
		mvn = `.\mvnw.cmd`
	}
	for key, want := range map[string]string{
		". Makefile":                    "Makefile targets: build, test",
		"services/api Go module":        "Go module example.com/api (build: go build ./..., test: go test ./...)",
		"web Node package":              "Node package web (build: pnpm run build (vite build), test: pnpm run test (vitest run), lint: pnpm run lint (eslint .), 2 member projects)",
		"ml Python project":             "Python project scorer (uv, test: uv run pytest)",
		"engine Rust workspace":         "Rust workspace (build: cargo build, test: cargo test, 2 member projects) [cargo not found on PATH]",
		"billing Maven project":         "Maven project (build: " + mvn + " compile, test: " + mvn + " test, 1 member projects)",
		"android Gradle project":        "Gradle project (build: gradle assemble, test: gradle test, 1 member projects) [gradle not found on PATH]",
		"desktop .NET solution":         ".NET solution (build: dotnet build, test: dotnet test, 2 member projects) [dotnet not found on PATH]",
		"admin Ruby project":            "Ruby project (test: bundle exec rspec) [bundle not found on PATH]",
		"cms PHP project":               "PHP project acme/cms (test: composer test) [composer not found on PATH]",
		"notify Elixir project":         "Elixir project notify (build: mix compile, test: mix test) [mix not found on PATH]",
		"infra Terraform configuration": "Terraform configuration (build: terraform init -backend=false && terraform validate, 1 member projects) [terraform not found on PATH]",
	} {
		if got[key] != want {
			t.Errorf("%s:\n got %q\nwant %q", key, got[key], want)
		}
	}
	if len(rep.Projects) != 12 {
		t.Errorf("projects: %d\n%v", len(rep.Projects), got)
	}
	for key := range got {
		if strings.Contains(key, "node_modules") || strings.Contains(key, "testdata") {
			t.Errorf("dependency or fixture directory reported: %s", key)
		}
	}
	if rep.Projects[0].Dir != "." {
		t.Errorf("shallowest first: %+v", rep.Projects[0])
	}
	if len(rep.CI) != 1 || rep.CI[0] != ".github/workflows/ci.yml" {
		t.Errorf("CI: %v", rep.CI)
	}
}

func TestDetectManagersAndRunners(t *testing.T) {
	root := tree(t, map[string]string{
		"a/package.json":       `{"name":"a","scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`,
		"a/tsconfig.json":      "{}",
		"b/package.json":       `{"name":"b","packageManager":"yarn@4.1.0","scripts":{"test":"jest"}}`,
		"c/package.json":       `{"name":"c","scripts":{"test":"node --test"}}`,
		"c/package-lock.json":  "{}",
		"d/package.json":       "{not json",
		"e/setup.py":           "",
		"e/requirements.txt":   "flask\n",
		"f/pyproject.toml":     "[tool.poetry]\nname = \"f\"\n\n[tool.poetry.group.dev.dependencies]\npytest = \"*\"\n",
		"g/requirements.txt":   "sphinx\n",
		"h/Gemfile":            "",
		"i/composer.json":      `{"name":"x/i","require-dev":{"phpunit/phpunit":"^11"}}`,
		"j/pubspec.yaml":       "name: shop\ndependencies:\n  flutter:\n    sdk: flutter\n",
		"k/deno.json":          "{}",
		"l/CMakeLists.txt":     "",
		"l/src/CMakeLists.txt": "",
		"m/justfile":           "test:\n  cargo test\nfmt arg:\n  cargo fmt\n",
		"n/env/package.json":   `{"name":"env-demo"}`,
	})
	got := lines(Detect(root, Options{MaxProjects: 50, LookPath: installed("npm", "yarn", "python3", "poetry", "flutter", "cmake", "deno", "php", "just")}))
	for key, want := range map[string]string{
		"a Node package":     "Node package a (TypeScript (tsconfig.json), no test script)",
		"b Node package":     "Node package b (test: yarn run test (jest))",
		"c Node package":     "Node package c (test: npm test (node --test))",
		"d Node package":     "Node package (package.json does not parse)",
		"e Python project":   "Python project (pip, test: python3 -m unittest)",
		"f Python project":   "Python project f (poetry, test: poetry run pytest)",
		"h Ruby project":     "Ruby project (test: bundle exec rake test) [bundle not found on PATH]",
		"i PHP project":      "PHP project x/i (test: vendor/bin/phpunit)",
		"j Flutter package":  "Flutter package shop (build: flutter analyze, test: flutter test)",
		"k Deno project":     "Deno project (build: deno check ., test: deno test)",
		"l CMake project":    "CMake project (build: cmake -S . -B build && cmake --build build, test: ctest --test-dir build, 1 member projects)",
		"m justfile":         "justfile (recipes: test, fmt)",
		"n/env Node package": "Node package env-demo",
	} {
		if got[key] != want {
			t.Errorf("%s:\n got %q\nwant %q", key, got[key], want)
		}
	}
	if _, ok := got["g Python project"]; ok {
		t.Error("a requirements.txt without Python code below the root is not a project")
	}
}

func TestDetectFallbackAndLimits(t *testing.T) {
	root := tree(t, map[string]string{
		"README.md":        "",
		"scripts/run.lua":  "",
		".gitlab-ci.yml":   "",
		"a/b/c/d/e/go.mod": "module deep\n",
		"a/b/c/d/go.mod":   "module four\n",
	})
	rep := Detect(root, Options{LookPath: installed()})
	if len(rep.Projects) != 1 || rep.Projects[0].Dir != "a/b/c/d" || !rep.Truncated {
		t.Fatalf("depth limit: %+v", rep)
	}
	if len(rep.CI) != 1 || rep.CI[0] != ".gitlab-ci.yml" {
		t.Fatalf("CI: %v", rep.CI)
	}
	rep = Detect(root, Options{MaxEntries: 3, LookPath: installed()})
	if !rep.Truncated || len(rep.Projects) != 0 {
		t.Fatalf("entry budget: %+v", rep)
	}
	rep = Detect(tree(t, map[string]string{"x/go.mod": "module x\n", "y/go.mod": "module y\n", "z/go.mod": "module z\n"}), Options{MaxProjects: 2, LookPath: installed("go")})
	if len(rep.Projects) != 2 || rep.More != 1 {
		t.Fatalf("project cap: %+v", rep)
	}
	if rep := Detect(filepath.Join(root, "missing"), Options{}); len(rep.Projects) != 0 {
		t.Fatalf("missing root: %+v", rep)
	}
}

func TestDetectDoesNotFollowSymlinks(t *testing.T) {
	outside := tree(t, map[string]string{"go.mod": "module outside\n"})
	root := tree(t, map[string]string{"README.md": ""})
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks:", err)
	}
	if rep := Detect(root, Options{LookPath: installed()}); len(rep.Projects) != 0 {
		t.Fatalf("followed a symlink out of the root: %+v", rep.Projects)
	}
}
