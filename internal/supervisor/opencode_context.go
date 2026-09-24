package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/akynte/boundedcode/internal/artifacts"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
)

// OpenCodeContext reconstructs the authoritative part of an editor task from
// the workspace store. It is deliberately independent of OpenCode's summaries.
// A session binding wins; an unbound new session can inherit the sole unfinished
// supervised task. Ambiguous tasks require an explicit bc_task_resume call.
func OpenCodeContext(ctx context.Context, st *store.Store, repoRoot, sessionID string) (string, error) {
	if !strings.HasPrefix(sessionID, "ses_") || len(sessionID) > 100 {
		return "", fmt.Errorf("invalid OpenCode session ID")
	}
	var taskID string
	err := st.Ledger().SQL().QueryRowContext(ctx, `
		SELECT task_id FROM operations
		WHERE kind = 'session_start' AND json_valid(intent)
		  AND json_extract(intent, '$.session_id') = ?
		ORDER BY id DESC LIMIT 1`, sessionID).Scan(&taskID)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if taskID == "" {
		rows, err := st.Ledger().SQL().QueryContext(ctx, `SELECT id FROM tasks WHERE kind = 'supervised'
			AND state IN ('pending','running','paused','blocked','review') ORDER BY created_at DESC LIMIT 2`)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return "", err
			}
			if taskID != "" {
				return "Multiple supervised tasks are active. Call bc_task_resume with the intended task ID.", nil
			}
			taskID = id
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
	}
	if taskID == "" {
		return "", nil
	}
	t, err := task.NewStore(st).Get(ctx, taskID)
	if err != nil {
		return "", err
	}
	// Only a recent page is needed for the hot context; the ledger retains
	// older decisions for bc_task_history.
	var decisionCount int
	decisions, decisionCount, err := DecisionPage(ctx, st, taskID, 0, 0)
	if err != nil {
		return "", err
	}
	offset := decisionCount - 30
	if offset < 0 {
		offset = 0
	}
	decisions, _, err = DecisionPage(ctx, st, taskID, offset, 30)
	if err != nil {
		return "", err
	}
	verified, hasVerification, err := latestVerification(ctx, st, taskID)
	if err != nil {
		return "", err
	}
	phase, err := CurrentPhase(ctx, st, taskID)
	if err != nil {
		return "", err
	}
	candidate, err := LatestRecordedCandidate(ctx, st, taskID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "BoundedCode authoritative task state (task %s; %s).\n", t.ID, t.State)
	fmt.Fprintf(&b, "Current phase: %s\n", phase)
	if candidate == "" {
		b.WriteString("Current candidate: not recorded; no controlled edit or verification has run yet.\n")
	} else {
		fmt.Fprintf(&b, "Current candidate last recorded by BoundedCode: %s (recompute/reverify after any checkout change).\n", candidate)
	}
	fmt.Fprintf(&b, "Verification level: %s (required checks: %s)\n", t.Verification, requiredChecks(t.Verification))
	fmt.Fprintf(&b, "Original objective: %s\n", boundedText(t.Title, 1500))
	var promptHash string
	_ = st.Ledger().SQL().QueryRowContext(ctx, `SELECT json_extract(intent,'$.original_prompt_hash') FROM operations WHERE task_id=? AND kind='session_start' AND json_extract(intent,'$.original_prompt_hash') != '' ORDER BY id LIMIT 1`, taskID).Scan(&promptHash)
	if promptHash != "" {
		fmt.Fprintf(&b, "Original raw user request: evidence=%s (immutable; call bc_task_memory to recover full text).\n", promptHash)
		if body, err := artifacts.New(st).Get(promptHash); err == nil {
			const maxPromptExcerpt = 1200
			fmt.Fprintf(&b, "Original request excerpt: %s\n", boundedText(string(body), maxPromptExcerpt))
		}
	}
	rows, err := st.Ledger().SQL().QueryContext(ctx, `SELECT intent FROM operations
		WHERE task_id = ? AND kind = ? AND json_valid(intent)
		  AND (COALESCE(json_array_length(intent, '$.requirements'), 0) > 0
		    OR COALESCE(json_array_length(intent, '$.acceptance_criteria'), 0) > 0
		    OR COALESCE(json_array_length(intent, '$.constraints'), 0) > 0
		    OR COALESCE(json_array_length(intent, '$.non_goals'), 0) > 0)
		ORDER BY seq`, taskID, ledger.KindSessionStart)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var details struct {
		Requirements       []string `json:"requirements"`
		AcceptanceCriteria []string `json:"acceptance_criteria"`
		Constraints        []string `json:"constraints"`
		NonGoals           []string `json:"non_goals"`
	}
	seenDetails := map[string]bool{}
	appendDetail := func(category string, values *[]string, items []string) {
		for _, item := range items {
			key := category + "\x00" + item
			if !seenDetails[key] {
				seenDetails[key] = true
				*values = append(*values, item)
			}
		}
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		var eventDetails struct {
			Requirements       []string `json:"requirements"`
			AcceptanceCriteria []string `json:"acceptance_criteria"`
			Constraints        []string `json:"constraints"`
			NonGoals           []string `json:"non_goals"`
		}
		if json.Unmarshal([]byte(raw), &eventDetails) != nil {
			continue
		}
		appendDetail("requirement", &details.Requirements, eventDetails.Requirements)
		appendDetail("acceptance", &details.AcceptanceCriteria, eventDetails.AcceptanceCriteria)
		appendDetail("constraint", &details.Constraints, eventDetails.Constraints)
		appendDetail("non_goal", &details.NonGoals, eventDetails.NonGoals)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, item := range details.Requirements {
		fmt.Fprintf(&b, "User requirement: %s\n", boundedText(item, 2000))
	}
	for _, item := range details.AcceptanceCriteria {
		fmt.Fprintf(&b, "User acceptance criterion: %s\n", boundedText(item, 2000))
	}
	for _, item := range details.Constraints {
		fmt.Fprintf(&b, "User constraint: %s\n", boundedText(item, 2000))
	}
	for _, item := range details.NonGoals {
		fmt.Fprintf(&b, "User non-goal (explicitly out of scope): %s\n", boundedText(item, 2000))
	}
	if len(t.Budget.Scope) > 0 {
		fmt.Fprintf(&b, "Declared write scope: %s\n", boundedText(strings.Join(t.Budget.Scope, ", "), 2000))
	}
	// Decisions remain in the ledger. Keep the active card small even when the
	// task spans many sessions; older decisions are available by task ID.
	const maxDecisionBytes = 3000
	used := 0
	shown := 0
	for i := len(decisions) - 1; i >= 0; i-- {
		d := decisions[i]
		line := fmt.Sprintf("User decision: %s → %s\n", d.Question, d.Answer)
		if d.Rationale != "" {
			line += "Decision rationale: " + d.Rationale + "\n"
		}
		if used+len(line) > maxDecisionBytes {
			break
		}
		used += len(line)
		shown++
	}
	for _, d := range decisions[len(decisions)-shown:] {
		fmt.Fprintf(&b, "User decision: %s → %s\n", d.Question, d.Answer)
		if d.Rationale != "" {
			fmt.Fprintf(&b, "Decision rationale: %s\n", d.Rationale)
		}
	}
	if shown < decisionCount {
		fmt.Fprintf(&b, "%d earlier user decisions remain in the task ledger. Call bc_task_history with task_id=%s to retrieve them.\n", decisionCount-shown, t.ID)
	}
	if changed := changedFiles(ctx, repoRoot); len(changed) > 0 {
		const maxChangedBytes = 2000
		var listed []string
		used := 0
		for _, path := range changed {
			if used+len(path)+2 > maxChangedBytes {
				break
			}
			listed = append(listed, path)
			used += len(path) + 2
		}
		fmt.Fprintf(&b, "Files changed in the checkout (%d total): %s\n", len(changed), strings.Join(listed, ", "))
		if len(listed) < len(changed) {
			b.WriteString("The checkout has more changed paths; inspect git status for the complete list.\n")
		}
	}
	if hasVerification {
		fmt.Fprintf(&b, "Last verification (historical candidate only): accepted=%t candidate=%s level=%s phase=%s. Reverify after any edit before finishing.\n", verified.Accepted, verified.Candidate, verified.Level, verified.Phase)
		if len(verified.Checks) > 0 {
			b.WriteString("Last verification checks:\n")
			for i, check := range verified.Checks {
				if i >= 10 {
					fmt.Fprintf(&b, "%d more verification checks remain in the ledger.\n", len(verified.Checks)-i)
					break
				}
				fmt.Fprintf(&b, "  %s: %s — %s\n", check.Name, check.Status, check.Headline)
			}
		}
		for i, reason := range verified.Reasons {
			if i >= 10 {
				fmt.Fprintf(&b, "%d more verification findings remain in the ledger.\n", len(verified.Reasons)-i)
				break
			}
			if len(reason) > 400 {
				reason = reason[:400] + "…"
			}
			if verified.Accepted {
				fmt.Fprintf(&b, "Verification rationale: %s\n", reason)
			} else {
				fmt.Fprintf(&b, "Open verification finding: %s\n", reason)
			}
		}
	}
	// Rebuilt from typed ledger rows on every call. Superseded claims are
	// excluded, and older evidence stays retrievable by stable operation ID.
	memories, err := HotMemory(ctx, st, taskID)
	if err != nil {
		return "", err
	}
	usedMemory := 0
	for _, m := range memories {
		line := fmt.Sprintf("Task memory #%d [%s; source=%s]: %s", m.ID, m.Type, m.Source, m.Text)
		if m.Evidence != "" {
			line += " evidence=" + m.Evidence
		}
		if m.Candidate != "" {
			line += " candidate=" + m.Candidate
		}
		if m.Supersedes != 0 {
			line += fmt.Sprintf(" supersedes=#%d", m.Supersedes)
		}
		line += "\n"
		if usedMemory+len(line) > 2500 {
			break
		}
		b.WriteString(line)
		usedMemory += len(line)
	}
	if len(memories) > 0 {
		fmt.Fprintf(&b, "Full typed task memory and evidence are available through bc_task_memory for task %s.\n", taskID)
	}
	b.WriteString("This record comes from BoundedCode's task ledger and checkout. Treat OpenCode summaries as working notes; check evidence before treating a hypothesis as fact.\n")
	return fitContext(b.String()), nil
}

