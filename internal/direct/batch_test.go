package direct

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/capability"
)

// slowTool adds a concurrent read-only tool that takes d and records how
// many of its calls run at once.
func slowTool(t *testing.T, r *Runner, d time.Duration, peak *atomic.Int32) {
	t.Helper()
	builtinCapabilities["slow_read"] = capability.Descriptor{Effects: capability.Read, Concurrent: true}
	t.Cleanup(func() { delete(builtinCapabilities, "slow_read") })
	var running atomic.Int32
	r.toolList()
	r.tools = append(r.tools, Tool{
		Name: "slow_read", Description: "test", Schema: schema(map[string]any{"path": prop("string", "")}),
		Summary: func(a map[string]any) string { return "slow " + str(a, "path") },
		Run: func(ctx context.Context, _ *Runner, a map[string]any) (string, error) {
			n := running.Add(1)
			defer running.Add(-1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return "", ctx.Err()
			}
			if str(a, "path") == "missing" {
				return "", errors.New("no such file")
			}
			return "content of " + str(a, "path"), nil
		},
	})
}

func slowCalls(paths ...string) []call {
	var out []call
	for _, p := range paths {
		out = append(out, call{"slow_read", map[string]any{"path": p}})
	}
	return out
}

func toolMessages(r *Runner) []string {
	var out []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			out = append(out, m.ToolCallID+"="+m.Content)
		}
	}
	return out
}

// Calls of one reply that change nothing run together; their results and
// events come in call order, an error in one does not affect the others.
func TestConcurrentCallsRunTogether(t *testing.T) {
	up := &scripted{replies: []reply{{calls: slowCalls("a", "b", "missing", "d")}, {text: "done"}}}
	r, _, evs := newRunner(t, up, ApprovalAuto)
	var peak atomic.Int32
	slowTool(t, r, 300*time.Millisecond, &peak)
	start := time.Now()
	if _, err := r.Send(context.Background(), "read them"); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 900*time.Millisecond {
		t.Fatalf("took %s: the calls did not run together", el)
	}
	if peak.Load() != 4 {
		t.Fatalf("peak %d running at once, want 4", peak.Load())
	}
	got := toolMessages(r)
	if len(got) != 4 || !strings.HasSuffix(got[0], "=content of a") || !strings.HasSuffix(got[1], "=content of b") ||
		!strings.Contains(got[2], "error: no such file") || !strings.HasSuffix(got[3], "=content of d") {
		t.Fatalf("results %q", got)
	}
	var order []string
	batches := 0
	for _, e := range *evs {
		switch e.Kind {
		case EventToolStart, EventToolEnd:
			order = append(order, string(e.Kind)+":"+e.CallID)
		case EventBatch:
			batches++
			if e.Args["calls"] != 4 {
				t.Fatalf("batch %+v", e.Args)
			}
		}
		if e.Kind == EventToolEnd && strings.Contains(e.Err, "no such file") && e.Code != capability.CodeFailed {
			t.Fatalf("code %q", e.Code)
		}
	}
	var ids []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			ids = append(ids, m.ToolCallID)
		}
	}
	var want []string
	for _, k := range []string{"tool_start", "tool_end"} {
		for _, id := range ids {
			want = append(want, k+":"+id)
		}
	}
	if !reflect.DeepEqual(order, want) || batches != 1 {
		t.Fatalf("events %v (%d batches), want %v", order, batches, want)
	}
}

// direct.parallel_tools: 1 runs every call in turn.
func TestParallelToolsOne(t *testing.T) {
	up := &scripted{replies: []reply{{calls: slowCalls("a", "b")}, {text: "done"}}}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	r.Opts.ParallelTools = 1
	var peak atomic.Int32
	slowTool(t, r, 50*time.Millisecond, &peak)
	if _, err := r.Send(context.Background(), "read them"); err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 1 {
		t.Fatalf("peak %d", peak.Load())
	}
}

