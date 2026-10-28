// Package retrieve finds the places in a repository that a coding task
// needs, for any language and for polyglot repositories, before the model
// spends turns searching. It does not index: one bounded walk lists the
// files, and the strategies a request calls for (Select) share content
// scans over them. Strategies:
//
//   - stack_trace: frames of a panic, exception or failed test's trace,
//     mapped onto the repository's files (traces print absolute paths from
//     another machine);
//   - build_error: the locations compilers, linters and test runners print;
//   - test_to_impl: a failing or named test, then the code it exercises (by
//     each ecosystem's naming conventions and the names its body uses);
//   - lexical: quoted error messages and configuration keys, literally;
//   - symbols: definitions of the names a request mentions, from a language
//     server where one serves the file's language, definition patterns
//     (internal/lang) everywhere else;
//   - graph: the uses of the main symbol, for renames and refactors;
//   - paths: files and directories the request names;
//   - modules: qualified names (tokio::sync::broadcast, django.db.models,
//     PhpOffice\PhpSpreadsheet\Calculation, com.acme.Orders) mapped onto
//     the files and directories that hold those modules;
//   - discovery: files whose path matches the request's words, when nothing
//     more specific is known;
//   - contracts: HTTP routes and RPC methods in API definitions and handlers;
//   - docs_config: environment variables where they are read, set and
//     documented (configuration keys are lexical);
//   - history: the files of commits the request names.
//
// Every item is ranked by how reliable the strategy that found it is and
// how specific its anchor is (a name found in two files says more than one
// found in two hundred), not by text similarity, and carries its
// provenance: the file, the revision, the content's git blob id and the
// strategies that found it, so a later reader can tell when it went stale.
package retrieve

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/execbox"
	"github.com/akynte/boundedcode/internal/repointel"
)

// Strategy names a retrieval strategy.
type Strategy string

// Strategies (see the package comment).
const (
	StackTrace Strategy = "stack_trace"
	BuildError Strategy = "build_error"
	TestToImpl Strategy = "test_to_impl"
	Lexical    Strategy = "lexical"
	Symbols    Strategy = "symbols"
	Graph      Strategy = "graph"
	Paths      Strategy = "paths"
	Modules    Strategy = "modules"
	Discovery  Strategy = "discovery"
	Contracts  Strategy = "contracts"
	DocsConfig Strategy = "docs_config"
	History    Strategy = "history"
)

// Options bound and configure a retrieval.
type Options struct {
	// Root is the checkout to search.
	Root string
	// Nav answers symbol questions where a language server serves the
	// language (repointel.Composite routes by language); nil uses the
	// definitions the scan finds.
	Nav repointel.Navigator
	// Budget bounds the whole retrieval (default 3 s); MaxFiles the files
	// listed (default 50,000); MaxBytes the bytes a retrieval reads
	// (default 192 MiB); MaxFileBytes one file (default 1 MiB).
	Budget       time.Duration
	MaxFiles     int
	MaxBytes     int64
	MaxFileBytes int64
	// Known are the files already in the model's context, with the blob
	// id of the content it saw: evidence already collected. They are
	// ranked like any other file but marked, so a renderer can skip them.
	Known map[string]string
	// Disable turns strategies off (benchmark ablations).
	Disable map[Strategy]bool
	// Always runs a strategy even when the selection would not (ablations).
	Always map[Strategy]bool
	// Limit is the number of items returned (default 12).
	Limit int
}

func (o *Options) defaults() {
	if o.Budget <= 0 {
		o.Budget = 3 * time.Second
	}
	if o.MaxFiles <= 0 {
		o.MaxFiles = 50000
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 192 << 20
	}
	if o.MaxFileBytes <= 0 {
		o.MaxFileBytes = 1 << 20
	}
	if o.Limit <= 0 {
		o.Limit = 12
	}
}

// Input is what retrieval works from.
type Input struct {
	// Request is the user's message or the task's request.
	Request string
	// Evidence is output collected so far (a failing test run, a build,
	// verification failures). It is untrusted: it only supplies places and
	// names to look up in the repository.
	Evidence []string
}

// Reason is one strategy's finding about a file.
type Reason struct {
	Strategy Strategy `json:"strategy"`
	// Anchor is what was looked up: the frame, the name, the message.
	Anchor string `json:"anchor"`
	Line   int    `json:"line,omitempty"`
	// Weight is the reason's contribution: the strategy's reliability
	// times the anchor's specificity (0-1).
	Weight float64 `json:"weight"`
	// Named is the name from the request's prose this reason answers
	// for (the symbols strategy); the ranking keeps one place for each.
	Named string `json:"-"`
}

