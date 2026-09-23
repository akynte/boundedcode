package judgment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// The TypeSafe System One client.
//
// This is the only file in the repository that sends repository-derived
// content to a host this project does not run. Everything it may send has
// already been through State; everything it returns is a number.

// Deps are the collaborators a live judge uses. All are optional: a judge with
// none of them still answers questions, it just records and caches nothing.
// That is what keeps this usable from a test and from `bcode doctor`.
type Deps struct {
	// HTTP overrides the client, for tests.
	HTTP *http.Client
	// Recorder journals each request. See internal/ledger.
	Recorder Recorder
	// Cache stores answers content-addressed by state and question set.
	Cache AnswerCache
	// Logf receives transport failures. A judgment that did not happen is not
	// an error anybody is shown, so this is the only place it surfaces.
	Logf func(format string, args ...any)
}

// Recorder journals judgment requests. It is an interface here rather than a
// *ledger.Ledger so that internal/judgment does not depend on the store: the
// supervisor supplies the real one.
type Recorder interface {
	// BeginJudgment records the intent to make a request and returns the
	// function that records how it went. It is called before the request
	// leaves, so an interrupted call is visible as an uncertain row.
	BeginJudgment(ctx context.Context, intent JournalIntent) (func(JournalOutcome), error)
}

// JournalIntent is what is recorded before a request leaves. It carries a
// digest of the state and never the state: ledgers are attached to bug
// reports.
type JournalIntent struct {
	Endpoint    string   `json:"endpoint"`
	Model       string   `json:"model"`
	Phase       string   `json:"phase,omitempty"`
	QuestionIDs []string `json:"question_ids"`
	StateDigest string   `json:"state_digest"`
	StateBytes  int      `json:"state_bytes"`
	RedactMode  string   `json:"redact_mode"`
	// Refused marks an entry for a judgment that was declined locally, so no
	// request was built and no state digest exists.
	Refused bool `json:"refused,omitempty"`
	// RepoMetadata and RepoText count what of the operator's repository the
	// request actually carried, by kind. Two numbers rather than one because
	// they are different disclosures: metadata is a path list, text is source.
	RepoMetadata int `json:"repo_metadata_fields"`
	RepoText     int `json:"repo_text_fields"`
}

// JournalOutcome is what is recorded after.
type JournalOutcome struct {
	Source string `json:"source"`
	// ModelServed is the model id the service reported running, when it
	// reports one. A benchmark that cannot show the requested and the served
	// id side by side cannot prove which model produced its numbers.
	ModelServed string             `json:"model_served,omitempty"`
	Answered    int                `json:"answered"`
	Confidence  map[string]float64 `json:"confidence,omitempty"`
	Usage       Usage              `json:"usage"`
	DurationMS  int64              `json:"duration_ms"`
	Err         string             `json:"error,omitempty"`
}

// AnswerCache is the content-addressed store for answers. internal/cache
// supplies it; the key derivation lives there, per §2.3.
type AnswerCache interface {
	GetJudgment(key string) ([]byte, bool)
	PutJudgment(key string, body []byte)
}

// Client is a live judge.
type Client struct {
	cfg  Config
	key  string
	http *http.Client
	deps Deps

	mu        sync.Mutex
	lastUsage Usage
	lastModel string
	lastCache bool
}

// New builds a judge from configuration.
//
// Every reason not to have a judge — no file, enabled false — returns Off()
// and no error, because those are not mistakes. Whether a site may proceed
// without one is not decided here: Mandatory reports which sites may not, and
// Require turns a missing judge into a typed failure at the call site.
func New(cfg Config, deps Deps) (Judge, error) {
	if !cfg.Enabled {
		return Off(), nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	httpc := deps.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: cfg.Timeout()}
	}
	return &Client{
		cfg:  cfg,
		key:  os.Getenv(cfg.APIKeyEnv),
		http: httpc,
		deps: deps,
	}, nil
}

