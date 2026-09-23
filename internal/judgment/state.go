package judgment

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/akynte/boundedcode/internal/firewall"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/trust"
)

// State is what a question is asked about, and it is the only thing in this
// system that carries anything derived from the operator's repository to a
// third party.
//
// It is a type rather than a map because the guarantee is about what cannot be
// put in it, and the first version of this type did not have that guarantee: it
// had a Fact(key string, value any) that marshalled whatever it was handed, so
// "everything goes through a guarded door" was true of the doors and false of
// the building. This version has no generic serialization entry point at all.
//
// Three doors, because there are three different things a question needs to
// know and three different policies they are subject to:
//
//   - Objective and TrustedFact carry what this system or its operator wrote:
//     the task as typed, a count, a status. Scalars only.
//   - RepoItem carries repository *metadata* — a path, a symbol, a line range.
//     Permitted in every mode, but only for an origin that has passed the
//     deterministic egress-eligibility check.
//   - RepoItem.Text and RepoText carry repository *source*. Permitted only in
//     repo_text mode, and only after the credential scan and neutralisation.
//
// A refusal fails the whole request rather than dropping the field. Dropping
// it would produce a confident judgment about evidence the judge never saw,
// which is worse than no judgment at all — internal/llm refuses rather than
// degrades for the same reason (DR-4).
type State struct {
	mode   RedactMode
	facts  map[string]any
	repo   map[string]repoField
	items  map[string][]*RepoItem
	output map[string]outputField

	// metadata and text count what of the operator's actually left, by kind,
	// so the journal reports the fact rather than the permission.
	// outputCount does the same for verification output, kept apart from
	// text because it is a different disclosure: a repository author never
	// wrote it, a running program did.
	metadata    int
	text        int
	outputCount int
	frozen      bool
}

// repoField is repository text together with where it came from. Provenance
// travels as a sibling field rather than as an in-band marker: the state is
// structured JSON, so there is nothing to delimit, and the fence's markers
// carry a per-run nonce that has no business leaving this machine.
type repoField struct {
	Origin string `json:"origin"`
	Body   string `json:"body"`
}

// outputField is verification output together with the recipe that produced
// it, the same way repoField pairs repository text with its origin. It is a
// separate type, not a repoField, because Output's own comment explains why
// this content needs a scan repoField's does not: a program's stdout is
// neither source nor metadata, and it can contain anything the program
// printed, including a credential it failed while holding.
type outputField struct {
	Recipe string `json:"recipe"`
	Body   string `json:"output"`
}

// RedactMode says how much of the repository a judgment may see.
type RedactMode string

const (
	// RedactStrict forbids repository source. Judgments run on metadata:
	// paths, symbol names, kinds, line ranges, counts. This is the default.
	//
	// Note what it does and does not promise. It promises that no line of
	// anybody's source leaves. It does not promise that nothing derived from
	// the repository leaves: a path and a symbol name are repository
	// information, they are what makes the question answerable, and they go.
	RedactStrict RedactMode = "strict"
	// RedactRepoText additionally permits signatures, excerpts and diffs,
	// subject to every check below. It is an explicit operator decision and
	// `bcode doctor` says so on every run.
	RedactRepoText RedactMode = "repo_text"
	// RedactOutput additionally permits normalized tool and verification
	// output through State.Output. It is its own tier above repo_text,
	// deliberately: output is neither source nor metadata, and it can
	// contain anything a running program printed, so it gets a separate
	// operator decision and its own scan on top of the credential check
	// every field already gets.
	RedactOutput RedactMode = "output"
)

// Valid reports whether the mode is one this package implements. An unknown
// mode is a configuration error rather than a reason to guess.
func (m RedactMode) Valid() bool {
	return m == RedactStrict || m == RedactRepoText || m == RedactOutput
}

// rank orders the modes so AtLeast can compare them. An unrecognised mode
// ranks below RedactStrict, the same conservative-on-the-unknown choice
// judgment.Tier.rank makes.
func (m RedactMode) rank() int {
	switch m {
	case RedactStrict:
		return 1
	case RedactRepoText:
		return 2
	case RedactOutput:
		return 3
	default:
		return 0
	}
}

// AtLeast reports whether this mode permits at least what min permits. Modes
// are cumulative — RedactOutput permits everything RedactRepoText does, plus
// normalized output — so a call site gating repo_text-only content should
// almost always ask mode.AtLeast(RedactRepoText) rather than compare for
// exact equality; the latter would stop working the moment an operator who
// already trusted repo_text set redact: output instead.
func (m RedactMode) AtLeast(min RedactMode) bool { return m.rank() >= min.rank() }

