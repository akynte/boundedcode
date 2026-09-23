package native

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/workflow"
)

// What one inspection call is worth remembering across a context boundary.
//
// The conversation is what a continuation throws away, and the conversation is
// where every file the model read was sitting. Replacing it with nothing makes
// re-reading the only available move, which refills the context and reaches
// the next boundary with no more known than before. These bounds are what
// stop the replacement from becoming the same problem: an excerpt is a few
// lines, not a file, and the whole section the summary renders is capped again
// on top of this.
const (
	// maxExcerptBytes is how much verbatim opening one result keeps.
	maxExcerptBytes = 220
	// maxDeclsPerRead is how many declaration names one result keeps.
	maxDeclsPerRead = 8
	// maxEvidenceRecords bounds the cache itself, independently of what any
	// one summary chooses to show. Oldest useful-looking records are dropped
	// first; the summary's own priority rules decide what is rendered.
	maxEvidenceRecords = 120
)

// declPrefixes are the line openings that name a declaration.
//
// A fixed table rather than a parser: this runs on every tool result in the
// loop, it has to work on whatever language the task is in, and being wrong
// about a line costs a slightly worse summary rather than a wrong answer. The
// analyzers do real symbol extraction where a real answer is needed.
var declPrefixes = []string{
	"func ", "type ", "class ", "def ", "interface ", "struct ", "enum ",
	"const ", "var ", "public ", "private ", "protected ", "async def ",
	"export function ", "export class ", "export const ", "impl ", "fn ",
}

// observeRead records what a tool call found, keyed by the same fingerprint
// the loop guard uses so the two never disagree about what "the same call"
// means.
func (p *progress) observeRead(name, arguments, content string) {
	if p.reads == nil {
		p.reads = map[string]*workflow.ReadEvidence{}
	}
	fp := fingerprint(name, arguments)
	p.seq++

	path, detail := describeArgs(arguments)
	trimmed := strings.TrimSpace(content)
	ev := &workflow.ReadEvidence{
		Key:     fp,
		Tool:    name,
		Path:    path,
		Detail:  detail,
		Digest:  digest(content),
		Bytes:   len(content),
		Empty:   trimmed == "",
		Seq:     p.seq,
		Decls:   declarationsIn(trimmed),
		Excerpt: excerptOf(trimmed),
	}
	// A repeat of the same call with the same answer refreshes recency only.
	// Overwriting the record keeps one entry per distinct call rather than
	// one per invocation, which is what makes the cache bounded by the number
	// of distinct things looked at rather than by how long the model looped.
	p.reads[fp] = ev
	p.evictEvidence()
}

// evictEvidence keeps the cache within its bound, dropping least recent first.
func (p *progress) evictEvidence() {
	if len(p.reads) <= maxEvidenceRecords {
		return
	}
	type entry struct {
		fp  string
		seq int
	}
	es := make([]entry, 0, len(p.reads))
	for fp, ev := range p.reads {
		es = append(es, entry{fp, ev.Seq})
	}
	sort.Slice(es, func(i, j int) bool { return es[i].seq < es[j].seq })
	for i := 0; i < len(es)-maxEvidenceRecords; i++ {
		delete(p.reads, es[i].fp)
	}
}

// Reads exports the evidence cache, most recent last, so a continuation can
// be seeded with it and the next one can add to it.
func (p *progress) Reads() []workflow.ReadEvidence {
	out := make([]workflow.ReadEvidence, 0, len(p.reads))
	for _, ev := range p.reads {
		out = append(out, *ev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// seedReads restores a previous phase's evidence, and continues its sequence
// so recency stays meaningful across the boundary.
func (p *progress) seedReads(reads []workflow.ReadEvidence) {
	if len(reads) == 0 {
		return
	}
	if p.reads == nil {
		p.reads = map[string]*workflow.ReadEvidence{}
	}
	for _, ev := range reads {
		if ev.Seq > p.seq {
			p.seq = ev.Seq
		}
		if ev.Key == "" {
			// A record from before this field existed, or from a caller that
			// built one by hand. Dropping it is safer than guessing a key
			// that would not match the next identical call.
			continue
		}
		record := ev
		p.reads[ev.Key] = &record
	}
	p.evictEvidence()
}

// describeArgs pulls the path and the distinguishing detail out of a call's
// arguments.
//
// The detail is what makes two calls on one file different questions: a line
// range, a query. Keeping it means a continuation can say "you read lines
// 1-240 of this" rather than "you read this", which is the difference between
// the model knowing it still needs the rest and the model re-reading all of
// it.
func describeArgs(arguments string) (path, detail string) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return "", clip(strings.TrimSpace(arguments), 60)
	}
	for _, k := range []string{"path", "file", "filename"} {
		if v, ok := parsed[k].(string); ok && v != "" {
			path = v
			break
		}
	}
	var parts []string
	start, hasStart := numberField(parsed, "start", "start_line", "from", "offset")
	end, hasEnd := numberField(parsed, "end", "end_line", "to", "limit")
	switch {
	case hasStart && hasEnd:
		parts = append(parts, fmt.Sprintf("lines %d-%d", start, end))
	case hasStart:
		parts = append(parts, fmt.Sprintf("from line %d", start))
	case hasEnd:
		parts = append(parts, fmt.Sprintf("to line %d", end))
	}
	for _, k := range []string{"query", "pattern", "symbol", "regex"} {
		if v, ok := parsed[k].(string); ok && v != "" {
			parts = append(parts, fmt.Sprintf("%s=%q", k, clip(v, 40)))
			break
		}
	}
	return path, strings.Join(parts, " ")
}

func numberField(parsed map[string]any, keys ...string) (int, bool) {
	for _, k := range keys {
		if v, ok := parsed[k].(float64); ok {
			return int(v), true
		}
	}
	return 0, false
}

// declarationsIn names what a result declares, by a fixed scan.
func declarationsIn(content string) []string {
	if content == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		for _, p := range declPrefixes {
			if !strings.HasPrefix(t, p) {
				continue
			}
			name := clip(t, 72)
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
			break
		}
		if len(out) >= maxDeclsPerRead {
			break
		}
	}
	return out
}

// excerptOf keeps a short verbatim opening, so the model sees what kind of
// thing this was rather than only that it was looked at.
func excerptOf(content string) string {
	if content == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(" / ")
		}
		b.WriteString(strings.TrimSpace(line))
		if b.Len() >= maxExcerptBytes {
			break
		}
	}
	return clip(b.String(), maxExcerptBytes)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
