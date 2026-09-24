package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
)

// The lifecycle primitives below are what an executor that is not
// BoundedCode's own model loop needs in order to be supervised.
//
// `bcode task run` drives a model and owns everything between start and finish, so
// it never needed these as separate calls. An editor's agent does own that
// middle: it edits, it asks the user questions, it decides when it is done. What
// it cannot do is judge itself, and what it does not do is remember. These
// record what happened and judge the result, without taking the keyboard.
//
// The states are the ones the ledger's CHECK constraint already permits. A
// parallel vocabulary — STARTED, EXECUTING, VERIFYING — would read well in a
// diagram and would not be writable to the database, so the diagram's states are
// mapped onto the existing ones and the finer detail lives in the journal, which
// is where a sequence of events belongs anyway.

// Started maps to pending: the task exists and its intent is recorded.
// Executing maps to running. Verified maps to review — the work passed the
// contract and is awaiting the human's reading of it. Finished maps to accepted.

// Review is the final record of a supervised task.
//
// It is not an approval. By the time it exists the agent has already edited the
// working tree, so there is nothing left to withhold; the change is in git and
// `git diff` is the authoritative view of it. What this adds is the part git
// cannot reconstruct: what was asked for, what the user decided along the way,
// what was checked, and what the contract concluded.
type Review struct {
	TaskID            string        `json:"task_id"`
	Intent            string        `json:"intent"`
	Verdict           string        `json:"verdict"`
	Decisions         []Decision    `json:"decisions,omitempty"`
	FilesChangedCount int           `json:"files_changed_count"`
	FilesChanged      []string      `json:"files_changed,omitempty"`
	Checks            []CheckResult `json:"checks,omitempty"`
	Protected         []string      `json:"protected_violations,omitempty"`
	Warnings          []string      `json:"warnings,omitempty"`
	FinishedAt        time.Time     `json:"finished_at"`
}

// Decision is one question the executor had to ask, and what it was told.
type Decision struct {
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Rationale string    `json:"rationale,omitempty"`
	At        time.Time `json:"at"`
}

// CheckResult is one verification recipe's verdict, flattened for a reader.
type CheckResult struct {
	Name      string      `json:"name"`
	Kind      recipe.Kind `json:"kind,omitempty"`
	Status    string      `json:"status"`
	Headline  string      `json:"headline"`
	Candidate string      `json:"candidate,omitempty"`
}

// StartTask opens a supervised task and journals the intent before any work.
//
// §7.1's ordering is the reason this is a call the executor makes rather than
// something inferred later: the intent is durable before the side effect, so an
// interrupted session leaves a state that can be reconciled instead of guessed.
func StartTask(ctx context.Context, st *store.Store, objective string, level recipe.Level) (task.Task, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return task.Task{}, fmt.Errorf("supervisor: a task needs an objective")
	}
	t := task.Task{
		ID:           task.NewID("oc"),
		Title:        objective,
		Kind:         "supervised",
		Verification: level,
		Budget:       task.Budget{MaxAttempts: 3, MaxWallTime: 30 * time.Minute},
	}
	if err := task.NewStore(st).Create(ctx, t); err != nil {
		return task.Task{}, err
	}
	if err := RecordEvent(ctx, st, t.ID, ledger.KindSessionStart,
		map[string]any{"objective": objective, "executor": "opencode", "phase": "EDITOR"}); err != nil {
		return t, err
	}
	return t, nil
}

// RecordEvent writes one journal entry for something the executor did.
//
// It completes immediately: the executor is reporting an action it has already
// taken, so there is no window between intent and effect for this journal to
// protect. What it preserves is the sequence, which is what makes a session
// reconstructable afterwards.
func RecordEvent(ctx context.Context, st *store.Store, taskID string, kind ledger.Kind, detail any) error {
	return RecordEventCandidate(ctx, st, taskID, kind, detail, "")
}

// RecordEventCandidate is the event form used when a task needs an initial
// candidate identity. The operation column is part of the recovery chain, not
// just presentation: without it, an editor task cannot distinguish a clean
// no-op from changes that predated the task.
func RecordEventCandidate(ctx context.Context, st *store.Store, taskID string, kind ledger.Kind, detail any, candidate string) error {
	l := ledger.New(st)
	h, err := l.Begin(ctx, taskID, kind, detail, candidate)
	if err != nil {
		return err
	}
	return h.Complete(ctx, detail, candidate, "")
}

