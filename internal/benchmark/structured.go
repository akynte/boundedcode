package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/orchestrator"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/verify"
)

// The structured-output benchmark runs the calls that produce JSON for the
// control plane (the task contract, the ambiguity grounding check and the
// counterexample proposal) on real issues, once with the prompt alone
// asking for the JSON ("prompt") and once with the reply constrained to its
// schema ("schema"), and scores whether the control plane can use each
// reply: the same parsers decide as in a task run.

// Structured-output arms.
const (
	ArmPrompt = "prompt"
	ArmSchema = "schema"
)

// structuredSeed orders the instances; fixed before any run.
const structuredSeed = "boundedcode-structured-2026-10-28:"

// StructuredBench is the benchmark.
type StructuredBench struct {
	Tasks       []RetrievalTask
	Split       string // instances of this split ("" = all)
	PerLanguage int    // instances per language, in seeded order
	Arms        []string
	// NewGateway returns a gateway to the model under test.
	NewGateway func() *inference.Gateway
	Progress   func(string)
}

// StructuredCall is one call's outcome.
type StructuredCall struct {
	Instance   string  `json:"instance"`
	Language   string  `json:"language"`
	Kind       string  `json:"kind"` // contract | grounding | counterexample
	Arm        string  `json:"arm"`
	Usable     bool    `json:"usable"`
	Failure    string  `json:"failure,omitempty"`
	Detail     string  `json:"detail,omitempty"`
	Status     int     `json:"status"`
	WallMS     float64 `json:"wall_ms"`
	Completion int     `json:"completion_tokens"`
	// Counterexamples: test files proposed, and how many are named as test
	// files by the project's language conventions.
	Tests     int `json:"tests,omitempty"`
	TestNamed int `json:"test_named,omitempty"`
	// Contracts: items required and material ambiguities.
	Required int `json:"required,omitempty"`
	Material int `json:"material,omitempty"`
}

// StructuredReport is the run.
type StructuredReport struct {
	Started   time.Time        `json:"started"`
	Seconds   float64          `json:"seconds"`
	Model     string           `json:"model"`
	Provider  string           `json:"provider"`
	Instances []string         `json:"instances"`
	Calls     []StructuredCall `json:"calls"`
}

// Run runs every instance's calls in every arm. The arm that goes first
// alternates between instances, so the server's prompt cache (the arms
// send the same prompt) favours neither.
func (b *StructuredBench) Run(ctx context.Context) (*StructuredReport, error) {
	if len(b.Arms) == 0 {
		b.Arms = []string{ArmPrompt, ArmSchema}
	}
	insts := b.instances()
	if len(insts) == 0 {
		return nil, fmt.Errorf("no instances in split %q", b.Split)
	}
	rep := &StructuredReport{Started: time.Now().UTC()}
	gw := b.NewGateway()
	rep.Model, rep.Provider = gw.Model, gw.Provider()
	for i, t := range insts {
		rep.Instances = append(rep.Instances, t.ID)
		arms := slices.Clone(b.Arms)
		if i%2 == 1 {
			slices.Reverse(arms)
		}
		for _, arm := range arms {
			calls := b.instance(ctx, gw, t, arm)
			rep.Calls = append(rep.Calls, calls...)
			if b.Progress != nil {
				var parts []string
				for _, c := range calls {
					ok := "ok"
					if !c.Usable {
						ok = c.Failure
					}
					parts = append(parts, fmt.Sprintf("%s %s %.0fs", c.Kind, ok, c.WallMS/1000))
				}
				b.Progress(fmt.Sprintf("%d/%d %s %s: %s", i+1, len(insts), t.ID, arm, strings.Join(parts, ", ")))
			}
			if ctx.Err() != nil {
				return rep, ctx.Err()
			}
		}
	}
	rep.Seconds = time.Since(rep.Started).Seconds()
	return rep, nil
}

