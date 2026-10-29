package routing

import (
	"strings"
	"testing"
)

func TestRoleOf(t *testing.T) {
	cases := []struct {
		s    TurnState
		want Role
	}{
		{TurnState{}, RoleAnswer},
		{TurnState{Calls: 3}, RoleAnswer},
		{TurnState{Change: true}, RolePlan},
		{TurnState{Change: true, Calls: 2}, RoleExplore},
		{TurnState{Change: true, Calls: 2, Edited: true}, RoleEdit},
		{TurnState{Calls: 2, Edited: true}, RoleEdit}, // a chat turn that switched to code at its first edit
		{TurnState{Change: true, Calls: 4, Edited: true, Failed: true}, RoleDebug},
		{TurnState{Calls: 1, Failed: true}, RoleDebug},
	}
	for _, c := range cases {
		if got := RoleOf(c.s); got != c.want {
			t.Errorf("RoleOf(%+v) = %s, want %s", c.s, got, c.want)
		}
	}
}

func TestThinking(t *testing.T) {
	if got := Thinking(PolicyOff, RoleExplore, nil, "toggle"); got != "" {
		t.Errorf("policy off set %q", got)
	}
	for _, caps := range []string{"always", "none", ""} {
		if got := Thinking(PolicyAdaptive, RoleExplore, nil, caps); got != "" {
			t.Errorf("model thinking %q: set %q", caps, got)
		}
	}
	if Thinking(PolicyAdaptive, RoleExplore, nil, "toggle") != ThinkOff || Thinking(PolicyAdaptive, RoleDebug, nil, "toggle") != ThinkOn ||
		Thinking(PolicyAdaptive, RoleClassify, nil, "toggle") != ThinkSkip {
		t.Error("defaults")
	}
	if got := Thinking(PolicyAdaptive, RoleExplore, map[Role]string{RoleExplore: ThinkOn}, "toggle"); got != ThinkOn {
		t.Errorf("override ignored: %q", got)
	}
	for _, r := range Roles {
		if _, ok := DefaultThinking[r]; !ok {
			t.Errorf("no default for %s", r)
		}
	}
}

func TestChooseNeedsEvidence(t *testing.T) {
	cur := Candidate{Profile: "big", Fast: true, LoadSeconds: 30, Perf: map[string]Perf{"": {Tasks: 26, Passed: 24, Mean: 40}}}
	// Better on paper, but three tasks are not evidence.
	small := Candidate{Profile: "small", Fast: true, LoadSeconds: 5, Perf: map[string]Perf{"": {Tasks: 3, Passed: 3, Mean: 10}}}
	if c := Choose("big", []Candidate{cur, small}, "", 10, 20, DefaultHysteresis); c.Switch {
		t.Fatalf("switched on 3 tasks: %+v", c)
	}
	// A clearly weaker model never replaces it, however fast.
	small.Perf[""] = Perf{Tasks: 26, Passed: 12, Mean: 10}
	if c := Choose("big", []Candidate{cur, small}, "", 10, 20, DefaultHysteresis); c.Switch {
		t.Fatalf("switched to a weaker model: %+v", c)
	}
	// As reliable and faster: switch when the time saved repays the load.
	small.Perf[""] = Perf{Tasks: 26, Passed: 23, Mean: 20}
	if c := Choose("big", []Candidate{cur, small}, "", 10, 20, DefaultHysteresis); !c.Switch || c.Profile != "small" {
		t.Fatalf("did not switch: %+v", c)
	}
	// ... but not for one task when the saving does not repay twice the load.
	small.LoadSeconds = 15
	if c := Choose("big", []Candidate{cur, small}, "", 10, 1, DefaultHysteresis); c.Switch {
		t.Fatalf("switched for one task: %+v", c)
	}
	// A switch whose cost was never measured is not made.
	small.LoadSeconds = 0
	if c := Choose("big", []Candidate{cur, small}, "", 10, 20, DefaultHysteresis); c.Switch {
		t.Fatalf("switched at an unmeasured cost: %+v", c)
	}
	// A slow fit is never chosen.
	small.Fast, small.LoadSeconds = false, 1
	if c := Choose("big", []Candidate{cur, small}, "", 10, 20, DefaultHysteresis); c.Switch {
		t.Fatalf("switched to a slow fit: %+v", c)
	}
}

func TestChooseReliabilityAndDwell(t *testing.T) {
	cur := Candidate{Profile: "a", Fast: true, Perf: map[string]Perf{"hard": {Tasks: 10, Passed: 2, Mean: 300}, "": {Tasks: 30, Passed: 20, Mean: 100}}}
	b := Candidate{Profile: "b", Fast: true, LoadSeconds: 60, Perf: map[string]Perf{"hard": {Tasks: 10, Passed: 9, Mean: 600}}}
	c := Choose("a", []Candidate{cur, b}, "hard", 5, 1, DefaultHysteresis)
	if !c.Switch || c.Profile != "b" || !strings.Contains(c.Reason, "beyond chance") {
		t.Fatalf("more reliable on the category, slower: %+v", c)
	}
	if c := Choose("a", []Candidate{cur, b}, "hard", 1, 1, DefaultHysteresis); c.Switch {
		t.Fatalf("switched within the dwell: %+v", c)
	}
	// Compared on the same kind of task only: b's category figures do not
	// stand against a's overall ones.
	cur.Perf = map[string]Perf{"": {Tasks: 30, Passed: 20, Mean: 100}}
	if c := Choose("a", []Candidate{cur, b}, "hard", 5, 1, DefaultHysteresis); c.Switch {
		t.Fatalf("compared different kinds of task: %+v", c)
	}
	if c := Choose("unknown", []Candidate{cur, b}, "hard", 5, 1, DefaultHysteresis); c.Switch || c.Profile != "unknown" {
		t.Fatalf("unmeasured current model: %+v", c)
	}
}

func TestWilson(t *testing.T) {
	lo, hi := wilson(24, 26)
	if lo < 0.75 || lo > 0.85 || hi < 0.95 || hi > 1 {
		t.Fatalf("wilson(24,26) = %.3f, %.3f", lo, hi)
	}
	if lo, hi := wilson(0, 0); lo != 0 || hi != 1 {
		t.Fatal("no data must be the whole interval")
	}
}
