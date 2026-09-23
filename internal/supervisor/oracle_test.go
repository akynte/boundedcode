package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const oneCheck = "checks:\n  - id: x\n    argv: [\"true\"]\n"

func oracleDir(t *testing.T, parent string) string {
	t.Helper()
	dir := filepath.Join(parent, "oracle")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.yaml"), []byte(oneCheck), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A hidden check the model can read is a visible check. The model reads the
// repository, so an oracle inside it is refused rather than loaded.
func TestLoadOracleRefusesOneInsideTheRepository(t *testing.T) {
	repo := t.TempDir()
	_, err := loadOracle(oracleDir(t, repo), repo)
	if err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("an oracle inside the repository must be refused, got %v", err)
	}
}

func TestLoadOracleAcceptsOneOutside(t *testing.T) {
	suite, err := loadOracle(oracleDir(t, t.TempDir()), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if suite == nil || len(suite.Checks) != 1 {
		t.Fatalf("suite = %+v", suite)
	}
}

func TestLoadOracleWithNoDirectoryIsNone(t *testing.T) {
	suite, err := loadOracle("", t.TempDir())
	if err != nil || suite != nil {
		t.Fatalf("no directory must mean no suite, got %+v, %v", suite, err)
	}
}

// Naming an oracle that is not there must fail loudly: the operator would
// otherwise believe work was judged by checks that never ran.
func TestLoadOracleThatIsMissingFails(t *testing.T) {
	if _, err := loadOracle(filepath.Join(t.TempDir(), "missing"), t.TempDir()); err == nil {
		t.Fatal("a missing oracle directory must be an error")
	}
}
