package benchmark

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/akynte/boundedcode/internal/direct"
	"github.com/akynte/boundedcode/internal/inference"
)

// RouterCase is a request the router's rules cannot classify with
// confidence, so the model is asked. Want is the mode the request most
// plausibly asks for (the author's judgement, stated with each case).
type RouterCase struct {
	Request, Want, Why string
}

// RouterCases are the requests: questions about how to change something
// (the rules answer them, but not with confidence) and problem statements
// with no verb.
func RouterCases() []RouterCase {
	return []RouterCase{
		{"how do I add a --verbose flag to the CLI?", direct.ModeChat, "asks how"},
		{"what would it take to migrate the store to postgres?", direct.ModeChat, "asks for an assessment"},
		{"where should I add validation for negative amounts?", direct.ModeChat, "asks where"},
		{"why does the build fail after I update the uuid dependency?", direct.ModeChat, "asks why"},
		{"is it safe to remove the legacy handler?", direct.ModeChat, "asks for a judgement"},
		{"how would you refactor the parser to stream its input?", direct.ModeChat, "asks for an approach"},
		{"should we rename Ledger.Post to Ledger.Record?", direct.ModeChat, "asks for an opinion"},
		{"which tests would I need to change to support a second currency?", direct.ModeChat, "asks which"},
		{"Typo in docs/usage.md: 'recieve' should be 'receive'.", direct.ModeCode, "a correction to apply"},
		{"the timeout in client.go should be 30 seconds, not 10", direct.ModeCode, "a correction to apply"},
		{"the CLI's --help text for --out is missing a full stop", direct.ModeCode, "a correction to apply"},
		{"README.md: the install command still says v0.4", direct.ModeCode, "a correction to apply"},
	}
}

// RouterOutcome is one case in one arm.
type RouterOutcome struct {
	Request   string  `json:"request"`
	Arm       string  `json:"arm"`
	Rules     string  `json:"rules_mode"`
	Mode      string  `json:"mode"`
	Want      string  `json:"want"`
	Answered  bool    `json:"answered"` // the model's answer was used (else the rules' choice stood)
	Correct   bool    `json:"correct"`
	Generated int     `json:"generated_tokens"`
	Seconds   float64 `json:"seconds"`
}

// RouterReport is the bench's.
type RouterReport struct {
	Started  time.Time       `json:"started"`
	Model    string          `json:"model"`
	Outcomes []RouterOutcome `json:"outcomes"`
}

// RunRouter classifies each case in each arm ("default": the model's own
// thinking; "off": thinking off), runs times. newGateway returns a fresh
// gateway that keeps its call log.
func RunRouter(ctx context.Context, model string, newGateway func() *inference.Gateway, runs int, progress func(string)) (*RouterReport, error) {
	rep := &RouterReport{Started: time.Now().UTC(), Model: model}
	caps := direct.Capabilities{BoundedAvailable: true}
	for run := 0; run < max(runs, 1); run++ {
		for _, c := range RouterCases() {
			for _, arm := range []string{"default", "off"} {
				if err := ctx.Err(); err != nil {
					return rep, err
				}
				think := ""
				if arm == "off" {
					think = "off"
				}
				gw := newGateway()
				gw.KeepLog = true
				t0 := time.Now()
				d := direct.RouteWithModel(ctx, gw, c.Request, direct.RiskRules{}, caps, think)
				o := RouterOutcome{Request: c.Request, Arm: arm, Rules: direct.Route(c.Request, direct.RiskRules{}, caps).Mode, Mode: d.Mode, Want: c.Want,
					Answered: d.Confidence == "low" && d.Reason != direct.Route(c.Request, direct.RiskRules{}, caps).Reason, Seconds: time.Since(t0).Seconds()}
				o.Correct = o.Mode == o.Want
				for _, cs := range gw.CallLog() {
					o.Generated += cs.Completion
				}
				rep.Outcomes = append(rep.Outcomes, o)
				if progress != nil {
					progress(fmt.Sprintf("%-7s %-6s answered=%v correct=%v %4d tokens %.1fs  %s", arm, o.Mode, o.Answered, o.Correct, o.Generated, o.Seconds, c.Request))
				}
			}
		}
	}
	return rep, nil
}

// WriteRouterMarkdown writes the per-arm summary and the outcomes.
func WriteRouterMarkdown(w io.Writer, r *RouterReport) error {
	fmt.Fprintf(w, "# Router classification\n\nModel `%s`, started %s. %d requests the rules cannot classify with confidence.\n\n", r.Model, r.Started.Format(time.RFC3339), len(RouterCases()))
	fmt.Fprintln(w, "| Thinking | Answered by the model | Matches the label | Same as the rules alone | Generated tokens per request | Seconds per request |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")
	for _, arm := range []string{"default", "off"} {
		var n, ans, ok, rules, gen int
		var secs float64
		for _, o := range r.Outcomes {
			if o.Arm != arm {
				continue
			}
			n++
			if o.Answered {
				ans++
			}
			if o.Correct {
				ok++
			}
			if o.Rules == o.Want {
				rules++
			}
			gen += o.Generated
			secs += o.Seconds
		}
		if n == 0 {
			continue
		}
		fmt.Fprintf(w, "| %s | %d / %d | %d / %d | %d / %d | %.0f | %.2f |\n", arm, ans, n, ok, n, rules, n, float64(gen)/float64(n), secs/float64(n))
	}
	fmt.Fprintln(w, "\n| Request | Label | Rules | Thinking | Mode | Answered | Tokens | s |\n|---|---|---|---|---|---|---|---|")
	for _, o := range r.Outcomes {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %v | %d | %.1f |\n", o.Request, o.Want, o.Rules, o.Arm, o.Mode, o.Answered, o.Generated, o.Seconds)
	}
	return nil
}
