package workflow

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
)

// Verification integrity: a judgment site over the diff's own test hunks.
//
// The problem this answers is not "did the tests pass" — recipe.Result
// already says that, from a compiler or a test binary, and nothing here may
// change it (design R1: "a judgment may never accept work, reject work, or
// change what a recipe reports"). The problem is that a green result can be
// produced by weakening the check instead of fixing the code: removing an
// assertion, widening an accepted range, skipping the case. No deterministic
// signal distinguishes that from a legitimate update to an expectation the
// objective asked to change, because both look like "a test file changed and
// the suite is green". That is exactly the kind of one-item-at-a-time,
// literal, no-arithmetic judgment TypeSafe's primitives are built for.
//
// This never fails a build or blocks a phase. It produces findings; a caller
// decides what to do with them, and design.md's tier system decides whether
// the caller is allowed to do anything beyond recording them (see
// judgment.Tier and IntegritySite below).

// IntegritySite names this judgment site in judgment.yaml's `sites` map.
const IntegritySite = "verification_integrity"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            IntegritySite,
		Description:     "judges whether a diff hunk in a test, fixture or golden file weakens what it checks rather than legitimately updating it",
		Mechanism:       "M4",
		Version:         "1",
		MaxEffect:       judgment.TierRouting,
		Redaction:       judgment.RedactRepoText,
		Outcome:         "the gate decision on the change these hunks were part of",
		EffectThreshold: 0,
	})
}

// Hunk is one changed region of one file, as SplitDiff produces it.
type Hunk struct {
	// Path is the file's repository-relative path after the change. For a
	// deleted file it is the path that was removed.
	Path string
	// Header is the "@@ -a,b +c,d @@ ..." line that opens the hunk.
	Header string
	// Body is Header followed by every context, added and removed line up to
	// the next hunk or file header, verbatim. It is what a reader — human or
	// judgment — needs to understand the change; Added and Removed below are
	// a coarser, code-usable view of the same lines.
	Body string
	// Added and Removed are the hunk's changed lines with their +/- markers
	// stripped, in file order. Context lines are in neither.
	Added   []string
	Removed []string
	// IsTest marks a path this package's heuristic treats as a test,
	// fixture, golden file or snapshot — see isTestPath. It is exported so a
	// caller can filter without re-deriving the rule, and it is a heuristic
	// in the same sense firewall.Generated is one: wrong in a bounded and
	// safe direction, because everything downstream of it is advisory.
	IsTest bool
}

// SplitDiff parses a unified diff — the format worktree.Diff already
// produces via `git diff --cached` — into per-hunk chunks.
//
// It reads the `--- a/…` / `+++ b/…` header pair for each file rather than
// the `diff --git` summary line, because that pair is what git uses for a
// rename and is unambiguous about which of the two names is the path this
// hunk now lives at (the `+++` line, unless the file was deleted). A stray or
// malformed diff yields fewer hunks rather than an error: this function feeds
// an advisory judgment, and a diff it cannot fully parse should be read less,
// never treated as a reason to stop.
func SplitDiff(diff string) []Hunk {
	var out []Hunk
	var curPath string
	var cur *Hunk
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			curPath = ""
		case strings.HasPrefix(line, "--- "):
			flush()
			p := strings.TrimPrefix(line, "--- ")
			if p != "/dev/null" {
				curPath = strings.TrimPrefix(p, "a/")
			}
		case strings.HasPrefix(line, "+++ "):
			flush()
			p := strings.TrimPrefix(line, "+++ ")
			if p != "/dev/null" {
				curPath = strings.TrimPrefix(p, "b/")
			}
		case strings.HasPrefix(line, "@@ "):
			flush()
			cur = &Hunk{Path: curPath, Header: line, Body: line, IsTest: isTestPath(curPath)}
		default:
			if cur == nil {
				continue
			}
			cur.Body += "\n" + line
			switch {
			case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
				cur.Added = append(cur.Added, line[1:])
			case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
				cur.Removed = append(cur.Removed, line[1:])
			}
		}
	}
	flush()
	return out
}

