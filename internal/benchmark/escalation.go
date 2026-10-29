package benchmark

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // read the recorded ledgers

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/frontier"
)

// EscalationRun is a recorded bounded-mode run: its attempts as its ledger
// kept them and its hidden checks' verdict.
type EscalationRun struct {
	Report       string   `json:"report"`
	Task         string   `json:"task"`
	Passed       bool     `json:"passed"` // hidden checks
	FailureClass string   `json:"failure_class,omitempty"`
	Request      string   `json:"-"`
	Goal         string   `json:"-"`
	Repos        int      `json:"repos"`         // repositories of the task
	ChangedRepos int      `json:"changed_repos"` // repositories its change touched
	Changed      []string `json:"-"`
	Attempts     []EscalationAttempt
	Ambiguities  int      `json:"material_ambiguities"`
	MaxAttempts  int      `json:"max_attempts"`
	MaxEsc       int      `json:"max_escalations"`
	Recorded     []string `json:"recorded_triggers"` // what the run's own policy fired (declined: frontier was off)
	// Ledger is the run's state database and TaskID its task.
	Ledger string `json:"-"`
	TaskID string `json:"-"`
}

// EscalationAttempt is one attempt: rejected (with its failure) or not.
type EscalationAttempt struct {
	Rejected bool   `json:"rejected"`
	Reason   string `json:"reason"`
}

// NeedsHelp: the run ended failing its hidden checks for a reason in the
// change (not the environment), so advice could have mattered.
func (r EscalationRun) NeedsHelp() bool {
	return !r.Passed && r.FailureClass != "environment" && r.FailureClass != "infrastructure"
}

// LoadEscalationRuns reads every bounded-mode report matching pattern
// whose tasks' ledgers are still on disk.
func LoadEscalationRuns(ctx context.Context, pattern string) ([]EscalationRun, error) {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	var runs []EscalationRun
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var rep struct {
			Results []struct {
				ID           string `json:"id"`
				StateDir     string `json:"state_dir"`
				Success      bool   `json:"success"`
				FailureClass string `json:"failure_class"`
			} `json:"results"`
		}
		if json.Unmarshal(b, &rep) != nil {
			continue
		}
		for _, res := range rep.Results {
			dir := strings.Replace(res.StateDir, "~", home, 1)
			db := filepath.Join(dir, "data", "state.db")
			if _, err := os.Stat(db); err != nil {
				continue
			}
			run, err := readLedger(ctx, db)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", db, err)
			}
			run.Report, run.Task, run.Passed, run.FailureClass, run.Ledger = filepath.Base(f), res.ID, res.Success, res.FailureClass, db
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func readLedger(ctx context.Context, path string) (EscalationRun, error) {
	var run EscalationRun
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return run, err
	}
	defer db.Close()
	var budget, changed string
	var taskID string
	if err := db.QueryRowContext(ctx, `SELECT id, original_request, goal, budget, changed_files FROM tasks LIMIT 1`).Scan(&taskID, &run.Request, &run.Goal, &budget, &changed); err != nil {
		return run, err
	}
	run.TaskID = taskID
	var b struct {
		MaxAttempts    int `json:"max_attempts"`
		MaxEscalations int `json:"max_escalations"`
	}
	_ = json.Unmarshal([]byte(budget), &b)
	run.MaxAttempts, run.MaxEsc = b.MaxAttempts, b.MaxEscalations
	_ = json.Unmarshal([]byte(changed), &run.Changed)
	repos := map[string]bool{}
	for _, f := range run.Changed {
		repos[strings.SplitN(f, "/", 2)[0]] = true
	}
	run.ChangedRepos = len(repos)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_worktrees WHERE task_id = ?`, taskID).Scan(&run.Repos)
	rows, err := db.QueryContext(ctx, `SELECT outcome, reason FROM strategies WHERE task_id = ? ORDER BY attempt, id`, taskID)
	if err != nil {
		return run, err
	}
	for rows.Next() {
		var outcome, reason string
		if err := rows.Scan(&outcome, &reason); err != nil {
			rows.Close()
			return run, err
		}
		run.Attempts = append(run.Attempts, EscalationAttempt{Rejected: outcome == "rejected", Reason: reason})
	}
	rows.Close()
	var contract string
	if db.QueryRowContext(ctx, `SELECT data FROM events WHERE task_id = ? AND kind = 'task.contract' ORDER BY id DESC LIMIT 1`, taskID).Scan(&contract) == nil {
		var c struct {
			Material int `json:"material"`
		}
		_ = json.Unmarshal([]byte(contract), &c)
		run.Ambiguities = c.Material
	}
	trs, err := db.QueryContext(ctx, `SELECT trigger FROM escalations WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return run, err
	}
	defer trs.Close()
	for trs.Next() {
		var c string
		if err := trs.Scan(&c); err != nil {
			return run, err
		}
		run.Recorded = append(run.Recorded, c)
	}
	return run, trs.Err()
}

