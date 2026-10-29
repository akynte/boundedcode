package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/prune"
)

// The pruning benchmark measures how command output is shortened for the
// model, with no agent: each corpus output (real output of real
// toolchains, benchmarks/prune/generate.sh) is fitted to a budget by each
// strategy, and scored on the evidence its case says must survive (the
// failing test, the assertion, the error and its location, the outcome)
// and on the bytes and tokens saved.

// Pruning strategies.
const (
	StrategyClip  = "clip"  // the head and the tail, as tool results were cut before
	StrategyPrune = "prune" // internal/prune
	StrategyModel = "model" // a model picks the lines, given the goal (experimental)
)

// PruneCase is one corpus output.
type PruneCase struct {
	ID       string   `yaml:"id" json:"id"`
	Category string   `yaml:"category" json:"category"`
	Must     []string `yaml:"must" json:"must"`
	// Goal is what the user asked when the output came back (the model
	// strategy is told it); the category's default when empty.
	Goal string `yaml:"goal,omitempty" json:"goal,omitempty"`
	Set  string `yaml:"-" json:"set"`
	text string
}

// LoadPruneCases reads the cases of each set directory (cases.yaml and
// corpus/<id>.txt).
func LoadPruneCases(dirs map[string]string) ([]PruneCase, error) {
	var out []PruneCase
	sets := make([]string, 0, len(dirs))
	for s := range dirs {
		sets = append(sets, s)
	}
	sort.Strings(sets)
	for _, set := range sets {
		dir := dirs[set]
		raw, err := os.ReadFile(filepath.Join(dir, "cases.yaml"))
		if err != nil {
			return nil, err
		}
		var cs []PruneCase
		if err := yaml.Unmarshal(raw, &cs); err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		for _, c := range cs {
			b, err := os.ReadFile(filepath.Join(dir, "corpus", c.ID+".txt"))
			if err != nil {
				return nil, err
			}
			c.Set, c.text = set, string(b)
			out = append(out, c)
		}
	}
	return out, nil
}

// Text is the case's output.
func (c PruneCase) Text() string { return c.text }

// PruneOutcome is one case fitted by one strategy to one budget.
type PruneOutcome struct {
	Case      string   `json:"case"`
	Category  string   `json:"category"`
	Set       string   `json:"set"`
	Strategy  string   `json:"strategy"`
	Budget    int      `json:"budget"`
	Bytes     int      `json:"bytes"`
	OutBytes  int      `json:"out_bytes"`
	Tokens    int      `json:"tokens"`
	OutTokens int      `json:"out_tokens"`
	Must      int      `json:"must"`
	Kept      int      `json:"kept"`
	Missing   []string `json:"missing,omitempty"`
	Millis    float64  `json:"ms"`
	// Model strategy: the call's tokens and time, and a failure.
	PromptTokens int    `json:"prompt_tokens,omitempty"`
	Error        string `json:"error,omitempty"`
}

// PruneReport is a run.
type PruneReport struct {
	Started  time.Time      `json:"started"`
	Budgets  []int          `json:"budgets"`
	Cases    []PruneCase    `json:"cases"`
	Outcomes []PruneOutcome `json:"outcomes"`
	// Model describes the model strategy's setup (model, residency).
	Model map[string]any `json:"model,omitempty"`
}

// ModelPruner picks lines with a model: it returns the shortened text and
// the prompt tokens it spent.
type ModelPruner func(ctx context.Context, goal, text string, budget int) (string, int, error)

// PruneBench is the benchmark.
type PruneBench struct {
	Cases      []PruneCase
	Budgets    []int
	Strategies []string
	Model      ModelPruner
	Progress   func(string)
}