// isTestPath is a naming-convention heuristic covering the languages this
// project analyzes plus common fixture and snapshot layouts. It is
// deliberately conservative in what it excludes: a false positive costs one
// extra (cheap, advisory) judgment on an ordinary file; a false negative
// means a weakened check is not looked at twice, which is the status quo
// this mechanism exists to improve on, not a regression from it.
func isTestPath(p string) bool {
	if p == "" {
		return false
	}
	base := path.Base(p)
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, "_test.go"),
		strings.HasSuffix(lower, "_test.py"),
		strings.HasSuffix(lower, "_test.rs"),
		strings.HasSuffix(lower, ".test.ts"), strings.HasSuffix(lower, ".test.tsx"),
		strings.HasSuffix(lower, ".test.js"), strings.HasSuffix(lower, ".test.jsx"),
		strings.HasSuffix(lower, ".spec.ts"), strings.HasSuffix(lower, ".spec.tsx"),
		strings.HasSuffix(lower, ".spec.js"), strings.HasSuffix(lower, ".spec.jsx"),
		strings.HasSuffix(lower, ".golden"), strings.HasSuffix(lower, ".snap"):
		return true
	case strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py"):
		return true
	}
	for _, seg := range strings.Split(strings.ToLower(p), "/") {
		switch seg {
		case "testdata", "fixtures", "fixture", "__snapshots__", "__tests__", "test", "tests":
			return true
		}
	}
	return false
}

// IntegrityTuning bounds one check, the way retrieval.RerankTuning bounds a
// rerank: a struct with a withDefaults so internal/eval can vary it per arm
// without a rebuild, once this site has an arm (design.md §9).
type IntegrityTuning struct {
	// MaxHunks caps how many test hunks one check judges. A diff with more
	// than this many test hunks is unusual, and judging a bounded prefix
	// costs a bounded amount rather than growing with the diff.
	MaxHunks int
	// Budget bounds the whole check. A slow judge is treated as an absent
	// one, matching retrieval.DefaultRerankBudget's rule.
	Budget time.Duration
	// WeakenFloor is the probability at or above which "weakens" is treated
	// as a real signal rather than noise.
	WeakenFloor float64
	// LegitimateCeiling is the probability below which "legitimate" is not
	// treated as explaining the weakening away. A hunk judged high on both
	// questions — a real weakening that is also plausibly intentional — is
	// exactly the ambiguous case a person should read, so it still reports.
	LegitimateCeiling float64
	// BatchSize bounds how many hunks share one request. Each hunk supplies
	// two questions (weakens, legitimate) plus its diff body as a repo_text
	// field, so this is sized to stay well under State's 64 KiB cap even
	// when every hunk is near the 8 KiB per-field ceiling.
	BatchSize int
}

// Defaults for IntegrityTuning.
const (
	DefaultIntegrityMaxHunks          = 40
	DefaultIntegrityBatchSize         = 8
	DefaultIntegrityBudget            = 10 * time.Second
	DefaultIntegrityWeakenFloor       = 0.7
	DefaultIntegrityLegitimateCeiling = 0.5
)

func (t IntegrityTuning) withDefaults() IntegrityTuning {
	if t.MaxHunks <= 0 {
		t.MaxHunks = DefaultIntegrityMaxHunks
	}
	if t.BatchSize <= 0 {
		t.BatchSize = DefaultIntegrityBatchSize
	}
	if t.Budget <= 0 {
		t.Budget = DefaultIntegrityBudget
	}
	if t.WeakenFloor <= 0 {
		t.WeakenFloor = DefaultIntegrityWeakenFloor
	}
	if t.LegitimateCeiling <= 0 {
		t.LegitimateCeiling = DefaultIntegrityLegitimateCeiling
	}
	return t
}

// IntegrityFinding is one test hunk a judgment flagged.
type IntegrityFinding struct {
	Path       string  `json:"path"`
	Header     string  `json:"header"`
	Weakens    float64 `json:"weakens"`
	Legitimate float64 `json:"legitimate_update"`
	Detail     string  `json:"detail"`
}