// InitialCandidate returns the candidate recorded when an editor task opened.
// A missing row is possible for tasks created by older builds; callers must
// apply their compatibility policy rather than treating missing as clean.
func InitialCandidate(ctx context.Context, st *store.Store, taskID string) (string, error) {
	var candidate string
	err := st.Ledger().SQL().QueryRowContext(ctx, `
		SELECT COALESCE(candidate_before, '') FROM operations
		WHERE task_id=? AND kind='session_start' AND candidate_before IS NOT NULL AND candidate_before != ''
		ORDER BY id LIMIT 1`, taskID).Scan(&candidate)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return candidate, err
}

// RecordAnswer stores a question the executor asked the user and the answer it
// was given.
//
// BoundedCode does not ask the questions — it has no channel to the user, and
// the agent holding the conversation does. But the answer is a decision about
// this project, and a decision that lives only in a chat transcript is lost to
// the next session. This is where it stops being conversational and becomes part
// of the record.
func RecordAnswer(ctx context.Context, st *store.Store, taskID, question, answer string) error {
	return RecordAnswerWithRationale(ctx, st, taskID, question, answer, "")
}

// RecordAnswerWithRationale is the lossless form of a user decision. The
// answer says what the user chose; the rationale says why that choice is
// binding when a later session reconstructs the task. Keeping them in the
// same durable operation prevents a future context card from presenting the
// decision as an unexplained preference.
func RecordAnswerWithRationale(ctx context.Context, st *store.Store, taskID, question, answer, rationale string) error {
	question, answer, rationale = strings.TrimSpace(question), strings.TrimSpace(answer), strings.TrimSpace(rationale)
	if question == "" || answer == "" {
		return fmt.Errorf("supervisor: a decision needs both the question and the answer")
	}
	if len(rationale) > 4000 {
		return fmt.Errorf("supervisor: decision rationale exceeds 4000 characters")
	}
	return RecordEvent(ctx, st, taskID, ledger.KindDecision, map[string]any{
		"question":  question,
		"answer":    answer,
		"rationale": rationale,
		"source":    "user",
	})
}

