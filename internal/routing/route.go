// Package routing chooses how a model call is served: how much the model
// reasons for the call's role, and which local model a task runs on, from
// measured performance and the machine's resources. It is experimental and
// off by default (direct.routing, routing.models).
//
// Routing never decides what is allowed. Approval, the guard, sandboxing
// and verification run the same whatever the route, and a model's
// confidence lowers no requirement.
package routing

import (
	"fmt"
	"math"
	"sort"
)

// Role is what a model call is for.
type Role string

// Roles. Ranking context is not a model call here: retrieval ranks
// deterministically (internal/retrieve), so it has no role.
const (
	// RoleClassify: the router's one-word classification of a request.
	RoleClassify Role = "classify"
	// RoleAnswer: a call of a turn that changes nothing (a question,
	// a review, a command to run).
	RoleAnswer Role = "answer"
	// RoleExplore: a call of a change turn before its first edit, after
	// tools that only read.
	RoleExplore Role = "explore"
	// RoleEdit: a call of a change turn once it has edited.
	RoleEdit Role = "edit"
	// RoleDebug: a call after a failed command, check or edit.
	RoleDebug Role = "debug"
	// RolePlan: the first call of a change turn.
	RolePlan Role = "plan"
)

// Roles lists every role, in a fixed order for reports.
var Roles = []Role{RoleClassify, RolePlan, RoleExplore, RoleEdit, RoleDebug, RoleAnswer}

// Thinking values: on, off, or "" to leave the model's default. ThinkSkip
// makes no model call: the deterministic answer stands (classify only).
const (
	ThinkOn   = "on"
	ThinkOff  = "off"
	ThinkSkip = "skip"
)

// Policies.
const (
	PolicyOff      = "off"      // every call as the model's default
	PolicyAdaptive = "adaptive" // thinking chosen per role
)

// DefaultThinking is the adaptive policy's choice per role:
//   - no model call for the router's classification: with thinking, the
//     model never answered within the call's 80 tokens (the rules' choice
//     stood, 2.3 s later); without, it answered but sent most questions
//     to a change (docs/benchmarks/adaptive-2026-10.md#routing);
//   - no thinking for calls that only decide what to read next or answer
//     from what was read;
//   - the model's own thinking where a change is planned, written or
//     debugged.
var DefaultThinking = map[Role]string{
	RoleClassify: ThinkSkip,
	RoleAnswer:   ThinkOff,
	RoleExplore:  ThinkOff,
	RolePlan:     ThinkOn,
	RoleEdit:     ThinkOn,
	RoleDebug:    ThinkOn,
}

// Thinking returns the thinking setting a call of role gets under policy
// for a model whose thinking capability is modelThinking ("toggle",
// "always", "none" or "" unknown). Only a model that can turn thinking off
// per request gets a setting; every other gets "".
func Thinking(policy string, role Role, overrides map[Role]string, modelThinking string) string {
	if policy != PolicyAdaptive || modelThinking != "toggle" {
		return ""
	}
	if v, ok := overrides[role]; ok {
		return v
	}
	return DefaultThinking[role]
}

// TurnState is what a direct-mode turn has done so far, from which the
// next call's role follows.
type TurnState struct {
	// Change is set for a turn routed to change files (code mode).
	Change bool
	// Calls is the model calls made so far in the turn.
	Calls int
	// Edited: a file was changed in the turn.
	Edited bool
	// Failed: the last tool results include a failure (a command that
	// exited non-zero, a check that failed, an edit that did not apply).
	Failed bool
}

// RoleOf is the role of the next model call of a turn.
func RoleOf(s TurnState) Role {
	switch {
	case s.Failed:
		return RoleDebug
	case !s.Change && !s.Edited:
		return RoleAnswer
	case s.Calls == 0:
		return RolePlan
	case s.Edited:
		return RoleEdit
	default:
		return RoleExplore
	}
}

// Perf is a model's measured results on one kind of task.
type Perf struct {
	Tasks  int     `json:"tasks"`
	Passed int     `json:"passed"`
	Mean   float64 `json:"mean_seconds"` // per task
}

// Candidate is a local model routing may run a task on.
type Candidate struct {
	Profile string `json:"profile"`
	// Fast: the model runs on this machine's GPU (model.Fit.Fast); a slow
	// fit is never chosen.
	Fast bool `json:"fast"`
	// LoadSeconds is the measured time to load it (the switch cost); a
	// model whose load was never measured is not chosen.
	LoadSeconds float64 `json:"load_seconds"`
	// Perf is per task category; "" is all categories together.
	Perf map[string]Perf `json:"perf"`
}

// Choice is a routing decision for one task.
type Choice struct {
	Profile string `json:"profile"`
	Switch  bool   `json:"switch"`
	Reason  string `json:"reason"`
}

