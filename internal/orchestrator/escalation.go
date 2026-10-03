package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
)

// escalate builds a packet, asks for approval, consults the provider and
// persists everything. It returns advice for the local agent ("" if the
// escalation was declined, deferred or failed — the task continues locally).
func (r *Runner) escalate(ctx context.Context, t *task.Task, wts []task.Worktree, tr frontier.Trigger, mode contextplan.Mode) string {
	r.Rec.Emit(ctx, t.ID, "frontier.triggered", tr)
	if r.Frontier == nil {
		r.say("escalation %s suggested (%s) but frontier is disabled; continuing locally", tr.Code, trunc(tr.Reason, 160))
		r.recordEscalation(ctx, t.ID, tr, "none", "declined", "", 0, "", "frontier disabled")
		return ""
	}
	strategies, _ := r.Ledger.Strategies(ctx, t.ID)
	pack, err := contextplan.Build(ctx, contextplan.Inputs{Task: t, Worktrees: wts, WorkDir: r.WorkDir(t.ID), Strategies: strategies,
		Verification: r.latestVerification(ctx, t.ID, wts), Intel: r.Intel, Mode: mode,
		BudgetTokens: r.Cfg.Frontier.MaxPacketTokens, MaxAttempts: t.Budget.MaxAttempts})
	if err != nil {
		r.Log.Error("build frontier pack", "err", err)
		return ""
	}
	packet := frontier.BuildPacket(tr, pack, frontier.DefaultQuestion(tr))
	tokens := contextplan.EstimateTokens(packet)
	dir := filepath.Join(r.Paths.TaskDir(t.ID), "frontier")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	n := r.escalationCount(ctx, t.ID) + 1
	packetPath := filepath.Join(dir, fmt.Sprintf("%03d-%s-packet.md", n, tr.Code))
	if err := config.WriteFileAtomic(packetPath, []byte(packet), 0o600); err != nil {
		return ""
	}
	id := r.recordEscalation(ctx, t.ID, tr, r.Frontier.Name(), "proposed", packetPath, tokens, "", "")
	if r.Cfg.Frontier.RequireApproval && tr.Code != frontier.Z4 {
		if r.Approve == nil || !r.Approve(ctx, tr, packetPath, tokens) {
			r.setEscalation(ctx, id, "declined", "", "")
			r.say("escalation %s declined; continuing locally", tr.Code)
			return ""
		}
	}
	t.Phase = task.PhaseEscalating
	t.Budget.UsedEscalations++
	_ = r.Ledger.Save(ctx, t)
	r.setEscalation(ctx, id, "sent", "", "")
	r.say("escalating %s to %s (%d-token packet): %s", tr.Code, r.Frontier.Name(), tokens, trunc(tr.Reason, 160))
	answer, err := r.Frontier.Ask(ctx, packet, dir)
	if errors.Is(err, frontier.ErrPending) {
		r.say("packet written to %s; answer with `frontier answer %s FILE`, then `task resume %s`", packetPath, t.ID, t.ID)
		return ""
	}
	if err != nil {
		r.setEscalation(ctx, id, "failed", "", "")
		r.Rec.Emit(ctx, t.ID, "frontier.failed", map[string]any{"error": trunc(err.Error(), 500)})
		r.say("frontier call failed: %v; continuing locally", trunc(err.Error(), 200))
		return ""
	}
	respPath := filepath.Join(dir, fmt.Sprintf("%03d-%s-response.md", n, tr.Code))
	_ = config.WriteFileAtomic(respPath, []byte(answer), 0o600)
	r.setEscalation(ctx, id, "answered", respPath, "")
	t.Decide("frontier", fmt.Sprintf("%s advice received (%s)", tr.Code, firstLine(answer)))
	_ = r.Ledger.Save(ctx, t)
	r.Rec.Emit(ctx, t.ID, "frontier.answered", map[string]any{"code": tr.Code, "packet_tokens": tokens, "answer_chars": len(answer)})
	return answer
}

