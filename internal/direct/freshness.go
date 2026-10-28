package direct

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/retrieve"
)

// What the model was shown can go stale: the agent's own commands rewrite
// files, the user edits them, a branch switch replaces them, a session is
// resumed days later. Each tool result that shows or writes a file is
// stamped with the git blob id of the file's content then (stampsFor);
// each user message records the checkout (head). Before a message is
// sent, freshnessNote names the files whose content differs from what the
// conversation last saw and says when the checkout moved; recall says when
// the result it brings back is stale. Retrieval treats a file read earlier
// and unchanged as evidence already collected (retrievalPack).

// FileStamp is a file as a tool result showed or wrote it.
type FileStamp struct {
	Path string `json:"path"` // relative to the root, slash-separated
	Blob string `json:"blob"` // git blob id of the content
	// Op is "read" (the result shows the content) or "write" (the tool
	// changed the file; the result shows only part of it).
	Op string `json:"op"`
}

// RetrievalRecord is what retrieval put in front of the model for a
// message, with each item's provenance.
type RetrievalRecord struct {
	Revision   string          `json:"revision,omitempty"`
	Strategies []string        `json:"strategies,omitempty"`
	Items      []RetrievedItem `json:"items,omitempty"`
	Millis     float64         `json:"ms"`
	Truncated  string          `json:"truncated,omitempty"`
}

// RetrievedItem is one place retrieval listed.
type RetrievedItem struct {
	File    string   `json:"file"`
	Line    int      `json:"line,omitempty"`
	Symbol  string   `json:"symbol,omitempty"`
	Blob    string   `json:"blob,omitempty"`
	Methods []string `json:"methods"`
	Known   bool     `json:"known,omitempty"`
}

var readManyHeader = func(line string) (string, bool) {
	if strings.HasPrefix(line, "==> ") && strings.HasSuffix(line, " <==") {
		return strings.TrimSuffix(strings.TrimPrefix(line, "==> "), " <=="), true
	}
	return "", false
}

// stampsFor stamps the files a tool result shows or wrote. It also counts
// a read of a file region the conversation already shows, unchanged
// (RedundantReads).
func (r *Runner) stampsFor(call ToolCall, result string) []FileStamp {
	if strings.HasPrefix(result, "error:") || strings.HasPrefix(result, "not run:") {
		return nil
	}
	a := callArgs(call)
	var paths []string
	op := "read"
	switch call.Function.Name {
	case "read", "notebook_read":
		paths = []string{str(a, "path")}
	case "read_many":
		for _, l := range strings.Split(result, "\n") {
			if p, ok := readManyHeader(l); ok {
				paths = append(paths, p)
			}
		}
	case "edit", "multi_edit", "write", "notebook_edit":
		paths, op = []string{str(a, "path")}, "write"
	default:
		return nil
	}
	var out []FileStamp
	for _, p := range paths {
		abs, rel, err := r.resolve(p)
		if err != nil || r.checkReadable(abs) != nil {
			continue
		}
		blob := retrieve.FileBlob(r.Opts.Root, rel)
		if blob == "" {
			continue
		}
		out = append(out, FileStamp{Path: rel, Blob: blob, Op: op})
	}
	if call.Function.Name == "read" && len(out) == 1 {
		key := out[0].Path + "\x00" + fmt.Sprint(a["offset"]) + "\x00" + fmt.Sprint(a["limit"])
		r.readsMu.Lock()
		if r.lastRead == nil {
			r.lastRead = map[string]readMark{}
		}
		if prev, ok := r.lastRead[key]; ok && prev.blob == out[0].Blob && r.verbatim(prev.id) {
			r.redundantReads++
		}
		r.lastRead[key] = readMark{blob: out[0].Blob, id: call.ID}
		r.readsMu.Unlock()
	}
	return out
}

type readMark struct {
	blob string
	id   string // the tool call whose result shows it
}

// verbatim: the result of call id is in the history as sent (not set
// aside, not summarised).
func (r *Runner) verbatim(id string) bool {
	for i := len(r.Session.Messages) - 1; i >= 0; i-- {
		m := &r.Session.Messages[i]
		if m.Role == "tool" && m.ToolCallID == id {
			return m.Elided == ""
		}
	}
	return false
}

// RedundantReads counts reads of a file region whose unchanged content
// the history already showed verbatim.
func (r *Runner) RedundantReads() int {
	r.readsMu.Lock()
	defer r.readsMu.Unlock()
	return r.redundantReads
}

// head is the checkout's branch and commit ("" outside git).
func (r *Runner) head() string {
	sha := gitOutput(r.Opts.Root, "rev-parse", "--verify", "-q", "HEAD")
	if sha == "" {
		return ""
	}
	branch := gitOutput(r.Opts.Root, "rev-parse", "--abbrev-ref", "HEAD")
	return branch + "@" + sha[:min(12, len(sha))]
}

// latestStamps is, per file, the last stamp in the conversation (the
// archived part included).
func (r *Runner) latestStamps() map[string]FileStamp {
	out := map[string]FileStamp{}
	for _, ms := range [][]Message{r.Session.Archive, r.Session.Messages} {
		for _, m := range ms {
			for _, s := range m.Stamps {
				out[s.Path] = s
			}
		}
	}
	return out
}

