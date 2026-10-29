package direct

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/policy"
)

// The agent reads the same files in both modes: what bounded mode masks in
// its container (policy.FindSecretPaths), direct mode refuses to read
// (checkReadable), and the other way round.
func TestSecretPathsAgreeAcrossModes(t *testing.T) {
	r, root, _ := newRunner(t, &scripted{}, ApprovalAuto)
	files := []string{
		".env", ".env.example", ".env.production", "id_rsa", "id_ed25519.pub", "keys/server.pem", "keys/README.md",
		".ssh/config", ".aws/credentials", "config/app.yaml", "config/secrets.yaml", "src/main.go",
		"credentials/store.go", "credentials/token.json", "secrets/db.txt", "terraform.tfvars", ".npmrc", "docs/secrets.md",
	}
	for _, f := range files {
		writeFile(t, root, f, "x\n")
	}
	masked, err := policy.FindSecretPaths(root, 500)
	if err != nil {
		t.Fatal(err)
	}
	isMasked := func(f string) bool {
		for _, m := range masked {
			m = filepath.ToSlash(m)
			if f == m || strings.HasPrefix(f, m+"/") {
				return true
			}
		}
		return false
	}
	for _, f := range files {
		refused := r.checkReadable(filepath.Join(root, f)) != nil
		if refused != isMasked(f) {
			t.Errorf("%s: direct mode refuses=%v, bounded mode masks=%v", f, refused, isMasked(f))
		}
	}
}
