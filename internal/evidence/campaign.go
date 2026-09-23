package evidence

import (
	"context"
	"fmt"
)

// CampaignStepResult is what RunCampaignStep reports for one planned run —
// used by the CLI to print progress and by tests to assert on behavior
// without depending on stdout formatting.
type CampaignStepResult struct {
	Key    string
	Action ResumeAction
	Ran    bool
	Result Result
	Err    error
}

// RunCampaign drives every planned run in frozen order through the real
// resume decision, Prepare, ExecuteRun, and PersistResult — the single
// orchestration loop `bcode evidence run --live` calls (design instruction
// GAP A §2). It is the ONLY caller of ExecuteRun in this codebase; a CLI
// command that wants to run the campaign calls this function, never
// reimplements resume/persistence logic itself (§4: "do not duplicate
// this policy in CLI code").
//
// A run whose Decide result is ActionSkip is not touched. ActionStart and
// ActionRetryInfra both execute (ActionRetryInfra under a new, linked run
// ID — §14). ActionRecoverInterrupted marks the stale RUNNING record
// infrastructure-interrupted first, then starts a new linked run, exactly
// like ActionRetryInfra.
//
// onStep, if non-nil, is called after every planned run is decided
// (including skips) — the CLI uses it to print a running "PARTIAL
// CAMPAIGN" line per §21; it is never required for correctness.
func RunCampaign(ctx context.Context, runRoot string, led *Ledger, rf *RunnerFactory, plans []RunPlan, onStep func(CampaignStepResult)) ([]CampaignStepResult, error) {
	current, err := led.Current()
	if err != nil {
		return nil, fmt.Errorf("evidence: reading ledger: %w", err)
	}

	var steps []CampaignStepResult
	for _, p := range Ordered(plans) {
		key := p.Key()
		var latest *Record
		if rec, ok := current[key]; ok {
			r := rec
			latest = &r
		}
		action := Decide(latest)

		step := CampaignStepResult{Key: key, Action: action}
		if action == ActionSkip {
			steps = append(steps, step)
			if onStep != nil {
				onStep(step)
			}
			continue
		}

		if action == ActionRecoverInterrupted {
			if err := led.Append(Record{
				SuiteHash: p.SuiteHash, TaskID: p.TaskID, Arm: p.Arm, RunID: latest.RunID,
				Status: StatusInfraErr, Detail: "previous process died mid-run; recovered on resume",
			}); err != nil {
				step.Err = fmt.Errorf("marking interrupted run infrastructure-failed: %w", err)
				steps = append(steps, step)
				if onStep != nil {
					onStep(step)
				}
				continue
			}
		}

		runID := NewRunID(p.TaskID, p.Arm)
		previousRunID, retryReason := "", ""
		if latest != nil && (action == ActionRetryInfra || action == ActionRecoverInterrupted) {
			previousRunID, retryReason = latest.RunID, "linked infrastructure retry: previous status "+string(latest.Status)
		}

		if err := led.Append(Record{
			SuiteHash: p.SuiteHash, TaskID: p.TaskID, Arm: p.Arm, RunID: runID, Status: StatusRunning,
			PreviousRunID: previousRunID, RetryReason: retryReason,
		}); err != nil {
			step.Err = fmt.Errorf("recording RUNNING: %w", err)
			steps = append(steps, step)
			if onStep != nil {
				onStep(step)
			}
			continue
		}

		res, patch, evaluatorRaw, judgments, runErr := ExecuteRun(ctx, runRoot, rf, p, runID)
		step.Ran = true
		step.Result = res

		finalStatus := res.OfficialBenchmarkStatus
		if runErr != nil {
			finalStatus = StatusInfraErr
			step.Err = runErr
		}

		if _, persistErr := PersistResult(runRoot, res, patch, evaluatorRaw, judgments); persistErr != nil &&
			runErr == nil {
			// A run that completed with a usable Result but could not be
			// durably persisted must not be recorded as its official
			// status — design instruction §13: no half-valid completed
			// result. Record it as an infrastructure failure instead so
			// resume retries it.
			finalStatus = StatusInfraErr
			step.Err = fmt.Errorf("persisting result: %w", persistErr)
		}

		if err := led.Append(Record{
			SuiteHash: p.SuiteHash, TaskID: p.TaskID, Arm: p.Arm, RunID: runID, Status: finalStatus,
			PreviousRunID: previousRunID, RetryReason: retryReason, Detail: res.Detail,
		}); err != nil {
			step.Err = fmt.Errorf("recording final status: %w", err)
		}

		// CHECK 2 (3rd finalization pass, §the report must regenerate
		// from persisted data after every ExecuteRun -> PersistResult ->
		// ledger update, never from the in-memory `res` this loop is
		// still holding): RefreshCampaignReport re-reads the ledger and
		// every result.json fresh from disk.
		if _, reportErr := RefreshCampaignReport(runRoot, p.SuiteHash, plans, led); reportErr != nil &&
			step.Err == nil {
			step.Err = fmt.Errorf("refreshing the campaign report: %w", reportErr)
		}

		steps = append(steps, step)
		if onStep != nil {
			onStep(step)
		}
	}
	return steps, nil
}