// Field and state size caps.
//
// These bound what one mistake can cost. A question is a judgment about a
// small amount of evidence; a field that wants eight kilobytes is a call site
// that meant to send a summary and sent a file.
const (
	MaxFieldBytes = 8 << 10
	MaxStateBytes = 64 << 10
	// MaxScalarBytes bounds one trusted scalar. The objective is the longest
	// thing that legitimately goes through that door.
	MaxScalarBytes = 4 << 10
)

// NewState opens a state in the given redaction mode.
func NewState(mode RedactMode) *State {
	return &State{
		mode:   mode,
		facts:  map[string]any{},
		repo:   map[string]repoField{},
		items:  map[string][]*RepoItem{},
		output: map[string]outputField{},
	}
}

// Mode reports the redaction mode this state was opened in.
func (s *State) Mode() RedactMode { return s.mode }

// Objective records what the caller is trying to do, in the words it was asked
// in. It is the one free-text door for operator-authored content.
//
// It is still credential-scanned, because "the operator typed it" includes an
// operator who pasted a stack trace with a token in it.
func (s *State) Objective(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("judgment: the objective is empty")
	}
	return s.scalar("objective", text)
}

// TrustedFact records one supervisor-authored scalar: a count, a status, a
// flag, a duration.
//
// Scalars only, and that restriction is the point rather than an omission. The
// previous API took `any` and marshalled it, which meant any repository
// structure could be serialised straight past every check in this file. A
// composite value is refused here so that the only way to send a *collection*
// of repository-derived records is RepoItem, where each record's origin is
// checked.
func (s *State) TrustedFact(key string, value any) error {
	switch v := value.(type) {
	case string:
		return s.scalar(key, v)
	case bool:
		return s.put(key, v, strconv.FormatBool(v))
	case int:
		return s.put(key, v, strconv.Itoa(v))
	case int64:
		return s.put(key, v, strconv.FormatInt(v, 10))
	case float64:
		return s.put(key, v, strconv.FormatFloat(v, 'g', -1, 64))
	default:
		return fmt.Errorf("judgment: trusted fact %q is %T; only scalars may go through "+
			"this door. Repository-derived records go through RepoItem, where each "+
			"record's origin is checked", key, value)
	}
}

// ClaimText records text a model wrote about its own work — a plan's stated
// reason for waiving an obligation, a hypothesis, a completion summary — so a
// judgment can be asked whether that text squares with evidence code selected
// separately.
//
// It is a distinct door from TrustedFact on purpose, even though both end up
// as scalars subject to the same size cap and credential scan. TrustedFact's
// contract is "this system or its operator wrote this"; a call site that used
// it for model output would be quietly asking a judgment to trust the exact
// kind of claim the whole package exists not to trust. Keeping the doors
// separate means a reviewer of a new call site can tell which guarantee it is
// relying on without reading the value.
func (s *State) ClaimText(key, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("judgment: claim %q is empty", key)
	}
	return s.scalar(key, text)
}

func (s *State) scalar(key, text string) error {
	if len(text) > MaxScalarBytes {
		return fmt.Errorf("judgment: %q is %d bytes, over the %d-byte scalar cap",
			key, len(text), MaxScalarBytes)
	}
	return s.put(key, text, text)
}

func (s *State) put(key string, value any, inspect string) error {
	if err := s.writable(key); err != nil {
		return err
	}
	if err := firewall.CheckContentSecrets("judgment state field "+key, inspect); err != nil {
		return fmt.Errorf("judgment: %w", err)
	}
	s.facts[key] = value
	return nil
}

// RepoItem is one repository-derived record: a retrieval candidate, a file, a
// declaration.
//
// It cannot be constructed from outside this package — the fields are
// unexported and there is no literal form — so every one in existence came
// from State.NewRepoItem and has had its origin checked. Its JSON encoding is
// this file's, so there is no path by which an unchecked field reaches the
// wire.
type RepoItem struct {
	id     string
	origin string
	fields map[string]any
	order  []string
}

// NewRepoItem opens a record for one repository path.
//
// id is the stable identifier the question refers to. origin is the
// repository-relative path the record describes, and it is checked here:
// policy.EgressSensitive decides, deterministically, whether this path may be
// disclosed at all. A refusal is an error rather than a silently skipped
// field, so a caller assembling a list either sends a complete one or none.
func (s *State) NewRepoItem(id, origin string) (*RepoItem, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("judgment: a repository record needs a stable id")
	}
	clean, err := s.eligible(origin)
	if err != nil {
		return nil, err
	}
	it := &RepoItem{id: id, origin: clean, fields: map[string]any{}}
	it.set("id", id)
	it.set("path", clean)
	return it, nil
}

// Origin reports the path this record describes, normalised.
func (it *RepoItem) Origin() string { return it.origin }