// instances picks PerLanguage instances of each language in seeded order.
func (b *StructuredBench) instances() []RetrievalTask {
	key := func(id string) string {
		h := sha256.Sum256([]byte(structuredSeed + id))
		return hex.EncodeToString(h[:])
	}
	var pool []RetrievalTask
	for _, t := range b.Tasks {
		if (b.Split == "" || t.Split == b.Split) && strings.TrimSpace(t.Request) != "" && t.Patch != "" {
			pool = append(pool, t)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return key(pool[i].ID) < key(pool[j].ID) })
	per := map[string]int{}
	var out []RetrievalTask
	for _, t := range pool {
		if b.PerLanguage <= 0 || per[t.Language] < b.PerLanguage {
			per[t.Language]++
			out = append(out, t)
		}
	}
	return out
}

// instance runs one instance's calls in one arm: the contract, its
// grounding check when it has material ambiguities, and a counterexample
// proposal against the fix.
func (b *StructuredBench) instance(ctx context.Context, gw *inference.Gateway, t RetrievalTask, arm string) []StructuredCall {
	structured := arm == ArmSchema
	req := task.StripHTMLComments(t.Request)
	var out []StructuredCall

	call := orchestrator.ContractDerivation(req)
	reply, c := b.call(ctx, gw, t, arm, "contract", orchestrator.ReaderBody(call, 1500, 0.2, structured))
	var contract task.Contract
	if c.Failure == "" {
		var err error
		contract, err = task.ParseContract(reply)
		switch {
		case errors.Is(err, task.ErrNothingRequired):
			c.Failure, c.Detail = "nothing_required", ""
		case err != nil:
			c.Failure, c.Detail = parseFailure(reply), trunc1(err.Error(), 200)
		default:
			c.Usable = true
		}
		c.Required, c.Material = len(contract.Required), len(contract.Material())
	}
	out = append(out, c)

	if c.Usable {
		if gcall, idx, ok := orchestrator.GroundingCheck(req, &contract); ok {
			reply, g := b.call(ctx, gw, t, arm, "grounding", orchestrator.ReaderBody(gcall, 1000, 0, structured))
			if g.Failure == "" {
				answers, err := task.ParseGrounding(reply)
				switch {
				case err != nil:
					g.Failure, g.Detail = parseFailure(reply), trunc1(err.Error(), 200)
				default:
					g.Usable = true
					for _, a := range answers {
						if a.ID < 1 || a.ID > len(idx) {
							g.Usable, g.Failure, g.Detail = false, "ids_out_of_range", fmt.Sprintf("id %d of %d", a.ID, len(idx))
							break
						}
					}
				}
			}
			out = append(out, g)
		}
	}

	ccall := orchestrator.CounterexampleProposal(req, t.Patch, "")
	reply, x := b.call(ctx, gw, t, arm, "counterexample", orchestrator.ReaderBody(ccall, 3000, 0.2, structured))
	if x.Failure == "" {
		files, ok := orchestrator.ParseCounterexamples(reply)
		if ok {
			x.Usable, x.Tests = true, len(files)
			for p := range files {
				if verify.IsTestFile(p) {
					x.TestNamed++
				}
			}
		} else {
			x.Failure = parseFailure(reply)
		}
	}
	return append(out, x)
}

// call sends one request and returns the reply text with the call's
// figures; Failure is set when there is no reply to parse.
func (b *StructuredBench) call(ctx context.Context, gw *inference.Gateway, t RetrievalTask, arm, kind string, body map[string]any) (string, StructuredCall) {
	c := StructuredCall{Instance: t.ID, Language: t.Language, Kind: kind, Arm: arm}
	start := time.Now()
	status, resp, err := gw.ForwardFor(ctx, "bench", "/v1/chat/completions", body)
	c.WallMS = float64(time.Since(start)) / float64(time.Millisecond)
	c.Status = status
	if err != nil {
		c.Failure, c.Detail = "error", trunc1(err.Error(), 200)
		return "", c
	}
	raw, _ := json.Marshal(resp)
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if status != 200 || json.Unmarshal(raw, &r) != nil || len(r.Choices) == 0 {
		c.Failure, c.Detail = "http_error", trunc1(string(raw), 200)
		return "", c
	}
	c.Completion = r.Usage.Completion
	if r.Choices[0].FinishReason == "length" {
		c.Detail = "cut at the output limit"
	}
	return r.Choices[0].Message.Content, c
}

