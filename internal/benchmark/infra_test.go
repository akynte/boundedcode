package benchmark

import (
	"bytes"
	"strings"
	"testing"
)

func TestRecommendPrefersLargestCtxThenDecode(t *testing.T) {
	cands := []Candidate{
		{CtxSize: 32768, NCPUMoE: 20, DecodeTPS: 40, PromptTPS: 900, VRAMFreeMiB: 900},
		{CtxSize: 65536, NCPUMoE: 24, DecodeTPS: 30, PromptTPS: 800, VRAMFreeMiB: 600},
		{CtxSize: 65536, NCPUMoE: 22, DecodeTPS: 33, PromptTPS: 700, VRAMFreeMiB: 200},    // too little headroom
		{CtxSize: 65536, NCPUMoE: 26, DecodeTPS: 29.5, PromptTPS: 1000, VRAMFreeMiB: 900}, // near tie, better pp
		{CtxSize: 65536, NCPUMoE: 21, Error: "oom"},
	}
	got := recommend(cands, 400)
	if got == nil || got.NCPUMoE != 26 {
		t.Fatalf("recommend = %+v", got)
	}
	if recommend(nil, 0) != nil {
		t.Fatal("expected nil")
	}
}

func TestFillerDeterministic(t *testing.T) {
	a, b := fillerText(5000, "x"), fillerText(5000, "x")
	if a != b || len(a) != 5000 || fillerText(5000, "y") == a {
		t.Fatal("filler must be deterministic per seed and exact length")
	}
}

func TestMarkdownRenders(t *testing.T) {
	r := &InfraReport{ID: "x", Candidates: []Candidate{{CtxSize: 1, Runs: []PromptRun{{PromptN: 10}}}}}
	r.Recommended = &r.Candidates[0]
	var buf bytes.Buffer
	if err := WriteInfraMarkdown(&buf, r); err != nil || !strings.Contains(buf.String(), "Recommended") {
		t.Fatalf("render: %v\n%s", err, buf.String())
	}
}
