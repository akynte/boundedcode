package ecosystem

import (
	"errors"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/lang"
)

const polyglot = "testdata/polyglot"

// Every project of the polyglot monorepo is found where it is, with its
// build system and languages; the root is no build root.
func TestDetectPolyglot(t *testing.T) {
	installed := map[string]bool{"go": true, "npm": true, "python3": true}
	rep := Detect(polyglot, Options{MaxProjects: 50, LookPath: func(bin string) (string, error) {
		if installed[bin] {
			return "/usr/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}})
	got := map[string]Project{}
	for _, p := range rep.Projects {
		got[p.Dir+" "+p.System] = p
	}
	for key, want := range map[string]struct {
		langs, test string
		missing     bool
	}{
		"services/api go":    {"go", "go test ./...", false},
		"web node":           {"javascript typescript", "npm test (node --test)", false},
		"web/admin node":     {"typescript javascript", "", false},
		"ml python":          {"python", "python3 -m unittest", false},
		"engine cargo":       {"rust", "cargo test", true},
		"jvm/orders maven":   {"java kotlin", "mvn test", true},
		"jvm/billing gradle": {"kotlin java", "gradle test", true},
		"native/geom cmake":  {"c cpp", "ctest --test-dir build", true},
		"native/vec make":    {"", "make test", true},
		"dotnet dotnet":      {"csharp fsharp", "dotnet test", true},
		"php composer":       {"php", "composer test", true},
		"ruby bundler":       {"ruby", "bundle exec rake test", true},
		"swift swiftpm":      {"swift", "swift test", true},
		"dart pub":           {"dart", "dart test", true},
		"infra terraform":    {"terraform", "", true},
	} {
		p, ok := got[key]
		if !ok {
			t.Errorf("%s not found (have %v)", key, keys(got))
			continue
		}
		if strings.Join(p.Languages, " ") != want.langs || p.Test != want.test || p.Missing != want.missing {
			t.Errorf("%s: languages %v test %q missing %v", key, p.Languages, p.Test, p.Missing)
		}
	}
	if _, ok := got[". make"]; ok {
		t.Error("the root has no build of its own")
	}
	// .NET projects under the solution are counted under it.
	if p := got["dotnet dotnet"]; p.Kind != ".NET solution" || p.Members != 2 {
		t.Errorf(".NET: %+v", p)
	}
}

func keys(m map[string]Project) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The capability report tells missing tools from unsupported functions,
// per language and per project.
func TestCapabilitiesPolyglot(t *testing.T) {
	f := &lang.Finder{LookPath: func(bin string) (string, error) {
		switch bin {
		case "go", "node", "npm", "python3", "bash":
			return "/usr/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}}
	rep := Capabilities(polyglot, f, Options{MaxProjects: 50})
	caps := map[string]map[string]string{}
	for _, l := range rep.Languages {
		caps[l.ID] = map[string]string{}
		for _, c := range l.Capabilities {
			caps[l.ID][c.Kind] = c.Status
		}
	}
	for lid, want := range map[string]map[string]string{
		"go":         {lang.CapCheck: lang.Available, lang.CapLSP: lang.MissingTool},
		"python":     {lang.CapCheck: lang.Available},
		"javascript": {lang.CapCheck: lang.Available},
		"shell":      {lang.CapCheck: lang.Available},
		"yaml":       {lang.CapCheck: lang.Available},
		"ruby":       {lang.CapCheck: lang.MissingTool},
		"php":        {lang.CapCheck: lang.MissingTool},
		"typescript": {lang.CapCheck: lang.Unsupported, lang.CapLint: lang.MissingTool},
		"rust":       {lang.CapCheck: lang.Unsupported, lang.CapFormat: lang.MissingTool},
		"sql":        {lang.CapCheck: lang.Unsupported, lang.CapFormat: lang.Unsupported},
		"csharp":     {lang.CapCheck: lang.Unsupported},
	} {
		for k, st := range want {
			if caps[lid][k] != st {
				t.Errorf("%s %s: %q, want %q", lid, k, caps[lid][k], st)
			}
		}
	}
	proj := map[string]ProjectReport{}
	for _, p := range rep.Projects {
		proj[p.Dir] = p
	}
	if p := proj["services/api"]; p.BuildStatus != lang.Available || p.TestStatus != lang.Available {
		t.Errorf("go project: %+v", p)
	}
	if p := proj["engine"]; p.TestStatus != lang.MissingTool {
		t.Errorf("rust project without cargo: %+v", p)
	}
	if p := proj["web/admin"]; p.TestStatus != lang.Unsupported {
		t.Errorf("a package with no test script: %+v", p)
	}
	s := rep.String()
	for _, want := range []string{"Go", "Rust", "check: missing (ruby)", "generic support", "Every file, any language"} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %q:\n%s", want, s)
		}
	}
}