func (r *Runner) recordEscalation(ctx context.Context, taskID string, tr frontier.Trigger, provider, status, packet string, tokens int, resp, outcome string) int64 {
	now := store.Now()
	res, err := r.DB.ExecContext(ctx, `INSERT INTO escalations(task_id, trigger, reason, provider, status, packet_path, packet_tokens, response_path, outcome, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, taskID, string(tr.Code), tr.Reason, provider, status, packet, tokens, resp, outcome, now, now)
	if err != nil {
		r.Log.Error("record escalation", "err", err)
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

func (r *Runner) setEscalation(ctx context.Context, id int64, status, resp, outcome string) {
	_, _ = r.DB.ExecContext(ctx, `UPDATE escalations SET status = ?, response_path = COALESCE(NULLIF(?, ''), response_path),
		outcome = COALESCE(NULLIF(?, ''), outcome), updated_at = ? WHERE id = ?`, status, resp, outcome, store.Now(), id)
}

func (r *Runner) escalationCount(ctx context.Context, taskID string) int {
	var n int
	_ = r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM escalations WHERE task_id = ?`, taskID).Scan(&n)
	return n
}

func (r *Runner) hasEscalation(ctx context.Context, taskID string, code frontier.Code) bool {
	var n int
	_ = r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM escalations WHERE task_id = ? AND trigger = ?`, taskID, string(code)).Scan(&n)
	return n > 0
}

// pendingAdvice returns an answered-but-unused escalation response (e.g. a
// manual answer supplied while the task was stopped).
func (r *Runner) pendingAdvice(ctx context.Context, taskID string) string {
	var id int64
	var path string
	err := r.DB.QueryRowContext(ctx, `SELECT id, response_path FROM escalations WHERE task_id = ? AND status = 'answered_manual'
		ORDER BY id DESC LIMIT 1`, taskID).Scan(&id, &path)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	r.setEscalation(ctx, id, "answered", "", "")
	return strings.TrimSpace(string(b))
}

// finishEscalations records, for answered escalations without an outcome,
// whether the task completed after them ("helped") or was abandoned
// ("no_effect"). It is correlational, not causal; the benchmark harness
// measures causal effect by running tasks with frontier disabled.
func (r *Runner) finishEscalations(ctx context.Context, taskID, outcome string) {
	_, _ = r.DB.ExecContext(ctx, `UPDATE escalations SET outcome = ?, updated_at = ? WHERE task_id = ? AND status = 'answered' AND outcome = ''`,
		outcome, store.Now(), taskID)
}

// AnswerEscalation stores a manually obtained frontier answer for the latest
// pending escalation of a task; the next run uses it as advice.
func AnswerEscalation(ctx context.Context, db *sql.DB, taskDir, taskID string, answer []byte) (int64, error) {
	var id int64
	var trig string
	err := db.QueryRowContext(ctx, `SELECT id, trigger FROM escalations WHERE task_id = ? AND status IN ('sent','proposed')
		ORDER BY id DESC LIMIT 1`, taskID).Scan(&id, &trig)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("task %s has no pending escalation", taskID)
	}
	if err != nil {
		return 0, err
	}
	p := filepath.Join(taskDir, "frontier", fmt.Sprintf("manual-%d-%s-response.md", id, trig))
	if err := config.WriteFileAtomic(p, answer, 0o600); err != nil {
		return 0, err
	}
	_, err = db.ExecContext(ctx, `UPDATE escalations SET status = 'answered_manual', response_path = ?, updated_at = ? WHERE id = ?`, p, store.Now(), id)
	return id, err
}

// FinishEscalations is exported for task cancellation.
func FinishEscalations(ctx context.Context, db *sql.DB, taskID, outcome string) {
	_, _ = db.ExecContext(ctx, `UPDATE escalations SET outcome = ?, updated_at = ? WHERE task_id = ? AND status = 'answered' AND outcome = ''`,
		outcome, store.Now(), taskID)
}