// Fired is an escalation a policy would have made.
type Fired struct {
	Attempt int    `json:"attempt"` // 0: before implementing
	Code    string `json:"code"`
	// Actionable: an attempt remained to apply the advice (a pre-merge
	// review is shown to the user either way).
	Actionable bool   `json:"actionable"`
	Reason     string `json:"reason"`
}

var digitsRE = regexp.MustCompile(`[0-9]+`)

// signature stands for the runner's failure signature: the rejection
// reason without numbers (line numbers, durations).
func signature(reason string) string {
	return digitsRE.ReplaceAllString(strings.TrimSpace(reason), "#")
}

// Replay runs a policy over a recorded run, as the task runner would have
// evaluated it at each point (before implementing, after each failed
// attempt, before merge), counting every trigger as sent within the
// task's escalation budget. The attempts are the recorded ones: what the
// advice would have changed is not known.
func Replay(policy string, cfg config.EscalationConfig, run EscalationRun) []Fired {
	var out []Fired
	used, reviewedZ1, reviewedZ3 := 0, false, false
	var escalated []string
	fire := func(attempt int, t frontier.Trigger, actionable bool) {
		used++
		out = append(out, Fired{Attempt: attempt, Code: string(t.Code), Actionable: actionable, Reason: t.Reason})
	}
	remaining := func(k int) int {
		if run.MaxAttempts <= 0 {
			return -1
		}
		return max(run.MaxAttempts-k, 0)
	}
	ambiguities := 0
	if policy == frontier.PolicyAdaptive {
		ambiguities = run.Ambiguities
	}
	for _, t := range frontier.EvaluatePolicy(policy, cfg, frontier.Signals{Text: run.Request + "\n" + run.Goal, ChangedRepos: run.Repos,
		EscalationsUsed: used, MaxEscalations: run.MaxEsc, Ambiguities: ambiguities, RemainingAttempts: remaining(0)}) {
		if t.Code == frontier.Z1 {
			fire(0, t, true)
			reviewedZ1 = true
			break
		}
	}
	consecutive, rejected, lastSig := 0, 0, ""
	for i, a := range run.Attempts {
		k := i + 1
		if !a.Rejected {
			if reviewedZ3 {
				continue
			}
			trs := frontier.EvaluatePolicy(policy, cfg, frontier.Signals{Text: run.Request, ChangedFiles: run.Changed, ChangedRepos: run.ChangedRepos,
				PreMerge: true, EscalationsUsed: used, MaxEscalations: run.MaxEsc, AlreadyReviewedZ1: reviewedZ1})
			for _, t := range trs {
				if t.Code == frontier.Z3 {
					fire(k, t, true)
					reviewedZ3 = true
					break
				}
			}
			continue
		}
		consecutive++
		rejected++
		sig := signature(a.Reason)
		repeated := sig != "" && sig == lastSig
		lastSig = sig
		trs := frontier.EvaluatePolicy(policy, cfg, frontier.Signals{Text: run.Request, ChangedFiles: run.Changed, ChangedRepos: run.ChangedRepos,
			ConsecutiveFailures: consecutive, RejectedStrategies: rejected, RepeatedFailure: repeated, EscalationsUsed: used,
			MaxEscalations: run.MaxEsc, AlreadyReviewedZ1: reviewedZ1, Signature: sig, Escalated: escalated,
			NoChanges: strings.Contains(a.Reason, "made no changes"), Ambiguities: ambiguities, RemainingAttempts: remaining(k)})
		for _, code := range []frontier.Code{frontier.Z2, frontier.Z1} {
			if i := slices.IndexFunc(trs, func(t frontier.Trigger) bool { return t.Code == code }); i >= 0 {
				fire(k, trs[i], remaining(k) != 0)
				if code == frontier.Z1 {
					reviewedZ1 = true
				}
				if code == frontier.Z2 {
					escalated = append(escalated, sig)
				}
				break
			}
		}
	}
	return out
}