// Decisions reads back what the user was asked during a task.
func Decisions(ctx context.Context, st *store.Store, taskID string) ([]Decision, error) {
	ops, err := ledger.New(st).Operations(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var out []Decision
	for _, op := range ops {
		if op.Kind != ledger.KindDecision {
			continue
		}
		var d struct {
			Question  string `json:"question"`
			Answer    string `json:"answer"`
			Rationale string `json:"rationale"`
			Source    string `json:"source"`
		}
		if err := json.Unmarshal(op.Intent, &d); err != nil || d.Source != "user" {
			continue
		}
		// The ledger stamps in milliseconds; the index stamps in seconds. Being
		// explicit here rather than assuming is how the other one was found.
		out = append(out, Decision{Question: d.Question, Answer: d.Answer, Rationale: d.Rationale, At: time.UnixMilli(op.StartedAt).UTC()})
	}
	return out, nil
}

// VerificationRecord is a verification result as the journal keeps it.
type VerificationRecord struct {
	ID         int64         `json:"id,omitempty"`
	Accepted   bool          `json:"accepted"`
	Candidate  string        `json:"candidate"`
	Level      string        `json:"level,omitempty"`
	Phase      string        `json:"phase,omitempty"`
	Reasons    []string      `json:"reasons,omitempty"`
	OutOfScope []string      `json:"out_of_scope,omitempty"`
	Checks     []CheckResult `json:"checks,omitempty"`
	RecordedAt time.Time     `json:"recorded_at,omitempty"`
}

// RecordVerification journals a verification against the task it judged.
//
// Durable rather than held in memory: the server is long-lived and may be
// restarted between a verification and the finish that reads it, and a review
// that silently forgot what was verified would report UNVERIFIED for work that
// passed. The journal already survives that, so it is where this belongs.
func RecordVerification(ctx context.Context, st *store.Store, taskID string, o *task.Outcome) error {
	if o == nil {
		return nil
	}
	rec := VerificationRecord{
		Accepted: o.Accepted, Candidate: o.Candidate,
		Phase: "VERIFY", RecordedAt: time.Now().UTC(),
		Reasons: o.Reasons, OutOfScope: o.OutOfScope,
	}
	if o.VerificationLevel != "" {
		rec.Level = string(o.VerificationLevel)
	} else if t, err := task.NewStore(st).Get(ctx, taskID); err == nil {
		rec.Level = string(t.Verification)
	}
	for _, r := range o.Results {
		rec.Checks = append(rec.Checks, CheckResult{
			Name: r.Recipe, Kind: r.Kind, Status: string(r.Status), Headline: r.Summary.Headline, Candidate: r.Candidate,
		})
	}
	return RecordEvent(ctx, st, taskID, ledger.KindRecipeRun, rec)
}

// LastVerification returns the most recent verification recorded for a task.
//
// The most recent rather than the first: an executor that fixes what the
// contract reported and verifies again must be judged on the second result, or
// the loop the design asks for could never succeed.
func LastVerification(ctx context.Context, st *store.Store, taskID string) (VerificationRecord, bool, error) {
	ops, err := ledger.New(st).Operations(ctx, taskID)
	if err != nil {
		return VerificationRecord{}, false, err
	}
	var found bool
	var latest VerificationRecord
	for _, op := range ops {
		if op.Kind != ledger.KindRecipeRun {
			continue
		}
		var rec VerificationRecord
		if err := json.Unmarshal(op.Intent, &rec); err != nil {
			continue
		}
		latest, found = rec, true
	}
	return latest, found, nil
}

// ControlledChangedFiles returns paths changed through the supervisor's
// intent-first edit operations. Git status also contains workspace setup files
// and pre-existing operator changes; those are not edits owned by this task.
func ControlledChangedFiles(ctx context.Context, st *store.Store, taskID string) ([]string, error) {
	rows, err := st.Ledger().SQL().QueryContext(ctx, `
		SELECT DISTINCT json_extract(intent, '$.path')
		FROM operations
		WHERE task_id=? AND kind='edit' AND outcome IS NOT NULL
		  AND json_valid(intent) AND json_extract(intent, '$.path') != ''
		ORDER BY json_extract(intent, '$.path')`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

// FinishTask closes a supervised task and produces its final review.
//
// The verdict comes from the verification that was actually run, not from the
// executor's opinion of its own work: a caller that never verified gets a review
// saying so rather than one saying the work is fine.
func FinishTask(ctx context.Context, st *store.Store, repoRoot, taskID string) (Review, error) {
	return finishTask(ctx, st, repoRoot, repoRoot, taskID)
}

// FinishTaskWithPolicyRoot is the editor/native shared completion entry point
// when the candidate lives in a task worktree but repository policy files live
// at the operator workspace root. The candidate and the policy source are
// deliberately separate: an uncommitted policy file must not disappear merely
// because it was not part of the candidate commit.
func FinishTaskWithPolicyRoot(ctx context.Context, st *store.Store, repoRoot, policyRoot, taskID string) (Review, error) {
	return finishTask(ctx, st, repoRoot, policyRoot, taskID)
}

func finishTask(ctx context.Context, st *store.Store, repoRoot, policyRoot, taskID string) (Review, error) {
	ts := task.NewStore(st)
	t, err := ts.Get(ctx, taskID)
	if err != nil {
		return Review{}, err
	}

	rev := Review{TaskID: t.ID, Intent: t.Title, FinishedAt: time.Now().UTC()}
	if rev.Decisions, err = Decisions(ctx, st, taskID); err != nil {
		return Review{}, err
	}

	verified, ok, err := LastVerification(ctx, st, taskID)
	if err != nil {
		return Review{}, err
	}
	currentCandidate, candidateErr := ledger.ContentManifest(repoRoot)
	if candidateErr != nil {
		return Review{}, fmt.Errorf("supervisor: fingerprint current candidate: %w", candidateErr)
	}
	checksOK, checksWhy := verificationChecksSatisfy(verified, t.Verification)
	switch {
	case !ok:
		rev.Verdict = "UNVERIFIED"
		rev.Warnings = append(rev.Warnings,
			"this task was finished without a verification run, so nothing checked the work")
	case verified.Candidate == "" || verified.Candidate != currentCandidate:
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings,
			fmt.Sprintf("the last verification described candidate %s, but the current checkout is %s; reverify after every edit", shortCandidate(verified.Candidate), shortCandidate(currentCandidate)))
	case !verificationLevelSatisfies(verified.Level, t.Verification):
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings,
			fmt.Sprintf("the last verification used level %q, below the task's required level %q", verified.Level, t.Verification))
	case len(verified.Checks) == 0:
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings,
			"the last verification record has no objective check results")
	case !checksOK:
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings, checksWhy)
	case verified.Accepted:
		rev.Verdict = "VERIFIED"
	default:
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings, verified.Reasons...)
	}
	rev.Checks = verified.Checks
	rev.Protected = verified.OutOfScope

	var statusErr error
	statusFiles, statusErr := changedFiles(ctx, repoRoot)
	if statusErr != nil {
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings, "could not inspect the candidate diff: "+statusErr.Error())
	} else {
		controlledFiles, controlledErr := ControlledChangedFiles(ctx, st, taskID)
		if controlledErr != nil {
			rev.Verdict = "NOT VERIFIED"
			rev.Warnings = append(rev.Warnings, "could not read the task's controlled edit journal: "+controlledErr.Error())
		} else if initial, initialErr := InitialCandidate(ctx, st, taskID); initialErr == nil && initial != "" {
			rev.FilesChanged = controlledFiles
			if len(controlledFiles) == 0 && currentCandidate != initial {
				rev.Verdict = "NOT VERIFIED"
				rev.Warnings = append(rev.Warnings,
					"the candidate changed outside the controlled edit journal; BoundedCode cannot attribute that change to this task")
			}
		} else {
			// Compatibility for tasks created before initial-candidate journaling
			// and for native callers that do not use the editor lifecycle.
			rev.FilesChanged = statusFiles
		}
		rev.FilesChangedCount = len(rev.FilesChanged)
		if rev.FilesChangedCount == 0 {
			rev.Warnings = append(rev.Warnings,
				"the working tree has no task-owned edits")
		}
	}
	if t.Budget.MaxWallTime > 0 && time.Since(t.CreatedAt) > t.Budget.MaxWallTime {
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings,
			"the task exceeded its wall-clock budget; the supervisor will not accept a late candidate")
	}

	initialCandidate, initialErr := InitialCandidate(ctx, st, taskID)
	if initialErr != nil {
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings, "could not read the task's initial candidate: "+initialErr.Error())
	} else if t.Kind != "verification" && initialCandidate != "" && currentCandidate == initialCandidate {
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings,
			"the task changed nothing from the candidate recorded when it opened; verification only describes the baseline")
	}

	if len(t.Budget.Scope) > 0 {
		var outside []string
		for _, path := range rev.FilesChanged {
			if !policy.Covers(t.Budget.Scope, path) {
				outside = append(outside, path)
			}
		}
		if len(outside) > 0 {
			rev.Verdict = "NOT VERIFIED"
			rev.Warnings = append(rev.Warnings,
				"files changed outside the task's declared write scope: "+strings.Join(outside, ", "))
		}
	}

	violations, policyErr := protectedViolations(policyRoot, rev.FilesChanged)
	if policyErr != nil {
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings, "repository policy could not be evaluated: "+policyErr.Error())
	} else if len(violations) > 0 {
		rev.Protected = append(rev.Protected, violations...)
		rev.Verdict = "NOT VERIFIED"
		rev.Warnings = append(rev.Warnings,
			"files this repository protects were changed; the task cannot be accepted")
	}

	if err := RecordEvent(ctx, st, taskID, ledger.KindReview, rev); err != nil {
		return rev, err
	}
	state := task.StateAccepted
	if rev.Verdict != "VERIFIED" {
		// A task that did not pass is not accepted. Recording it as such would
		// make the journal agree with the executor rather than with the
		// evidence, which is the failure the contract exists to prevent.
		state = task.StateFailed
	}
	if err := ts.SetState(ctx, taskID, state); err != nil {
		return rev, err
	}
	return rev, nil
}

