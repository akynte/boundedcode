package ledger_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/attest"
	"github.com/akynte/boundedcode/internal/ledger"
)

func signedChain(t *testing.T, n int) (*ledger.Ledger, []ledger.ChainRecord, map[string]ed25519.PublicKey, *attest.Key) {
	t.Helper()
	l, _ := newLedger(t)
	key, err := attest.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	l.SetSigner(key)
	ctx := context.Background()
	for i := 0; i < n; i++ {
		task := "t1"
		if i%2 == 1 {
			task = "t2"
		}
		if _, err := l.AppendChain(ctx, task, "cand", map[string]any{"run": i, "status": "pass"}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := l.Chain(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	return l, records, map[string]ed25519.PublicKey{key.ID(): key.Public()}, key
}

func TestAChainAsWrittenVerifies(t *testing.T) {
	l, records, keys, _ := signedChain(t, 4)
	rep, err := l.VerifyChain(context.Background(), keys)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Intact() || len(rep.Issues) != 0 || rep.Records != 4 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Head != records[3].Hash {
		t.Error("the head must be the newest record's hash")
	}
	if records[0].PrevHash != "" || records[1].PrevHash != records[0].Hash {
		t.Error("each record must link to the one before it")
	}
	mine, err := l.Chain(context.Background(), "t2")
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 {
		t.Errorf("t2 has %d records, want 2", len(mine))
	}
}

func TestTheChainCannotBeEditedInPlace(t *testing.T) {
	l, st := newLedger(t)
	if _, err := l.AppendChain(context.Background(), "t", "c", map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE evidence_chain SET payload = '{"a":2}'`,
		`DELETE FROM evidence_chain`,
	} {
		_, err := st.Ledger().SQL().Exec(q)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: want the append-only trigger to refuse it, got %v", q, err)
		}
	}
}

func brokenAt(rep ledger.ChainReport, seq int64) bool {
	for _, i := range rep.Issues {
		if i.Seq == seq && i.Broken {
			return true
		}
	}
	return false
}

// Someone with the database file does not go through the triggers. These are
// the edits they could make, done to a copy of the records, and each must be
// visible.
func TestTamperingIsDetected(t *testing.T) {
	_, records, keys, _ := signedChain(t, 4)
	clone := func() []ledger.ChainRecord { return append([]ledger.ChainRecord(nil), records...) }

	modified := clone()
	modified[1].Payload = json.RawMessage(`{"run":1,"status":"fail"}`)
	if rep := ledger.VerifyRecords(modified, keys); !brokenAt(rep, modified[1].Seq) {
		t.Errorf("a modified payload went unnoticed: %+v", rep)
	}

	removed := append(clone()[:1], clone()[2:]...)
	if rep := ledger.VerifyRecords(removed, keys); rep.Intact() {
		t.Errorf("a removed record went unnoticed: %+v", rep)
	}

	// The strong forgery: change a record, then recompute every hash and link
	// after it so the chain is internally consistent again. Without the key
	// the signatures cannot be redone, so they are stripped.
	forged := clone()
	forged[1].Payload = json.RawMessage(`{"run":1,"status":"fail"}`)
	prev := forged[0].Hash
	for i := 1; i < len(forged); i++ {
		forged[i].PrevHash = prev
		forged[i].Hash = ledger.ChainHash(prev, forged[i].Payload)
		forged[i].Signature, forged[i].KeyID = "", ""
		prev = forged[i].Hash
	}
	if rep := ledger.VerifyRecords(forged, keys); rep.Intact() {
		t.Errorf("a re-hashed forgery passed with the key given: %+v", rep)
	}
	// Re-signed with a key the verifier was not given: also broken.
	impostor, err := attest.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(forged); i++ {
		forged[i].KeyID, forged[i].Signature = impostor.ID(), impostor.Sign(forged[i].Hash)
	}
	if rep := ledger.VerifyRecords(forged, keys); rep.Intact() {
		t.Errorf("a forgery signed by another key passed: %+v", rep)
	}
}

// Truncation leaves a valid, shorter chain. The hashes cannot catch it; the
// head recorded outside the ledger (the commit trailer) is what does. This
// test pins the limit so nobody claims otherwise.
func TestTruncationNeedsAnExternalHead(t *testing.T) {
	_, records, keys, _ := signedChain(t, 3)
	rep := ledger.VerifyRecords(records[:2], keys)
	if !rep.Intact() {
		t.Fatalf("a truncated chain is internally valid: %+v", rep)
	}
	if rep.Head == records[2].Hash {
		t.Fatal("test setup: the truncated head must differ")
	}
}

func TestWithoutKeysUnsignedIsNotedNotBroken(t *testing.T) {
	l, _ := newLedger(t)
	if _, err := l.AppendChain(context.Background(), "t", "c", map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	rep, err := l.VerifyChain(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Intact() || len(rep.Issues) != 1 || rep.Issues[0].Problem != "unsigned" {
		t.Fatalf("report = %+v", rep)
	}
}