// Item is a ranked place.
type Item struct {
	File   string   `json:"file"`
	Line   int      `json:"line,omitempty"`
	Symbol string   `json:"symbol,omitempty"`
	Score  float64  `json:"score"`
	Why    []Reason `json:"why"`
	// Test marks a test file; Known a file already in the model's context
	// with the same content.
	Test  bool       `json:"test,omitempty"`
	Known bool       `json:"known,omitempty"`
	Prov  Provenance `json:"provenance"`
}

// Step reports one strategy of the plan.
type Step struct {
	Strategy Strategy `json:"strategy"`
	// Why the selection chose it (the signal), or why it did not run.
	Why     string  `json:"why"`
	Ran     bool    `json:"ran"`
	Anchors int     `json:"anchors"`
	Found   int     `json:"found"` // files
	Millis  float64 `json:"ms"`
}

// Result is a retrieval's outcome.
type Result struct {
	Intent Intent  `json:"intent"`
	Items  []Item  `json:"items"`
	Steps  []Step  `json:"steps"`
	Files  int     `json:"files"` // files listed
	Read   int     `json:"read"`  // files read by scans
	Bytes  int64   `json:"bytes"` // bytes read by scans
	Millis float64 `json:"ms"`    // wall time
	// ScanMillis is the shared content scan's part of Millis.
	ScanMillis float64 `json:"scan_ms"`
	// Revision is the commit checked out (HEAD) when retrieval ran; ""
	// outside a git repository.
	Revision string `json:"revision,omitempty"`
	// Truncated says why a budget cut the retrieval short ("" if not).
	Truncated string `json:"truncated,omitempty"`
}

// Retrieve selects strategies for the input and runs them.
func Retrieve(ctx context.Context, in Input, opt Options) (Result, error) {
	opt.defaults()
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(ctx, opt.Budget+time.Second)
	defer cancel()
	r, err := openRepo(ctx, opt.Root, &opt)
	if err != nil {
		return Result{}, err
	}
	sig := Extract(in.Request, in.Evidence...)
	rev := gitRevision(ctx, r.root)
	caps := Capabilities{Git: rev != "", Nav: opt.Nav != nil && opt.Nav.Name() != "text"}
	res := Result{Intent: sig.Intent, Files: len(r.files), Revision: rev}
	c := newCollector(r)
	steps := Select(sig, caps, opt)
	x := &runner{ctx: ctx, sig: sig, r: r, c: c, opt: &opt}
	s0 := time.Now()
	x.scan = r.scan(ctx, x.needles(steps), nil)
	res.ScanMillis = float64(time.Since(s0).Microseconds()) / 1000
	for i := range steps {
		st := &steps[i]
		if !st.Ran {
			continue
		}
		s0 := time.Now()
		before := len(c.files)
		st.Anchors = x.run(st.Strategy)
		st.Found = len(c.files) - before
		st.Millis = float64(time.Since(s0).Microseconds()) / 1000
	}
	// Evidence collected so far decides the fallback: when nothing the
	// specific strategies found is reliable, the request's words against
	// file paths are the next best thing.
	if !opt.Disable[Discovery] && !ranDiscovery(steps) && c.best() < fallbackBelow && len(sig.Terms) > 0 {
		s0 := time.Now()
		before := len(c.files)
		st := Step{Strategy: Discovery, Why: "the other strategies found nothing reliable", Ran: true}
		st.Anchors = x.run(Discovery)
		st.Found = len(c.files) - before
		st.Millis = float64(time.Since(s0).Microseconds()) / 1000
		steps = append(steps, st)
	}
	res.Steps = steps
	res.Items = c.rank(sig, opt)
	res.Read, res.Bytes, res.Truncated = r.scanned, r.bytesRead, r.truncated
	stamp(r, res.Items, rev, opt.Known)
	res.Millis = float64(time.Since(t0).Microseconds()) / 1000
	return res, nil
}

// fallbackBelow: a collected reason weaker than this does not locate the
// task by itself (a name used in many files, a word in a path).
const fallbackBelow = 0.5

func ranDiscovery(steps []Step) bool {
	for _, s := range steps {
		if s.Strategy == Discovery && s.Ran {
			return true
		}
	}
	return false
}

// gitRevision is HEAD's commit id, "" outside a git repository.
func gitRevision(ctx context.Context, root string) string {
	c := exec.CommandContext(ctx, "git", append(append([]string{}, execbox.SafeGitArgs...), "rev-parse", "--verify", "-q", "HEAD")...)
	c.Dir = root
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
