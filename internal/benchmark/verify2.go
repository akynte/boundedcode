package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/verify"
)

// The verification benchmark runs the corpus (VerifyCases) through the
// pipeline as it was (the gate and behavioural evidence) and as it is (the
// test-integrity layer and mutation assessment as well), and measures what
// each accepts: false acceptance (a wrong change labelled task_verified)
// and false rejection (a correct change not labelled task_verified),
// separately, and the time each takes.

// VerifyCaseResult is one case's outcome under each pipeline.
type VerifyCaseResult struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	Kind     string `json:"kind"`
	Correct  bool   `json:"correct"`
	// HiddenPass is the hidden tests' verdict on the change's code; it
	// must agree with Correct (TruthAgrees) for the case to count.
	HiddenPass  bool `json:"hidden_pass"`
	TruthAgrees bool `json:"truth_agrees"`
	// Labels: the old pipeline; the new one (mutation advisory); the new
	// one with mutation required; Strong: task_verified with mutation
	// established and no untested boundary.
	Old      string `json:"old"`
	New      string `json:"new"`
	Required string `json:"required"`
	Strict   string `json:"strict"`
	Strong   bool   `json:"strong"`
	Summary  string `json:"summary"`
	Mutation string `json:"mutation"`
	// Integrity are the test-integrity findings.
	Integrity  []string `json:"integrity,omitempty"`
	Failures   []string `json:"failures,omitempty"`
	GateMS     int64    `json:"gate_ms"`
	EvidenceMS int64    `json:"evidence_ms"`
	MutationMS int64    `json:"mutation_ms"`
	Error      string   `json:"error,omitempty"`
}

// VerifyVariant summarises one pipeline over the cases whose truth holds.
type VerifyVariant struct {
	Name            string  `json:"name"`
	FalseAccept     int     `json:"false_accept"`
	Incorrect       int     `json:"incorrect"`
	FalseReject     int     `json:"false_reject"`
	Correct         int     `json:"correct"`
	FalseAcceptRate float64 `json:"false_accept_rate"`
	FalseRejectRate float64 `json:"false_reject_rate"`
	MeanSeconds     float64 `json:"mean_seconds"`
}

// VerifyReport is a run of the benchmark.
type VerifyReport struct {
	Started  time.Time          `json:"started"`
	Finished time.Time          `json:"finished"`
	Sandbox  string             `json:"sandbox"`
	Results  []VerifyCaseResult `json:"results"`
	Variants []VerifyVariant    `json:"variants"`
}

// VerifyBench runs the corpus.
type VerifyBench struct {
	Engine   *verify.Engine
	WorkDir  string
	Only     []string // case ID prefixes ("" = all)
	Mutation verify.MutationOptions
	Progress func(string)
}