const maxOpenCodeContextBytes = 12 << 10

// fitContext is the final admission guard for the card. Individual fields
// have bounds, but a task with several individually valid fields could still
// exceed the physical window. Keep the ledger-derived prefix and current
// failures/memory first; drop older decisions and noisy observations before
// truncating anything. The dropped material remains paged through the tools.
func fitContext(body string) string {
	if len(body) <= maxOpenCodeContextBytes {
		return body
	}
	lines := strings.SplitAfter(body, "\n")
	kept := make([]string, 0, len(lines))
	decisions, memories, findings := 0, 0, 0
	skipDecision := false
	for _, line := range lines {
		if skipDecision && !strings.HasPrefix(line, "Decision rationale:") {
			skipDecision = false
		}
		switch {
		case strings.HasPrefix(line, "User decision:"):
			decisions++
			skipDecision = decisions > 8
		case strings.HasPrefix(line, "Decision rationale:"):
			if skipDecision {
				continue
			}
		case strings.HasPrefix(line, "Task memory #"):
			memories++
			if memories > 16 {
				continue
			}
		case strings.HasPrefix(line, "Open verification finding:"):
			findings++
			if findings > 5 {
				continue
			}
		}
		if !skipDecision {
			kept = append(kept, line)
		}
		if strings.HasPrefix(line, "User decision:") {
			skipDecision = false
		}
	}
	body = strings.Join(kept, "")
	note := "\n[active context truncated; retrieve omitted state with bc_task_memory, bc_task_history, or bc_task_verification]\n"
	if len(body)+len(note) <= maxOpenCodeContextBytes {
		return body + note
	}
	return truncateUTF8(body, maxOpenCodeContextBytes-len(note)) + note
}

func requiredChecks(level recipe.Level) string {
	checks := recipe.Required(level)
	if len(checks) == 0 {
		return "(none recorded)"
	}
	names := make([]string, 0, len(checks))
	for _, check := range checks {
		names = append(names, string(check))
	}
	return strings.Join(names, ", ")
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func boundedText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return truncateUTF8(s, max) + "… [full value remains in task ledger]"
}
