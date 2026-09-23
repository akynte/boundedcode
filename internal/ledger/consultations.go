package ledger

// The denominator for every site-level rate.
//
// judgment_predictions records a finding; the operations journal records that
// a request left. A site consulted on clean work writes to neither, so before
// this table "the site was never reached" and "the site was reached, asked,
// and found nothing" were the same absence of rows. No fire rate, skip rate,
// decline rate or finding rate could be computed from that, which is the
// difference between "Jev was called" and "Jev is worth its authority".
//
// This is observational only. Nothing reads it back into a decision: the
// supervisor never consults it, no tier is derived from it, and a write that
// fails is dropped rather than surfaced, for the same reason
// CalibrationStore's writes are best-effort — a diagnostic layer must not be
// able to stop a task.

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/store"
)

// Consultation is one site's use of the judge, whatever the outcome.
//
// It mirrors judgment.Consultation rather than importing it, for the same
// reason journal mirrors JournalIntent: internal/judgment must not depend on
// the store, and internal/worktree already reaches this package, so importing
// judgment here would close a cycle. internal/supervisor owns the adapter.
type Consultation struct {
	ID            string
	TaskID        string
	Site          string
	Phase         string
	Reached       bool
	Requested     bool
	Status        string
	SkipReason    string
	Questions     int
	Subjects      int
	Findings      int
	Model         string
	Endpoint      string
	Latency       time.Duration
	PredictionIDs []string
	At            time.Time
}

// ConsultationStore persists Consultation rows.
type ConsultationStore struct {
	db *store.DB
}

// NewConsultationStore binds one to a workspace's ledger database.
func NewConsultationStore(s *store.Store) *ConsultationStore {
	return &ConsultationStore{db: s.Ledger()}
}

// Consultation implements judgment.Observer.
//
// It takes no error return by design: observation is not a thing a task may
// fail on. A caller that needs the error — a test, a report — uses Write.
func (c *ConsultationStore) Consultation(ctx context.Context, rec Consultation) {
	_ = c.Write(ctx, rec)
}

// Write persists one consultation and reports whether it could.
func (c *ConsultationStore) Write(ctx context.Context, rec Consultation) error {
	if c == nil || c.db == nil || rec.Site == "" {
		return nil
	}
	if rec.At.IsZero() {
		rec.At = time.Now()
	}
	return c.db.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO judgment_consultations
				(id, task_id, site, phase, reached, requested, status, skip_reason,
				 questions, subjects, findings, model, endpoint, latency_ms,
				 prediction_ids, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.ID, rec.TaskID, rec.Site, rec.Phase,
			boolToInt(rec.Reached), boolToInt(rec.Requested),
			rec.Status, rec.SkipReason,
			rec.Questions, rec.Subjects, rec.Findings,
			rec.Model, rec.Endpoint, rec.Latency.Milliseconds(),
			strings.Join(rec.PredictionIDs, ","), rec.At.UnixMilli())
		return err
	})
}

// SiteTotals is one site's recorded behaviour, which is what a promotion
// argument needs and what no earlier table could supply.
type SiteTotals struct {
	Site        string
	Reached     int // consultations: the denominator
	Requested   int // of those, ones that sent a request
	Findings    int // findings produced across them
	WithFinding int // consultations that produced at least one finding
	Questions   int
	ByStatus    map[string]int
	BySkip      map[string]int
	LatencyMS   []int64
}

// Totals reports per-site counts over every recorded consultation.
func (c *ConsultationStore) Totals(ctx context.Context) (map[string]*SiteTotals, error) {
	out := map[string]*SiteTotals{}
	if c == nil || c.db == nil {
		return out, nil
	}
	err := c.db.ReadTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT site, requested, status, skip_reason, questions, findings, latency_ms
			FROM judgment_consultations`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var site, status, skip string
			var requested, questions, findings int
			var latency int64
			if err := rows.Scan(&site, &requested, &status, &skip, &questions, &findings, &latency); err != nil {
				return err
			}
			s, ok := out[site]
			if !ok {
				s = &SiteTotals{Site: site, ByStatus: map[string]int{}, BySkip: map[string]int{}}
				out[site] = s
			}
			s.Reached++
			s.Requested += requested
			s.Questions += questions
			s.Findings += findings
			if findings > 0 {
				s.WithFinding++
			}
			s.ByStatus[status]++
			if skip != "" {
				s.BySkip[skip]++
			}
			s.LatencyMS = append(s.LatencyMS, latency)
		}
		return rows.Err()
	})
	return out, err
}
