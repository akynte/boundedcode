package benchmark

import (
	"testing"

	"github.com/akynte/boundedcode/internal/direct"
)

// Every case is one the rules cannot classify with confidence, so the
// model is asked.
func TestRouterCasesAreUnsure(t *testing.T) {
	for _, c := range RouterCases() {
		if d := direct.Route(c.Request, direct.RiskRules{}, direct.Capabilities{BoundedAvailable: true}); d.Confidence != "low" {
			t.Errorf("%q: the rules are sure (%s: %s)", c.Request, d.Mode, d.Reason)
		}
	}
}