// A call that changes something runs alone and keeps its place: reads
// before it run together, reads after it see its change.
func TestChangeSplitsBatch(t *testing.T) {
	up := &scripted{}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	writeFile(t, root, "f.txt", "one\n")
	var peak atomic.Int32
	slowTool(t, r, 10*time.Millisecond, &peak)
	up.replies = []reply{{calls: []call{
		{"read", map[string]any{"path": "f.txt"}},
		{"read", map[string]any{"path": "f.txt"}},
		{"write", map[string]any{"path": "f.txt", "content": "two\n"}},
		{"read", map[string]any{"path": "f.txt"}},
	}}, {text: "done"}}
	if _, err := r.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	got := toolMessages(r)
	if len(got) != 4 || !strings.Contains(got[0], "one") || !strings.Contains(got[1], "one") || !strings.Contains(got[3], "two") {
		t.Fatalf("results %q", got)
	}
	if got := r.batch(r.Session.Messages[1].ToolCalls); len(got) != 2 {
		t.Fatalf("batch of %d", len(got))
	}
}

// An interrupted batch records every call of it and skips the rest of the
// reply, so each call has an answer.
func TestInterruptedBatch(t *testing.T) {
	up := &scripted{replies: []reply{{calls: append(slowCalls("a", "b"), call{"ls", map[string]any{}})}}}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	var peak atomic.Int32
	slowTool(t, r, 5*time.Second, &peak)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	_, err := r.Send(ctx, "go")
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the batch did not stop")
	}
	// ls is concurrent too, so it was in the batch.
	got := toolMessages(r)
	if len(got) != 3 {
		t.Fatalf("results %q", got)
	}
	for _, g := range got[:2] {
		if !strings.Contains(g, "context canceled") {
			t.Fatalf("results %q", got)
		}
	}
}

// The session record is the same whether calls run together or in turn,
// and a batch's events come in the same order every time: the starts in
// call order, then the ends in call order.
func TestBatchingKeepsSessionDeterministic(t *testing.T) {
	run := func(parallel int) ([]Message, []string) {
		up := &scripted{}
		r, root, evs := newRunner(t, up, ApprovalAuto)
		r.Opts.ParallelTools = parallel
		for i := range 6 {
			writeFile(t, root, fmt.Sprintf("f%d.go", i), fmt.Sprintf("package p\n\nfunc F%d() {}\n", i))
		}
		var calls []call
		for i := range 6 {
			calls = append(calls, call{"read", map[string]any{"path": fmt.Sprintf("f%d.go", i)}})
		}
		calls = append(calls, call{"grep", map[string]any{"pattern": "func F"}}, call{"glob", map[string]any{"pattern": "*.go"}})
		up.replies = []reply{{calls: calls}, {calls: []call{{"edit", map[string]any{"path": "f0.go", "old_string": "F0", "new_string": "G0"}}}}, {text: "done"}}
		if _, err := r.Send(context.Background(), "go"); err != nil {
			t.Fatal(err)
		}
		var ks []string
		for _, e := range *evs {
			if e.Kind == EventBatch || e.Kind == EventTextDelta {
				continue
			}
			ks = append(ks, string(e.Kind)+":"+e.CallID+":"+e.Err)
		}
		msgs := append([]Message(nil), r.Session.Messages...)
		for i := range msgs {
			msgs[i].Stamps = nil // blobs of temp files differ per run only by path
		}
		return msgs, ks
	}
	m1, _ := run(1)
	m4, e4 := run(4)
	_, e4again := run(4)
	if len(m1) != len(m4) {
		t.Fatalf("%d messages vs %d", len(m1), len(m4))
	}
	for i := range m1 {
		if m1[i].Role != m4[i].Role || m1[i].ToolCallID != m4[i].ToolCallID || m1[i].Content != m4[i].Content {
			t.Fatalf("message %d differs:\n%+v\n%+v", i, m1[i], m4[i])
		}
	}
	if !reflect.DeepEqual(e4, e4again) {
		t.Fatalf("events differ between runs:\n%v\n%v", e4, e4again)
	}
}