const (
	freshnessMaxFiles = 300
	freshnessMaxBytes = 4 << 20
)

// freshnessNote says what changed since the conversation last looked: the
// checkout's move, and the files whose content differs from the last
// stamp. Each change is reported once.
func (r *Runner) freshnessNote(head string) string {
	if r.Session == nil || len(r.Session.Messages) == 0 {
		return ""
	}
	var lines []string
	prev := ""
	for _, ms := range [][]Message{r.Session.Archive, r.Session.Messages} {
		for _, m := range ms {
			if m.Role == "user" && m.Head != "" {
				prev = m.Head
			}
		}
	}
	if prev != "" && head != "" && prev != head {
		lines = append(lines, fmt.Sprintf("The checkout changed since the previous message (%s, now %s): file contents shown earlier in this conversation may be out of date.", prev, head))
	}
	latest := r.latestStamps()
	paths := make([]string, 0, len(latest))
	for p := range latest {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) > freshnessMaxFiles {
		paths = paths[len(paths)-freshnessMaxFiles:]
	}
	r.readsMu.Lock()
	if r.noted == nil {
		r.noted = map[string]string{}
	}
	var changed []string
	for _, p := range paths {
		if st, err := os.Stat(filepath.Join(r.Opts.Root, filepath.FromSlash(p))); err == nil && st.Size() > freshnessMaxBytes {
			continue // not hashed every turn
		}
		now := retrieve.FileBlob(r.Opts.Root, p)
		if now == latest[p].Blob || r.noted[p] == now {
			continue
		}
		r.noted[p] = now
		if now == "" {
			changed = append(changed, p+" (deleted)")
		} else {
			changed = append(changed, p)
		}
	}
	r.readsMu.Unlock()
	if len(changed) > 0 {
		more := ""
		if len(changed) > 10 {
			more = fmt.Sprintf(" and %d more", len(changed)-10)
			changed = changed[:10]
		}
		lines = append(lines, "These files changed since this conversation last read or wrote them; re-read them before relying on what was shown: "+strings.Join(changed, ", ")+more+".")
	}
	return strings.Join(lines, "\n")
}

// staleIn names the files a result's stamps show that changed since.
func (r *Runner) staleIn(m *Message) []string {
	var out []string
	for _, s := range m.Stamps {
		if retrieve.FileBlob(r.Opts.Root, s.Path) != s.Blob {
			out = append(out, s.Path)
		}
	}
	return out
}

// knownFiles are the files whose content a result still in the history
// shows verbatim, with the blob it showed: evidence already collected.
func (r *Runner) knownFiles() map[string]string {
	out := map[string]string{}
	for _, m := range r.Session.Messages {
		if m.Role != "tool" || m.Elided != "" {
			continue
		}
		for _, s := range m.Stamps {
			if s.Op == "read" {
				out[s.Path] = s.Blob
			} else {
				delete(out, s.Path) // changed after it was read
			}
		}
	}
	return out
}

// recentEvidence is the output of the commands run since the previous
// user message (a failing test run, a build), newest last, for retrieval
// to locate.
func (r *Runner) recentEvidence() []string {
	var out []string
	msgs := r.Session.Messages
	for i := len(msgs) - 1; i >= 0 && len(out) < 3; i-- {
		m := msgs[i]
		if m.Role == "user" && m.Provenance != nil && m.Provenance.Source == SourceUser {
			break
		}
		if m.Role == "tool" && (m.Name == "bash" || m.Name == "bash_output") && m.Elided == "" {
			out = append([]string{tailBytes(m.Content, 8<<10)}, out...)
		}
	}
	return out
}

func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

const (
	retrievalTokens = 1200
	retrievalBudget = 3 * time.Second
)

// retrievalPack is the block of likely relevant places that travels with
// a message (direct.retrieval), and its record; "" when nothing was found.
func (r *Runner) retrievalPack(ctx context.Context, text string) (string, *RetrievalRecord) {
	if r.Opts.NoSeedPack {
		return "", nil
	}
	if r.Opts.Retrieval == "basic" {
		return r.seedPack(ctx, text), nil
	}
	opt := retrieve.Options{Root: r.Opts.Root, Nav: r.navigator(), Budget: retrievalBudget}
	var evidence []string
	if r.Session != nil {
		opt.Known = r.knownFiles()
		evidence = r.recentEvidence()
	}
	res, err := retrieve.Retrieve(ctx, retrieve.Input{Request: text, Evidence: evidence}, opt)
	if err != nil {
		return "", nil
	}
	block := retrieve.Render(res, retrievalTokens)
	if block == "" {
		return "", nil
	}
	rec := &RetrievalRecord{Revision: res.Revision, Millis: res.Millis, Truncated: res.Truncated}
	for _, st := range res.Steps {
		if st.Ran {
			rec.Strategies = append(rec.Strategies, string(st.Strategy))
		}
	}
	for _, it := range res.Items {
		if !it.Known && !strings.Contains(block, "- "+it.File) {
			continue // not rendered
		}
		rec.Items = append(rec.Items, RetrievedItem{File: it.File, Line: it.Line, Symbol: it.Symbol, Blob: it.Prov.Blob, Methods: it.Prov.Methods, Known: it.Known})
	}
	return block, rec
}
