// Package frontier decides when to consult a frontier model (Z1-Z4), builds
// compact escalation packets, and talks to subscription-backed providers.
// Frontier advice returns to the local workflow; the local agent implements.
package frontier

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
)

// Code identifies an escalation trigger.
type Code string

// Triggers.
const (
	Z1 Code = "Z1" // architectural risk (before/while implementing)
	Z2 Code = "Z2" // repeated local failure
	Z3 Code = "Z3" // high-risk pre-merge review
	Z4 Code = "Z4" // explicit user request
)

// Trigger is a fired escalation condition.
type Trigger struct {
	Code   Code   `json:"code"`
	Reason string `json:"reason"`
}

// Signals are the deterministic facts the policy evaluates.
type Signals struct {
	Text                string   // request + goal
	ChangedFiles        []string // workspace-relative, prefixed with repo name
	ChangedRepos        int
	ConsecutiveFailures int
	RejectedStrategies  int
	RepeatedFailure     bool // same failure signature as the previous attempt
	Stuck               bool // runtime stuck detector fired
	PreMerge            bool // verification passed; about to become a merge candidate
	UserRequested       bool
	EscalationsUsed     int
	MaxEscalations      int
	AlreadyReviewedZ1   bool // Z1 fires at most once per task
	AlreadyReviewedZ3   bool
}

var contractPaths = []string{"*.proto", "*openapi*.yaml", "*openapi*.json", "*swagger*", "*.sql", "*/migrations/*", "*.avsc", "*schema*.graphql"}

// Evaluate returns the triggers that fire, most important first. It returns
// nothing when the escalation budget is spent (except Z4, which the user
// asked for explicitly).
func Evaluate(cfg config.EscalationConfig, s Signals) []Trigger {
	var out []Trigger
	if s.UserRequested {
		out = append(out, Trigger{Z4, "user requested frontier review"})
	}
	if s.MaxEscalations > 0 && s.EscalationsUsed >= s.MaxEscalations {
		return out
	}
	text := strings.ToLower(s.Text)
	if !s.AlreadyReviewedZ1 {
		var why []string
		if hits := keywordHits(text, cfg.ArchitecturalRisk); len(hits) > 0 {
			why = append(why, "touches "+strings.Join(hits, ", "))
		}
		if s.ChangedRepos > 1 {
			why = append(why, fmt.Sprintf("changes span %d repositories", s.ChangedRepos))
		}
		if c := matchingFiles(s.ChangedFiles, contractPaths); len(c) > 0 {
			why = append(why, "changes contracts/schemas: "+strings.Join(first(c, 4), ", "))
		}
		// Keywords alone are a weak signal; require two independent reasons
		// or a contract change so routine tasks stay local.
		if len(why) >= 2 || len(matchingFiles(s.ChangedFiles, contractPaths)) > 0 && len(why) >= 1 && s.ChangedRepos > 1 {
			out = append(out, Trigger{Z1, "architectural risk: " + strings.Join(why, "; ")})
		}
	}
	switch {
	case s.Stuck:
		out = append(out, Trigger{Z2, "agent stuck (runtime loop detector)"})
	case s.RepeatedFailure && s.ConsecutiveFailures >= 2:
		out = append(out, Trigger{Z2, "the same verification failure repeated across attempts"})
	case s.ConsecutiveFailures >= cfg.FailedAttempts:
		out = append(out, Trigger{Z2, fmt.Sprintf("%d consecutive failed verification attempts", s.ConsecutiveFailures)})
	case s.RejectedStrategies >= cfg.RejectedStrategies && s.ConsecutiveFailures >= 2:
		out = append(out, Trigger{Z2, fmt.Sprintf("%d strategies rejected", s.RejectedStrategies)})
	}
	if s.PreMerge && !s.AlreadyReviewedZ3 {
		var hits []string
		for _, f := range s.ChangedFiles {
			lf := strings.ToLower(f)
			for _, k := range cfg.HighRiskReview {
				if strings.Contains(lf, k) {
					hits = append(hits, f)
					break
				}
			}
		}
		if len(hits) > 0 {
			out = append(out, Trigger{Z3, "high-risk change before merge: " + strings.Join(first(hits, 5), ", ")})
		}
	}
	return out
}

func keywordHits(text string, kws []string) []string {
	var hits []string
	for _, k := range kws {
		if strings.Contains(text, strings.ToLower(k)) {
			hits = append(hits, k)
		}
	}
	return hits
}

func matchingFiles(files, globs []string) []string {
	var out []string
	for _, f := range files {
		lf := strings.ToLower(filepath.ToSlash(f))
		for _, g := range globs {
			if ok, _ := filepath.Match(g, filepath.Base(lf)); ok {
				out = append(out, f)
				break
			}
			if strings.Contains(g, "/") && strings.Contains(lf, strings.Trim(g, "*")) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

func first(xs []string, n int) []string {
	if len(xs) > n {
		return xs[:n]
	}
	return xs
}
