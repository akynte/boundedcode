package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/ecosystem"
	"github.com/akynte/boundedcode/internal/lang"
)

// A language and its build system come from the user's configuration, with
// no change to BoundedCode: recognised, outlined, checked, detected.
func TestLanguagesFromConfig(t *testing.T) {
	src := `
languages:
  - id: nimcfg
    name: NimCfg
    extensions: [.nimcfg]
    definitions: ['^proc\s+(?P<name>\w+)']
    tests: ['_test\.nimcfg$']
    check: {what: nim check, tools: [nim], args: [check, "{file}"]}
    lsp: [nimlangserver]
build_systems:
  - id: nimble-cfg
    kind: Nimble package
    markers: ["*.nimble"]
    languages: [nimcfg]
    tool: nimble
    build: nimble build
    test: nimble test
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil && strings.Contains(err.Error(), "languages") {
		t.Fatal(err)
	}
	registerLanguages(&cfg)
	l := lang.ForPath("src/app.nimcfg", "")
	if l == nil || l.Name != "NimCfg" || !l.IsTest("src/app_test.nimcfg") || l.Check == nil || l.LSP[0].Bin != "nimlangserver" {
		t.Fatalf("language %+v", l)
	}
	if name, _ := l.Definition("proc greet() ="); name != "greet" {
		t.Fatalf("definition %q", name)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shop.nimble"), []byte("version = \"0.1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := ecosystem.Detect(dir, ecosystem.Options{})
	if len(rep.Projects) != 1 || rep.Projects[0].System != "nimble-cfg" || rep.Projects[0].Test != "nimble test" {
		t.Fatalf("projects %+v", rep.Projects)
	}
}

func TestLanguageConfigValidation(t *testing.T) {
	for _, src := range []string{
		"languages: [{id: x}]",
		"languages: [{id: x, extensions: [.x], definitions: ['(unclosed']}]",
		"languages: [{id: x, extensions: [.x], check: {tools: [x]}}]",
		"build_systems: [{id: b, markers: [x]}]",
	} {
		var cfg config.Config
		if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err == nil || !(strings.Contains(err.Error(), "languages") || strings.Contains(err.Error(), "build_systems")) {
			t.Errorf("%s: %v", src, err)
		}
	}
}