// IntegrityResult is one check's outcome, reported the way
// retrieval.RerankResult is: Attempted/Applied kept apart from the count of
// candidates and the count actually judged, so a caller — and eventually a
// benchmark — can tell "nothing to check" from "checked and found nothing"
// from "paid for it and got a partial answer".
type IntegrityResult struct {
	// Total is every hunk SplitDiff produced. Candidates is the subset this
	// package judged relevant to ask about (IsTest).
	Total, Candidates  int
	Attempted, Applied bool
	// Tier is the authority the configured judge reported for IntegritySite
	// at the time of the call, recorded so a caller need not read config
	// twice to decide what it may do with Findings.
	Tier judgment.Tier
	// SkipReason names why Attempted is false, or why Candidates was
	// truncated, using the same short, enumerable vocabulary
	// retrieval.RerankResult uses for its SkipReason.
	SkipReason                          string
	Judged                              int
	Findings                            []IntegrityFinding
	Requests, InputTokens, OutputTokens int
	Latency                             time.Duration
}

// CheckTestIntegrity judges every test-classified hunk in hunks against
// objective and reports which ones look like they weaken a check rather than
// legitimately update one.
//
// The error is non-nil when the test hunks could not be judged. A diff with
// no test hunks in it is not that: there is nothing for this site to have an
// opinion about, and nil is returned.
//
// One failed batch fails the whole check, rather than returning the batches
// that did succeed. A finding here is per-hunk, and an unjudged hunk is
// indistinguishable from a clean one to everything downstream — so a partial
// result would report "no concern" about hunks nobody looked at.
func CheckTestIntegrity(ctx context.Context, j judgment.Judge, objective string, hunks []Hunk,
	tuning IntegrityTuning, logf func(string, ...any)) (IntegrityResult, error) {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	tn := tuning.withDefaults()
	res := IntegrityResult{Total: len(hunks), Tier: judgment.SiteTier(j, IntegritySite)}

	var candidates []Hunk
	for _, h := range hunks {
		if h.IsTest {
			candidates = append(candidates, h)
		}
	}
	res.Candidates = len(candidates)

	switch {
	case len(candidates) == 0:
		// Nothing to decide, tested before the judge so a diff that touches
		// no tests never depends on the service being reachable.
		res.SkipReason = "no_test_hunks"
		return res, nil
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res, judgment.Unmet(IntegritySite, judgment.FailureNotConfigured,
			judgment.Unready(j))
	case strings.TrimSpace(objective) == "":
		res.SkipReason = "no_objective"
		return res, nil
	case !judgment.RedactModeOf(j).AtLeast(judgment.RedactRepoText):
		// A diff hunk is repository source, not metadata. Under redact:
		// strict — the default — this site has nothing it is permitted to
		// send. For a site configured at routing that is a contradiction,
		// and Preflight reports it before a task starts rather than letting
		// every task discover it here.
		res.SkipReason = "redact_strict"
		return res, judgment.Unmet(IntegritySite, judgment.FailureRefused,
			"this site needs repo_text and the configured redact mode is stricter")
	}
	if len(candidates) > tn.MaxHunks {
		candidates = candidates[:tn.MaxHunks]
		res.SkipReason = "too_many_hunks"
	}
	res.Attempted = true

	ctx, cancel := context.WithTimeout(ctx, tn.Budget)
	defer cancel()
	started := time.Now()

	var batchErr error
	for start := 0; start < len(candidates) && batchErr == nil; start += tn.BatchSize {
		end := min(start+tn.BatchSize, len(candidates))
		findings, judged, note, err := checkIntegrityBatch(ctx, j, objective, candidates[start:end], tn, logf)
		res.Judged += judged
		res.Findings = append(res.Findings, findings...)
		res.Requests += note.Usage.Requests
		res.InputTokens += note.Usage.InputTokens
		res.OutputTokens += note.Usage.OutputTokens
		batchErr = err
	}
	res.Latency = time.Since(started)
	res.Applied = res.Judged > 0
	sort.Slice(res.Findings, func(i, j int) bool {
		if res.Findings[i].Weakens != res.Findings[j].Weakens {
			return res.Findings[i].Weakens > res.Findings[j].Weakens
		}
		return res.Findings[i].Path < res.Findings[j].Path
	})
	if batchErr != nil {
		res.Applied = false
		return res, batchErr
	}
	return res, nil
}

const integrityCollection = "hunks"

