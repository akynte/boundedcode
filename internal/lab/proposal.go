package lab

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
)

// ProposalSchema is the version of the proposal format.
const ProposalSchema = 1

// Proposal is a change the laboratory recommends, with its evidence. It is
// only a document: nothing changes until a person approves it (Approve)
// and promotes it (Promote), and a code change is never applied by the
// laboratory at all.
type Proposal struct {
	Schema     int       `json:"schema"`
	ID         string    `json:"id"`
	Created    time.Time `json:"created"`
	Experiment string    `json:"experiment"`
	Hypothesis string    `json:"hypothesis"`
	Arm        string    `json:"arm"`
	Verdict    string    `json:"verdict"`
	// Scope are the languages a workload-specific change is for; empty for
	// every workload.
	Scope []string `json:"scope,omitempty"`
	// Settings is a configuration change; Patch a code change to review
	// and land through the ordinary process.
	Settings config.ExperimentSettings `json:"settings"`
	Pairs    []string                  `json:"pairs,omitempty"`
	Patch    *PatchInfo                `json:"patch,omitempty"`
	Result   ArmResult                 `json:"result"`
	// Evidence pins the files the proposal rests on: the provenance
	// record, the decision and every report.
	Evidence    []FileRef `json:"evidence"`
	Limitations []string  `json:"limitations"`
	Status      string    `json:"status"` // pending-review
}

// Approval is a person's approval of one proposal's exact content.
type Approval struct {
	Proposal FileRef   `json:"proposal"`
	Reviewer string    `json:"reviewer"`
	Time     time.Time `json:"time"`
	Note     string    `json:"note,omitempty"`
}

// Proposals writes a proposal for each promotable arm of a decision into
// dir, and returns their paths.
func Proposals(dir string, e *Experiment, d *Decision, rec *Record, evidence []FileRef) ([]string, error) {
	var out []string
	for _, a := range d.Arms {
		if !Promotable(a.Verdict) || !a.Independent {
			continue
		}
		var arm Arm
		for _, x := range e.Arms {
			if x.Name == a.Arm {
				arm = x
			}
		}
		p := Proposal{Schema: ProposalSchema, ID: e.ID + "-" + a.Arm, Created: time.Now().UTC(), Experiment: e.ID, Hypothesis: strings.TrimSpace(e.Hypothesis),
			Arm: a.Arm, Verdict: a.Verdict, Scope: a.Scope, Settings: arm.Set, Pairs: arm.Set.Pairs(), Result: a, Evidence: evidence, Status: "pending-review"}
		if rec != nil {
			for _, ar := range rec.Arms {
				if ar.Name == a.Arm {
					p.Patch = ar.Patch
				}
			}
		}
		p.Limitations = proposalLimitations(d, a, rec)
		path := filepath.Join(dir, "proposal-"+a.Arm+".json")
		if err := writeJSON(path, p); err != nil {
			return nil, err
		}
		f, err := os.Create(strings.TrimSuffix(path, ".json") + ".md")
		if err != nil {
			return nil, err
		}
		WriteProposalMarkdown(f, &p)
		f.Close()
		out = append(out, path)
	}
	return out, nil
}

func proposalLimitations(d *Decision, a ArmResult, rec *Record) []string {
	out := []string{
		"one machine and one model: the result is for this model and machine, and the intervals treat repeated runs of a task as independent",
		fmt.Sprintf("held-out languages: %s; other languages are not measured", strings.Join(d.HeldoutLanguages, ", ")),
	}
	if a.MinDetectable > 0 {
		out = append(out, fmt.Sprintf("with these runs only a success difference of about %.0f points could show; smaller effects, good or bad, are invisible", 100*a.MinDetectable))
	}
	if rec != nil && len(rec.Repo.Dirty) > 0 {
		out = append(out, fmt.Sprintf("the repository had %d uncommitted files when the experiment ran; no arm's binary contains them", len(rec.Repo.Dirty)))
	}
	if rec == nil {
		out = append(out, "retrospective: decided from recorded runs, not run by the laboratory")
	}
	return out
}

