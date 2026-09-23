package ledger

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akynte/boundedcode/internal/attest"
)

// Signer signs evidence-chain hashes. *attest.Key is the implementation.
type Signer interface {
	ID() string
	Sign(hash string) string
}

// SetSigner makes every later chain record signed by s.
func (l *Ledger) SetSigner(s Signer) { l.signer = s }

// ChainRecord is one entry of the evidence chain: a verification run, linked
// to the entry before it by hash.
type ChainRecord struct {
	Seq       int64           `json:"seq"`
	TaskID    string          `json:"task_id"`
	Candidate string          `json:"candidate"`
	Payload   json.RawMessage `json:"payload"`
	PrevHash  string          `json:"prev_hash"`
	Hash      string          `json:"hash"`
	KeyID     string          `json:"key_id,omitempty"`
	Signature string          `json:"signature,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// ChainHash is the hash a record with this predecessor and payload must have.
func ChainHash(prev string, payload []byte) string {
	h := sha256.New()
	h.Write([]byte(prev))
	h.Write([]byte("\n"))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// AppendChain adds a record to the evidence chain. The payload is stored as
// the exact bytes hashed, so verification never depends on re-encoding it.
func (l *Ledger) AppendChain(ctx context.Context, taskID, candidate string, record any) (ChainRecord, error) {
	payload, err := json.Marshal(record)
	if err != nil {
		return ChainRecord{}, err
	}
	var out ChainRecord
	err = l.db.Tx(ctx, func(tx *sql.Tx) error {
		var prev string
		switch err := tx.QueryRowContext(ctx,
			`SELECT hash FROM evidence_chain ORDER BY seq DESC LIMIT 1`).Scan(&prev); {
		case errors.Is(err, sql.ErrNoRows):
			prev = ""
		case err != nil:
			return err
		}
		out = ChainRecord{
			TaskID: taskID, Candidate: candidate, Payload: payload,
			PrevHash: prev, Hash: ChainHash(prev, payload), CreatedAt: time.Now().UTC(),
		}
		if l.signer != nil {
			out.KeyID, out.Signature = l.signer.ID(), l.signer.Sign(out.Hash)
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO evidence_chain (task_id, candidate, payload, prev_hash, hash, key_id, signature, created_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			out.TaskID, out.Candidate, string(payload), out.PrevHash, out.Hash,
			out.KeyID, out.Signature, out.CreatedAt.UnixMilli())
		if err != nil {
			return err
		}
		out.Seq, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return ChainRecord{}, fmt.Errorf("ledger: append evidence chain: %w", err)
	}
	return out, nil
}

// Chain lists chain records in order, for one task or, with an empty taskID,
// all of them.
func (l *Ledger) Chain(ctx context.Context, taskID string) ([]ChainRecord, error) {
	query := `SELECT seq, task_id, candidate, payload, prev_hash, hash, key_id, signature, created_at
		FROM evidence_chain`
	var args []any
	if taskID != "" {
		query += ` WHERE task_id = ?`
		args = append(args, taskID)
	}
	rows, err := l.db.SQL().QueryContext(ctx, query+` ORDER BY seq`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ChainRecord
	for rows.Next() {
		var rec ChainRecord
		var payload string
		var created int64
		if err := rows.Scan(&rec.Seq, &rec.TaskID, &rec.Candidate, &payload, &rec.PrevHash,
			&rec.Hash, &rec.KeyID, &rec.Signature, &created); err != nil {
			return nil, err
		}
		rec.Payload = json.RawMessage(payload)
		rec.CreatedAt = time.UnixMilli(created).UTC()
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ChainIssue is one thing wrong with the chain.
type ChainIssue struct {
	Seq int64 `json:"seq"`
	// Broken is true when the record's integrity fails: its hash, its link or
	// its signature. False marks something weaker, such as a record signed by
	// a key the verifier was not given.
	Broken  bool   `json:"broken"`
	Problem string `json:"problem"`
}

// ChainReport is the result of verifying the whole chain.
type ChainReport struct {
	Records int          `json:"records"`
	Head    string       `json:"head"`
	Issues  []ChainIssue `json:"issues,omitempty"`
}

// Intact reports whether no record's integrity failed.
func (r ChainReport) Intact() bool {
	for _, i := range r.Issues {
		if i.Broken {
			return false
		}
	}
	return true
}

// VerifyChain recomputes every hash and link and checks every signature made
// by a key in keys (by key ID). Integrity is checked over the whole chain even
// when a caller cares about one task: a record can only be trusted if nothing
// before it was rewritten.
//
// A chain can still be truncated: removing the newest records leaves a valid
// shorter chain. Only a head recorded somewhere else — the Evidence-Head
// trailer on a task's commit, or a copy an auditor kept — detects that.
func (l *Ledger) VerifyChain(ctx context.Context, keys map[string]ed25519.PublicKey) (ChainReport, error) {
	records, err := l.Chain(ctx, "")
	if err != nil {
		return ChainReport{}, err
	}
	return VerifyRecords(records, keys), nil
}

// VerifyRecords checks a complete, ordered chain.
func VerifyRecords(records []ChainRecord, keys map[string]ed25519.PublicKey) ChainReport {
	rep := ChainReport{Records: len(records)}
	issue := func(seq int64, broken bool, format string, args ...any) {
		rep.Issues = append(rep.Issues, ChainIssue{Seq: seq, Broken: broken, Problem: fmt.Sprintf(format, args...)})
	}
	prev := ""
	for _, rec := range records {
		if rec.PrevHash != prev {
			issue(rec.Seq, true, "links to %s, but the record before it hashes to %s: a record was removed, "+
				"inserted or reordered", short(rec.PrevHash), short(prev))
		}
		if want := ChainHash(rec.PrevHash, rec.Payload); want != rec.Hash {
			issue(rec.Seq, true, "payload does not hash to the recorded hash: the record was modified")
		}
		// With keys, every record must verify against one of them. A record
		// that is unsigned, or signed by a key nobody vouched for, is exactly
		// what rewriting the chain and recomputing its hashes would leave:
		// the hashes can be recomputed by anyone, the signatures cannot.
		// Without keys only the hashes are checked, and saying so is all an
		// unsigned record merits.
		strict := len(keys) > 0
		switch pub, known := keys[rec.KeyID]; {
		case rec.Signature == "":
			issue(rec.Seq, strict, "unsigned")
		case !known:
			issue(rec.Seq, strict, "signed by key %s, which was not provided", rec.KeyID)
		case !attest.Verify(pub, rec.Hash, rec.Signature):
			issue(rec.Seq, true, "signature does not verify against key %s", rec.KeyID)
		}
		prev = rec.Hash
	}
	rep.Head = prev
	return rep
}

func short(h string) string {
	if h == "" {
		return "(start of chain)"
	}
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