// EscalationSummary is a policy's figures over the recorded runs.
type EscalationSummary struct {
	Policy      string         `json:"policy"`
	Runs        int            `json:"runs"`
	NeedHelp    int            `json:"runs_needing_help"`
	Covered     int            `json:"needing_help_with_actionable_escalation"`
	Escalations int            `json:"escalations"`
	OnPassed    int            `json:"escalations_on_passed_runs"`
	Passed      int            `json:"passed_runs"`
	PassedRuns  int            `json:"passed_runs_escalated"`
	Wasted      int            `json:"not_actionable"` // fired with no attempt left to apply them
	ByCode      map[string]int `json:"by_code"`
	// Precision is the share of escalations made on runs that needed help.
	Precision float64 `json:"precision"`
}

// EscalationReport is the bench's.
type EscalationReport struct {
	Started time.Time `json:"started"`
	Runs    int       `json:"runs"`
	// RulesAgreement is the runs whose replayed rules fired exactly the
	// triggers the run recorded (a check of the replay itself).
	RulesAgreement int                   `json:"rules_agreement"`
	Summaries      []EscalationSummary   `json:"summaries"`
	PerRun         []EscalationRunReplay `json:"per_run"`
}

// EscalationRunReplay is one run under every policy.
type EscalationRunReplay struct {
	Task     string             `json:"task"`
	Report   string             `json:"report"`
	Passed   bool               `json:"passed"`
	Recorded []string           `json:"recorded"`
	Fired    map[string][]Fired `json:"fired"`
}

// ReplayEscalations replays each policy over runs.
func ReplayEscalations(cfg config.EscalationConfig, runs []EscalationRun, policies []string) *EscalationReport {
	rep := &EscalationReport{Started: time.Now().UTC(), Runs: len(runs)}
	sums := map[string]*EscalationSummary{}
	for _, p := range policies {
		sums[p] = &EscalationSummary{Policy: p, ByCode: map[string]int{}}
	}
	for _, run := range runs {
		rr := EscalationRunReplay{Task: run.Task, Report: run.Report, Passed: run.Passed, Recorded: run.Recorded, Fired: map[string][]Fired{}}
		for _, p := range policies {
			fired := Replay(p, cfg, run)
			rr.Fired[p] = fired
			s := sums[p]
			s.Runs++
			s.Escalations += len(fired)
			actionable := false
			for _, f := range fired {
				s.ByCode[f.Code]++
				if f.Actionable {
					actionable = true
				} else {
					s.Wasted++
				}
			}
			switch {
			case run.NeedsHelp():
				s.NeedHelp++
				if actionable {
					s.Covered++
				}
			case run.Passed:
				s.Passed++
				s.OnPassed += len(fired)
				if len(fired) > 0 {
					s.PassedRuns++
				}
			}
			if p == frontier.PolicyRules {
				var codes []string
				for _, f := range fired {
					codes = append(codes, f.Code)
				}
				if slices.Equal(uniqSorted(codes), uniqSorted(run.Recorded)) {
					rep.RulesAgreement++
				}
			}
		}
		rep.PerRun = append(rep.PerRun, rr)
	}
	for _, p := range policies {
		s := sums[p]
		if s.Escalations > 0 {
			s.Precision = float64(s.Escalations-s.OnPassed) / float64(s.Escalations)
		}
		rep.Summaries = append(rep.Summaries, *s)
	}
	return rep
}

func uniqSorted(xs []string) []string {
	out := slices.Clone(xs)
	sort.Strings(out)
	return slices.Compact(out)
}

// WriteEscalationMarkdown writes the policies' figures.
func WriteEscalationMarkdown(w io.Writer, r *EscalationReport) error {
	fmt.Fprintf(w, "# Escalation policies replayed on recorded runs\n\n%d recorded bounded-mode runs with their ledgers. The replayed rules fire exactly the recorded triggers in %d of them.\n\n", r.Runs, r.RulesAgreement)
	fmt.Fprintln(w, "| Policy | Escalations | Per run | On runs that passed | Passed runs escalated | Runs needing help covered | Not actionable | By trigger |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|")
	for _, s := range r.Summaries {
		var codes []string
		for _, c := range []string{"Z1", "Z2", "Z3", "Z4"} {
			if n := s.ByCode[c]; n > 0 {
				codes = append(codes, fmt.Sprintf("%s %d", c, n))
			}
		}
		fmt.Fprintf(w, "| %s | %d | %.2f | %d | %d / %d | %d / %d | %d | %s |\n", s.Policy, s.Escalations, float64(s.Escalations)/float64(max(s.Runs, 1)),
			s.OnPassed, s.PassedRuns, s.Passed, s.Covered, s.NeedHelp, s.Wasted, strings.Join(codes, ", "))
	}
	return nil
}
