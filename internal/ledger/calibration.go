package ledger

// Judgment calibration: a prediction a judgment site made, paired later
// with what was actually observed, so a site's calibration can be read from
// ordinary use rather than assumed or benchmarked.
//
// This is deliberately not internal/judgment's own concern. That package
// declares Recorder and AnswerCache as interfaces and leaves storage to
// whoever assembles a workspace, for the same reason this does: a judgment
// site (internal/workflow, internal/task, internal/retrieval) records a
// prediction through a narrow interface it is handed, and this package is
// where the row actually lives, next to the operations and evidence tables
// that already answer "what happened in this task".
//
// A prediction and its outcome are written at different times — sometimes
// across a process restart, since a task can pause for a gate approval and
// resume under a different `bcode` invocation — which is why CalibrationStore
// is a dedicated table with real, queryable columns (site, subject,
// outcome) rather than a JSON blob in `operations`: that table's
// intent/outcome pair assumes both halves are written from the same code
// path shortly apart.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/akynte/boundedcode/internal/store"
)

// Prediction is one judgment site's answer about one subject, recorded so it
// can later be paired with what actually happened.
type Prediction struct {
	TaskID string
	Site   string
	// Subject names what the prediction was about, in a form stable enough
	// to find again later: "hunk:pkg/order/order_test.go",
	// "obligation:internal/billing/validate.go::Validate", "attempt:3". The
	// call site owns this vocabulary; CalibrationStore only stores it.
	Subject string
	// Predicted is the probability the site reported: a Noul's value
	// directly, or a Choice/Score answer's probability for the option the
	// call site is pairing an outcome against.
	Predicted float64
	// Model is the model id that answered, for provenance — the same
	// distinction judgment.yaml's PinnedModel comment makes: what was
	// requested is not evidence of what served it, so a calibration report
	// that cannot say which model produced its numbers is not reproducible.
	Model string
	// SiteVersion is the site's declared question version
	// (judgment.SiteInfo.Version). Calibration partitions on it: a materially
	// reworded proposition is a different measurement, and pooling the two
	// would produce a figure describing neither.
	SiteVersion string
	// TierAtPrediction is the authority the site held when this prediction
	// was made. Together with Intervened it is what separates shadow
	// evidence from operational evidence — see Report's doc comment for why
	// scoring them together is self-confirming.
	TierAtPrediction string
	// Intervened says an effect derived from this prediction was actually
	// applied to this subject, so the outcome that follows was influenced by
	// the prediction being made. False at the logged tier by construction,
	// and false at a higher tier for a finding that did not cross its own
	// threshold.
	Intervened bool
	// Detail is one line, for a person reading `bcode judgment calibrate`.
	Detail string
	// ConsultationID names the judgment_consultations row this prediction
	// came out of, so a finding can be read against the clean consultations
	// of the same site. Empty is allowed: a prediction recorded outside an
	// observed consultation is still a prediction.
	ConsultationID string
}

// Pair is one resolved (prediction, outcome) row, the unit Calibrate reads.
type Pair struct {
	TaskID           string
	Site             string
	Subject          string
	Predicted        float64
	Outcome          bool
	Model            string
	SiteVersion      string
	TierAtPrediction string
	// Intervened distinguishes an operational row — one whose own effect
	// could have moved the outcome it is scored against — from a shadow row,
	// where the prediction changed nothing and the outcome is an
	// observation.
	Intervened bool
}

// CalibrationStore records judgment predictions and their later outcomes,
// and reads back paired rows for a calibration report.
//
// Every method here is best-effort in the same sense internal/judgment's own
// Consult is: calibration is a diagnostic layer over production behaviour,
// never load-bearing for it. A call site that recorded a prediction and
// later fails to record its outcome has produced one fewer paired row, not a
// broken task — RecordOutcome and ResolveOpenForTask silently do nothing
// when there is no matching unresolved prediction, rather than erroring.
type CalibrationStore struct {
	db *store.DB
}

// NewCalibrationStore binds a calibration store to a workspace's ledger
// database.
func NewCalibrationStore(s *store.Store) *CalibrationStore {
	return &CalibrationStore{db: s.Ledger()}
}

// RecordPrediction persists one prediction. It never returns an error to a
// caller that treats calibration as advisory; call sites that want to know
// about a write failure (tests, `bcode judgment calibrate`) still get one.
func (c *CalibrationStore) RecordPrediction(ctx context.Context, p Prediction) error {
	if c == nil || c.db == nil {
		return nil
	}
	if p.TaskID == "" || p.Site == "" || p.Subject == "" {
		return fmt.Errorf("ledger: prediction needs a task id, site and subject")
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	return c.db.Tx(ctx, func(tx *sql.Tx) error {
		// seq is allocated inside the same transaction as the insert, so two
		// concurrent writers cannot be handed the same number: SQLite
		// serialises write transactions on this database, and the read and
		// the insert are one unit. It is an explicit declared column rather
		// than a reliance on rowid — see ledger_007.sql.
		var seq int64
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(seq), 0) + 1 FROM judgment_predictions`).Scan(&seq); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO judgment_predictions
				(id, seq, task_id, site, subject, predicted, model, site_version,
				 tier_at_prediction, intervened, detail, consultation_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, seq, p.TaskID, p.Site, p.Subject, p.Predicted, p.Model, p.SiteVersion,
			p.TierAtPrediction, boolToInt(p.Intervened), p.Detail, p.ConsultationID,
			time.Now().UnixMilli())
		return err
	})
}

// MarkIntervened records that an effect derived from the most recent
// unresolved prediction for (taskID, site, subject) was actually applied.
//
// It is separate from RecordPrediction because a call site does not always
// know at prediction time whether it will act: a finding is recorded first,
// then composed with a threshold and the site's tier, and only some findings
// cross both. Calling this after the effect is applied keeps the record
// honest in the one direction that matters — a row marked intervened really
// was acted on.
func (c *CalibrationStore) MarkIntervened(ctx context.Context, taskID, site, subject string) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE judgment_predictions SET intervened = 1
			WHERE id = (
				SELECT id FROM judgment_predictions
				WHERE task_id = ? AND site = ? AND subject = ? AND outcome IS NULL
				ORDER BY seq DESC, rowid DESC LIMIT 1
			)`, taskID, site, subject)
		return err
	})
}