// Run fits every case with every strategy and budget.
func (b *PruneBench) Run(ctx context.Context) (*PruneReport, error) {
	rep := &PruneReport{Started: time.Now().UTC(), Budgets: b.Budgets, Cases: b.Cases}
	for _, c := range b.Cases {
		for _, s := range b.Strategies {
			for _, budget := range b.Budgets {
				if err := ctx.Err(); err != nil {
					return rep, err
				}
				o := PruneOutcome{Case: c.ID, Category: c.Category, Set: c.Set, Strategy: s, Budget: budget,
					Bytes: len(c.text), Tokens: contextplan.EstimateTokens(c.text), Must: len(c.Must)}
				t0 := time.Now()
				var out string
				switch s {
				case StrategyClip:
					out = prune.Clip(c.text, budget)
				case StrategyPrune:
					out = prune.Prune(c.text, prune.Options{Budget: budget}).Text
				case StrategyModel:
					if b.Model == nil {
						return rep, fmt.Errorf("the model strategy needs a model")
					}
					var err error
					out, o.PromptTokens, err = b.Model(ctx, c.goal(), c.text, budget)
					if err != nil {
						o.Error = err.Error()
						out = prune.Clip(c.text, budget)
					}
				default:
					return rep, fmt.Errorf("unknown strategy %q", s)
				}
				o.Millis = float64(time.Since(t0).Microseconds()) / 1000
				o.OutBytes, o.OutTokens = len(out), contextplan.EstimateTokens(out)
				o.Kept, o.Missing = evidence(c.Must, out)
				rep.Outcomes = append(rep.Outcomes, o)
				if b.Progress != nil {
					b.Progress(fmt.Sprintf("%s %s %d: %d/%d evidence, %d → %d bytes, %.1f ms", c.ID, s, budget, o.Kept, o.Must, o.Bytes, o.OutBytes, o.Millis))
				}
			}
		}
	}
	return rep, nil
}

// goal is what the model strategy is told the user wants.
func (c PruneCase) goal() string {
	if c.Goal != "" {
		return c.Goal
	}
	switch c.Category {
	case "test failure":
		return "Find out why the tests fail and fix the code."
	case "compiler":
		return "Make the build pass."
	case "linter":
		return "Fix the problems the linter reports."
	case "stack trace":
		return "Find out why the program crashed and fix it."
	case "dependency resolution":
		return "Install the dependencies and say whether it worked."
	case "search results":
		return "Find where this is used in the code."
	}
	return "Say what the command did and whether it succeeded."
}

// evidence counts the must patterns found in out (line by line).
func evidence(must []string, out string) (int, []string) {
	kept := 0
	var missing []string
	for _, m := range must {
		if regexp.MustCompile("(?m)" + m).MatchString(out) {
			kept++
		} else {
			missing = append(missing, m)
		}
	}
	return kept, missing
}

// WritePruneMarkdown renders the report.
func WritePruneMarkdown(w io.Writer, r *PruneReport) error {
	type key struct {
		set, strategy string
		budget        int
	}
	type agg struct {
		cases, whole, kept, must, bytes, out, tokens, outTokens, prompt, errors int
		ms                                                                      []float64
	}
	sums := map[key]*agg{}
	var keys []key
	for _, o := range r.Outcomes {
		k := key{o.Set, o.Strategy, o.Budget}
		a := sums[k]
		if a == nil {
			a = &agg{}
			sums[k] = a
			keys = append(keys, k)
		}
		a.cases++
		if o.Kept == o.Must {
			a.whole++
		}
		a.kept += o.Kept
		a.must += o.Must
		a.bytes += o.Bytes
		a.out += o.OutBytes
		a.tokens += o.Tokens
		a.outTokens += o.OutTokens
		a.prompt += o.PromptTokens
		a.ms = append(a.ms, o.Millis)
		if o.Error != "" {
			a.errors++
		}
	}
	fmt.Fprintf(w, "# Pruning benchmark\n\n%d cases; budgets %v bytes.\n\n", len(r.Cases), r.Budgets)
	fmt.Fprintln(w, "| Set | Strategy | Budget | Cases with all evidence | Evidence kept | Tokens (estimate) | Reduction | Median ms | Model prompt tokens | Errors |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|")
	for _, k := range keys {
		a := sums[k]
		sort.Float64s(a.ms)
		fmt.Fprintf(w, "| %s | %s | %d | %d / %d | %d / %d | %d → %d | %.0f%% | %.1f | %d | %d |\n", k.set, k.strategy, k.budget, a.whole, a.cases, a.kept, a.must,
			a.tokens, a.outTokens, 100*(1-float64(a.out)/float64(max(a.bytes, 1))), a.ms[len(a.ms)/2], a.prompt, a.errors)
	}
	fmt.Fprintln(w, "\nEvidence lost (case: strategy budget: patterns missing):")
	fmt.Fprintln(w)
	for _, o := range r.Outcomes {
		if len(o.Missing) > 0 {
			fmt.Fprintf(w, "- %s (%s): %s %d: `%s`\n", o.Case, o.Set, o.Strategy, o.Budget, strings.Join(o.Missing, "`, `"))
		}
	}
	return nil
}

