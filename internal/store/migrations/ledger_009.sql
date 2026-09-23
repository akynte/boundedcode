-- ledger.db, schema 9. The evidence chain: one append-only record per
-- verification run, each linked to the one before by hash and signed by the
-- verifier's key.
--
-- The evidence table keeps the latest result per (task, kind, position) and
-- is updated in place, so it answers "what is true now" and loses history. The
-- chain answers "what was verified, against which exact bytes, judged by which
-- oracle", for every run, and makes any later edit to that history visible:
-- a changed payload no longer hashes to its row, and a removed or reordered row
-- breaks the link from its successor.
--
-- payload is stored as the exact bytes that were hashed. hash is
-- sha256(prev_hash || "\n" || payload); the first row's prev_hash is empty.
-- signature is over the domain-separated hash (internal/attest).
-- prev_hash is UNIQUE so the chain cannot fork: two records claiming the same
-- predecessor is exactly what a concurrent or replayed write would produce.
--
-- The triggers stop the application, or a careless query, from rewriting the
-- chain. They do not stop someone with the database file; the hashes and
-- signatures are what detect that.
CREATE TABLE evidence_chain (
  seq        INTEGER PRIMARY KEY,
  task_id    TEXT NOT NULL,
  candidate  TEXT NOT NULL DEFAULT '',
  payload    TEXT NOT NULL,
  prev_hash  TEXT NOT NULL UNIQUE,
  hash       TEXT NOT NULL UNIQUE,
  key_id     TEXT NOT NULL DEFAULT '',
  signature  TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX evidence_chain_by_task ON evidence_chain(task_id, seq);

CREATE TRIGGER evidence_chain_no_update BEFORE UPDATE ON evidence_chain
BEGIN
  SELECT RAISE(ABORT, 'evidence_chain is append-only');
END;

CREATE TRIGGER evidence_chain_no_delete BEFORE DELETE ON evidence_chain
BEGIN
  SELECT RAISE(ABORT, 'evidence_chain is append-only');
END;
