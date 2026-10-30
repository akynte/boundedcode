// Package lab is the harness optimization laboratory: it reads what the
// agent did in benchmark runs (reports and session transcripts), finds
// recurring failure patterns, turns them into hypotheses, runs controlled
// experiments in isolated homes and worktrees, decides them against fixed
// baselines on held-out tasks, and writes proposals that change nothing
// until a person approves and promotes them.
//
// It never edits the live system on its own, and what it can vary is
// limited by construction: configuration candidates are
// config.ExperimentSettings (no security setting has a field there), and
// code candidates are patches confined to an allowlist of paths (protect.go).
package lab

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/benchmark"
)

// Run is one task run as the laboratory sees it: the report's figures and,
// when its session transcript was found, every tool call.
type Run struct {
	Report    string   `json:"report"` // report file
	ReportID  string   `json:"report_id"`
	Suite     string   `json:"suite"` // direct or bounded
	Task      string   `json:"task"`
	Category  string   `json:"category,omitempty"`
	Group     string   `json:"group,omitempty"`
	Split     string   `json:"split,omitempty"`
	Languages []string `json:"languages,omitempty"`
	Success   bool     `json:"success"`
	Skipped   bool     `json:"skipped,omitempty"`
	// Figures from the report.
	Turns, ModelCalls, ToolCalls int
	PromptTokens, CachedTokens   int
	GeneratedTokens, PeakPrompt  int
	ResultBytes                  int
	Compactions, Recalls         int
	RedundantReads               int
	WallSeconds                  float64
	FilesChanged                 []string
	FailedChecks                 []string
	Answer                       string
	Error                        string
	MaxTurns                     int // the run's turn limit (0: unknown)
	ReadOnly                     bool
	// Bounded mode.
	Attempts, Escalations int
	SelfVerified          bool
	FailureClass          string
	// Transcript is the session file; TranscriptRef the same relative to
	// the sessions directory it was found in ("<sessions>/…"), for records
	// that leave this machine. Steps are its tool calls in order.
	Transcript    string `json:"-"`
	TranscriptRef string `json:"transcript,omitempty"`
	Steps         []Step `json:"-"`
	// Compacted are the step indexes at which the history was compacted.
	Compacted []int `json:"-"`
	// Targets are files the task expects the agent to find or change
	// (from the task definition), when it says.
	Targets []string `json:"-"`
}

// Step is one tool call and its result.
type Step struct {
	Turn   int            // assistant message number
	Tool   string         // tool name
	Args   map[string]any // decoded arguments
	Key    string         // tool and canonical arguments: equal keys are the same call
	Error  string         // the result when it reports an error, shortened
	Bytes  int            // result size
	Paths  []string       // files the call names
	Mutate bool           // the call can change files
}

// Ref names a run for evidence: report, task and session.
func (r *Run) Ref() string {
	ref := fmt.Sprintf("%s:%s", filepath.Base(r.Report), r.Task)
	if r.TranscriptRef != "" {
		ref += " (" + r.TranscriptRef + ")"
	}
	return ref
}

// Tokens is the run's processed tokens: uncached prompt and generated.
func (r *Run) Tokens() int { return r.PromptTokens - r.CachedTokens + r.GeneratedTokens }

// Changes reports whether the run was a change task (not read-only).
func (r *Run) Changes() bool {
	return !r.ReadOnly && r.Category != "explain" && r.Category != "locate" && r.Category != "locate-heldout"
}

