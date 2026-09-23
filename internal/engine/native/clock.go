package native

import (
	"context"
	"fmt"
	"time"

	"github.com/akynte/boundedcode/internal/llm"
)

// editClock makes an EDIT attempt aware of the time it has.
//
// The supervisor gives EDIT a deadline — its share of the task's wall-clock
// budget, ahead of what VERIFY and REVIEW need — but the model could not see
// it. On the recorded task Bonsai had the code in front of it, spent ten
// steps reading files, one of them unrelated, and one step thinking for 97
// seconds, and reached the deadline without an edit. Two things follow from
// knowing the deadline: the model is told, once, when half the time has gone
// with nothing changed; and a step's reasoning is sized to the time left, so a
// late step cannot spend the rest of the window thinking.
type editClock struct {
	deadline time.Time
	window   time.Duration
	tps      float64
	nudged   bool
	now      func() time.Time
}

const (
	// reasoningShare is the most of the remaining EDIT time one step's
	// reasoning may take.
	reasoningShare = 0.25
	// outputMargin keeps a step's whole output inside the time left.
	outputMargin = 0.8
	// minStepTokens is the least a step can do anything useful with.
	minStepTokens = 256
)

func newEditClock(ctx context.Context) *editClock {
	c := &editClock{now: time.Now}
	if d, ok := ctx.Deadline(); ok {
		c.deadline = d
		c.window = d.Sub(c.now())
	}
	return c
}

func (c *editClock) remaining() time.Duration {
	if c.deadline.IsZero() {
		return 0
	}
	return c.deadline.Sub(c.now())
}

// nudge returns the one-time note for a model that has used half its time
// without editing, or "".
func (c *editClock) nudge(edited bool) string {
	if c.deadline.IsZero() || c.nudged || edited || c.window <= 0 {
		return ""
	}
	left := c.remaining()
	if left > c.window/2 {
		return ""
	}
	c.nudged = true
	return fmt.Sprintf("[supervisor] About %s remain for editing before the change must be "+
		"verified, and nothing has been changed yet. Make the edit now with what you have read; "+
		"verification will show what is still wrong.\n", left.Round(time.Second))
}

// reasoning caps a step's reasoning budget to a share of the time left, at
// the decode rate this attempt has measured.
func (c *editClock) reasoning(budget int) int {
	if c.deadline.IsZero() || c.tps <= 0 {
		return budget
	}
	affordable := int(c.remaining().Seconds() * reasoningShare * c.tps)
	return min(budget, max(1, affordable))
}

// observe folds a response's decode rate into the measured one.
func (c *editClock) observe(out *llm.ChatResponse) {
	if out == nil || out.DecodeMS <= 0 || out.OutputTokens <= 0 {
		return
	}
	rate := float64(out.OutputTokens) / (float64(out.DecodeMS) / 1000)
	if c.tps <= 0 {
		c.tps = rate
	} else {
		c.tps = (c.tps + rate) / 2
	}
}

// output caps a step's output to what the measured rate can produce before
// the deadline, and reports false when that is too little to start a step.
//
// A call the supervisor abandons at the deadline is not abandoned by the
// server: llama-server does not notice a non-streaming client going away and
// generates on. The recorded step started six seconds before EDIT's deadline
// with room for 4096 tokens, generated for a minute after it, and held the
// single slot while REVIEW's request waited behind it until the task's
// budget ran out. A step that could not finish is not started.
func (c *editClock) output(limit int) (int, bool) {
	if c.deadline.IsZero() || c.tps <= 0 {
		return limit, true
	}
	affordable := int(c.remaining().Seconds() * c.tps * outputMargin)
	if affordable < minStepTokens {
		return 0, false
	}
	return min(limit, affordable), true
}