// NewModelPruner asks a model, through gw, which lines of the output the
// goal needs (ranges of line numbers, schema-constrained) and keeps those
// lines, marking the gaps. The output is shown with numbered lines, each
// cut to 300 bytes.
func NewModelPruner(gw *inference.Gateway) ModelPruner {
	schema := json.RawMessage(`{"type":"object","properties":{"keep":{"type":"array","items":{"type":"object","properties":{"from":{"type":"integer"},"to":{"type":"integer"}},"required":["from","to"],"additionalProperties":false}}},"required":["keep"],"additionalProperties":false}`)
	return func(ctx context.Context, goal, text string, budget int) (string, int, error) {
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		var b strings.Builder
		for i, l := range lines {
			if len(l) > 300 {
				l = l[:300] + "…"
			}
			fmt.Fprintf(&b, "%d| %s\n", i+1, l)
		}
		body := map[string]any{
			"messages": []map[string]any{
				{"role": "system", "content": "You shorten command output for a coding agent. Keep every line the goal needs: errors, failing tests and their assertions, file locations, stack frames in the project, and the final summary or exit status. Drop progress, downloads, passing tests and repeated lines."},
				{"role": "user", "content": fmt.Sprintf("Goal: %s\n\nThe output has %d lines. Keep at most about %d bytes. Answer with the line ranges to keep.\n\n%s", goal, len(lines), budget, b.String())},
			},
			"max_tokens":      800,
			"temperature":     0,
			"response_format": inference.JSONSchemaFormat("keep_lines", schema),
			// A thinking model spends the reply on reasoning otherwise.
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
		}
		status, resp, err := gw.ForwardFor(ctx, "prune", "/v1/chat/completions", body)
		if err != nil {
			return "", 0, err
		}
		raw, _ := json.Marshal(resp)
		var r struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				Prompt int `json:"prompt_tokens"`
			} `json:"usage"`
		}
		if status != 200 || json.Unmarshal(raw, &r) != nil || len(r.Choices) == 0 {
			return "", 0, fmt.Errorf("model call failed (http %d)", status)
		}
		var keep struct {
			Keep []struct{ From, To int } `json:"keep"`
		}
		if err := json.Unmarshal([]byte(r.Choices[0].Message.Content), &keep); err != nil {
			return "", r.Usage.Prompt, fmt.Errorf("reply is not the schema's JSON: %w", err)
		}
		on := make([]bool, len(lines))
		for _, k := range keep.Keep {
			for i := max(k.From, 1); i <= min(k.To, len(lines)); i++ {
				on[i-1] = true
			}
		}
		var out strings.Builder
		gap := 0
		for i, l := range lines {
			if !on[i] {
				gap++
				continue
			}
			if gap > 0 {
				fmt.Fprintf(&out, "  … [%d lines]\n", gap)
				gap = 0
			}
			out.WriteString(l + "\n")
		}
		if gap > 0 {
			fmt.Fprintf(&out, "  … [%d lines]\n", gap)
		}
		return out.String(), r.Usage.Prompt, nil
	}
}