// Meta attaches one metadata field: a symbol name, a kind, a line range. The
// value is repository-derived, but it is not source, so it travels in every
// mode. Empty values are skipped rather than sent as empty strings.
func (it *RepoItem) Meta(field, value string) *RepoItem {
	if value != "" {
		it.set(field, value)
	}
	return it
}

// Count attaches one numeric field: how many lines matched, how many callers
// there are.
func (it *RepoItem) Count(field string, n int) *RepoItem {
	it.set(field, n)
	return it
}

// Lines attaches the record's line range, when it has one.
func (it *RepoItem) Lines(start, end int) *RepoItem {
	if start > 0 && end >= start {
		it.set("lines", strconv.Itoa(start)+"-"+strconv.Itoa(end))
	}
	return it
}

// Text attaches repository source to this record — a signature, a short
// excerpt.
//
// It returns an error rather than being chainable, because unlike the metadata
// setters it can refuse, and a chainable call that silently dropped its
// argument would be the escape hatch this design exists to remove.
func (it *RepoItem) Text(field, body string, s *State) error {
	if !s.mode.AtLeast(RedactRepoText) {
		return fmt.Errorf("judgment: %q on %s is repository source, which redact mode "+
			"%q does not permit", field, it.origin, s.mode)
	}
	if body == "" {
		return nil
	}
	if len(body) > MaxFieldBytes {
		return fmt.Errorf("judgment: %q on %s is %d bytes, over the %d-byte field cap",
			field, it.origin, len(body), MaxFieldBytes)
	}
	if err := firewall.CheckContentSecrets("judgment "+field+" from "+it.origin, body); err != nil {
		return fmt.Errorf("judgment: %w", err)
	}
	it.set(field, trust.Neutralise(body))
	s.text++
	return nil
}

func (it *RepoItem) set(field string, value any) {
	if _, seen := it.fields[field]; !seen {
		it.order = append(it.order, field)
	}
	it.fields[field] = value
}

// MarshalJSON renders the record. It is the only encoding of a RepoItem, which
// is what makes "no unguarded serialization path" a property of the type
// rather than a convention.
func (it *RepoItem) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(it.fields))
	for k, v := range it.fields {
		out[k] = v
	}
	return json.Marshal(out)
}

// AddItems files a list of repository records under one key.
func (s *State) AddItems(key string, items []*RepoItem) error {
	if err := s.writable(key); err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("judgment: %q has no records", key)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if it == nil {
			return fmt.Errorf("judgment: %q contains an unbuilt record", key)
		}
		if seen[it.id] {
			return fmt.Errorf("judgment: %q repeats record id %q, so a question naming "+
				"it would be ambiguous", key, it.id)
		}
		seen[it.id] = true
	}
	s.items[key] = items
	s.metadata += len(items)
	return nil
}

// RepoText files a whole field of repository source: a diff, one file's body.
func (s *State) RepoText(key, origin, body string) error {
	if err := s.writable(key); err != nil {
		return err
	}
	if !s.mode.AtLeast(RedactRepoText) {
		return fmt.Errorf("judgment: state field %q carries repository source, "+
			"which redact mode %q does not permit; set redact: repo_text to allow it", key, s.mode)
	}
	clean, err := s.eligible(origin)
	if err != nil {
		return fmt.Errorf("%w (state field %q)", err, key)
	}
	if err := firewall.CheckContentSecrets("judgment state field "+key, body); err != nil {
		return fmt.Errorf("judgment: %w", err)
	}
	if len(body) > MaxFieldBytes {
		return fmt.Errorf("judgment: state field %q is %d bytes, over the %d-byte field cap",
			key, len(body), MaxFieldBytes)
	}
	s.repo[key] = repoField{Origin: clean, Body: trust.Neutralise(body)}
	s.text++
	return nil
}

// Output files normalized tool or verification output — never a raw process
// output, always output that has already been through the same
// normalization workflow.Normalize applies before failure fingerprinting
// (paths relativised, timestamps, addresses and durations replaced).
//
// It is its own tier above repo_text and its own door, not a RepoText call
// with a recipe name as the origin, because a recipe's stdout is not
// repository source: eligible() checks whether a *path* may be disclosed,
// and a recipe name is not a path. What output needs instead is the
// credential scan alone — a failing test can print a token it was holding,
// and that is a risk repository text does not carry, which is the whole
// reason this has a tier of its own rather than folding into repo_text.
func (s *State) Output(key, recipeName, normalized string) error {
	if err := s.writable(key); err != nil {
		return err
	}
	if !s.mode.AtLeast(RedactOutput) {
		return fmt.Errorf("judgment: state field %q carries verification output, "+
			"which redact mode %q does not permit; set redact: output to allow it", key, s.mode)
	}
	if strings.TrimSpace(recipeName) == "" {
		return fmt.Errorf("judgment: state field %q has no recipe name", key)
	}
	if len(normalized) > MaxFieldBytes {
		return fmt.Errorf("judgment: state field %q is %d bytes, over the %d-byte field cap",
			key, len(normalized), MaxFieldBytes)
	}
	if err := firewall.CheckContentSecrets("judgment state field "+key+" ("+recipeName+" output)",
		normalized); err != nil {
		return fmt.Errorf("judgment: %w", err)
	}
	s.output[key] = outputField{Recipe: recipeName, Body: normalized}
	s.outputCount++
	return nil
}

