package direct

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Calls of one model reply that change nothing (capability.Descriptor.
// Concurrent: reads, searches, symbol lookups, fetches) run together when
// they come one after another. Everything around the tool itself stays in
// call order: approval, rules and hooks before (prepareTool), checks,
// hooks and the result after (finishTool). Calls that change files, run
// commands, ask the user or change the session always run alone, so
// state-changing operations keep their order.

// DefaultParallelTools is direct.parallel_tools when it is not set.
const DefaultParallelTools = 4

func (r *Runner) parallelTools() int {
	if r.Opts.ParallelTools > 0 {
		return r.Opts.ParallelTools
	}
	return DefaultParallelTools
}

// batch is the calls at the head of calls that run together: the leading
// run of concurrent calls, or the first call alone.
func (r *Runner) batch(calls []ToolCall) []ToolCall {
	if r.parallelTools() < 2 {
		return calls[:1]
	}
	n := 0
	for _, c := range calls {
		t := r.findTool(c.Function.Name)
		if t == nil || !describe(t).Concurrent {
			break
		}
		n++
	}
	if n < 2 {
		return calls[:1]
	}
	return calls[:n]
}

// runCalls runs a batch and returns the results in call order.
func (r *Runner) runCalls(ctx context.Context, calls []ToolCall) []string {
	if len(calls) == 1 {
		return []string{r.runTool(ctx, calls[0])}
	}
	cs := make([]*toolCall, len(calls))
	for i, call := range calls {
		cs[i] = r.prepareTool(ctx, call)
	}
	sem := make(chan struct{}, r.parallelTools())
	var wg sync.WaitGroup
	start := time.Now()
	ran := 0
	for _, c := range cs {
		if c.done {
			continue
		}
		ran++
		wg.Add(1)
		go func(c *toolCall) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				c.err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			defer func() {
				if p := recover(); p != nil {
					c.err = fmt.Errorf("the tool failed: %v", p)
				}
			}()
			r.execTool(ctx, c)
		}(c)
	}
	wg.Wait()
	wall := time.Since(start)
	out := make([]string, len(cs))
	var sum time.Duration
	for i, c := range cs {
		if c.done {
			out[i] = c.result
			continue
		}
		sum += c.elapsed
		out[i] = r.finishTool(ctx, c)
	}
	if ran > 1 {
		r.emit(Event{Kind: EventBatch, Text: fmt.Sprintf("%d calls ran together in %s (%s one after another)", ran, wall.Round(time.Millisecond), sum.Round(time.Millisecond)),
			Args: map[string]any{"calls": ran, "wall_us": wall.Microseconds(), "sum_us": sum.Microseconds()}})
	}
	return out
}
