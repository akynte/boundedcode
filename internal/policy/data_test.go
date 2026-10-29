package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataPolicy(t *testing.T) {
	root := t.TempDir()
	if d, err := LoadDataPolicy(root); err != nil || d.ExternalModels != ExternalAllow {
		t.Fatalf("absent file: %+v %v", d, err)
	}
	if denied, _ := ExternalDenied(root); denied {
		t.Fatal("absent file denied")
	}
	write := func(s string) {
		if err := os.MkdirAll(filepath.Join(root, ".boundedcode"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, DataPolicyFile), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("external_models: deny\n")
	if denied, why := ExternalDenied(t.TempDir(), root); !denied || why == "" {
		t.Fatal("deny not applied")
	}
	// Anything unclear fails closed.
	for _, bad := range []string{"external_models: maybe\n", "external: deny\n", "external_models: [\n"} {
		write(bad)
		if denied, _ := ExternalDenied(root); !denied {
			t.Errorf("%q allowed", bad)
		}
	}
	write("external_models: allow\n")
	if denied, _ := ExternalDenied(root); denied {
		t.Fatal("allow denied")
	}
	if !IsProtectedPath(DataPolicyFile) {
		t.Fatal("the data policy must be a protected path")
	}
}
