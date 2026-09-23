-- ledger.db, schema 7. Three things schema 6 left implicit, each of which
-- makes a calibration figure either unreadable or misleading without it.
--
-- 1. Ordering. Schema 6 resolved "the most recent unresolved prediction for
--    this subject" with `ORDER BY created_at DESC, rowid DESC`, where rowid
--    is an SQLite implementation detail this schema never declared and no
--    migration guarantees (a VACUUM may renumber it). `seq` is an explicit,
--    monotonic, declared column: the ordering guarantee now lives in the
--    schema rather than in an assumption about the engine.
--
-- 2. Version binding. A calibration figure pools rows; pooling rows produced
--    by different models, or by materially different versions of the same
--    question, averages two measurements into a number describing neither.
--    site_version and the existing model column let a report partition
--    rather than pool. judgment.yaml already insists on a pinned model for
--    the same reason.
--
-- 3. Intervention. At `logged` a prediction changes nothing, so the outcome
--    that follows is an observation. Once a site is promoted, its own effect
--    can move the outcome it is later scored against — a tainted hunk that
--    draws a rejection, an attempt stopped early that therefore never
--    verifies. Scoring those together with shadow rows is self-confirming.
--    tier_at_prediction records the authority in force when the prediction
--    was made, and intervened records whether an effect was actually applied
--    to this subject, so the two populations can be reported apart.
ALTER TABLE judgment_predictions ADD COLUMN seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE judgment_predictions ADD COLUMN site_version TEXT NOT NULL DEFAULT '';
ALTER TABLE judgment_predictions ADD COLUMN tier_at_prediction TEXT NOT NULL DEFAULT '';
ALTER TABLE judgment_predictions ADD COLUMN intervened INTEGER NOT NULL DEFAULT 0;

-- Existing rows keep seq 0 and sort among themselves by rowid, which is what
-- they were written under; new rows get a real sequence. The index is on
-- (site, seq) because every read either partitions by site or resolves the
-- latest row for one subject, and both want seq ordered within a site.
CREATE INDEX judgment_predictions_by_site_seq ON judgment_predictions(site, seq);
CREATE INDEX judgment_predictions_by_subject ON judgment_predictions(task_id, site, subject, seq);
