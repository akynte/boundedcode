package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/akynte/boundedcode/internal/artifacts"
	"strings"
	"unicode/utf8"

	"github.com/akynte/boundedcode/internal/ledger"
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
	var b strings.Builder
	fmt.Fprintf(&b, "BoundedCode authoritative task state (task %s; %s).\n", t.ID, t.State)
	fmt.Fprintf(&b, "Original objective: %s\n", boundedText(t.Title,1500))
	var promptHash string
	_ = st.Ledger().SQL().QueryRowContext(ctx, `SELECT json_extract(intent,'$.original_prompt_hash') FROM operations WHERE task_id=? AND kind='session_start' AND json_extract(intent,'$.original_prompt_hash') != '' ORDER BY id LIMIT 1`, taskID).Scan(&promptHash)
	if promptHash != "" {
		fmt.Fprintf(&b, "Original raw user request: evidence=%s (immutable; call bc_task_memory to recover full text).\n", promptHash)
		if body, err := artifacts.New(st).Get(promptHash); err == nil {
			const maxPromptExcerpt = 1200
			fmt.Fprintf(&b, "Original request excerpt: %s\n", boundedText(string(body),maxPromptExcerpt))
		}
	}
	rows, err := st.Ledger().SQL().QueryContext(ctx, `SELECT intent FROM operations
		WHERE task_id = ? AND kind = ? AND json_valid(intent)
		  AND (COALESCE(json_array_length(intent, '$.requirements'), 0) > 0
		    OR COALESCE(json_array_length(intent, '$.constraints'), 0) > 0)
		ORDER BY seq LIMIT 1`, taskID, ledger.KindSessionStart)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		var details struct {
			Requirements []string `json:"requirements"`
			Constraints  []string `json:"constraints"`
		}
		if json.Unmarshal([]byte(raw), &details) != nil {
			continue
		}
		if len(details.Requirements) == 0 && len(details.Constraints) == 0 {
			continue
		}
		for _, item := range details.Requirements {
			fmt.Fprintf(&b, "User requirement: %s\n", item)
		}
		for _, item := range details.Constraints {
			fmt.Fprintf(&b, "User constraint: %s\n", item)
		}
		// A task has one originating request. Resume events carry a session ID,
		// never a replacement requirement set.
		break
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(t.Budget.Scope) > 0 {
		fmt.Fprintf(&b, "Declared write scope: %s\n", boundedText(strings.Join(t.Budget.Scope, ", "),2000))
	}
	// Decisions remain in the ledger. Keep the active card small even when the
	// task spans many sessions; older decisions are available by task ID.
	const maxDecisionBytes = 3000
	used := 0
	shown := 0
	for i := len(decisions) - 1; i >= 0; i-- {
		d := decisions[i]
		line := fmt.Sprintf("User decision: %s → %s\n", d.Question, d.Answer)
		if used+len(line) > maxDecisionBytes {
			break
		}
		used += len(line)
		shown++
	}
	for _, d := range decisions[len(decisions)-shown:] {
		fmt.Fprintf(&b, "User decision: %s → %s\n", d.Question, d.Answer)
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
		fmt.Fprintf(&b, "Last verification (historical candidate only): accepted=%t candidate=%s. Reverify after any edit before finishing.\n", verified.Accepted, verified.Candidate)
		for i, reason := range verified.Reasons {
			if i >= 10 {
				fmt.Fprintf(&b, "%d more verification findings remain in the ledger.\n", len(verified.Reasons)-i)
				break
			}
			if len(reason) > 400 {
				reason = reason[:400] + "…"
			}
			fmt.Fprintf(&b, "Open verification finding: %s\n", reason)
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
		line := fmt.Sprintf("Task memory #%d [%s]: %s", m.ID, m.Type, m.Text)
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
	return b.String(), nil
}

func boundedText(s string, max int) string {
	if len(s)<=max {return s}
	cut:=max
	for cut>0 && !utf8.RuneStart(s[cut]) {cut--}
	return s[:cut]+"… [full value remains in task ledger]"
}
