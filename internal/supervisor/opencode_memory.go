package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/artifacts"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
)

// MemoryRecord is a typed claim. The artifact hash names the full, immutable
// supporting observation; the active context contains only the bounded claim.
type MemoryRecord struct {
	ID         int64     `json:"id"`
	Source     string    `json:"source"`
	SourceTool string    `json:"source_tool,omitempty"`
	Type       string    `json:"type"`
	Text       string    `json:"text"`
	Evidence   string    `json:"evidence,omitempty"`
	Path       string    `json:"path,omitempty"`
	FileHash   string    `json:"file_hash,omitempty"`
	StartLine  int       `json:"start_line,omitempty"`
	EndLine    int       `json:"end_line,omitempty"`
	Repository string    `json:"repository,omitempty"`
	Phase      string    `json:"phase,omitempty"`
	RecordedAt time.Time `json:"recorded_at,omitempty"`
	Candidate  string    `json:"candidate,omitempty"`
	Supersedes int64     `json:"supersedes,omitempty"`
}

var memoryTypes = map[string]bool{
	"tool_observation": true, "repository_fact": true,
	"model_hypothesis": true, "contradicted_hypothesis": true,
	"supervisor_decision": true, "external_judgment": true,
	"open_failure": true, "resolved_failure": true,
	"pending_action": true, "acceptance_criterion": true,
}

// RecordMemory persists a claim without changing its semantic origin. Claims
// made by the editor cannot manufacture user or supervisor authority.
func RecordMemory(ctx context.Context, st *store.Store, taskID string, r MemoryRecord, editor bool) (int64, error) {
	if _, err := task.NewStore(st).Get(ctx, taskID); err != nil {
		return 0, err
	}
	r.Type, r.Text = strings.TrimSpace(r.Type), strings.TrimSpace(r.Text)
	r.Source, r.SourceTool = strings.TrimSpace(r.Source), strings.TrimSpace(r.SourceTool)
	r.Path, r.Repository, r.Phase = strings.TrimSpace(r.Path), strings.TrimSpace(r.Repository), strings.TrimSpace(r.Phase)
	if r.RecordedAt.IsZero() {
		r.RecordedAt = time.Now().UTC()
	}
	if !memoryTypes[r.Type] || r.Text == "" || len(r.Text) > 1000 || len(r.Path) > 500 ||
		len(r.Source) > 80 || len(r.SourceTool) > 80 || len(r.Repository) > 500 || len(r.Phase) > 80 ||
		r.StartLine < 0 || r.EndLine < 0 {
		return 0, fmt.Errorf("invalid memory type or claim length")
	}
	// tool_observation is the editor's own account of what a tool returned:
	// legitimately editor-sourced, like a hypothesis, and distinct from
	// repository_fact, which requires the deterministic quote match bc_task_fact
	// performs. The remaining types name an authority the editor cannot grant
	// itself: a supervisor verdict, an external judgment, an acceptance
	// criterion derived from the user's own requirements, or a fact confirmed
	// independently of the model's say-so.
	if editor && (r.Type == "supervisor_decision" || r.Type == "external_judgment" || r.Type == "acceptance_criterion" || r.Type == "repository_fact") {
		return 0, fmt.Errorf("editor cannot assert %s", r.Type)
	}
	if editor {
		r.Source = "model"
	} else if r.Source == "" {
		switch r.Type {
		case "repository_fact":
			r.Source = "repository"
		case "acceptance_criterion":
			r.Source = "user"
		case "external_judgment":
			r.Source = "external"
		case "tool_observation":
			r.Source = "tool"
		default:
			r.Source = "supervisor"
		}
	}
	if r.Type == "repository_fact" && r.Evidence == "" {
		return 0, fmt.Errorf("repository fact requires an immutable evidence artifact")
	}
	if r.Evidence != "" {
		if _, err := artifacts.New(st).Get(r.Evidence); err != nil {
			return 0, fmt.Errorf("evidence: %w", err)
		}
	}
	if r.Supersedes != 0 {
		var prior string
		if err := st.Ledger().SQL().QueryRowContext(ctx, `SELECT intent FROM operations WHERE id=? AND task_id=? AND kind='memory'`, r.Supersedes, taskID).Scan(&prior); err != nil {
			return 0, fmt.Errorf("superseded record: %w", err)
		}
		var old MemoryRecord
		if err := json.Unmarshal([]byte(prior), &old); err != nil {
			return 0, err
		}
		if r.Type == "contradicted_hypothesis" && old.Type != "model_hypothesis" {
			return 0, fmt.Errorf("only a model hypothesis can be contradicted")
		}
	}
	h, err := ledger.New(st).Begin(ctx, taskID, ledger.KindMemory, r, r.Candidate)
	if err != nil {
		return 0, err
	}
	if err := h.Complete(ctx, r, r.Candidate, r.Evidence); err != nil {
		return 0, err
	}
	return h.ID(), nil
}

// MemoryPage retrieves historical records by stable operation ID. The card
// uses a bounded page; callers can fetch older evidence on demand.
func MemoryPage(ctx context.Context, st *store.Store, taskID string, before int64, limit int) ([]MemoryRecord, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("memory page limit must be 1..100")
	}
	if before <= 0 {
		before = 1<<63 - 1
	}
	rows, err := st.Ledger().SQL().QueryContext(ctx, `SELECT id,intent FROM operations WHERE task_id=? AND kind='memory' AND id<? AND outcome IS NOT NULL ORDER BY id DESC LIMIT ?`, taskID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryRecord
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var r MemoryRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		r.ID = id
		out = append(out, r)
	}
	return out, rows.Err()
}

// HotMemory gives durable high-importance claims priority over recent noisy
// observations. SQL excludes records with a later superseding operation, so
// an obsolete hypothesis or decision cannot reappear after compaction.
func HotMemory(ctx context.Context, st *store.Store, taskID string) ([]MemoryRecord, error) {
	quotas := []struct {
		kind  string
		count int
	}{
		{"open_failure", 6}, {"resolved_failure", 3}, {"repository_fact", 8},
		{"contradicted_hypothesis", 4}, {"pending_action", 6}, {"model_hypothesis", 4},
		{"acceptance_criterion", 6}, {"supervisor_decision", 6}, {"external_judgment", 4},
		{"tool_observation", 3},
	}
	var out []MemoryRecord
	for _, q := range quotas {
		if err := func() error {
			rows, err := st.Ledger().SQL().QueryContext(ctx, `SELECT o.id,o.intent FROM operations o
				WHERE o.task_id=? AND o.kind='memory' AND o.outcome IS NOT NULL
				AND json_extract(o.intent,'$.type')=?
				AND NOT EXISTS(SELECT 1 FROM operations n WHERE n.task_id=o.task_id AND n.kind='memory'
					AND json_extract(n.intent,'$.supersedes')=o.id AND n.outcome IS NOT NULL)
				ORDER BY o.id DESC LIMIT ?`, taskID, q.kind, q.count)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var r MemoryRecord
				var raw string
				if err := rows.Scan(&r.ID, &raw); err != nil {
					return err
				}
				id := r.ID
				if err := json.Unmarshal([]byte(raw), &r); err != nil {
					return err
				}
				r.ID = id
				out = append(out, r)
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// EvidenceBody verifies the artifact hash before returning the original bytes.
func EvidenceBody(st *store.Store, hash string) ([]byte, error) {
	if len(hash) != 64 {
		return nil, sql.ErrNoRows
	}
	return artifacts.New(st).Get(hash)
}