// weakensProposition and legitimateProposition name the hunk by id inside the
// instructions, the same way retrieval's relevanceProposition does — never in
// the criteria and never by the question's own map key, because TypeSafe does
// not send either of those to the model.
func weakensProposition(id string) string {
	return "Considering the diff hunk with id `" + id + "` in `" + integrityCollection + "`, " +
		"against `objective`: does this hunk remove or comment out an assertion or " +
		"expectation, widen what the test accepts (a broader error match, a looser " +
		"comparison, a larger tolerance, a removed expected field), or mark the test " +
		"skipped, disabled, or expected-to-fail? A hunk that only adds a new check, or " +
		"that changes unrelated non-test code, is not this."
}

func legitimateProposition(id string) string {
	return "Considering the diff hunk with id `" + id + "` in `" + integrityCollection + "` " +
		"and `objective`: does this hunk change an expected value specifically to match new " +
		"behaviour that `objective` describes, rather than removing or loosening a check " +
		"that still applies?"
}

// checkIntegrityBatch asks about one batch of hunks in a single request, the
// same shape internal/retrieval's rerankBatch uses: one state, one RepoItem
// per hunk, two questions per hunk naming that hunk's id.
func checkIntegrityBatch(ctx context.Context, j judgment.Judge, objective string, batch []Hunk,
	tn IntegrityTuning, logf func(string, ...any)) ([]IntegrityFinding, int, judgment.Note, error) {

	st := judgment.NewState(judgment.RedactRepoText)
	if err := st.Objective(objective); err != nil {
		logf("test integrity: %v", err)
		return nil, 0, judgment.Note{Source: judgment.SourceError, Detail: err.Error()},
			judgment.Unmet(IntegritySite, judgment.FailureRefused, err.Error())
	}

	items := make([]*judgment.RepoItem, 0, len(batch))
	ids := make([]string, 0, len(batch))
	// used parallels ids: used[n] is the Hunk that produced ids[n]. batch and
	// ids diverge whenever a hunk is skipped below, so an answer must be
	// matched back to its hunk through this rather than through batch's own
	// index.
	used := make([]Hunk, 0, len(batch))
	qs := make(map[string]judgment.Question, len(batch)*2)
	for n, h := range batch {
		id := "h" + strconv.Itoa(n)
		item, err := st.NewRepoItem(id, h.Path)
		if err != nil {
			// Not eligible for egress. Skip this one hunk rather than the
			// whole batch: unlike rerank's ordering, one finding does not
			// depend on another, so there is no units problem in judging
			// some hunks and not others.
			logf("test integrity: %v", err)
			continue
		}
		item.Meta("hunk_header", h.Header)
		if err := item.Text("diff", h.Body, st); err != nil {
			logf("test integrity: %v", err)
			continue
		}
		items = append(items, item)
		ids = append(ids, id)
		used = append(used, h)
		qs["weakens_"+id] = judgment.Noul(weakensProposition(id))
		qs["legitimate_"+id] = judgment.Noul(legitimateProposition(id))
	}
	if len(items) == 0 {
		// The hunks existed and none could be sent — a local refusal, not an
		// absent subject.
		return nil, 0, judgment.Note{Source: judgment.SourceRefused, Detail: "no eligible hunks in batch"},
			judgment.Unmet(IntegritySite, judgment.FailureRefused, "no hunk in this batch was eligible to send")
	}
	if err := st.AddItems(integrityCollection, items); err != nil {
		logf("test integrity: %v", err)
		return nil, 0, judgment.Note{Source: judgment.SourceError, Detail: err.Error()},
			judgment.Unmet(IntegritySite, judgment.FailureRefused, err.Error())
	}

	answers, note, askErr := judgment.RequireAll(ctx, j, IntegritySite, st, qs)
	var findings []IntegrityFinding
	judged := 0
	for n, id := range ids {
		weakens := answers["weakens_"+id]
		if !weakens.Answered {
			continue
		}
		judged++
		legitimate := 0.0
		if a := answers["legitimate_"+id]; a.Answered {
			legitimate = a.Noul
		}
		if weakens.Noul >= tn.WeakenFloor && legitimate < tn.LegitimateCeiling {
			h := used[n]
			findings = append(findings, IntegrityFinding{
				Path: h.Path, Header: h.Header, Weakens: weakens.Noul, Legitimate: legitimate,
				Detail: fmt.Sprintf(
					"a diff hunk in %s may weaken what it checks (p=%.2f); read it before "+
						"relying on this result", h.Path, weakens.Noul),
			})
		}
	}
	return findings, judged, note, askErr
}
