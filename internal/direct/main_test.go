package direct

import (
	"os"
	"testing"

	"github.com/akynte/boundedcode/internal/execbox"
)

// The sandbox starts the test binary as its helper.
func TestMain(m *testing.M) {
	execbox.Main()
	os.Exit(m.Run())
}