// Load reads reports (direct or bounded) into runs, joining each direct run
// with its transcript found under sessionRoots (directories holding
// <task>-<n>/state/sessions/<repo>/<session>.jsonl, as bench direct leaves
// them). tasks supplies definitions by id, for turn limits and targets
// (nil: none).
func Load(reports []string, sessionRoots []string, tasks map[string]benchmark.TaskSpec) ([]*Run, error) {
	index, refs := map[string]string{}, map[string]string{}
	for _, root := range sessionRoots {
		indexTranscripts(root, index, refs)
	}
	var out []*Run
	for _, p := range reports {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var probe struct {
			Summary  map[string]any `json:"summary"`
			Variants any            `json:"variants"`
		}
		if err := json.Unmarshal(b, &probe); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		switch {
		case probe.Variants != nil:
			continue // a verification corpus run: no agent ran
		case probe.Summary["self_verified_but_hidden_failed"] != nil:
			var rep benchmark.SuiteReport
			if err := json.Unmarshal(b, &rep); err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			for _, t := range rep.Results {
				out = append(out, boundedRun(p, rep.ID, t, tasks))
			}
		default:
			var rep benchmark.DirectReport
			if err := json.Unmarshal(b, &rep); err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			maxTurns := 0
			if v, ok := rep.Settings["max_turns"].(float64); ok {
				maxTurns = int(v)
			}
			for _, t := range rep.Results {
				r := directRun(p, rep.ID, t, tasks, maxTurns)
				if path := index[t.SessionID]; t.SessionID != "" && path != "" {
					r.Transcript, r.TranscriptRef = path, refs[t.SessionID]
					if err := readTranscript(r); err != nil {
						return nil, fmt.Errorf("%s: %w", path, err)
					}
				}
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func directRun(path, id string, t benchmark.DirectResult, tasks map[string]benchmark.TaskSpec, maxTurns int) *Run {
	r := &Run{Report: path, ReportID: id, Suite: "direct", Task: t.ID, Category: t.Category, Group: t.Group, Split: t.Split, Languages: t.Languages,
		Success: t.Success, Skipped: t.Skipped != "", Turns: t.Turns, ModelCalls: t.Calls, ToolCalls: t.ToolCalls, PromptTokens: t.PromptTokens,
		CachedTokens: t.CachedTokens, GeneratedTokens: t.GeneratedTokens, PeakPrompt: t.PeakPrompt, ResultBytes: t.ResultBytes,
		Compactions: t.Compactions, Recalls: t.Recalls, RedundantReads: t.RedundantReads, WallSeconds: t.WallSeconds,
		FilesChanged: t.FilesChanged, FailedChecks: t.FailedChecks, Answer: t.Answer, Error: t.Error, MaxTurns: maxTurns}
	if len(r.Languages) == 0 && t.Language != "" && t.Language != "multi" {
		r.Languages = []string{t.Language}
	}
	fillFromSpec(r, tasks)
	if len(r.Languages) == 0 && t.Language != "" {
		r.Languages = []string{t.Language}
	}
	return r
}

func boundedRun(path, id string, t benchmark.TaskResult, tasks map[string]benchmark.TaskSpec) *Run {
	r := &Run{Report: path, ReportID: id, Suite: "bounded", Task: t.ID, Category: t.Category, Group: t.Group, Split: t.Split, Languages: t.Languages,
		Success: t.Success, Skipped: t.Skipped != "", ModelCalls: t.Calls.Calls, PromptTokens: t.LocalTokens + t.CachedTokens, CachedTokens: t.CachedTokens,
		GeneratedTokens: 0, WallSeconds: t.WallSeconds, FailedChecks: t.FailedChecks, Error: t.Error,
		Attempts: t.Attempts, Escalations: t.Escalations, SelfVerified: t.SelfVerified, FailureClass: t.FailureClass}
	r.ToolCalls = t.Intel.AgentToolCalls
	if len(r.Languages) == 0 && t.Language != "" && t.Language != "multi" {
		r.Languages = []string{t.Language}
	}
	fillFromSpec(r, tasks)
	if len(r.Languages) == 0 && t.Language != "" {
		r.Languages = []string{t.Language}
	}
	return r
}

// fillFromSpec takes what the report lacks from the task definition.
func fillFromSpec(r *Run, tasks map[string]benchmark.TaskSpec) {
	spec, ok := tasks[r.Task]
	if !ok {
		return
	}
	if r.Group == "" {
		r.Group = spec.Group
	}
	if r.Split == "" {
		r.Split = spec.Split
	}
	if len(r.Languages) == 0 {
		r.Languages = spec.Languages
		if len(r.Languages) == 0 && spec.Language != "" && spec.Language != "multi" {
			r.Languages = []string{spec.Language}
		}
	}
	r.ReadOnly = spec.ReadOnly
	if spec.MaxTurns > 0 {
		r.MaxTurns = spec.MaxTurns
	}
	for _, e := range spec.Reference {
		r.Targets = appendUnique(r.Targets, e.File)
	}
	for _, x := range spec.Expect {
		// Locate tasks expect a path pattern such as (?:src/jv\.c).
		if p := pathFromPattern(x); p != "" {
			r.Targets = appendUnique(r.Targets, p)
		}
	}
}

var patternPath = regexp.MustCompile(`^\(\?:([\w./\\-]+)\)$`)

// pathFromPattern turns an expected-answer pattern that is one escaped path
// back into the path ("" when it is anything else).
func pathFromPattern(x string) string {
	m := patternPath.FindStringSubmatch(x)
	if m == nil || !strings.Contains(m[1], "/") && !strings.Contains(m[1], `\.`) {
		return ""
	}
	return strings.ReplaceAll(m[1], `\`, "")
}

// indexTranscripts maps session ids to transcript paths under root, and
// to the same paths relative to root.
func indexTranscripts(root string, index, refs map[string]string) {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, d.Name(), "state", "sessions", "*", "*.jsonl"))
		for _, m := range matches {
			id := strings.TrimSuffix(filepath.Base(m), ".jsonl")
			index[id] = m
			if rel, err := filepath.Rel(root, m); err == nil {
				refs[id] = "<sessions>/" + filepath.ToSlash(rel)
			}
		}
	}
}

type transcriptLine struct {
	Type    string `json:"type"`
	Message struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
		ToolCallID string `json:"tool_call_id"`
	} `json:"message"`
}

var mutatingTools = map[string]bool{"write": true, "edit": true, "multi_edit": true, "notebook_edit": true, "replace_symbol": true}

// readTranscript fills r.Steps and r.Compacted from r.Transcript.
func readTranscript(r *Run) error {
	f, err := os.Open(r.Transcript)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	byID := map[string]int{}
	turn := 0
	for sc.Scan() {
		var l transcriptLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch l.Type {
		case "compact":
			r.Compacted = append(r.Compacted, len(r.Steps))
		case "message":
			m := l.Message
			switch m.Role {
			case "assistant":
				turn++
				for _, c := range m.ToolCalls {
					s := Step{Turn: turn, Tool: c.Function.Name, Args: map[string]any{}}
					_ = json.Unmarshal([]byte(c.Function.Arguments), &s.Args)
					s.Key = s.Tool + " " + canonical(s.Args)
					s.Paths = argPaths(s.Args)
					s.Mutate = mutatingTools[s.Tool]
					byID[c.ID] = len(r.Steps)
					r.Steps = append(r.Steps, s)
				}
			case "tool":
				i, ok := byID[m.ToolCallID]
				if !ok {
					continue
				}
				r.Steps[i].Bytes = len(m.Content)
				if e := resultError(m.Content); e != "" {
					r.Steps[i].Error = e
				}
			}
		}
	}
	return sc.Err()
}

// canonical renders arguments with sorted keys (json.Marshal sorts map
// keys), so equal calls have equal keys.
func canonical(a map[string]any) string {
	b, _ := json.Marshal(a)
	return string(b)
}

func argPaths(a map[string]any) []string {
	var out []string
	add := func(v any) {
		if s, ok := v.(string); ok && s != "" {
			out = appendUnique(out, filepath.ToSlash(strings.TrimPrefix(s, "./")))
		}
	}
	add(a["path"])
	if ps, ok := a["paths"].([]any); ok {
		for _, p := range ps {
			add(p)
		}
	}
	if es, ok := a["edits"].([]any); ok {
		for _, e := range es {
			if m, ok := e.(map[string]any); ok {
				add(m["path"])
			}
		}
	}
	return out
}

// resultError returns a tool result's error line, shortened, or "": the
// runner reports a failure as "error: …", alone or after the output.
func resultError(content string) string {
	c := strings.TrimSpace(content)
	var line string
	switch {
	case strings.HasPrefix(c, "error: "):
		line = c
	case strings.HasPrefix(c, "The user did not approve"), strings.HasPrefix(c, "Plan mode is on"):
		line = c
	default:
		i := strings.LastIndex(c, "\nerror: ")
		if i < 0 {
			return ""
		}
		line = c[i+1:]
	}
	line, _, _ = strings.Cut(line, "\n")
	if len(line) > 160 {
		line = line[:160] + "…"
	}
	return line
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// TaskIndex loads task definitions from dirs by id.
func TaskIndex(dirs []string) (map[string]benchmark.TaskSpec, error) {
	out := map[string]benchmark.TaskSpec{}
	for _, d := range dirs {
		ts, err := benchmark.LoadTasks(d)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			out[t.ID] = t
		}
	}
	return out, nil
}

// sortedKeys returns m's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
