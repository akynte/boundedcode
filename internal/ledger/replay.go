package ledger

// Deterministic replay of a task's judgments.
//
// What this replays, and what it does not, is the whole of its honesty, so
// it is stated here rather than in a footnote.
//
// The design asks for recorded judgments to be usable as deterministic
// fixtures, so control-flow behaviour can be replayed without live calls.
// Two things are recorded, and they are not the same:
//
//   - The journal (ledger.KindJudgment operations) records that a request
//     happened, against which model, with which question ids, a digest of
//     the state, and per-question *confidence*. It does not record the
//     answers. A Noul's probability and a Choice's selected option are not
//     in it, by construction: the journal exists so a trace can say what
//     left this machine, and a full answer log is not that.
//   - The prediction table records, per site and subject, the probability a
//     call site actually composed out of those answers, plus the model, the
//     site's question version, the authority in force, and whether an effect
//     was applied.
//
// So the answers themselves cannot be replayed from what is persisted, and
// this does not pretend to: it replays the *composed predictions*, which is
// the layer control flow reads. That is enough to answer the question a
// promotion decision actually asks — "on this task, under this proposed
// tier, what would this site have done?" — and it is deterministic, offline,
// and read-only. A replay that needed the model would not be a replay.
//
// Nothing here writes: no store mutation, no repository file, no tool, no
// network. ReplayTask takes a read transaction and returns a report.

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// ReplayRecord is one recorded prediction, as replay reads it back.
type ReplayRecord struct {
	Site             string  `json:"site"`
	Subject          string  `json:"subject"`
	Predicted        float64 `json:"predicted"`
	Model            string  `json:"model"`
	SiteVersion      string  `json:"site_version"`
	TierAtPrediction string  `json:"tier_at_prediction"`
	Intervened       bool    `json:"intervened"`
	Outcome          *bool   `json:"outcome,omitempty"`
	Detail           string  `json:"detail,omitempty"`
}

// ReplayDivergence is one place where replaying under the current
// configuration would not reproduce what was recorded.
type ReplayDivergence struct {
	Site    string `json:"site"`
	Subject string `json:"subject"`
	// Recorded and Replayed each describe an effect in one phrase:
	// "applied", "not applied".
	Recorded string `json:"recorded"`
	Replayed string `json:"replayed"`
	// Reason says which input differs — the configured tier, the site's
	// question version, or the model.
	Reason string `json:"reason"`
}

// ReplayReport is one task's replay.
type ReplayReport struct {
	TaskID string `json:"task_id"`
	// JudgmentOps is how many judgment requests the journal recorded for
	// this task, whether or not any produced a prediction. A task with
	// requests and no predictions is a task whose sites all declined or
	// found nothing, which is different from one that never asked.
	JudgmentOps int                `json:"judgment_operations"`
	Records     []ReplayRecord     `json:"records"`
	Divergences []ReplayDivergence `json:"divergences,omitempty"`
	// UnknownSites names sites the records mention that this binary does not
	// register — a record written by a different build. They are reported
	// and skipped rather than replayed against a guess.
	UnknownSites []string `json:"unknown_sites,omitempty"`
	// VersionMismatch names sites whose recorded question version differs
	// from the one this binary implements. Their records are reported and
	// not replayed: the recorded probability answers a question this build
	// no longer asks.
	VersionMismatch []string `json:"version_mismatch,omitempty"`
	// ModelsSeen lists the distinct models the records came from.
	ModelsSeen []string `json:"models_seen,omitempty"`
}

// Replayable reports whether there was anything to replay at all.
func (r ReplayReport) Replayable() bool { return len(r.Records) > 0 }

// ReplaySiteInfo is what a replay needs to know about one site: what
// question version this build implements, what floor its caller applies, and
// whether the authority configured *now* lets that caller act at all.
//
// It is supplied by the caller rather than read here because this package
// does not import internal/judgment — internal/judgment reaches
// internal/firewall, which reaches internal/worktree, which reaches this
// package, and the cycle is real. It is the same inversion Recorder and
// AnswerCache already use: this package declares the shape, and whoever
// assembles a workspace fills it in.
type ReplaySiteInfo struct {
	// Version is the question version this build implements for the site.
	Version string
	// EffectThreshold is the probability at or above which the site's caller
	// acts. Zero means the caller acts on any finding.
	EffectThreshold float64
	// EffectPermitted is whether the tier configured now lets this site
	// apply any effect at all.
	EffectPermitted bool
	// ConfiguredTier is the tier's name, for the divergence reason.
	ConfiguredTier string
}

