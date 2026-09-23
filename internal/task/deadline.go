package task

// Deadline-aware phases.
//
// A task's wall-clock budget used to bound only the task's context, so every
// phase spent what it liked and the phase that ran out was whichever came
// last. On the reference card Bonsai spent about 450 s of a 600 s budget
// planning, and the recorded tasks ended in REVIEW — one after its fix had
// passed verification, which is the worst place to stop: the work was done
// and nothing was left to accept it.
//
// So the supervisor divides the time. Each phase has a deadline that holds
// back what the phases after it need — a minimum for EDIT, the measured
// verification time, one review call — and a structured call is sized to fit
// the time left in its window, from the throughput this task has measured:
// when time is short the model thinks less rather than being cut off. A
// call that cannot fit even its answer is refused with the reason, before it
// starts. EDIT that reaches its deadline stops and verifies what it has.
//
// Nothing here applies without a wall-clock budget.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/workflow"
)

const (
	// Assumed until the task has measured its own throughput. Conservative
	// for the reference card, which decodes at about 28 tokens/s and
	// prefills at about 260.
	defaultDecodeTPS  = 20.0
	defaultPrefillTPS = 200.0

	// reviewCallTokens is one review call with a small reasoning budget:
	// what REVIEW needs held back so acceptance is always reachable.
	reviewCallTokens = 2048
	// callOverhead covers request setup and prefill not otherwise counted.
	callOverhead = 15 * time.Second
	// defaultVerifyReserve is held for VERIFY until the task has run it once.
	defaultVerifyReserve = 60 * time.Second
	// minEditReserve is the least EDIT is left, however small the budget.
	minEditReserve = 90 * time.Second
	// maxReservedShare bounds everything held back, so a small budget still
	// leaves the planning phases something.
	maxReservedShare = 0.6
	// decodeMargin keeps a sized call from planning to the last second.
	decodeMargin = 0.9
)

// callShare is the fraction of its phase's remaining window one call may
// take. LOCALIZE makes three calls and PLAN follows in the same window;
// PLAN keeps room for a correction; REVIEW is the last call there is.
func callShare(phase workflow.Phase) float64 {
	switch phase {
	case workflow.Localize:
		return 0.25
	case workflow.Planning:
		return 0.6
	default:
		return 1
	}
}

// answerTokens is the least a phase's structured answer needs.
func answerTokens(phase workflow.Phase) int {
	switch phase {
	case workflow.Planning:
		return 1500
	default:
		return 600
	}
}

// OutOfTimeError refuses a call that cannot fit its phase's time. It is a
// budget outcome, not a model or transport failure, and not a rejected plan.
type OutOfTimeError struct {
	Phase workflow.Phase
	Left  time.Duration
	Need  time.Duration
}

func (e *OutOfTimeError) Error() string {
	return fmt.Sprintf("wall-clock budget exhausted in %s: %s left before the time held back for "+
		"the phases after it, and the call needs at least %s", e.Phase,
		e.Left.Round(time.Second), e.Need.Round(time.Second))
}

// isOutOfTime reports a phase that ran out of its share of the budget.
func isOutOfTime(err error) bool {
	var oot *OutOfTimeError
	return errors.As(err, &oot)
}

// taskDeadline is when the task's wall-clock budget ends, or zero.
func taskDeadline(t *Task, s *workflow.State) time.Time {
	if t.Budget.MaxWallTime <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(s.StartedAt).Add(t.Budget.MaxWallTime)
}

// reserves is what is held back for the phases after planning.
type reserves struct {
	edit, verify, review time.Duration
}

func (r reserves) total() time.Duration { return r.edit + r.verify + r.review }

// planReserves sizes what each later phase needs from what this task has
// measured, scaled down together when they would take too much.
func planReserves(t *Task, s *workflow.State) reserves {
	total := t.Budget.MaxWallTime
	rs := reserves{
		edit:   max(minEditReserve, total*15/100),
		verify: verifyReserve(s),
		review: callOverhead + tokensTime(reviewCallTokens, decodeRate(s)),
	}
	if limit := time.Duration(float64(total) * maxReservedShare); rs.total() > limit {
		scale := float64(limit) / float64(rs.total())
		rs.edit = time.Duration(float64(rs.edit) * scale)
		rs.verify = time.Duration(float64(rs.verify) * scale)
		rs.review = time.Duration(float64(rs.review) * scale)
	}
	return rs
}

