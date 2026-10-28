package orchestrator

import "testing"

// A verification that failed only for missing tools or the environment did
// not judge the change: the task blocks instead of asking the agent to fix
// code that may be fine.
func TestNotTheChange(t *testing.T) {
	for summary, want := range map[string]bool{
		"api/cargo-test: cargo is not installed [missing tool]":                          true,
		"api/tool: x [missing tool]; web/npm-test: engine down [environment]":            true,
		"api/go-test: FAIL TestX":                                                        false,
		"api/go-test: FAIL TestX; web/cargo-test: cargo is not installed [missing tool]": false,
		"api/go-test: timeout after 20m0s (not verified) [timeout]":                      false,
		"": false,
	} {
		if got := notTheChange(summary); got != want {
			t.Errorf("%q: %v", summary, got)
		}
	}
}
