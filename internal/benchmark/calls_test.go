package benchmark

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/inference"
)

func TestSummarizeCalls(t *testing.T) {
	log := []inference.CallStat{
		{Prompt: 1000, Cached: 0, TotalMS: 2000, PrefillMS: 1500, DecodeMS: 400, TTFTMS: 1600, Status: "ok", ReasoningText: "let me think"},
		{Prompt: 1400, Cached: 990, TotalMS: 1000, TTFTMS: 300, Status: "ok", Reasoning: 40, ReasoningSource: "provider"},
		{Prompt: 1800, Cached: 200, TotalMS: 3000, TTFTMS: 2500, Status: "ok"}, // the cache was lost
		{Prompt: 900, Cached: 100, TotalMS: 1000, TTFTMS: 900, Status: "ok"},   // history rewritten before this call
		{Prompt: 0, TotalMS: 5, Status: "transport_error"},
	}
	tok := func(_ context.Context, s string) (int, error) { return len(strings.Fields(s)), nil }
	countReasoning(context.Background(), log, tok)
	if log[0].Reasoning != 3 || log[0].ReasoningSource != "tokenizer" || log[1].Reasoning != 40 {
		t.Fatalf("reasoning: %+v %+v", log[0], log[1])
	}
	s := summarizeCalls(log, map[int]bool{3: true})
	if s.Calls != 5 || s.Failed != 1 || s.ReasoningTokens != 43 || s.ReasoningCount != "mixed" {
		t.Fatalf("calls: %+v", s)
	}
	if s.Invalidations != 2 || s.AfterRewrite != 1 || s.RecomputedTokens != (1400-200)+(900-100) {
		t.Fatalf("invalidations: %+v", s)
	}
	if s.InferenceSeconds != 7.005 || s.PrefillSeconds != 1.5 || s.PeakCall != 2 {
		t.Fatalf("times: %+v", s)
	}
	if s.TTFTMedianMS != 1600 || s.TTFTMaxMS != 2500 {
		t.Fatalf("ttft: %+v", s)
	}

	// Without a tokenizer, reasoning is an estimate and says so.
	est := []inference.CallStat{{Status: "ok", ReasoningText: strings.Repeat("x", 320)}}
	countReasoning(context.Background(), est, nil)
	if est[0].Reasoning != 100 || est[0].ReasoningSource != "estimate" {
		t.Fatalf("estimate: %+v", est[0])
	}
}

func TestBreakdown(t *testing.T) {
	secs := map[string]string{"system": "a b c", "tool:read": "d e"}
	tok := func(_ context.Context, s string) (int, error) { return len(strings.Fields(s)), nil }
	b := breakdown(context.Background(), 4, 9, secs, tok)
	if b.Counting != "tokenizer" || b.Sections["system"].Tokens != 3 || b.Sections["tool:read"].Tokens != 2 || b.Framing != 4 || b.Call != 4 {
		t.Fatalf("tokenizer: %+v", b)
	}
	// A tokenizer that fails part-way: every section is estimated.
	n := 0
	bad := func(_ context.Context, s string) (int, error) {
		if n++; n > 1 {
			return 0, errors.New("down")
		}
		return 1, nil
	}
	if b := breakdown(context.Background(), 0, 9, secs, bad); b.Counting != "estimate" || b.Sections["system"].Tokens != 2 {
		t.Fatalf("fallback: %+v", b)
	}
}

func TestParseMiB(t *testing.T) {
	for in, want := range map[string]float64{"512MiB": 512, "1.5GiB": 1536, "2048KiB": 2, "bad": 0} {
		if got := parseMiB(in); got != want {
			t.Errorf("%s: %v", in, got)
		}
	}
	if rssMiB("/proc/self/status") <= 0 && strings.HasPrefix(runtimeOS(), "linux") {
		t.Error("own RSS not read")
	}
}

func runtimeOS() string { return goruntime.GOOS }

// In bounded mode, a change of caller and the condenser's calls are where
// the cache is expected to miss.
func TestExpectedMisses(t *testing.T) {
	got := expectedMisses([]string{"planner", "agent", "agent", "agent:condenser", "agent", "agent"})
	for i, want := range []bool{false, true, false, true, true, false} {
		if got[i] != want {
			t.Errorf("call %d: %v", i, got[i])
		}
	}
}