func writeTree(dir string, files map[string]string) error {
	for p, body := range files {
		f := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func commitTree(ctx context.Context, dir string, files map[string]string) (string, error) {
	if err := writeTree(dir, files); err != nil {
		return "", err
	}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=bench", "-c", "user.email=bench@localhost", "commit", "-qm", "base"}} {
		if _, err := gitops.Run(ctx, dir, a...); err != nil {
			return "", err
		}
	}
	return gitops.Run(ctx, dir, "rev-parse", "HEAD")
}

// Run runs every case.
func (b *VerifyBench) Run(ctx context.Context) (*VerifyReport, error) {
	rep := &VerifyReport{Started: time.Now().UTC(), Sandbox: b.Engine.Sandbox.Name()}
cases:
	for _, c := range VerifyCases() {
		if len(b.Only) > 0 && !hasPrefixAny(c.ID, b.Only) {
			continue
		}
		if b.Progress != nil {
			b.Progress("case " + c.ID)
		}
		r := b.runCase(ctx, c)
		rep.Results = append(rep.Results, r)
		if b.Progress != nil {
			b.Progress(fmt.Sprintf("  old=%s new=%s required=%s strong=%v truth=%v (%s) %s", r.Old, r.New, r.Required, r.Strong, r.TruthAgrees, r.Mutation, r.Error))
		}
		// A cancelled run stops after the case in hand; the cases it ran
		// are still a report, so it is returned without an error.
		select {
		case <-ctx.Done():
			break cases
		default:
		}
	}
	rep.Variants = summarise(rep.Results)
	rep.Finished = time.Now().UTC()
	return rep, nil
}

func hasPrefixAny(s string, ps []string) bool {
	for _, p := range ps {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func (b *VerifyBench) runCase(ctx context.Context, c VerifyCase) VerifyCaseResult {
	r := VerifyCaseResult{ID: c.ID, Language: c.Language, Kind: c.Kind, Correct: c.Correct}
	fail := func(err error) VerifyCaseResult { r.Error = err.Error(); return r }
	dir := filepath.Join(b.WorkDir, c.ID)
	_ = os.RemoveAll(dir)
	base, err := commitTree(ctx, dir, c.Base)
	if err != nil {
		return fail(err)
	}
	if err := writeTree(dir, c.Change); err != nil {
		return fail(err)
	}
	target := verify.RepoTarget{Name: c.ID, Worktree: dir, Base: base, TaskID: "verify-bench-" + c.ID, Source: dir}
	t0 := time.Now()
	full, err := b.Engine.Run(ctx, target, verify.Full)
	if err != nil {
		return fail(err)
	}
	r.GateMS = time.Since(t0).Milliseconds()
	for _, f := range full.Integrity {
		r.Integrity = append(r.Integrity, f.Severity+": "+f.String())
	}
	oldPassed := true
	for _, s := range full.Stages {
		if s.Status == "fail" || s.Status == "error" {
			r.Failures = append(r.Failures, s.Name+"["+s.Cause+"]")
			if s.Name != "test-integrity" {
				oldPassed = false
			}
		}
	}
	changed, _ := gitops.ChangedFiles(ctx, dir, base)
	if len(changed) == 0 {
		// The task runner fails an attempt that changed nothing before
		// verification runs; the corpus measures the system as it runs.
		r.Old, r.New, r.Required, r.Strict = verify.LabelFailing, verify.LabelFailing, verify.LabelFailing, verify.LabelFailing
		r.Summary = "the change is empty: the task runner fails it before verification"
		return b.truth(ctx, c, r, dir)
	}
	needed := verify.NeedsEvidence(changed)
	var ev *verify.Evidence
	if oldPassed && needed {
		t1 := time.Now()
		e, err := b.Engine.BehaviourEvidence(ctx, target)
		if err != nil {
			return fail(err)
		}
		ev = &e
		r.EvidenceMS = time.Since(t1).Milliseconds()
	}
	// The pipeline as it was: the gate without the integrity layer, and
	// behavioural evidence.
	switch {
	case !oldPassed:
		r.Old = verify.LabelFailing
	case needed && (ev == nil || !ev.Verified):
		r.Old = verify.LabelTestsGreen
	default:
		r.Old = verify.LabelTaskVerified
	}
	var mut *verify.MutationReport
	if full.Passed && needed {
		t2 := time.Now()
		m, err := b.Engine.Mutation(ctx, target, b.Mutation)
		if err != nil {
			return fail(err)
		}
		mut = &m
		r.MutationMS = time.Since(t2).Milliseconds()
		r.Mutation = fmt.Sprintf("%s %d/%d (+%d invalid, boundary %d/%d)", m.Status, m.Killed, m.Killed+m.Survived, m.Invalid, m.BoundaryKilled, m.Boundary)
	}
	in := verify.LayerInputs{Full: full, EvidenceNeeded: needed, Evidence: ev, Mutation: mut}
	cls := verify.Classify(in)
	r.New, r.Summary = cls.Label, cls.Summary()
	in.Require.Mutation = true
	r.Required = verify.Classify(in).Label
	in.Require.Boundaries = true
	r.Strict = verify.Classify(in).Label
	r.Strong = r.New == verify.LabelTaskVerified && mut != nil && mut.Status == verify.MutationEstablished && mut.BoundarySurvived == 0
	return b.truth(ctx, c, r, dir)
}

// truth runs the hidden tests on the base plus the change's code alone
// (with the base's tests and runner configuration) and records whether
// they agree with the case's label.
func (b *VerifyBench) truth(ctx context.Context, c VerifyCase, r VerifyCaseResult, dir string) VerifyCaseResult {
	fail := func(err error) VerifyCaseResult { r.Error = err.Error(); return r }
	tdir := filepath.Join(b.WorkDir, c.ID+"-truth")
	_ = os.RemoveAll(tdir)
	tbase, err := commitTree(ctx, tdir, c.Base)
	if err != nil {
		return fail(err)
	}
	if err := writeTree(tdir, c.Code); err != nil {
		return fail(err)
	}
	if err := writeTree(tdir, c.Hidden); err != nil {
		return fail(err)
	}
	truth, err := b.Engine.Run(ctx, verify.RepoTarget{Name: c.ID + "-truth", Worktree: tdir, Base: tbase, TaskID: "verify-bench-truth-" + c.ID, Source: tdir}, verify.Full)
	if err != nil {
		return fail(err)
	}
	r.HiddenPass = true
	ran := false
	for _, s := range truth.Stages {
		if verify.StageKind(verify.Stage{Name: s.Name, Run: strings.Fields(s.Command)}) == verify.KindTest {
			ran = ran || s.Status == "pass" || s.Status == "fail"
			if s.Status != "pass" {
				r.HiddenPass = false
			}
		}
	}
	r.HiddenPass = r.HiddenPass && ran
	r.TruthAgrees = r.HiddenPass == c.Correct
	_ = os.RemoveAll(dir)
	_ = os.RemoveAll(tdir)
	return r
}

func summarise(rs []VerifyCaseResult) []VerifyVariant {
	vs := []VerifyVariant{{Name: "old: gate + behavioural evidence"}, {Name: "new: + test integrity, mutation on (advisory, the default)"},
		{Name: "new: mutation required"}, {Name: "new: mutation strict (boundaries killed too)"}}
	secs := make([]float64, len(vs))
	n := 0
	for _, r := range rs {
		if !r.TruthAgrees || r.Error != "" {
			continue
		}
		n++
		accepted := []bool{r.Old == verify.LabelTaskVerified, r.New == verify.LabelTaskVerified, r.Required == verify.LabelTaskVerified, r.Strict == verify.LabelTaskVerified}
		for i := range vs {
			if r.Correct {
				vs[i].Correct++
				if !accepted[i] {
					vs[i].FalseReject++
				}
			} else {
				vs[i].Incorrect++
				if accepted[i] {
					vs[i].FalseAccept++
				}
			}
		}
		old := float64(r.GateMS+r.EvidenceMS) / 1000
		secs[0] += old
		for i := 1; i < len(vs); i++ {
			secs[i] += old + float64(r.MutationMS)/1000
		}
	}
	for i := range vs {
		if vs[i].Incorrect > 0 {
			vs[i].FalseAcceptRate = float64(vs[i].FalseAccept) / float64(vs[i].Incorrect)
		}
		if vs[i].Correct > 0 {
			vs[i].FalseRejectRate = float64(vs[i].FalseReject) / float64(vs[i].Correct)
		}
		if n > 0 {
			vs[i].MeanSeconds = secs[i] / float64(n)
		}
	}
	return vs
}

// MarshalVerify encodes a report.
func MarshalVerify(r *VerifyReport) ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// WriteVerifyMarkdown renders a report.
func WriteVerifyMarkdown(w io.Writer, r *VerifyReport) {
	fmt.Fprintf(w, "# Verification benchmark\n\nSandbox: %s. Started %s, finished %s.\n\n", r.Sandbox, r.Started.Format(time.RFC3339), r.Finished.Format(time.RFC3339))
	bad := 0
	for _, c := range r.Results {
		if !c.TruthAgrees || c.Error != "" {
			bad++
		}
	}
	fmt.Fprintf(w, "%d cases; %d excluded because the hidden tests disagree with the label or the case failed to run.\n\n", len(r.Results), bad)
	fmt.Fprintf(w, "| Pipeline | False acceptance | False rejection | Mean seconds per case |\n|---|---|---|---|\n")
	for _, v := range r.Variants {
		fmt.Fprintf(w, "| %s | %d/%d (%.0f%%) | %d/%d (%.0f%%) | %.1f |\n", v.Name, v.FalseAccept, v.Incorrect, 100*v.FalseAcceptRate,
			v.FalseReject, v.Correct, 100*v.FalseRejectRate, v.MeanSeconds)
	}
	fmt.Fprintf(w, "\n| Case | Correct | Hidden | Old | New | Mutation required | Mutation strict | Mutation | Integrity | s (gate, evidence, mutation) |\n|---|---|---|---|---|---|---|---|---|---|\n")
	sort.SliceStable(r.Results, func(i, j int) bool { return r.Results[i].ID < r.Results[j].ID })
	for _, c := range r.Results {
		note := c.Mutation
		if c.Error != "" {
			note = "error: " + c.Error
		}
		fmt.Fprintf(w, "| %s | %v | %v | %s | %s | %s | %s | %s | %s | %.1f, %.1f, %.1f |\n", c.ID, c.Correct, c.HiddenPass, c.Old, c.New, c.Required, c.Strict,
			note, strings.Join(c.Integrity, "; "), float64(c.GateMS)/1000, float64(c.EvidenceMS)/1000, float64(c.MutationMS)/1000)
	}
}