// Name identifies the backend.
func (c *Client) Name() string { return "typesafe:" + c.cfg.Model }

// ModelID reports the model this client was configured to request.
//
// It is the *requested* id, which judgment.yaml's own documentation is
// careful to distinguish from the served one: a 200 for a pinned id
// establishes that the service recognised it, not that it ran it. A
// calibration row carries this so a report can partition on it and say which
// configuration produced its numbers; `bcode judgment smoke` remains the way to
// check requested against served.
func (c *Client) ModelID() string { return c.cfg.Model }

// Available reports that a request would be attempted.
func (c *Client) Available() bool {
	return c != nil && c.cfg.Enabled && c.key != ""
}

// Endpoint reports the service this client talks to. It is read by preflight
// and by the consultation record; it carries no credential.
func (c *Client) Endpoint() string {
	if c == nil {
		return ""
	}
	return c.cfg.Endpoint
}

// Unready explains why Available is false, in one line an operator can act on.
//
// Six registered sites cannot run without this client, so "unavailable" is not
// a sufficient answer: turning judgments on and exporting a credential are
// different fixes.
func (c *Client) Unready() string {
	switch {
	case c == nil:
		return "no client was constructed"
	case !c.cfg.Enabled:
		return "enabled is false in " + ConfigFile
	case c.key == "":
		return "no credential in $" + c.cfg.APIKeyEnv
	default:
		return ""
	}
}

// MinConfidence is the operator's floor, read by Consult when a call site
// sets none.
func (c *Client) MinConfidence() float64 { return c.cfg.MinConfidence }

// Redact reports the configured mode, so a call site can build the richest
// state it is permitted rather than guessing.
func (c *Client) Redact() RedactMode { return c.cfg.Redact }

// LastUsage and LastFromCache report on the most recent request, for the Note
// that AskAll attaches. They are diagnostics, not control flow.
func (c *Client) LastUsage() Usage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUsage
}

func (c *Client) LastFromCache() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastCache
}