// MarkTaskIntervened marks every still-unresolved prediction for a site
// within one task as intervened. It is the bulk form for an effect that acts
// on the task rather than on one subject — a reroute, a stop, a pause.
func (c *CalibrationStore) MarkTaskIntervened(ctx context.Context, taskID, site string) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE judgment_predictions SET intervened = 1
			WHERE task_id = ? AND site = ? AND outcome IS NULL`, taskID, site)
		return err
	})
}

// RecordOutcome resolves the most recent unresolved prediction for
// (taskID, site, subject) with the observed outcome.
//
// "Most recent" rather than "the one prediction", because a subject can be
// judged more than once across a task's life — the same hunk reviewed after
// a repair attempt, the same waiver re-asked after a replan — and only the
// latest one describes the state the outcome actually resolves.
func (c *CalibrationStore) RecordOutcome(ctx context.Context, taskID, site, subject string, outcome bool, note string) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Tx(ctx, func(tx *sql.Tx) error {
		// seq, not created_at, decides "most recent": created_at is
		// millisecond resolution and two predictions for one subject
		// recorded inside a single millisecond — routine when a call site
		// loops over several findings — would otherwise resolve in an order
		// nothing guarantees. seq is an explicit monotonic column
		// (ledger_007.sql), so the ordering guarantee is in the schema
		// rather than in an assumption about SQLite's internals. created_at
		// remains a tiebreaker only for rows written under schema 6, which
		// have seq 0.
		row := tx.QueryRowContext(ctx, `
			SELECT id FROM judgment_predictions
			WHERE task_id = ? AND site = ? AND subject = ? AND outcome IS NULL
			ORDER BY seq DESC, created_at DESC, rowid DESC LIMIT 1`, taskID, site, subject)
		var id string
		if err := row.Scan(&id); err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE judgment_predictions
			SET outcome = ?, outcome_note = ?, resolved_at = ?
			WHERE id = ?`, boolToInt(outcome), note, time.Now().UnixMilli(), id)
		return err
	})
}

// ResolveOpenForTask resolves every still-unresolved prediction for a site
// within one task with the same outcome.
//
// This is the coarse instrument a task-level event — a gate decision, final
// acceptance — actually is: the gate approves or rejects the whole diff, not
// one hunk at a time, so every open verification_integrity or
// diff_conformance prediction for the task is resolved together. A finer
// per-hunk gate decision does not exist yet; when it does, this becomes the
// fallback for predictions a finer path did not reach rather than the only
// path.
func (c *CalibrationStore) ResolveOpenForTask(ctx context.Context, taskID, site string, outcome bool, note string) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE judgment_predictions
			SET outcome = ?, outcome_note = ?, resolved_at = ?
			WHERE task_id = ? AND site = ? AND outcome IS NULL`,
			boolToInt(outcome), note, time.Now().UnixMilli(), taskID, site)
		return err
	})
}

// Paired reads every resolved prediction for a site, oldest first.
func (c *CalibrationStore) Paired(ctx context.Context, site string) ([]Pair, error) {
	if c == nil || c.db == nil {
		return nil, nil
	}
	var out []Pair
	err := c.db.ReadTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT task_id, site, subject, predicted, outcome, model, site_version,
			       tier_at_prediction, intervened
			FROM judgment_predictions
			WHERE site = ? AND outcome IS NOT NULL
			ORDER BY seq ASC, created_at ASC, rowid ASC`, site)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Pair
			var outcome, intervened int
			if err := rows.Scan(&p.TaskID, &p.Site, &p.Subject, &p.Predicted, &outcome,
				&p.Model, &p.SiteVersion, &p.TierAtPrediction, &intervened); err != nil {
				return err
			}
			p.Outcome = outcome != 0
			p.Intervened = intervened != 0
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// PendingCount reports how many predictions for site have never been
// resolved — the honest number beside a calibration report: a site that has
// fired forty times and resolved four has a report describing one tenth of
// its behaviour.
func (c *CalibrationStore) PendingCount(ctx context.Context, site string) (int, error) {
	if c == nil || c.db == nil {
		return 0, nil
	}
	var n int
	err := c.db.ReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM judgment_predictions WHERE site = ? AND outcome IS NULL`, site).Scan(&n)
	})
	return n, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
