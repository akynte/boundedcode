package orchestrator

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/agent/scripted"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/task"
)

func action(tool, command, path string) agent.Event {
	a, _ := json.Marshal(map[string]string{"command": command, "path": path})
	raw, _ := json.Marshal(map[string]any{"kind": "ActionEvent", "tool": tool, "action": string(a)})
	return agent.Event{Kind: "ActionEvent", Tool: tool, Raw: raw}
}

func observation(text string) agent.Event {
	raw, _ := json.Marshal(map[string]any{"kind": "ObservationEvent", "tool": "terminal", "text": text})
	return agent.Event{Kind: "ObservationEvent", Tool: "terminal", Raw: raw}
}

var budget = config.StrategyBudget{NoProgressTokens: 60000, MaxTokens: 100000, MaxDuration: config.Duration(45 * time.Minute)}

// TestGovernorStopsUnproductiveStrategy replays the shape of the caddy
// failure (re-reading the same files, edits that never make a test pass)
// without anything task-specific: it is stopped once 60K tokens pass
// without progress, long before the hour it used to take.
func TestGovernorStopsUnproductiveStrategy(t *testing.T) {
	now := time.Now()
	g := newGovernor(budget, 0, now)
	gen := 0
	g.observe(action("file_editor", "str_replace", "/w/work/r/lexer.go"), gen) // first edit: progress
	for i := 0; i < 40; i++ {
		gen += 2500
		g.observe(action("file_editor", "view", "/w/work/r/lexer.go"), gen)
		g.observe(action("file_editor", "str_replace", "/w/work/r/lexer.go"), gen)
		g.observe(action("terminal", "go test ./caddyfile/", ""), gen)
		g.observe(observation("--- FAIL: TestX\n[The command completed with exit code 1.]"), gen)
		if stop, why := g.check(gen, now.Add(time.Duration(i)*time.Minute)); stop {
			if gen > 65000 || !strings.Contains(why, "since the last progress") {
				t.Fatalf("stopped late or for the wrong reason at %d tokens: %s", gen, why)
			}
			if s := g.summary(); !strings.Contains(s, "r/lexer.go (") || !strings.Contains(s, "first edit") {
				t.Fatalf("summary = %q", s)
			}
			return
		}
	}
	t.Fatal("unproductive strategy was never stopped")
}

// TestGovernorAllowsProductiveStrategy: a long attempt that keeps making
// progress (new tests, failing tests turning green) is not stopped until the
// hard cap.
func TestGovernorAllowsProductiveStrategy(t *testing.T) {
	now := time.Now()
	g := newGovernor(budget, 0, now)
	gen := 0
	for i := 0; i < 4; i++ { // 4 rounds of 20K tokens, each ending in progress
		gen += 20000
		g.observe(action("file_editor", "create", "/w/work/r/pkg/case"+string(rune('a'+i))+"_test.go"), gen)
		g.observe(action("terminal", "go test ./pkg/", ""), gen)
		g.observe(observation("FAIL\n[The command completed with exit code 1.]"), gen)
		g.observe(action("file_editor", "str_replace", "/w/work/r/pkg/x.go"), gen)
		g.observe(action("terminal", "go test ./pkg/", ""), gen)
		g.observe(observation("ok\n[The command completed with exit code 0.]"), gen)
		if stop, why := g.check(gen, now.Add(time.Duration(i)*5*time.Minute)); stop {
			t.Fatalf("productive strategy stopped at %d tokens: %s", gen, why)
		}
	}
	if stop, why := g.check(100000, now.Add(30*time.Minute)); !stop || !strings.Contains(why, "attempt generated") {
		t.Fatalf("hard cap not applied: %v %s", stop, why)
	}
	if stop, why := newGovernor(budget, 0, now).check(0, now.Add(46*time.Minute)); !stop || !strings.Contains(why, "ran") {
		t.Fatalf("duration cap not applied: %v %s", stop, why)
	}
	if stop, _ := newGovernor(config.StrategyBudget{}, 0, now).check(1<<30, now.Add(100*time.Hour)); stop {
		t.Fatal("zero budgets must disable the governor")
	}
}

// TestStoppedStrategyIsRecordedAndNotRepeated: the run loop stops a looping
// attempt, records why, compacts, tells the next attempt to change approach,
// and still completes when the next attempt makes the fix.
func TestStoppedStrategyIsRecordedAndNotRepeated(t *testing.T) {
	_, w, s, root := setup(t)
	defer s.Close()
	ctx := context.Background()
	old := governorTick
	governorTick = 20 * time.Millisecond
	t.Cleanup(func() { governorTick = old })
	loop := func(ws, _ string) (string, error) { return "", nil } // reads forever, never edits
	fix := func(ws, msg string) (string, error) {
		if !strings.Contains(msg, "stopped for lack of progress") || !strings.Contains(msg, "Do not repeat that approach") {
			return "", context.Canceled
		}
		if err := addReproTest(ws); err != nil {
			return "", err
		}
		return "negated", replaceIn(filepath.Join(ws, consumerFile), "AmountCents: ev.AmountCents, Currency: ev.Currency},\n\t)", "AmountCents: -ev.AmountCents, Currency: ev.Currency},\n\t)")
	}
	rt := &scripted.Runtime{Steps: []scripted.Step{loop, fix}, Hangs: map[int]bool{0: true}}
	r := newRunner(s, root, rt, nil)
	r.Cfg.Agent.Strategy = config.StrategyBudget{MaxDuration: config.Duration(300 * time.Millisecond)}
	tk, err := r.Create(ctx, w, "Fix the unbalanced ledger posting in HandlePaymentCharged", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Run(ctx, tk.ID, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.StatusCompleted || got.VerificationState != task.VerificationTaskVerified {
		t.Fatalf("status=%s verify=%s messages:\n%s", got.Status, got.VerificationState, strings.Join(rt.Messages, "\n---\n"))
	}
	if !r.hasEvent(ctx, tk.ID, "strategy.stopped") {
		t.Fatal("stopped strategy not recorded")
	}
	strats, _ := r.Ledger.Strategies(ctx, tk.ID)
	if len(strats) < 2 || strats[0].Outcome != "rejected" {
		t.Fatalf("strategies = %+v", strats)
	}
}