// wireQuestion is the request shape: type, instructions, criteria.
type wireQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type wireRequest struct {
	State     json.RawMessage         `json:"state"`
	Model     string                  `json:"model"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}

type wireResponse struct {
	Model   string                `json:"model"`
	Answers map[string]wireAnswer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Ask evaluates every question against one state, in one request.
func (c *Client) Ask(ctx context.Context, st *State, qs map[string]Question) (map[string]Answer, error) {
	if !c.Available() {
		return unanswered(qs), nil
	}

	payload, err := st.Payload()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(wireRequest{
		State:     payload,
		Model:     c.cfg.Model,
		Questions: wire(qs),
	})
	if err != nil {
		return nil, fmt.Errorf("judgment: encoding request: %w", err)
	}

	key := c.cacheKey(payload, qs)
	c.setCacheHit(false)
	if c.cfg.Cache && c.deps.Cache != nil {
		if cached, ok := c.deps.Cache.GetJudgment(key); ok {
			answers, err := decode(cached, qs)
			if err == nil {
				c.setCacheHit(true)
				// Journalled like any other answer. What is recorded is the
				// decision, not the packet: a trace that showed only the
				// requests that happened to miss the cache would under-report
				// what a run relied on, and on a second run of the same task
				// it would show nothing at all.
				answered := 0
				for _, a := range answers {
					if a.Answered {
						answered++
					}
				}
				c.record(ctx, st, payload, qs, JournalOutcome{
					Source: string(SourceCache), Answered: answered,
				})
				return answers, nil
			}
			// A cache entry that will not decode is a cache entry from an
			// older shape. Fall through and ask; it will be overwritten.
			c.logf("judgment: discarding undecodable cache entry: %v", err)
		}
	}

	finish := c.begin(ctx, st, payload, qs)
	started := time.Now()

	answers, usage, served, err := c.post(ctx, body, qs)
	outcome := JournalOutcome{
		Source:      string(SourceLive),
		Usage:       usage,
		ModelServed: served,
		DurationMS:  time.Since(started).Milliseconds(),
	}
	if err != nil {
		outcome.Source = string(SourceUnavailable)
		outcome.Err = err.Error()
		finish(outcome)
		return nil, err
	}
	for id, a := range answers {
		if a.Answered {
			outcome.Answered++
			if a.Kind != KindNoul {
				if outcome.Confidence == nil {
					outcome.Confidence = map[string]float64{}
				}
				outcome.Confidence[id] = a.Confidence
			}
		}
	}
	finish(outcome)

	c.mu.Lock()
	c.lastUsage = usage
	c.lastModel = served
	c.mu.Unlock()

	if c.cfg.Cache && c.deps.Cache != nil {
		if encoded, err := json.Marshal(answers); err == nil {
			c.deps.Cache.PutJudgment(key, encoded)
		}
	}
	return answers, nil
}

func (c *Client) post(ctx context.Context, body []byte, qs map[string]Question) (map[string]Answer, Usage, string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, Usage{}, "", fmt.Errorf("judgment: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// The request body is never in this error. It may contain repository
		// text, and an error string travels into logs and bug reports.
		//
		// Transport failures are typed so a mandatory decision can tell a
		// timeout from an outage: the two have different retry semantics and
		// the evidence record needs to say which one happened.
		class := FailureUnavailable
		if errors.Is(err, context.DeadlineExceeded) {
			class = FailureTimeout
		}
		return nil, Usage{}, "", &APIError{
			Class: class, Endpoint: c.cfg.Endpoint, Detail: "request failed"}
	}
	defer resp.Body.Close()

	// Bounded read: a judgment response is a handful of numbers per question,
	// and a body larger than this is not one.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, Usage{}, "", fmt.Errorf("judgment: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// The status is reported; the body is not, since a service may echo
		// the request in an error.
		return nil, Usage{}, "", &APIError{
			Class: classifyStatus(resp.StatusCode), Endpoint: c.cfg.Endpoint,
			Status: resp.StatusCode, Detail: resp.Status}
	}

	var out wireResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, Usage{}, "", &APIError{
			Class: FailureInvalid, Endpoint: c.cfg.Endpoint,
			Status: resp.StatusCode, Detail: "response is not the declared shape"}
	}
	answers := make(map[string]Answer, len(qs))
	for id, q := range qs {
		w, ok := out.Answers[id]
		if !ok {
			answers[id] = Answer{Kind: q.kind}
			continue
		}
		answers[id] = convert(q.kind, w)
	}
	usage := Usage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens, Requests: 1}
	// out.Model is what the service says it actually ran. It is recorded
	// beside the requested id rather than compared to it here: a mismatch is
	// a fact for the journal and for `bcode eval` to report, not a reason to
	// discard an answer the caller already paid for.
	return answers, usage, out.Model, nil
}

// convert maps one wire answer onto the typed one, refusing to invent a value
// the response did not carry. A noul with no probability is unanswered, not
// zero: zero means "certainly not", and the two must never be confused.
func convert(kind Kind, w wireAnswer) Answer {
	a := Answer{Kind: kind, Probabilities: w.Probabilities, Confidence: w.Confidence}
	switch kind {
	case KindNoul:
		if w.Noul == nil {
			return Answer{Kind: kind}
		}
		a.Noul = *w.Noul
	case KindChoice:
		if w.Choice == "" {
			return Answer{Kind: kind}
		}
		a.Choice = w.Choice
	case KindScore:
		if w.Score == nil {
			return Answer{Kind: kind}
		}
		a.Score = *w.Score
	default:
		return Answer{Kind: kind}
	}
	a.Answered = true
	return a
}

func wire(qs map[string]Question) map[string]wireQuestion {
	out := make(map[string]wireQuestion, len(qs))
	for id, q := range qs {
		out[id] = wireQuestion{Type: string(q.kind), Instructions: q.instructions, Criteria: q.criteria}
	}
	return out
}

func decode(body []byte, qs map[string]Question) (map[string]Answer, error) {
	var stored map[string]Answer
	if err := json.Unmarshal(body, &stored); err != nil {
		return nil, err
	}
	out := make(map[string]Answer, len(qs))
	for id, q := range qs {
		a, ok := stored[id]
		if !ok {
			return nil, fmt.Errorf("cached answers are missing %q", id)
		}
		a.Kind = q.kind
		out[id] = a
	}
	return out, nil
}

// cacheKey mixes everything that could change the answer. The model id is in
// it, which is why an unpinned alias makes cached judgments meaningless and
// `bcode doctor` warns about one.
func (c *Client) cacheKey(state json.RawMessage, qs map[string]Question) string {
	h := sha256.New()
	fmt.Fprintf(h, "judgment-v1\x00%s\x00%s\x00%s\x00%.4f\x00",
		c.cfg.Endpoint, c.cfg.Model, c.cfg.Redact, c.cfg.MinConfidence)
	h.Write(state)
	h.Write([]byte{0})
	h.Write([]byte(questionDigest(qs)))
	return hex.EncodeToString(h.Sum(nil))
}

// record writes a complete journal entry for something that did not involve
// the network: a cache hit, a local refusal. Intent and outcome in one call,
// because there was never a window in which the result was uncertain.
func (c *Client) record(ctx context.Context, st *State, payload json.RawMessage,
	qs map[string]Question, outcome JournalOutcome) {
	c.begin(ctx, st, payload, qs)(outcome)
}

// recordRefusal journals a judgment that was declined before any request was
// built — no judge, an empty state, a malformed question.
//
// These are local events and they matter to a trace: "no judgment was made
// here" and "a judgment was made and came back unhelpful" lead an operator to
// different places, and a journal that showed only the second would make the
// first look like it never happened.
func (c *Client) recordRefusal(ctx context.Context, source Source, detail string, ids []string) {
	if c == nil || c.deps.Recorder == nil {
		return
	}
	done, err := c.deps.Recorder.BeginJudgment(ctx, JournalIntent{
		Endpoint:    c.cfg.Endpoint,
		Model:       c.cfg.Model,
		Phase:       PhaseFrom(ctx),
		QuestionIDs: ids,
		RedactMode:  string(c.cfg.Redact),
		Refused:     true,
	})
	if err != nil {
		return
	}
	done(JournalOutcome{Source: string(source), Err: detail})
}

// begin journals the intent before the request leaves and returns the function
// that records how it went. A recorder that fails is not a reason to skip the
// request: the request is advisory, and so is the record of it.
func (c *Client) begin(ctx context.Context, st *State, payload json.RawMessage, qs map[string]Question) func(JournalOutcome) {
	if c.deps.Recorder == nil {
		return func(JournalOutcome) {}
	}
	ids := make([]string, 0, len(qs))
	for id := range qs {
		ids = append(ids, id)
	}
	digest := sha256.Sum256(payload)
	done, err := c.deps.Recorder.BeginJudgment(ctx, JournalIntent{
		Endpoint:     c.cfg.Endpoint,
		Model:        c.cfg.Model,
		Phase:        PhaseFrom(ctx),
		QuestionIDs:  ids,
		StateDigest:  hex.EncodeToString(digest[:]),
		StateBytes:   len(payload),
		RedactMode:   string(st.Mode()),
		RepoMetadata: st.RepoMetadataFields(),
		RepoText:     st.RepoTextFields(),
	})
	if err != nil {
		c.logf("judgment: could not journal the request: %v", err)
		return func(JournalOutcome) {}
	}
	return done
}

func (c *Client) setCacheHit(hit bool) {
	c.mu.Lock()
	c.lastCache = hit
	c.mu.Unlock()
}

func (c *Client) logf(format string, args ...any) {
	if c.deps.Logf != nil {
		c.deps.Logf(format, args...)
	}
}