// parseFailure classifies a reply the parser could not use.
func parseFailure(reply string) string {
	s := strings.TrimSpace(reply)
	switch {
	case s == "":
		return "empty"
	case !strings.Contains(s, "{"):
		return "no_json"
	}
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if j <= i || !json.Valid([]byte(s[i:j+1])) {
		return "invalid_json"
	}
	return "wrong_shape"
}

func trunc1(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// WriteStructuredMarkdown writes the report's tables.
func WriteStructuredMarkdown(w io.Writer, r *StructuredReport) {
	fmt.Fprintf(w, "# Structured-output benchmark\n\nModel %s (%s); %d instances; %.0f s.\n\n", r.Model, r.Provider, len(r.Instances), r.Seconds)
	type agg struct {
		n, usable, tokens int
		wall              []float64
		fail              map[string]int
		tests, named      int
	}
	kinds := []string{"contract", "grounding", "counterexample"}
	arms := map[string]bool{}
	by := map[string]*agg{}
	for _, c := range r.Calls {
		arms[c.Arm] = true
		k := c.Kind + "|" + c.Arm
		a := by[k]
		if a == nil {
			a = &agg{fail: map[string]int{}}
			by[k] = a
		}
		a.n++
		a.tokens += c.Completion
		a.wall = append(a.wall, c.WallMS)
		a.tests += c.Tests
		a.named += c.TestNamed
		if c.Usable {
			a.usable++
		} else {
			a.fail[c.Failure]++
		}
	}
	armList := []string{ArmPrompt, ArmSchema}
	fmt.Fprintln(w, "| Call | Arm | Usable | Failures | Median s | Mean output tokens | Tests proposed (named as tests) |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|")
	for _, k := range kinds {
		for _, arm := range armList {
			a := by[k+"|"+arm]
			if a == nil {
				continue
			}
			var fails []string
			for f, n := range a.fail {
				fails = append(fails, fmt.Sprintf("%s %d", f, n))
			}
			sort.Strings(fails)
			sort.Float64s(a.wall)
			tests := ""
			if k == "counterexample" {
				tests = fmt.Sprintf("%d (%d)", a.tests, a.named)
			}
			fmt.Fprintf(w, "| %s | %s | %d / %d | %s | %.1f | %d | %s |\n", k, arm, a.usable, a.n, strings.Join(fails, ", "),
				a.wall[len(a.wall)/2]/1000, a.tokens/max(a.n, 1), tests)
		}
	}
	// Paired: the same instance and call in both arms.
	if arms[ArmPrompt] && arms[ArmSchema] {
		type pk struct{ inst, kind string }
		got := map[pk]map[string]bool{}
		for _, c := range r.Calls {
			k := pk{c.Instance, c.Kind}
			if got[k] == nil {
				got[k] = map[string]bool{}
			}
			got[k][c.Arm] = c.Usable
		}
		fmt.Fprintln(w, "\nPaired (the same instance and call in both arms):\n\n| Call | Pairs | Schema usable, prompt not | Prompt usable, schema not | Two-sided sign test p |\n|---|---|---|---|---|")
		for _, k := range kinds {
			pairs, better, worse := 0, 0, 0
			for key, v := range got {
				if key.kind != k {
					continue
				}
				p, okp := v[ArmPrompt]
				s, oks := v[ArmSchema]
				if !okp || !oks {
					continue
				}
				pairs++
				if s && !p {
					better++
				}
				if p && !s {
					worse++
				}
			}
			if pairs > 0 {
				fmt.Fprintf(w, "| %s | %d | %d | %d | %.3f |\n", k, pairs, better, worse, signTest(better, worse))
			}
		}
	}
}
