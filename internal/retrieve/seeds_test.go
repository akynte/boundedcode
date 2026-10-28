package retrieve

import (
	"strings"
	"testing"
)

func seedTexts(ss []Seed) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Kind+":"+s.Text)
	}
	return out
}

func TestSeedRanking(t *testing.T) {
	req := "Parsing fails when a matcher uses quotes. `ParseConfig()` should accept it; see `server.read_timeout` and --strict-mode.\n" +
		"```\nfoo := NewThing(bar)\ncfg.Load(foo)\n```\n" +
		"Error output:\n```\nError: wrong argument count or unexpected line ending after 'expression', at Caddyfile:2\n```\n" +
		"The class `.foo` should get `bar` too."
	got := seedTexts(Seeds(req))
	if len(got) == 0 || !strings.HasPrefix(got[0], "diagnostic:wrong argument count or unexpected line ending after") {
		t.Fatalf("exact error message must come first: %v", got)
	}
	idx := func(s string) int {
		for i, g := range got {
			if g == s {
				return i
			}
		}
		return -1
	}
	if i, j := idx("name:ParseConfig"), idx("sample:NewThing"); i < 0 || j < 0 || i > j {
		t.Fatalf("explicit function name must beat sample identifiers: %v", got)
	}
	if idx("config:server.read_timeout") < 0 || idx("config:--strict-mode") < 0 {
		t.Fatalf("config key and flag must be kept: %v", got)
	}
	for _, g := range got {
		if strings.HasSuffix(g, ":foo") || strings.HasSuffix(g, ":bar") || strings.HasSuffix(g, ":.foo") {
			t.Fatalf("placeholder name kept as a seed: %v", got)
		}
	}
}

func TestDiagnosticFragment(t *testing.T) {
	cases := map[string]string{
		`panic: runtime error: index out of range [3] with length 3`:                    "runtime error",
		`level=error msg="failed to load config file /etc/app.yaml: permission denied"`: "failed to load config file",
		`TypeError: Cannot read properties of undefined (reading 'nodes')`:              "Cannot read properties of undefined",
		`just some ordinary sentence about the feature`:                                 "",
		`x := 1`: "",
	}
	for line, want := range cases {
		got := diagnosticFragment(line)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%q: got %q, want it to contain %q", line, got, want)
		}
	}
}
