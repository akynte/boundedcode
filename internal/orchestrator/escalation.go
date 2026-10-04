package orchestrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/gitops"
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
		BudgetTokens: r.Cfg.Frontier.MaxPacketTokens, MaxAttempts: t.Budget.MaxAttempts,
		Contracts: r.contracts(ctx, t, wts), ChangedFiles: t.ChangedFiles})
	if err != nil {
		r.Log.Error("build frontier pack", "err", err)
		return ""
	}
	home, _ := os.UserHomeDir()
	packet := frontier.BuildPacket(tr, pack, frontier.DefaultQuestion(tr), r.packetPaths(t, wts), home)
	dir := filepath.Join(r.Paths.TaskDir(t.ID), "frontier")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	n := r.escalationCount(ctx, t.ID) + 1
	if err := checkPacket(packet, home); err != nil {
		// Fail closed, but visibly: keep the refused packet on this host for
		// diagnosis and record the escalation as blocked.
		blocked := filepath.Join(dir, fmt.Sprintf("%03d-%s-blocked-packet.md", n, tr.Code))
		_ = config.WriteFileAtomic(blocked, []byte(packet), 0o600)
		r.recordEscalation(ctx, t.ID, tr, r.Frontier.Name(), "blocked", blocked, contextplan.EstimateTokens(packet), "", "")
		r.Rec.Emit(ctx, t.ID, "frontier.blocked", map[string]any{"code": tr.Code, "error": trunc(err.Error(), 400), "packet": blocked})
		r.say("escalation %s not sent: %v (packet kept at %s)", tr.Code, trunc(err.Error(), 200), blocked)
		return ""
	}
	tokens := contextplan.EstimateTokens(packet)
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
	// Record the model and the state of the code when advice was asked for,
	// so "did the advice change the solution" is measured, not assumed.
	model := r.Cfg.Frontier.Model
	if model == "" {
		model = "(provider default)"
	}
	_, _ = r.DB.ExecContext(ctx, `UPDATE escalations SET model = ?, diff_before = ? WHERE id = ?`, model, diffHash(ctx, wts), id)
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

// checkPacket is the fail-closed gate (a variable so tests can reach the
// blocked path, which sanitization otherwise makes unreachable).
var checkPacket = frontier.CheckPacket

// packetPaths names every host location a packet may mention: the task's
// work tree and repositories, its state, BoundedCode's own directories and
// the toolchain caches verification output and stack traces refer to.
// Anything else under the home directory is rewritten to $HOME by Sanitize.
func (r *Runner) packetPaths(t *task.Task, wts []task.Worktree) frontier.PathMap {
	paths := frontier.PathMap{r.WorkDir(t.ID): "."}
	for _, w := range wts {
		paths[w.RepoPath] = w.RepoName
		paths[w.Path] = w.RepoName
	}
	paths[r.Paths.TaskDir(t.ID)] = "<task-state>"
	for from, to := range map[string]string{r.Paths.Data: "<boundedcode-data>", r.Paths.Cache: "<boundedcode-cache>",
		r.Paths.State: "<boundedcode-state>", r.Paths.Config: "<boundedcode-config>", r.Paths.Runtime: "<boundedcode-runtime>"} {
		if from != "" {
			paths[from] = to
		}
	}
	if r.Verify != nil {
		if r.Verify.CacheDir != "" {
			paths[r.Verify.CacheDir] = "<build-cache>"
		}
		if r.Verify.GoModCache != "" {
			paths[r.Verify.GoModCache] = "$GOMODCACHE"
		}
	}
	return paths
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

// pendingAdvice returns an answered-but-unused escalation response: a manual
// answer supplied while the task was stopped, or an answer no attempt has
// applied yet (the run stopped or blocked right after receiving it).
func (r *Runner) pendingAdvice(ctx context.Context, taskID string) string {
	var id int64
	var path string
	err := r.DB.QueryRowContext(ctx, `SELECT id, response_path FROM escalations WHERE task_id = ? AND
		(status = 'answered_manual' OR status = 'answered' AND diff_before != '' AND diff_after = '')
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
	SetEscalationTaskOutcome(ctx, db, taskID, "cancelled")
}

// SetEscalationTaskOutcome records the task's final status on all of its
// escalations (sent, declined or failed alike).
func SetEscalationTaskOutcome(ctx context.Context, db *sql.DB, taskID, outcome string) {
	_, _ = db.ExecContext(ctx, `UPDATE escalations SET task_outcome = ?, updated_at = ? WHERE task_id = ?`, outcome, store.Now(), taskID)
}

// recordDiffAfter stores, on answered escalations still waiting for it, the
// code state after the first attempt that followed the advice.
func (r *Runner) recordDiffAfter(ctx context.Context, taskID string, wts []task.Worktree) {
	_, _ = r.DB.ExecContext(ctx, `UPDATE escalations SET diff_after = ? WHERE task_id = ? AND status = 'answered' AND diff_after = '' AND diff_before != ''`,
		diffHash(ctx, wts), taskID)
}

// diffHash fingerprints the task's combined diff against its base commits.
func diffHash(ctx context.Context, wts []task.Worktree) string {
	h := sha256.New()
	for _, w := range wts {
		d, _ := gitops.Diff(ctx, w.Path, w.BaseCommit, false)
		fmt.Fprintf(h, "%s\x00%s\x00", w.RepoName, d)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