// eligible is the deterministic egress gate on a repository path. It is the
// same answer for metadata and for source: a path this refuses is one whose
// existence is not disclosed, never mind its contents.
func (s *State) eligible(origin string) (string, error) {
	if strings.TrimSpace(origin) == "" {
		return "", fmt.Errorf("judgment: repository record with no origin path")
	}
	clean := strings.TrimPrefix(strings.ReplaceAll(origin, "\\", "/"), "./")
	if policy.EgressSensitive(clean) {
		return "", fmt.Errorf("judgment: %s is egress-sensitive; neither its contents nor "+
			"the fact that it exists may leave this machine", clean)
	}
	return clean, nil
}

// Eligible reports whether a path may be disclosed, without building anything.
//
// It is exported so a caller can decide *before* assembling a request whether
// it has a complete set to ask about — which matters because a partial set
// produces a partial answer, and this system treats a partial answer as no
// answer rather than mixing scales.
func Eligible(origin string) bool {
	if strings.TrimSpace(origin) == "" {
		return false
	}
	return !policy.EgressSensitive(strings.TrimPrefix(strings.ReplaceAll(origin, "\\", "/"), "./"))
}

func (s *State) writable(key string) error {
	if s.frozen {
		return fmt.Errorf("judgment: state field %q added after the state was sent", key)
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("judgment: state field has no key")
	}
	if s.has(key) {
		return fmt.Errorf("judgment: state field %q is already set", key)
	}
	return nil
}

func (s *State) has(key string) bool {
	if _, ok := s.facts[key]; ok {
		return true
	}
	if _, ok := s.repo[key]; ok {
		return true
	}
	if _, ok := s.output[key]; ok {
		return true
	}
	_, ok := s.items[key]
	return ok
}

// Empty reports a state with nothing in it. A question about nothing is a
// question whose answer is about nothing, so AskAll declines to ask one.
func (s *State) Empty() bool {
	return s == nil || len(s.facts)+len(s.repo)+len(s.items)+len(s.output) == 0
}

// RepoMetadataFields and RepoTextFields report what of the operator's actually
// left, by kind. The journal records both, so a trace answers "what was
// disclosed" rather than "what was permitted".
func (s *State) RepoMetadataFields() int {
	if s == nil {
		return 0
	}
	return s.metadata
}

func (s *State) RepoTextFields() int {
	if s == nil {
		return 0
	}
	return s.text
}

// OutputFields reports how many verification-output fields this state
// carries. Kept apart from RepoTextFields for the same reason Output is a
// separate door: it is a different disclosure, a program's own output rather
// than anything a repository author wrote.
func (s *State) OutputFields() int {
	if s == nil {
		return 0
	}
	return s.outputCount
}

// Payload renders the state as the JSON object a request carries, and freezes
// it against further writes.
func (s *State) Payload() (json.RawMessage, error) {
	if s == nil {
		return nil, fmt.Errorf("judgment: no state")
	}
	body, err := s.encode()
	if err != nil {
		return nil, err
	}
	if len(body) > MaxStateBytes {
		return nil, fmt.Errorf("judgment: state is %d bytes, over the %d-byte cap",
			len(body), MaxStateBytes)
	}
	s.frozen = true
	return body, nil
}

func (s *State) encode() ([]byte, error) {
	merged := make(map[string]any, len(s.facts)+len(s.repo)+len(s.items)+len(s.output))
	for k, v := range s.facts {
		merged[k] = v
	}
	for k, v := range s.repo {
		merged[k] = v
	}
	for k, v := range s.items {
		merged[k] = v
	}
	for k, v := range s.output {
		merged[k] = v
	}
	body, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("judgment: state is not encodable: %w", err)
	}
	return body, nil
}

// canonical renders the state without freezing it, which is what a test needs
// to inspect what would have been sent without ending the state it inspects.
func (s *State) canonical() (string, error) {
	body, err := s.encode()
	return string(body), err
}

// questionDigest renders a question set stably, for the cache key.
func questionDigest(qs map[string]Question) string {
	ids := make([]string, 0, len(qs))
	for id := range qs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	for _, id := range ids {
		q := qs[id]
		crit, _ := json.Marshal(q.criteria)
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\x00", id, q.kind, q.instructions, crit)
	}
	return b.String()
}
