# Derivability screening — slot large-prometheus

Method: scratch copies of each base source under ~/.cache/screen-prom (deleted afterwards). Ran base+hidden and base+hidden+gold with `GOFLAGS=-mod=mod GOPROXY=off`, single package, `-run` filters. All runs were offline and nothing needed the network.

| ID | Verdict | Reason |
|---|---|---|
| prometheus__prometheus-15142 | AMBIGUOUS | The test checks for a lost update, not the data race itself. The obvious fix of locking the write still fails it. |
| prometheus__prometheus-10720 | YES | The function name and its meaning are given. The expected values are the natural `time.YearDay()` results. |
| prometheus__prometheus-11859 | AMBIGUOUS | The test requires the snapshot to be **deleted** during `Head.Init` when it is ahead of the WAL. Other reasonable fixes fail. |
| prometheus__prometheus-10633 | NO | The test depends on fixture data (`port: 22`, `pi`, `buckets`, `coordinates`) that only the gold patch adds. |

## prometheus__prometheus-15142: race in headAppender.AppendHistogram (base 16bba78f1549)
- **Request:** a race-detector trace showing that `AppendHistogram` (head_append.go:668) writes `s.lastHistogramValue` without holding the series lock, while `memSeries.appendHistogram` writes it during Commit.
- **Test:** `TestHeadAppendHistogramAndCommitConcurrency`. Two goroutines each append the same histogram (ts=1, a new series per `i`, 10000 iterations) and commit. The test requires `NoError` on every append and commit, so a "duplicate sample for timestamp" error fails it. The check runs `go test ./tsdb -run ^TestHead` **without `-race`**.
- **(a)** On base the test fails deterministically (3/3 runs, "duplicate sample for timestamp"). With gold it passes, and the whole `^TestHead` set passes (4.1s).
- **(b)/(c)/(d)** Nothing unrelated is tested. No exact message is asserted. The test uses only symbols that exist at base (gold changes the private `getOrCreate` signature, but the test does not call it).
- **(e)** This is the key problem. The minimal fix suggested by the trace is to take `s.Lock()` around the initialising write. That removes the data race the issue reports, but the test still fails (3/3 runs). Here is why: the creating appender's late write of an empty histogram overwrites the value the other appender already committed. Gold avoids this by setting the value only when it is nil, under the lock. A careful engineer might reason this out, but the issue only asks for "no race condition". The test instead asserts an unstated functional property: concurrent duplicate appends must not error.
- **(f)** None.
- **Verdict: AMBIGUOUS.** The test rejects a fix that satisfies the issue's literal expectation (the race-detector warning goes away). It accepts only the stricter "do not clobber an existing lastHistogramValue" semantics.

## prometheus__prometheus-10720: PromQL day_of_year (base 89de30a0b754)
- **Request:** add the `day_of_year` function, alongside `day_of_month` and `day_of_week`.
- **Test:** `promql/testdata/functions.test` cases:
  - `day_of_year()` at time 0 gives 1.
  - `day_of_year(vector(1136239445))` gives 2.
  - 2016-12-31 gives 366.
  - 2022-12-31 gives 365.
- **(a)** On base: `parse error: unknown function with name "day_of_year"`. With gold, `TestEvaluations` passes.
- **(b)-(f)** Nothing unrelated is tested. The name and signature follow the siblings directly, and the values are standard `YearDay()` (range 1-366). The UI and docs changes in gold are not tested. No offline issues.
- **Verdict: YES.**

## prometheus__prometheus-11859: old snapshot loaded instead of newer (base 5ec1b4baaf01)
- **Request:** after the WAL index restarted, Prometheus keeps loading `chunk_snapshot.178079…` (a higher index but older) instead of the newer `013482…`. The issue's expectations:
  - the old snapshot should have been removed;
  - the new snapshot should be preferred.
- **Test:** `TestSnapshotAheadOfWALError`.
  1. Creates a snapshot at index 2, then wipes the WAL and writes segment 0 with snapshots disabled.
  2. Re-opens with snapshots enabled and calls `Init`.
  3. Asserts `LastChunkSnapshot` then returns `record.ErrNotFound`, meaning the snapshot directory was deleted.
- **(a)** Base fails (`expected not found, actual nil`). Gold passes (`^TestSnapshot`).
- **(d)** Every symbol the test uses exists at base: `LastChunkSnapshot`, `DeleteChunkSnapshots`, `wlog.NewSize`, `record.ErrNotFound`. None is gold-only.
- **(e)** The test accepts only one outcome: delete, at Init time, any snapshot whose index is greater than the last WAL segment. Several plausible fixes fail:
  - skipping or ignoring the stale snapshot without deleting it;
  - choosing a snapshot by mtime;
  - making snapshot creation delete all other snapshots (no new snapshot is written in the test).

  The issue does hint at removal ("should have been removed"). But it never says that the cause is the WAL index being reset, or where the cleanup should happen.
- **(c)/(f)** No messages are asserted, and there are no offline issues.
- **Verdict: AMBIGUOUS** (leaning NO). The cause can be derived from the snapshot naming. The required outcome (delete at Init, rather than just not loading the snapshot) is one choice among several reasonable ones.

## prometheus__prometheus-10633: PuppetDB numeric parameters (base 64fc3e58fec1)
- **Request:** convert int and float parameters into `__meta_puppetdb_parameter_*` labels (example: `port: 25` becomes `"25"`).
- **Test:** the expected label set in `TestPuppetDBRefreshWithParameters` gains:
  - `buckets="0,2,5"`
  - `coordinates="60.13464726551357,-2.0513768021728893"`
  - `pi="3.141592653589793"`
  - `port="22"`
- **(a)** Gold passes and base fails. **Base plus the gold code change alone (without the fixture edit) still FAILS**, because the values come from `discovery/puppetdb/fixtures/vhosts.json` entries that only the gold patch adds. The hidden patch touches only `puppetdb_test.go`.
- **(c)** Float formatting (`'g', -1`) and numeric arrays are reasonable to infer from the issue. The fixture content is not.
- **(d)** No new APIs are involved.
- **Verdict: NO.** The test cannot pass unless the agent invents fixture entries with these exact keys and values (`port: 22`, `pi`, `buckets [0,2,5]`, `coordinates`). The issue gives none of them, and they are not at base. Fix: move the fixture hunk into the hidden patch, then re-screen.
