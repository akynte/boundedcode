package native

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/workflow"
)

// repeatLimit is how many times the same call may return the same answer
// before the attempt is ended.
//
// Two is ordinary: a model re-reads a file it edited, or checks something it
// half-remembers. Three identical calls returning identical bytes is not
// checking, it is a loop, and every further step spends the budget to arrive
// back where it started.
const repeatLimit = 3

// staleLimit is how many consecutive steps may pass with no new information
// before the attempt is ended.
//
// A step is stale when every tool call in it was one already made, answered
// identically. A model can be stale once and recover — it re-reads two files
// then edits. Three steps in a row learning nothing means the strategy is not
// going to produce anything, and the remaining budget is better spent telling
// the operator what happened than on more of it.
const staleLimit = 3

// progress tracks whether a tool loop is still learning anything.
//
// The step limit already bounds cost, but it bounds it at the wrong place: a
// model that reads the same file sixty times costs sixty steps and produces
// the same report as one that gave up at three, except later and with a larger
// bill. This distinguishes "working" from "stuck" while there is still budget
// left to say so.
type progress struct {
	// seen maps a call fingerprint to the digest of the answer it last gave,
	// and how many times that exact pair has occurred.
	seen map[string]*record
	// stale counts consecutive steps in which nothing new was learned.
	stale int
	// corrected records the calls the supervisor has already explicitly told
	// the model to stop making. A repetition after that is the one that ends
	// the attempt; the first is worth a recovery.
	corrected map[string]bool
	// alternatives are the tool names the engine currently offers, so the
	// correction names actions that exist rather than a hard-coded list.
	alternatives []string
	// reads is the bounded evidence cache, keyed by the same fingerprint as
	// seen. It answers "what did that call find", which the transcript used
	// to answer and a continuation no longer has.
	reads map[string]*workflow.ReadEvidence
	// seq orders evidence by recency across boundaries.
	seq int
	// Counters, for the task's report.
	exact, canonical, recoveries, terminated int
}

type record struct {
	digest string
	count  int
}

func newProgress(alternatives []string, tried []workflow.TriedCall) *progress {
	p := &progress{seen: map[string]*record{}, corrected: map[string]bool{},
		reads: map[string]*workflow.ReadEvidence{}, alternatives: alternatives}
	// A phase boundary replaces the conversation, not the evidence. Without
	// this, resetting the context to escape an exhausted budget would also
	// erase the record that a call had already been made and answered
	// identically — and the fresh budget would go on repeating it.
	for _, t := range tried {
		p.seen[t.Fingerprint] = &record{digest: t.Digest, count: t.Count}
		if t.Corrected {
			p.corrected[t.Fingerprint] = true
		}
	}
	return p
}

// Tried exports the guard's state so a continuation can be seeded with it.
func (p *progress) Tried() []workflow.TriedCall {
	out := make([]workflow.TriedCall, 0, len(p.seen))
	for fp, r := range p.seen {
		out = append(out, workflow.TriedCall{
			Fingerprint: fp, Digest: r.digest, Count: r.count, Corrected: p.corrected[fp],
		})
	}
	// Sorted so a saved state is the same bytes for the same history.
	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out
}

// verdict is what a single tool call was worth.
type verdict int

const (
	// learned: the call was new, or answered differently than last time.
	learned verdict = iota
	// repeated: the same call, the same answer, again.
	repeated
)

// observe records one call and its result, and says whether anything was
// learned. times is how many times this exact call-and-answer has now occurred.
func (p *progress) observe(name, arguments, content string) (v verdict, times int) {
	fp := fingerprint(name, arguments)
	if fp != name+"\x00"+strings.TrimSpace(arguments) {
		// The call was only equal to a previous one after canonicalisation —
		// different key order or spacing. Counted separately because
		// "the model is looping" and "the model's serializer is unstable"
		// are different findings, and only one of them is its fault.
		p.canonical++
	}
	d := digest(content)
	r, ok := p.seen[fp]
	if !ok {
		p.seen[fp] = &record{digest: d, count: 1}
		return learned, 1
	}
	if r.digest != d {
		// The same question with a different answer is real information: the
		// file changed, the build now fails differently. Counting it as a
		// repeat would punish the model for checking its own work.
		r.digest, r.count = d, 1
		return learned, 1
	}
	r.count++
	p.exact++
	return repeated, r.count
}

// observeResult records both halves of what a call was worth: whether it
// taught the loop anything, and what it found. They are kept together so the
// evidence cache and the loop guard can never disagree about which calls
// happened.
func (p *progress) observeResult(name, arguments, content string) (verdict, int) {
	v, times := p.observe(name, arguments, content)
	p.observeRead(name, arguments, content)
	return v, times
}

