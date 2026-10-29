package benchmark

import "github.com/akynte/boundedcode/internal/routing"

// ModelPerfRuns is how many of a model's latest runs ModelPerf counts:
// older runs were made with other builds, so models are compared on runs
// made close together.
const ModelPerfRuns = 2

// ModelPerf is each model's measured results in the direct-mode suite, by
// the mode its requests were routed to ("chat", "code") and over all (""),
// from its ModelPerfRuns latest runs (reps are oldest first). Only runs
// with the default settings are counted (routing and speculative decoding
// off), so models are compared on equal terms; skipped tasks are not
// counted.
func ModelPerf(reps []*DirectReport) map[string]map[string]routing.Perf {
	out := map[string]map[string]routing.Perf{}
	runs := map[string]int{}
	for i := len(reps) - 1; i >= 0; i-- {
		rep := reps[i]
		if !baselineSettings(rep.Settings) || runs[rep.Model] >= ModelPerfRuns {
			continue
		}
		runs[rep.Model]++
		m := out[rep.Model]
		if m == nil {
			m = map[string]routing.Perf{}
			out[rep.Model] = m
		}
		for _, r := range rep.Results {
			if r.Skipped != "" {
				continue
			}
			for _, key := range []string{"", r.Mode} {
				p := m[key]
				p.Mean = (p.Mean*float64(p.Tasks) + r.WallSeconds) / float64(p.Tasks+1)
				p.Tasks++
				if r.Success {
					p.Passed++
				}
				m[key] = p
			}
		}
	}
	return out
}

func baselineSettings(s map[string]any) bool {
	for k, off := range map[string]string{"routing": "off", "speculative": "off"} {
		if v, ok := s[k]; ok && v != off {
			return false
		}
	}
	return true
}
