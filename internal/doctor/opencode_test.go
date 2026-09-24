package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/opencode"
	"github.com/akynte/boundedcode/internal/workspace"
)

func TestOpenCodeLimitsMatchTheActiveProfile(t *testing.T) {
	root := t.TempDir()
	if _, _, err := opencode.RegisterModel(root, "http://127.0.0.1:8080", "Ternary-Bonsai-2-27B-PTQ1_0.gguf"); err != nil {
		t.Fatal(err)
	}
	check := checkOpenCodeLimits(&workspace.Workspace{Root: root}, &config.Profile{ContextTokens: 32768, ReservedOutput: 8192})
	if check.Level != OK || check.Detail == "" {
		t.Fatalf("matching limits were not accepted: %+v", check)
	}
}

func TestOpenCodeLimitMismatchIsFailClosed(t *testing.T) {
	root := t.TempDir()
	if _, _, err := opencode.RegisterModel(root, "http://127.0.0.1:8080", "Ternary-Bonsai-2-27B-PTQ1_0.gguf"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "opencode.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A hand edit or an older setup must be visible to doctor rather than
	// silently changing the effective admission policy.
	edited := string(body)
	edited = replaceOnce(edited, "\"context\": 32768", "\"context\": 65536")
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	check := checkOpenCodeLimits(&workspace.Workspace{Root: root}, &config.Profile{ContextTokens: 32768, ReservedOutput: 8192})
	if check.Level != Fail || check.Fix == "" {
		t.Fatalf("mismatched limits were not fail-closed: %+v", check)
	}
}

func replaceOnce(value, old, replacement string) string {
	for i := 0; i+len(old) <= len(value); i++ {
		if value[i:i+len(old)] == old {
			return value[:i] + replacement + value[i+len(old):]
		}
	}
	return value
}
