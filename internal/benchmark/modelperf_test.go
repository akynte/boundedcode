package benchmark

import "testing"

func TestModelPerf(t *testing.T) {
	reps := []*DirectReport{
		{Model: "a", Results: []DirectResult{{Mode: "chat", Success: true, WallSeconds: 10}, {Mode: "code", WallSeconds: 30}, {Mode: "code", Skipped: "no rust"}}},
		{Model: "a", Settings: map[string]any{"routing": "off"}, Results: []DirectResult{{Mode: "code", Success: true, WallSeconds: 50}}},
		{Model: "a", Settings: map[string]any{"routing": "adaptive"}, Results: []DirectResult{{Mode: "code", Success: true, WallSeconds: 1}}},
	}
	reps = append([]*DirectReport{{Model: "a", Results: []DirectResult{{Mode: "chat", WallSeconds: 999}}}}, reps...) // older than ModelPerfRuns
	p := ModelPerf(reps)["a"]
	if all := p[""]; all.Tasks != 3 || all.Passed != 2 || all.Mean != 30 {
		t.Fatalf("all = %+v", all)
	}
	if code := p["code"]; code.Tasks != 2 || code.Passed != 1 || code.Mean != 40 {
		t.Fatalf("code = %+v", code)
	}
}