// phaseDeadline is when a phase must be done so the ones after it keep their
// reserve, or zero without a wall-clock budget.
func phaseDeadline(t *Task, s *workflow.State, phase workflow.Phase) time.Time {
	d := taskDeadline(t, s)
	if d.IsZero() {
		return d
	}
	rs := planReserves(t, s)
	switch phase {
	case workflow.Intake, workflow.Localize, workflow.Impact, workflow.Planning:
		return d.Add(-rs.total())
	case workflow.Edit:
		return d.Add(-(rs.verify + rs.review))
	case workflow.Verify:
		return d.Add(-rs.review)
	default:
		return d
	}
}

// sizeCall fits a structured call's output and reasoning to the time left in
// its phase. limit and reasoning are the phase's configured maxima; the
// result never exceeds them.
func sizeCall(t *Task, s *workflow.State, phase workflow.Phase, promptTokens, limit, reasoning int,
	now time.Time) (int, int, error) {
	pd := phaseDeadline(t, s, phase)
	if pd.IsZero() {
		return limit, reasoning, nil
	}
	left := pd.Sub(now)
	overhead := min(callOverhead, left/10)
	// Reading the prompt is a fixed cost of the call, paid whatever it then
	// generates, so it comes out of the whole window; only generation is
	// shared with the calls after this one. Charging prefill against the
	// share starved a LOCALIZE call with a 15K-token prompt down to no
	// reasoning at all while four minutes remained, and it answered with an
	// empty selection.
	prefill := tokensTime(promptTokens, prefillRate(s))
	generate := left - overhead - prefill
	tokensIn := func(d time.Duration) int { return int(d.Seconds() * decodeRate(s) * decodeMargin) }
	answer := answerTokens(phase)
	if tokensIn(generate) < answer {
		return 0, 0, &OutOfTimeError{Phase: phase, Left: max(left, 0),
			Need: overhead + prefill + tokensTime(answer, decodeRate(s))}
	}
	// The call's share of the generation time leaves room for the calls
	// after it; when the share cannot hold an answer, the call takes all of
	// it rather than being refused with time on the clock.
	tokens := tokensIn(time.Duration(float64(generate) * callShare(phase)))
	if tokens < answer {
		tokens = tokensIn(generate)
	}
	limit = min(limit, tokens)
	reasoning = min(reasoning, max(1, limit-answer))
	return limit, reasoning, nil
}

// observeThroughput folds a response's timing into the task's measured
// rates. The provider's own split is used when it reports one.
func observeThroughput(s *workflow.State, resp *llm.ChatResponse) {
	if resp == nil {
		return
	}
	if resp.DecodeMS > 0 && resp.OutputTokens > 0 {
		s.DecodeTPS = blend(s.DecodeTPS, float64(resp.OutputTokens)/(float64(resp.DecodeMS)/1000))
	}
	if fresh := resp.PromptTokens - resp.CachedTokens; resp.PrefillMS > 0 && fresh > 0 {
		s.PrefillTPS = blend(s.PrefillTPS, float64(fresh)/(float64(resp.PrefillMS)/1000))
	}
}

// blend is an even moving average, seeded by the first sample.
func blend(have, sample float64) float64 {
	if have <= 0 {
		return sample
	}
	return (have + sample) / 2
}

func decodeRate(s *workflow.State) float64 {
	if s.DecodeTPS > 0 {
		return s.DecodeTPS
	}
	return defaultDecodeTPS
}

func prefillRate(s *workflow.State) float64 {
	if s.PrefillTPS > 0 {
		return s.PrefillTPS
	}
	return defaultPrefillTPS
}

func tokensTime(tokens int, perSecond float64) time.Duration {
	if tokens <= 0 || perSecond <= 0 {
		return 0
	}
	return time.Duration(float64(tokens) / perSecond * float64(time.Second))
}

// verifyReserve is how long VERIFY took the last time it ran in this task,
// with headroom, or a default before it has.
func verifyReserve(s *workflow.State) time.Duration {
	var took time.Duration
	for _, res := range s.Results {
		took += res.Duration
	}
	if took <= 0 {
		return defaultVerifyReserve
	}
	return max(30*time.Second, took*3/2)
}

// withPhaseDeadline bounds ctx by a phase's deadline, when there is one.
func withPhaseDeadline(ctx context.Context, t *Task, s *workflow.State, phase workflow.Phase) (context.Context, context.CancelFunc) {
	if pd := phaseDeadline(t, s, phase); !pd.IsZero() {
		return context.WithDeadline(ctx, pd)
	}
	return ctx, func() {}
}