// changedFiles lists what the working tree has that HEAD does not.
//
// git is the source of truth for what changed: the executor's account of which
// files it touched is a claim, and this is the fact.
func changedFiles(ctx context.Context, repoRoot string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1") //nolint:gosec // a fixed argv against a workspace root
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	return parsePorcelain(string(out)), nil
}

// parsePorcelain reads `git status --porcelain=v1`.
//
// The whole output must not be trimmed before splitting: the first two columns
// are status codes and an unmodified-in-index file begins with a space, so
// trimming the blob shifts that line left by one and eats the first character
// of its path. That produced "imit.go" for a modified limit.go, which is the
// kind of defect that reads as a rendering quirk until someone tries to open
// the file.
func parsePorcelain(out string) []string {
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		// A rename is reported as "old -> new". The new name is the one that
		// exists now, and the one a reader wants.
		if _, after, ok := strings.Cut(path, " -> "); ok {
			path = after
		}
		if path = strings.TrimSpace(path); path != "" {
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files
}

// protectedViolations reports changed files that the repository's policy
// protects. It is checked here as well as during verification because a task
// may be finished without one.
func protectedViolations(repoRoot string, changed []string) ([]string, error) {
	set, err := policy.Load(repoRoot + "/policies")
	if err != nil {
		return nil, err
	}
	if len(set.Policies) == 0 {
		return nil, nil
	}
	hits := set.Check(changed)
	var out []string
	for _, h := range hits {
		out = append(out, h.Path)
	}
	return out, nil
}

// Format renders a review for a person to read.
func verificationChecksSatisfy(record VerificationRecord, required recipe.Level) (bool, string) {
	seen := make(map[recipe.Kind]CheckResult, len(record.Checks))
	for _, check := range record.Checks {
		if previous, ok := seen[check.Kind]; !ok || (previous.Status == "pass" && check.Status != "pass") {
			seen[check.Kind] = check
		}
	}
	for _, kind := range recipe.Required(required) {
		check, ok := seen[kind]
		if !ok || check.Status != "pass" {
			return false, fmt.Sprintf("verification is missing a passing %s result", kind)
		}
	}
	return true, ""
}

func verificationLevelSatisfies(recorded string, required recipe.Level) bool {
	level, ok := recipe.ParseLevel(recorded)
	if !ok {
		return false
	}
	switch required {
	case recipe.Low:
		return true
	case recipe.Standard:
		return level == recipe.Standard || level == recipe.High
	case recipe.High:
		return level == recipe.High
	default:
		return false
	}
}

func shortCandidate(candidate string) string {
	if candidate == "" {
		return "(none)"
	}
	if len(candidate) > 12 {
		return candidate[:12]
	}
	return candidate
}

func (r Review) Format() string {
	var b strings.Builder
	b.WriteString("FINAL REVIEW\n\n")
	fmt.Fprintf(&b, "Task:   %s\n", r.Intent)
	fmt.Fprintf(&b, "Status: %s\n\n", r.Verdict)

	fmt.Fprintf(&b, "Changes:\n  %d file(s) in the working tree\n", r.FilesChangedCount)
	for _, f := range r.FilesChanged {
		fmt.Fprintf(&b, "    %s\n", f)
	}
	if len(r.Checks) > 0 {
		b.WriteString("\nVerification:\n")
		for _, c := range r.Checks {
			mark := "x"
			if c.Status == "pass" {
				mark = "✓"
			}
			fmt.Fprintf(&b, "  %s %-16s %s\n", mark, c.Name, c.Headline)
		}
	}
	if len(r.Decisions) > 0 {
		b.WriteString("\nUser decisions:\n")
		for _, d := range r.Decisions {
			fmt.Fprintf(&b, "  - %s\n    %s\n", d.Question, d.Answer)
			if d.Rationale != "" {
				fmt.Fprintf(&b, "    Why: %s\n", d.Rationale)
			}
		}
	}
	if len(r.Protected) > 0 {
		b.WriteString("\nProtected or out-of-scope paths touched:\n")
		for _, p := range r.Protected {
			fmt.Fprintf(&b, "  %s\n", p)
		}
	}
	b.WriteString("\nWarnings:\n")
	if len(r.Warnings) == 0 {
		b.WriteString("  none\n")
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "  %s\n", w)
	}
	b.WriteString("\nThe change itself is in git. `git diff` is the authoritative view of it; " +
		"this record is what git cannot reconstruct.\n")
	return b.String()
}