// Hysteresis bounds model switching.
type Hysteresis struct {
	// MinTasks is the measured tasks of the category a model needs before
	// it is compared (fewer: not evidence).
	MinTasks int
	// Dwell is the tasks to run on a model after switching to it before
	// switching again.
	Dwell int
	// Payback is how many times its load time a switch must save over the
	// expected remaining tasks of the category.
	Payback float64
}

// DefaultHysteresis: four measured tasks, no switch back within three
// tasks, and a switch must save twice its cost.
var DefaultHysteresis = Hysteresis{MinTasks: 4, Dwell: 3, Payback: 2}

// Choose picks the model for a task of category, given the model loaded
// now (current), the tasks run on it since the last switch (sinceSwitch)
// and the tasks of this category expected before the next one of another
// (remaining, at least 1).
//
// A switch needs evidence. A candidate replaces the current model only
// when its measured pass rate on the category is better beyond chance
// (the lower Wilson bound of its rate above the current model's upper
// bound), or when the rates are indistinguishable and the time it saves
// over the remaining tasks repays its load time Payback times over.
// Without measurements for the category the overall ones are used; with
// none at all the current model stays.
func Choose(current string, cands []Candidate, category string, sinceSwitch, remaining int, h Hysteresis) Choice {
	remaining = max(remaining, 1)
	var cur *Candidate
	for i := range cands {
		if cands[i].Profile == current {
			cur = &cands[i]
		}
	}
	if cur == nil {
		return Choice{Profile: current, Reason: "the current model has no measurements; staying"}
	}
	if sinceSwitch < h.Dwell {
		return Choice{Profile: current, Reason: fmt.Sprintf("switched %d tasks ago (dwell %d)", sinceSwitch, h.Dwell)}
	}
	cp, ccat, ok := perfFor(*cur, category, h.MinTasks)
	if !ok {
		return Choice{Profile: current, Reason: "the current model is not measured on " + label(category) + "; staying"}
	}
	best := Choice{Profile: current, Reason: fmt.Sprintf("%s: %d/%d passed, %.0f s per task; no measured model is better beyond chance or by enough time", current, cp.Passed, cp.Tasks, cp.Mean)}
	bestGain := 0.0
	sorted := append([]Candidate(nil), cands...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Profile < sorted[j].Profile })
	for _, c := range sorted {
		if c.Profile == current || !c.Fast || c.LoadSeconds <= 0 {
			continue // a slow fit, or a switch cost never measured
		}
		p, cat, ok := perfFor(c, category, h.MinTasks)
		if !ok || cat != ccat {
			continue // compared on the same kind of task only
		}
		lo, _ := wilson(p.Passed, p.Tasks)
		_, hi := wilson(cp.Passed, cp.Tasks)
		switch {
		case lo > hi:
			gain := lo - hi + 1 // reliability beats time
			if gain > bestGain {
				bestGain = gain
				best = Choice{Profile: c.Profile, Switch: true, Reason: fmt.Sprintf("%s passed %d/%d on %s against %d/%d for %s: better beyond chance",
					c.Profile, p.Passed, p.Tasks, label(cat), cp.Passed, cp.Tasks, current)}
			}
		case indistinguishable(p, cp):
			saved := (cp.Mean - p.Mean) * float64(remaining)
			if saved > h.Payback*c.LoadSeconds && saved > 0 {
				gain := saved / (saved + c.LoadSeconds) // in (0, 1): below any reliability gain
				if gain > bestGain {
					bestGain = gain
					best = Choice{Profile: c.Profile, Switch: true, Reason: fmt.Sprintf("%s is as reliable on %s and saves %.0f s over %d tasks against %.0f s to load",
						c.Profile, label(cat), saved, remaining, c.LoadSeconds)}
				}
			}
		}
	}
	return best
}

func perfFor(c Candidate, category string, minTasks int) (Perf, string, bool) {
	if p, ok := c.Perf[category]; ok && p.Tasks >= minTasks {
		return p, category, true
	}
	if p, ok := c.Perf[""]; ok && p.Tasks >= minTasks {
		return p, "", true
	}
	return Perf{}, "", false
}

// indistinguishable: neither rate is better beyond chance.
func indistinguishable(a, b Perf) bool {
	alo, ahi := wilson(a.Passed, a.Tasks)
	blo, bhi := wilson(b.Passed, b.Tasks)
	return alo <= bhi && blo <= ahi
}

// wilson is the 90% Wilson score interval of k successes in n.
func wilson(k, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 1
	}
	const z = 1.645
	p := float64(k) / float64(n)
	nf := float64(n)
	d := 1 + z*z/nf
	c := p + z*z/(2*nf)
	w := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf))
	return (c - w) / d, (c + w) / d
}

func label(category string) string {
	if category == "" {
		return "all tasks"
	}
	return category
}
