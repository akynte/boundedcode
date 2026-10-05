package contextplan

import (
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Retrieval seeds are the strings from a request that the planner searches
// for. Taking every identifier in order of appearance let sample code
// dominate: in the 2026-10 runs packs were filled with `NewRecorder`,
// `NewRequest` and CSS class names `foo`/`bar` copied from issue examples,
// while an exact error message quoted in the same issue, which pointed at
// the code that raised it, was never searched. Seeds are now ranked:
//
//  1. diagnostics: error messages quoted in the request (fixed-string search
//     for their invariant text),
//  2. names discussed in prose (backticks, Func(), CamelCase, file paths),
//  3. configuration keys and flags,
//  4. identifiers that appear only inside code samples,
//
// and names that are placeholders or that match a large share of the
// repository are demoted.

// Seed is one thing to look up.
type Seed struct {
	Text string
	Kind string // diagnostic | name | config | sample
	rank int
	pos  int
}

const (
	maxDiagnostics      = 3
	minDiagnosticWords  = 3
	maxSeedFileMatches  = 25 // a sample name matching more files is too common to help
	frequencyCheckLimit = 10 // names checked against the repository per pack
)

var (
	fenceRE      = regexp.MustCompile("(?s)```[^\\n]*\\n(.*?)```")
	backtickRE   = regexp.MustCompile("`([^`\\n]{1,120})`")
	quotedRE     = regexp.MustCompile(`"([^"\n]{12,200})"`)
	callRE       = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\(\)`)
	camelRE      = regexp.MustCompile(`\b([A-Z][a-z0-9]+(?:[A-Z][a-z0-9]+)+|[a-z]+(?:[A-Z][a-z0-9]+)+)\b`)
	pathRE       = regexp.MustCompile(`\b((?:[\w.-]+/)+[\w.-]+\.[a-z]{1,5})\b`)
	flagRE       = regexp.MustCompile(`(?:^|\s)(--[a-z][a-z0-9-]{2,40})\b`)
	dottedKeyRE  = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z0-9_]+){1,5}$`)
	identOnlyRE  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	urlRE        = regexp.MustCompile(`\b(?:https?|ftp)://\S+`)
	blobURLRE    = regexp.MustCompile(`https?://\S+?/(?:blob|tree)/[^/\s]+/([\w./-]+\.[a-z]{1,5})(?:#L\d+)?\b`)
	mentionRE    = regexp.MustCompile(`(?:^|\s)@[A-Za-z0-9-]+`)
	errorWordsRE = regexp.MustCompile(`(?i)\b(error|errors|panic|failed|failure|cannot|can't|unable|unexpected|invalid|undefined|not found|exception|wrong|denied|refused|missing|illegal|unknown|mismatch|traceback|fatal)\b`)
	// Variable parts of a diagnostic: quoted values, paths, positions, numbers.
	diagVariableRE = regexp.MustCompile(`'[^']*'|"[^"]*"|` + "`[^`]*`" + `|(?:\S*/\S+)|\b\d+(?:[.:]\d+)*\b|0x[0-9a-f]+|[{}()\[\]<>]`)
)

// placeholders are metasyntactic names used in examples, not code.
var placeholders = map[string]bool{"foo": true, "bar": true, "baz": true, "qux": true, "quux": true, "corge": true,
	"grault": true, "garply": true, "waldo": true, "fred": true, "plugh": true, "xyzzy": true, "thud": true,
	"hoge": true, "fuga": true, "piyo": true, "foobar": true}

