package xservice

// Constant evaluation follows bindings, concatenations and template
// substitutions. Expanding the same binding many times per expression (or
// through a binding cycle) grows exponentially with depth: ordinary code such
// as vuejs/core's collectionHandlers.ts once drove a scan to ~58 GiB. Each
// top-level evaluation therefore gets a step budget and its result a length
// cap; exceeding either degrades the value to unresolved, never the scan.
const (
	maxEvalSteps = 20000
	maxEvalLen   = 2048
)

// evalBudget counts the work of one top-level evaluation. It is exhausted by
// too many steps or by a value longer than maxEvalLen.
type evalBudget struct {
	steps     int
	exhausted bool
}

// step records one unit of work and reports whether evaluation may go on.
func (b *evalBudget) step() bool {
	b.steps++
	if b.steps > maxEvalSteps {
		b.exhausted = true
	}
	return !b.exhausted
}

// capLen bounds an evaluated string; it reports whether s fit.
func capLen(s string) (string, bool) {
	if len(s) <= maxEvalLen {
		return s, true
	}
	return s[:maxEvalLen], false
}