// LoadProposal reads a proposal and pins its content.
func LoadProposal(path string) (*Proposal, FileRef, error) {
	ref, err := pin(path)
	if err != nil {
		return nil, ref, err
	}
	b, _ := os.ReadFile(path)
	var p Proposal
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, ref, fmt.Errorf("%s: %w", path, err)
	}
	if p.Schema != ProposalSchema {
		return nil, ref, fmt.Errorf("%s: schema %d is not %d", path, p.Schema, ProposalSchema)
	}
	return &p, ref, nil
}

// Review checks that a proposal can be approved: a promotable verdict, an
// allowed change, and evidence that is still what it was (dir is the
// proposal's directory, which evidence paths are relative to).
func Review(p *Proposal, dir string) []string {
	var problems []string
	if !Promotable(p.Verdict) {
		problems = append(problems, fmt.Sprintf("the verdict is %s, not accept or workload-specific", p.Verdict))
	}
	if !p.Result.Independent {
		problems = append(problems, "the verdict was not reached on held-out tasks")
	}
	if err := p.Settings.Validate(); err != nil {
		problems = append(problems, err.Error())
	}
	if len(p.Scope) > 0 && p.Patch == nil {
		rest := p.Settings
		rest.Direct = config.Tunables{}
		if !rest.IsZero() {
			problems = append(problems, "a workload-specific proposal can set direct-mode tunables only (direct.workloads holds no other settings)")
		}
	}
	if p.Patch == nil && p.Settings.IsZero() {
		problems = append(problems, "the proposal changes nothing")
	}
	for _, f := range p.Patch.filesOrNil() {
		if why := pathRefusal(f); why != "" {
			problems = append(problems, f+": "+why)
		}
	}
	for _, ev := range p.Evidence {
		now, err := pin(resolve(dir, ev.Path))
		switch {
		case err != nil:
			problems = append(problems, "evidence missing: "+ev.Path)
		case now.SHA256 != ev.SHA256:
			problems = append(problems, "evidence changed since the decision: "+ev.Path)
		}
	}
	return problems
}

func (p *PatchInfo) filesOrNil() []string {
	if p == nil {
		return nil
	}
	return p.Files
}

// Approve records a reviewer's approval of the proposal at path, after
// Review finds nothing wrong.
func Approve(path, reviewer, note string) (string, error) {
	if strings.TrimSpace(reviewer) == "" {
		return "", errors.New("approval needs the reviewer's name")
	}
	p, ref, err := LoadProposal(path)
	if err != nil {
		return "", err
	}
	if problems := Review(p, filepath.Dir(path)); len(problems) > 0 {
		return "", fmt.Errorf("the proposal cannot be approved:\n- %s", strings.Join(problems, "\n- "))
	}
	out := strings.TrimSuffix(path, ".json") + ".approval.json"
	return out, writeJSON(out, Approval{Proposal: ref, Reviewer: reviewer, Time: time.Now().UTC(), Note: note})
}

// Promotion is what promoting a proposal did or would do.
type Promotion struct {
	Proposal string `json:"proposal"`
	// Config is the configuration file changed (empty for a code change).
	Config string `json:"config,omitempty"`
	Backup string `json:"backup,omitempty"`
	// Change describes it; Written says whether the file was written.
	Change  string `json:"change"`
	Written bool   `json:"written"`
}

