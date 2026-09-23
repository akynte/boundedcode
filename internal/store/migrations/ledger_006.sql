-- ledger.db, schema 6. Judgment calibration: a prediction a judgment site
-- made, paired later with what was actually observed, so a site's
-- calibration can be read from ordinary use rather than assumed or
-- benchmarked.
--
-- A prediction and its outcome are written at different times, sometimes
-- across a process restart (a task can pause for a gate approval and resume
-- under a different `bcode` invocation), which is why this is its own table
-- with real, queryable columns rather than a JSON blob in `operations`: the
-- intent/outcome pair there assumes both halves are written from the same
-- code path shortly apart, and site/subject/outcome need to be filterable
-- without parsing JSON.
CREATE TABLE judgment_predictions (
  id           TEXT PRIMARY KEY,
  task_id      TEXT NOT NULL,
  site         TEXT NOT NULL,              -- judgment.SiteInfo.Name, e.g. "verification_integrity"
  subject      TEXT NOT NULL,              -- what was predicted about, e.g. "hunk:pkg/order/order_test.go"
  predicted    REAL NOT NULL,              -- the probability the site reported, 0..1
  model        TEXT NOT NULL DEFAULT '',   -- model id that answered, for provenance
  detail       TEXT NOT NULL DEFAULT '',   -- one line, for a person reading the table
  created_at   INTEGER NOT NULL,
  outcome      INTEGER,                    -- NULL = unresolved; 0 or 1 once known
  outcome_note TEXT NOT NULL DEFAULT '',
  resolved_at  INTEGER
) STRICT;
CREATE INDEX judgment_predictions_by_site ON judgment_predictions(site, resolved_at);
CREATE INDEX judgment_predictions_by_task_site ON judgment_predictions(task_id, site);