// recoverable reports whether this call has hit the repeat limit for the
// first time, which is a correction rather than an ending.
//
// The guard was right about what it detected and wrong about what to do with
// it. Ending the attempt on the third identical call threw away everything
// the model had already found — on Django it had located the prefetch code
// and was circling it — when what the situation needs is to be told, once,
// that the action produced nothing and that it must do something else.
func (p *progress) recoverable(name, arguments string) (bool, string) {
	fp := fingerprint(name, arguments)
	r, ok := p.seen[fp]
	if !ok || r.count < repeatLimit || p.corrected[fp] {
		return false, ""
	}
	p.corrected[fp] = true
	p.recoveries++
	// The stale counter is reset with the correction: the model is being
	// given a bounded chance to do something else, and counting the steps it
	// wasted before being told would spend that chance immediately.
	p.stale = 0
	return true, p.recoveryNote(name, r.count)
}

// recoveryNote is the correction. It states the fact, forbids the repeat, and
// offers the actions this engine actually has.
func (p *progress) recoveryNote(name string, times int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[supervisor] You have now executed %s with the same arguments %d times "+
		"and it produced no new evidence each time. Do not repeat it.\n", name, times)
	if len(p.alternatives) > 0 {
		b.WriteString("Choose a different action. Available tools: ")
		b.WriteString(strings.Join(p.alternatives, ", "))
		b.WriteString(".\n")
	}
	b.WriteString("Act on the evidence you already have above: inspect a file a search " +
		"returned, look at a related symbol or its consumers, revise your hypothesis, or " +
		"make the edit if you have enough. Repeating this call again will end the attempt.\n")
	return b.String()
}

// endOfStep closes a step. learnedAnything is whether any call in it, or an
// edit, produced something new.
func (p *progress) endOfStep(learnedAnything bool) {
	if learnedAnything {
		p.stale = 0
		return
	}
	p.stale++
}

// stuck reports whether the loop should be ended, and why.
//
// The reason is written for the operator reading the task's summary, so it
// names the actions rather than reporting a counter.
func (p *progress) stuck() (bool, string) {
	if p.stale >= staleLimit {
		return true, fmt.Sprintf(
			"stopped after %d consecutive steps that learned nothing new: %s",
			p.stale, p.topRepeats())
	}
	// Only a call the model was already told to stop making ends the
	// attempt, and only if it made it again afterwards.
	for fp, r := range p.seen {
		if p.corrected[fp] && r.count > repeatLimit {
			p.terminated++
			return true, fmt.Sprintf(
				"stopped after calling %s %d times with the same arguments: the supervisor "+
					"said so explicitly after the %d%s and it was repeated anyway",
				describeCall(fp), r.count, repeatLimit, ordinalSuffix(repeatLimit))
		}
	}
	return false, ""
}

func ordinalSuffix(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "st"
	case n%10 == 2 && n%100 != 12:
		return "nd"
	case n%10 == 3 && n%100 != 13:
		return "rd"
	}
	return "th"
}

// Counters reports what the guard did, for the task's record.
func (p *progress) Counters() (exact, canonical, recoveries, terminated int) {
	return p.exact, p.canonical, p.recoveries, p.terminated
}

// topRepeats names the calls the loop kept making, most repeated first.
func (p *progress) topRepeats() string {
	type entry struct {
		call string
		n    int
	}
	var es []entry
	for fp, r := range p.seen {
		if r.count > 1 {
			es = append(es, entry{describeCall(fp), r.count})
		}
	}
	if len(es) == 0 {
		return "no tool call produced a new answer"
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].n != es[j].n {
			return es[i].n > es[j].n
		}
		return es[i].call < es[j].call
	})
	var parts []string
	for i, e := range es {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d other repeated call(s)", len(es)-3))
			break
		}
		parts = append(parts, fmt.Sprintf("%s ×%d", e.call, e.n))
	}
	return strings.Join(parts, ", ")
}

// note is what the supervisor tells the model when it repeats itself.
//
// It is returned separately from the tool result because it is the supervisor
// speaking, not the repository: it must sit outside the untrusted fence, or the
// model is being told to treat its own supervisor's warning as data.
func repeatNote(name string, times int) string {
	return fmt.Sprintf(
		"[supervisor] You have now called %s with these exact arguments %d times and "+
			"received the same answer each time. You already have this result above. "+
			"Repeating it will not produce new information — either act on what you have, "+
			"or try something different.\n", name, times)
}

// fingerprint identifies a call by name and arguments, canonicalised so that
// two calls differing only in key order or spacing are recognised as the same.
func fingerprint(name, arguments string) string {
	canon := strings.TrimSpace(arguments)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(arguments), &parsed); err == nil {
		// Go marshals map keys in sorted order, which is the canonical form
		// wanted here.
		if b, err := json.Marshal(parsed); err == nil {
			canon = string(b)
		}
	}
	return name + "\x00" + canon
}

// describeCall renders a fingerprint back into something readable.
func describeCall(fp string) string {
	name, args, ok := strings.Cut(fp, "\x00")
	if !ok {
		return fp
	}
	if len(args) > 80 {
		args = args[:80] + "…"
	}
	return name + args
}

// digest keeps a fixed-size fingerprint of a result rather than the result, so
// a long tool loop does not retain every file it read.
func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
