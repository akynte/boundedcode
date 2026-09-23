-- ledger.db, schema 8. Judgment consultations: one row per site invocation,
-- including the ones that produce no finding.
--
-- The journal (operations, kind='judgment') records that a request left the
-- machine but not which site asked, and judgment_predictions records only a
-- finding. Between them a site that was consulted and reported nothing leaves
-- no trace, so "never reached" and "reached, asked, clean" read identically
-- and no site-level rate has a denominator. This table is that denominator.
--
-- It stores counts, statuses and identifiers only. No question text, no state,
-- no repository content, no credential: the same rule the operations journal
-- follows, because a ledger is attached to bug reports.
CREATE TABLE judgment_consultations (
  id             TEXT PRIMARY KEY,
  task_id        TEXT NOT NULL DEFAULT '',   -- '' outside a task
  site           TEXT NOT NULL,              -- judgment.SiteInfo.Name
  phase          TEXT NOT NULL DEFAULT '',   -- INTAKE..FINALIZE, '' if outside the workflow
  reached        INTEGER NOT NULL DEFAULT 1, -- the site ran; a missing row means it did not
  requested      INTEGER NOT NULL DEFAULT 0, -- a request actually left for the service
  status         TEXT NOT NULL,              -- judgment.Source: live, cache, disabled, ...
  skip_reason    TEXT NOT NULL DEFAULT '',   -- the site's own reason when it declined
  questions      INTEGER NOT NULL DEFAULT 0,
  subjects       INTEGER NOT NULL DEFAULT 0,
  findings       INTEGER NOT NULL DEFAULT 0, -- 0 with status live is a clean consultation
  model          TEXT NOT NULL DEFAULT '',
  endpoint       TEXT NOT NULL DEFAULT '',
  latency_ms     INTEGER NOT NULL DEFAULT 0,
  prediction_ids TEXT NOT NULL DEFAULT '',   -- comma-separated judgment_predictions ids
  created_at     INTEGER NOT NULL
) STRICT;

CREATE INDEX judgment_consultations_by_site ON judgment_consultations(site);
CREATE INDEX judgment_consultations_by_task ON judgment_consultations(task_id);

-- The other direction of the link: a prediction names the consultation that
-- produced it, so a finding can be traced back to the request that found it
-- and to the clean consultations of the same site around it.
ALTER TABLE judgment_predictions ADD COLUMN consultation_id TEXT NOT NULL DEFAULT '';
CREATE INDEX judgment_predictions_by_consultation ON judgment_predictions(consultation_id);