// Promote applies an approved configuration proposal to the configuration
// file at configPath, when write is set (otherwise it reports what it
// would do). A workload-specific proposal becomes a direct.workloads
// entry; one for every workload sets the keys. A code change is never
// applied: it is returned for the ordinary review.
func Promote(path, configPath string, write bool) (*Promotion, error) {
	p, ref, err := LoadProposal(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(strings.TrimSuffix(path, ".json") + ".approval.json")
	if err != nil {
		return nil, errors.New("not approved: run lab approve first")
	}
	var ap Approval
	if err := json.Unmarshal(b, &ap); err != nil {
		return nil, err
	}
	if ap.Proposal.SHA256 != ref.SHA256 {
		return nil, errors.New("the proposal changed after it was approved: review and approve it again")
	}
	if problems := Review(p, filepath.Dir(path)); len(problems) > 0 {
		return nil, fmt.Errorf("the proposal no longer passes review:\n- %s", strings.Join(problems, "\n- "))
	}
	pr := &Promotion{Proposal: path}
	if p.Patch != nil {
		pr.Change = fmt.Sprintf("a code change (%d files, sha256 %s): the laboratory does not apply code; land the arm's patch through review, with its proposal as the evidence", len(p.Patch.Files), p.Patch.SHA256[:12])
		return pr, nil
	}
	cur, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var next []byte
	if len(p.Scope) > 0 {
		w := config.WorkloadOverride{Languages: p.Scope, Set: p.Settings.Direct, Source: "lab proposal " + p.ID}
		pr.Change = fmt.Sprintf("add direct.workloads entry for %s: %s", strings.Join(p.Scope, ", "), strings.Join(p.Settings.Direct.Pairs(), " "))
		next, err = appendWorkload(cur, w)
	} else {
		pr.Change = "set " + strings.Join(p.Pairs, " ")
		next, err = applySettings(cur, p.Settings)
	}
	if err != nil {
		return nil, err
	}
	// The result must load as a valid configuration.
	tmp := configPath + ".lab-check"
	if err := os.WriteFile(tmp, next, 0o600); err != nil {
		return nil, err
	}
	_, lerr := config.Load(tmp)
	_ = os.Remove(tmp)
	if lerr != nil {
		return nil, fmt.Errorf("the promoted configuration would not load: %w", lerr)
	}
	pr.Config = configPath
	if !write {
		return pr, nil
	}
	if len(cur) > 0 {
		pr.Backup = configPath + ".before-" + p.ID
		if err := os.WriteFile(pr.Backup, cur, 0o600); err != nil {
			return nil, err
		}
	}
	if err := config.WriteFileAtomic(configPath, next, 0o600); err != nil {
		return nil, err
	}
	pr.Written = true
	return pr, nil
}

// WriteProposalMarkdown writes a proposal for a reviewer.
func WriteProposalMarkdown(w io.Writer, p *Proposal) {
	fmt.Fprintf(w, "# Proposal %s\n\nStatus: **%s**. Verdict: **%s**", p.ID, p.Status, p.Verdict)
	if len(p.Scope) > 0 {
		fmt.Fprintf(w, " for %s only", strings.Join(p.Scope, ", "))
	}
	fmt.Fprintf(w, ".\n\nHypothesis: %s\n\n## Change\n\n", p.Hypothesis)
	if p.Patch != nil {
		fmt.Fprintf(w, "A code change to %s (%d lines; sha256 %s). The laboratory never applies code: review the patch and land it as any other change.\n", strings.Join(p.Patch.Files, ", "), p.Patch.Lines, p.Patch.SHA256)
	} else {
		fmt.Fprintf(w, "Configuration: `%s`.\n", strings.Join(p.Pairs, " "))
	}
	fmt.Fprintln(w, "\n## Why")
	for _, r := range p.Result.Reasons {
		fmt.Fprintf(w, "- %s\n", r)
	}
	fmt.Fprintf(w, "- Held-out: %s from %.4g to %.4g (%s)\n", p.Result.Decided.Rule, p.Result.Decided.Baseline, p.Result.Decided.Cand, p.Result.Decided.Why)
	if pr := p.Result.Paired; pr != nil {
		fmt.Fprintf(w, "- Paired over tasks: %s\n", pr.Why)
	}
	for _, s := range sortedKeys(p.Result.VsSimpler) {
		v := p.Result.VsSimpler[s]
		fmt.Fprintf(w, "- Against the simpler %s: %s (%s)\n", s, v.Outcome, v.Why)
	}
	fmt.Fprintln(w, "\n## Limitations")
	for _, l := range p.Limitations {
		fmt.Fprintf(w, "- %s\n", l)
	}
	fmt.Fprintln(w, "\n## Evidence")
	for _, ev := range p.Evidence {
		fmt.Fprintf(w, "- `%s` (sha256 %s)\n", ev.Path, ev.SHA256[:12])
	}
	fmt.Fprintln(w, "\nTo approve, after review: `bcode lab approve PROPOSAL.json --reviewer NAME`; then `bcode lab promote PROPOSAL.json` shows the change, and `--write` makes it (a configuration change only; code is landed by review).")
}
