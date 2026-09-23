package native

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/llm"
)

// testClock is an editClock over a window, read at an adjustable time.
func testClock(window time.Duration) (*editClock, *time.Time) {
	start := time.Unix(1_800_000_000, 0)
	now := start
	c := &editClock{deadline: start.Add(window), window: window, now: func() time.Time { return now }}
	return c, &now
}

// The recorded attempt read for ten steps and reached its deadline without
// an edit, never told how long it had. Half the window gone with nothing
// changed gets one note, with the time left.
func TestTheModelIsToldWhenHalfItsEditTimeIsGone(t *testing.T) {
	c, now := testClock(200 * time.Second)
	if note := c.nudge(false); note != "" {
		t.Errorf("a note at the start of the window: %q", note)
	}
	*now = now.Add(120 * time.Second)
	note := c.nudge(false)
	if !strings.Contains(note, "1m20s") || !strings.Contains(note, "Make the edit now") {
		t.Errorf("the note does not give the time left and ask for the edit: %q", note)
	}
	if again := c.nudge(false); again != "" {
		t.Errorf("the note was repeated: %q", again)
	}
	edited, now2 := testClock(200 * time.Second)
	*now2 = now2.Add(150 * time.Second)
	if n := edited.nudge(true); n != "" {
		t.Errorf("a model that has edited was told to edit: %q", n)
	}
}

// A late step cannot spend what is left of the window thinking: one step
// thought for 97 seconds on the recorded task.
func TestAStepsReasoningIsSizedToTheTimeLeft(t *testing.T) {
	c, now := testClock(300 * time.Second)
	if got := c.reasoning(2048); got != 2048 {
		t.Errorf("before any rate is measured the budget changed: %d", got)
	}
	c.observe(&llm.ChatResponse{OutputTokens: 280, DecodeMS: 10_000}) // 28 tokens/s
	if got := c.reasoning(2048); got != 2048 {
		t.Errorf("with 300s left: %d, want the configured 2048", got)
	}
	*now = now.Add(260 * time.Second) // 40s left: a quarter is 10s, 280 tokens
	if got := c.reasoning(2048); got != 280 {
		t.Errorf("with 40s left: %d, want 280", got)
	}
}

// Without a deadline nothing changes.
func TestNoDeadlineNoClock(t *testing.T) {
	c := newEditClock(context.Background())
	if c.nudge(false) != "" || c.reasoning(2048) != 2048 {
		t.Error("the clock acted without a deadline")
	}
}

// A step that could not finish is not started. The recorded step began six
// seconds before EDIT's deadline with room for 4096 tokens, ran a minute past
// it on the server, and held the slot REVIEW needed.
func TestAStepThatCannotFinishIsNotStarted(t *testing.T) {
	c, now := testClock(300 * time.Second)
	if limit, ok := c.output(4096); !ok || limit != 4096 {
		t.Errorf("before a rate is measured: %d %v, want the limit unchanged", limit, ok)
	}
	c.observe(&llm.ChatResponse{OutputTokens: 280, DecodeMS: 10_000}) // 28 tokens/s
	*now = now.Add(240 * time.Second)                                 // 60s left
	if limit, ok := c.output(4096); !ok || limit != 1344 {
		t.Errorf("with 60s left: %d %v, want the output capped to 1344", limit, ok)
	}
	*now = now.Add(54 * time.Second) // 6s left
	if _, ok := c.output(4096); ok {
		t.Error("a step was allowed to start six seconds before the deadline")
	}
}
