// Package stats computes the product's success metrics (docs/product-spec.md
// §4.11) from the task ledger: local-only completion rate, frontier
// escalation rate, token volumes and verified tasks per hour. Nothing here is
// estimated; every number is derived from recorded state.
package stats

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/akynte/boundedcode/internal/task"
)

// Summary aggregates tasks created since a timestamp (RFC 3339; "" = all).
type Summary struct {
	Since              string         `json:"since,omitempty"`
	Tasks              int            `json:"tasks"`
	ByStatus           map[string]int `json:"by_status"`
	Completed          int            `json:"completed"`
	CompletedLocalOnly int            `json:"completed_local_only"`
	// LocalOnlyRate is completed-without-any-frontier-answer / completed.
	LocalOnlyRate float64 `json:"local_only_completion_rate"`
	// EscalationRate is tasks with at least one escalation sent / tasks.
	EscalationRate      float64 `json:"escalation_rate"`
	EscalationsSent     int     `json:"escalations_sent"`
	EscalationsDeclined int     `json:"escalations_declined"`
	// EscalationsBlocked were refused before sending because the packet
	// still contained host paths (frontier.CheckPacket).
	EscalationsBlocked int `json:"escalations_blocked"`
	LocalTokens        int `json:"local_tokens_processed"`
	GeneratedTokens    int `json:"local_tokens_generated"`
	CachedPromptTokens int `json:"local_cached_prompt_tokens"`
	FrontierPacketTok  int `json:"frontier_packet_tokens"`
	// FrontierTokenShare is frontier packet tokens / (local processed +
	// frontier packet tokens). Frontier responses are not tokenized locally
	// and are excluded.
	FrontierTokenShare     float64 `json:"frontier_token_share"`
	WallHours              float64 `json:"wall_hours"`
	VerifiedTasksPerHour   float64 `json:"verified_tasks_per_hour"`
	AttemptsPerCompleted   float64 `json:"attempts_per_completed_task"`
	Condensations          int     `json:"condensations"`
	SessionsResumed        int     `json:"sessions_resumed"`
	ContextResets          int     `json:"context_resets"`
	VerificationRuns       int     `json:"verification_runs"`
	FailedVerificationRuns int     `json:"failed_verification_runs"`
}

// Compute aggregates the ledger.
func Compute(ctx context.Context, db *sql.DB, since string) (Summary, error) {
	s := Summary{Since: since, ByStatus: map[string]int{}}
	rows, err := db.QueryContext(ctx, `SELECT t.id, t.status, t.attempt_count, t.budget,
		(SELECT COUNT(*) FROM escalations e WHERE e.task_id = t.id AND e.status IN ('sent','answered','answered_manual','failed')),
		(SELECT COUNT(*) FROM escalations e WHERE e.task_id = t.id AND e.status IN ('answered','answered_manual')),
		(SELECT COUNT(*) FROM escalations e WHERE e.task_id = t.id AND e.status = 'declined'),
		(SELECT COUNT(*) FROM escalations e WHERE e.task_id = t.id AND e.status = 'blocked'),
		(SELECT COALESCE(SUM(packet_tokens),0) FROM escalations e WHERE e.task_id = t.id AND e.status IN ('sent','answered','answered_manual','failed'))
		FROM tasks t WHERE t.created_at >= ?`, since)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	escalated, attempts := 0, 0
	for rows.Next() {
		var id, status, budget string
		var att, sent, answered, declined, blocked, packet int
		if err := rows.Scan(&id, &status, &att, &budget, &sent, &answered, &declined, &blocked, &packet); err != nil {
			return s, err
		}
		var b task.Budget
		_ = json.Unmarshal([]byte(budget), &b)
		s.Tasks++
		s.ByStatus[status]++
		s.EscalationsSent += sent
		s.EscalationsDeclined += declined
		s.EscalationsBlocked += blocked
		s.FrontierPacketTok += packet
		if sent > 0 {
			escalated++
		}
		s.LocalTokens += b.UsedLocalTokens
		s.GeneratedTokens += b.GeneratedTokens
		s.CachedPromptTokens += b.CachedTokens
		s.WallHours += b.UsedWallClockS / 3600
		s.Condensations += b.Condensations
		s.SessionsResumed += b.SessionsResumed
		s.ContextResets += b.ContextResets
		s.VerificationRuns += b.VerificationRuns
		s.FailedVerificationRuns += b.FailedVerifyRuns
		if status == string(task.StatusCompleted) {
			s.Completed++
			attempts += att
			if answered == 0 {
				s.CompletedLocalOnly++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return s, err
	}
	if s.Completed > 0 {
		s.LocalOnlyRate = float64(s.CompletedLocalOnly) / float64(s.Completed)
		s.AttemptsPerCompleted = float64(attempts) / float64(s.Completed)
	}
	if s.Tasks > 0 {
		s.EscalationRate = float64(escalated) / float64(s.Tasks)
	}
	if tot := s.LocalTokens + s.FrontierPacketTok; tot > 0 {
		s.FrontierTokenShare = float64(s.FrontierPacketTok) / float64(tot)
	}
	if s.WallHours > 0 {
		s.VerifiedTasksPerHour = float64(s.Completed) / s.WallHours
	}
	return s, nil
}