// Seeds ranks the retrieval seeds of a request (see the package comment).
func Seeds(request string) []Seed {
	var out []Seed
	seen := map[string]bool{}
	add := func(text, kind string, rank, pos int) {
		text = strings.TrimSpace(text)
		if text == "" || seen[text] || len(text) > 200 {
			return
		}
		if kind != "diagnostic" && placeholders[strings.ToLower(lastSegment(text))] {
			return
		}
		seen[text] = true
		out = append(out, Seed{Text: text, Kind: kind, rank: rank, pos: pos})
	}
	// Split the request into code samples and prose.
	var samples []string
	prose := fenceRE.ReplaceAllStringFunc(request, func(m string) string {
		samples = append(samples, fenceRE.FindStringSubmatch(m)[1])
		return "\n"
	})
	// 1. Diagnostics: error-like lines in samples (logs, traces) and quoted
	// error text in prose.
	var diagCandidates []string
	for _, s := range samples {
		diagCandidates = append(diagCandidates, strings.Split(s, "\n")...)
	}
	for _, m := range quotedRE.FindAllStringSubmatch(prose, -1) {
		diagCandidates = append(diagCandidates, m[1])
	}
	for _, m := range backtickRE.FindAllStringSubmatch(prose, -1) {
		if strings.Contains(m[1], " ") {
			diagCandidates = append(diagCandidates, m[1])
		}
	}
	nd := 0
	for i, line := range diagCandidates {
		if nd >= maxDiagnostics {
			break
		}
		if frag := diagnosticFragment(line); frag != "" {
			if !seen[frag] {
				nd++
			}
			add(frag, "diagnostic", 1, i)
		}
	}
	// 2. Names discussed in prose.
	for _, m := range backtickRE.FindAllStringSubmatchIndex(prose, -1) {
		s := prose[m[2]:m[3]]
		switch {
		case dottedKeyRE.MatchString(s) && !strings.Contains(s, "()"):
			add(s, "config", 3, m[0])
		case identOnlyRE.MatchString(strings.TrimSuffix(s, "()")):
			add(strings.TrimSuffix(s, "()"), "name", 2, m[0])
		case pathRE.MatchString(s):
			add(pathRE.FindString(s), "name", 2, m[0])
		}
	}
	// A link to a file in a repository host names that file.
	for _, m := range blobURLRE.FindAllStringSubmatchIndex(prose, -1) {
		add(prose[m[2]:m[3]], "name", 2, m[0])
	}
	// The remaining passes skip backticked spans (handled above), so a
	// qualified name is not searched again by its parts, and URLs and
	// @mentions, whose parts are hashes and user names, not code.
	blank := func(m string) string { return strings.Repeat(" ", len(m)) }
	bare := backtickRE.ReplaceAllStringFunc(prose, blank)
	bare = urlRE.ReplaceAllStringFunc(bare, blank)
	bare = mentionRE.ReplaceAllStringFunc(bare, blank)
	for _, m := range callRE.FindAllStringSubmatchIndex(bare, -1) {
		add(bare[m[2]:m[3]], "name", 2, m[0])
	}
	for _, m := range camelRE.FindAllStringSubmatchIndex(bare, -1) {
		add(bare[m[2]:m[3]], "name", 2, m[0])
	}
	for _, m := range pathRE.FindAllStringSubmatchIndex(bare, -1) {
		if !strings.Contains(bare[m[2]:m[3]], "://") {
			add(bare[m[2]:m[3]], "name", 2, m[0])
		}
	}
	// 3. Flags.
	for _, m := range flagRE.FindAllStringSubmatchIndex(bare, -1) {
		add(bare[m[2]:m[3]], "config", 3, m[0])
	}
	// 4. Identifiers that appear only in code samples.
	for i, s := range samples {
		for _, n := range identifiers(s) {
			add(n, "sample", 4, i*10000)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return out[i].pos < out[j].pos
	})
	return out
}

// diagnosticFragment returns the invariant part of an error-like line: the
// longest run of words between its variable parts (quoted values, paths,
// numbers), if it has at least minDiagnosticWords words. "" if the line is
// not a diagnostic.
func diagnosticFragment(line string) string {
	line = strings.TrimSpace(line)
	if len(line) < 12 || len(line) > 400 || !errorWordsRE.MatchString(line) {
		return ""
	}
	// Drop a leading log prefix ("level=error msg=", "Error:", timestamps).
	if i := strings.LastIndex(line, "msg="); i >= 0 {
		line = line[i+4:]
	}
	// A message that is quoted as a whole (structured logs) is the message.
	if len(line) > 2 && (line[0] == '"' && line[len(line)-1] == '"' || line[0] == '\'' && line[len(line)-1] == '\'') {
		line = line[1 : len(line)-1]
	}
	best := ""
	for _, part := range diagVariableRE.Split(line, -1) {
		for _, seg := range strings.FieldsFunc(part, func(r rune) bool { return r == ',' || r == ';' || r == ':' || r == '=' }) {
			seg = strings.Join(strings.Fields(seg), " ")
			if len(strings.Fields(seg)) >= minDiagnosticWords && len(seg) > len(best) && errorWordsRE.MatchString(seg) {
				best = seg
			}
		}
	}
	if best == "" {
		for _, part := range diagVariableRE.Split(line, -1) {
			seg := strings.Join(strings.Fields(part), " ")
			if len(strings.Fields(seg)) >= minDiagnosticWords+1 && len(seg) > len(best) {
				best = seg
			}
		}
	}
	return best
}

// fileMatches counts files containing name as a whole word under root,
// stopping at limit+1 (ripgrep; -1 if it cannot run).
func fileMatches(ctx context.Context, root, name string, limit int) int {
	rg, err := exec.LookPath(rgBinary)
	if err != nil {
		return -1
	}
	cmd := exec.CommandContext(ctx, rg, "-l", "-w", "--fixed-strings", "--max-filesize", "1M",
		"-g", "!vendor", "-g", "!node_modules", "--", lastSegment(name), ".")
	cmd.Dir = root
	out, _ := cmd.Output() // exit 1 means no match
	n := strings.Count(string(out), "\n")
	if n > limit {
		return limit + 1
	}
	return n
}