// SiteAuthority resolves a site name to what replay needs to know about it.
// Lookup returns false for a site this build does not register.
type SiteAuthority interface {
	Lookup(site string) (ReplaySiteInfo, bool)
}

// ReplayTask reads one task's recorded judgments and reports what the
// configured authority would do with them now.
//
// It opens a read transaction and nothing else. Passing a store whose
// database is read-only is fine and is what the CLI does.
func (c *CalibrationStore) ReplayTask(ctx context.Context, l *Ledger, taskID string, sites SiteAuthority) (ReplayReport, error) {
	rep := ReplayReport{TaskID: taskID}
	if c == nil || c.db == nil {
		return rep, fmt.Errorf("ledger: no calibration store")
	}

	// The journal half: how many judgment requests this task made. Counted
	// so a report can distinguish "no site ran" from "sites ran and none
	// produced a prediction", which lead a reader to different places.
	if l != nil {
		ops, err := l.Operations(ctx, taskID)
		if err != nil {
			return rep, err
		}
		for _, op := range ops {
			if op.Kind == KindJudgment {
				rep.JudgmentOps++
			}
		}
	}

	err := c.db.ReadTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT site, subject, predicted, model, site_version, tier_at_prediction,
			       intervened, outcome, detail
			FROM judgment_predictions
			WHERE task_id = ?
			ORDER BY seq ASC, created_at ASC, rowid ASC`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rec ReplayRecord
			var intervened int
			var outcome sql.NullInt64
			if err := rows.Scan(&rec.Site, &rec.Subject, &rec.Predicted, &rec.Model,
				&rec.SiteVersion, &rec.TierAtPrediction, &intervened, &outcome,
				&rec.Detail); err != nil {
				return err
			}
			rec.Intervened = intervened != 0
			if outcome.Valid {
				v := outcome.Int64 != 0
				rec.Outcome = &v
			}
			rep.Records = append(rep.Records, rec)
		}
		return rows.Err()
	})
	if err != nil {
		return rep, err
	}

	models := map[string]bool{}
	unknown := map[string]bool{}
	mismatch := map[string]bool{}
	for _, rec := range rep.Records {
		if rec.Model != "" {
			models[rec.Model] = true
		}
		info, known := sites.Lookup(rec.Site)
		if !known {
			unknown[rec.Site] = true
			continue
		}
		if rec.SiteVersion != "" && info.Version != "" && rec.SiteVersion != info.Version {
			mismatch[fmt.Sprintf("%s (recorded %s, this build %s)", rec.Site, rec.SiteVersion, info.Version)] = true
			continue
		}
		replayed := wouldApply(info, rec.Predicted)
		if replayed == rec.Intervened {
			continue
		}
		reason := fmt.Sprintf("tier at prediction %q, configured now %q",
			orUnset(rec.TierAtPrediction), orUnset(info.ConfiguredTier))
		rep.Divergences = append(rep.Divergences, ReplayDivergence{
			Site: rec.Site, Subject: rec.Subject,
			Recorded: applied(rec.Intervened), Replayed: applied(replayed), Reason: reason,
		})
	}
	rep.UnknownSites = sortedSet(unknown)
	rep.VersionMismatch = sortedSet(mismatch)
	rep.ModelsSeen = sortedSet(models)
	return rep, nil
}

// wouldApply is the replay's model of a call site: an effect is applied when
// the configured tier permits the site to act at all and the recorded
// probability clears the floor the site declared its caller uses.
//
// It is deliberately the *authority* question rather than a re-execution of
// each call site's composition. A site's own internal thresholds already ran
// when the prediction was recorded — a finding exists only because it
// crossed them — so what remains, and what configuration actually changes,
// is whether the caller was permitted to act and whether the composed
// probability cleared the caller's floor. ReplayReport's own comment says
// what this does not reproduce.
func wouldApply(info ReplaySiteInfo, predicted float64) bool {
	if !info.EffectPermitted {
		return false
	}
	if info.EffectThreshold > 0 && predicted < info.EffectThreshold {
		return false
	}
	return true
}

func applied(b bool) string {
	if b {
		return "applied"
	}
	return "not applied"
}

func orUnset(s string) string {
	if s == "" {
		return "unrecorded"
	}
	return s
}

func sortedSet(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
