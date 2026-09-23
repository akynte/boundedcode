package judgment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file are about one property: that a judgment cannot make
// anything worse. A judge that is off, broken, slow or unconfident must leave
// the system behaving exactly as it did before, and nothing a call site can
// write may send content the operator did not permit.

func TestOffReturnsFallbackUnchanged(t *testing.T) {
	st := NewState(RedactStrict)
	if err := st.Objective("rename the account limit"); err != nil {
		t.Fatal(err)
	}
	got, note := Consult(context.Background(), Off(), Query[int]{
		Question:  Noul("Is this relevant?"),
		State:     st,
		Fallback:  7,
		Interpret: func(a Answer) (int, bool) { return 99, true },
	})
	if got != 7 {
		t.Fatalf("fallback not preserved: got %d, want 7", got)
	}
	if note.Source != SourceDisabled {
		t.Fatalf("source = %q, want %q", note.Source, SourceDisabled)
	}
	if note.Used() {
		t.Fatal("a disabled judge reported that its judgment was used")
	}
}

func TestNilJudgeIsOff(t *testing.T) {
	st := NewState(RedactStrict)
	_ = st.Objective("v")
	got, note := Consult(context.Background(), nil, Query[string]{
		Question:  Noul("?"),
		State:     st,
		Fallback:  "kept",
		Interpret: func(Answer) (string, bool) { return "replaced", true },
	})
	if got != "kept" || note.Source != SourceDisabled {
		t.Fatalf("nil judge: got %q / %q", got, note.Source)
	}
}

func TestTransportFailureIsANoteNotAnError(t *testing.T) {
	st := NewState(RedactStrict)
	_ = st.Objective("v")
	j := &Fake{Err: errors.New("connection refused")}
	got, note := Consult(context.Background(), j, Query[int]{
		Question:  Noul("?"),
		State:     st,
		Fallback:  3,
		Interpret: func(Answer) (int, bool) { return 4, true },
	})
	if got != 3 {
		t.Fatalf("a failed request changed the answer: %d", got)
	}
	if note.Source != SourceUnavailable {
		t.Fatalf("source = %q", note.Source)
	}
}

func TestConfidenceBoundary(t *testing.T) {
	const min = 0.75
	for _, tc := range []struct {
		name string
		conf float64
		want string
	}{
		{"just below", min - 0.0001, "fallback"},
		{"exactly at", min, "judged"},
		{"above", min + 0.1, "judged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := NewState(RedactStrict)
			_ = st.Objective("v")
			j := &Fake{Answers: map[string]Answer{"q": {Choice: "a", Confidence: tc.conf}}}
			got, _ := Consult(context.Background(), j, Query[string]{
				Question:      Choice("?", map[string]string{"a": "A", "b": "B"}),
				State:         st,
				Fallback:      "fallback",
				MinConfidence: min,
				Interpret:     func(Answer) (string, bool) { return "judged", true },
			})
			if got != tc.want {
				t.Fatalf("confidence %v: got %q, want %q", tc.conf, got, tc.want)
			}
		})
	}
}

func TestNoulIgnoresConfidenceGate(t *testing.T) {
	// A noul reports no confidence. Gating on the zero value would make every
	// noul unusable, which is the bug this test exists to prevent.
	st := NewState(RedactStrict)
	_ = st.Objective("v")
	j := &Fake{Answers: map[string]Answer{"q": {Noul: 0.9}}}
	got, note := Consult(context.Background(), j, Query[bool]{
		Question:      Noul("?"),
		State:         st,
		Fallback:      false,
		MinConfidence: 0.99,
		Interpret:     func(a Answer) (bool, bool) { return a.Noul > 0.5, true },
	})
	if !got {
		t.Fatalf("noul was gated on a confidence it does not report (%q)", note.Source)
	}
}

func TestInterpretDecliningKeepsFallback(t *testing.T) {
	st := NewState(RedactStrict)
	_ = st.Objective("v")
	j := &Fake{Answers: map[string]Answer{"q": {Noul: 0.51}}}
	got, note := Consult(context.Background(), j, Query[int]{
		Question:  Noul("?"),
		State:     st,
		Fallback:  1,
		Interpret: func(a Answer) (int, bool) { return 2, a.Noul > 0.8 },
	})
	if got != 1 || note.Source != SourceLowConfidence {
		t.Fatalf("got %d / %q", got, note.Source)
	}
}

func TestUnansweredQuestionsArePresentNotMissing(t *testing.T) {
	st := NewState(RedactStrict)
	_ = st.Objective("v")
	j := &Fake{Answers: map[string]Answer{"a": {Noul: 0.8}}}
	qs := map[string]Question{
		"a": Noul("?"),
		"b": Noul("?"),
	}
	answers, _ := AskAll(context.Background(), j, st, qs)
	if len(answers) != 2 {
		t.Fatalf("got %d answers, want 2", len(answers))
	}
	if answers["b"].Answered {
		t.Fatal("an omitted answer came back answered")
	}
	if answers["b"].Noul != 0 || answers["a"].Noul != 0.8 {
		t.Fatalf("answers = %+v", answers)
	}
}

func TestMalformedQuestionIsRefusedNotSent(t *testing.T) {
	st := NewState(RedactStrict)
	_ = st.Objective("v")
	j := &Fake{Answers: map[string]Answer{"q": {Choice: "a", Confidence: 1}}}
	_, note := AskAll(context.Background(), j, st, map[string]Question{
		"q": Choice("?", map[string]string{"only": "one"}),
	})
	if note.Source != SourceRefused {
		t.Fatalf("source = %q, want %q", note.Source, SourceRefused)
	}
	if len(j.Calls()) != 0 {
		t.Fatal("a malformed question reached the judge")
	}
}

// --- the egress gate -------------------------------------------------------

func TestStrictModeRefusesAllRepositoryContent(t *testing.T) {
	st := NewState(RedactStrict)
	err := st.RepoText("body", "internal/task/runner.go", "func Run() {}")
	if err == nil {
		t.Fatal("strict mode accepted repository content")
	}
	if !strings.Contains(err.Error(), "repo_text") {
		t.Fatalf("the error does not say how to permit it: %v", err)
	}
	if st.RepoTextFields() != 0 {
		t.Fatal("a refused field was stored anyway")
	}
}

func TestRepoRefusesSensitivePaths(t *testing.T) {
	// The same paths internal/policy keeps out of the local model's context.
	// A third-party host is strictly more exposed, so the list is a floor.
	for _, path := range []string{
		".env", ".env.local", "deploy/tls.pem", "certs/server.key",
		"config/credentials.json", ".agent/secrets/token",
		".git/config", ".bc/workspace.yaml", ".agent/memory.json",
	} {
		t.Run(path, func(t *testing.T) {
			st := NewState(RedactRepoText)
			if err := st.RepoText("body", path, "harmless"); err == nil {
				t.Fatalf("%s was accepted", path)
			}
			if st.RepoTextFields() != 0 {
				t.Fatal("a refused field was stored")
			}
		})
	}
}

func TestRepoRefusesCredentialsWithoutEchoingThem(t *testing.T) {
	const secret = "AKIAIOSFODNN7EXAMPLE"
	st := NewState(RedactRepoText)
	err := st.RepoText("body", "internal/x.go", "const key = \""+secret+"\"")
	if err == nil {
		t.Fatal("a credential pattern was accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error repeated the credential: %v", err)
	}
	if !strings.Contains(err.Error(), "body") {
		t.Fatalf("the error does not name the field: %v", err)
	}
}

func TestFactIsAlsoCheckedForCredentials(t *testing.T) {
	st := NewState(RedactStrict)
	if err := st.TrustedFact("headline", "token ghp_"+strings.Repeat("a", 36)+" rejected"); err == nil {
		t.Fatal("a credential in a fact was accepted")
	}
}

func TestRepoNeutralisesFenceMarkers(t *testing.T) {
	st := NewState(RedactRepoText)
	if err := st.RepoText("body", "docs/x.md", "text <<<UNTRUSTED nonce more"); err != nil {
		t.Fatal(err)
	}
	body, err := st.canonical()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<<<UNTRUSTED") {
		t.Fatalf("a marker survived into the payload: %s", body)
	}
}

func TestStateRefusesOversizeFieldAndState(t *testing.T) {
	st := NewState(RedactRepoText)
	if err := st.RepoText("big", "a.go", strings.Repeat("x", MaxFieldBytes+1)); err == nil {
		t.Fatal("an oversize field was accepted")
	}
	full := NewState(RedactRepoText)
	for i := 0; i < 16; i++ {
		_ = full.RepoText(string(rune('a'+i))+"f", "a.go", strings.Repeat("x", MaxFieldBytes-1))
	}
	if _, err := full.Payload(); err == nil {
		t.Fatal("an oversize state was accepted")
	}
}

func TestStateRefusesWritesAfterSending(t *testing.T) {
	st := NewState(RedactStrict)
	_ = st.Objective("1")
	if _, err := st.Payload(); err != nil {
		t.Fatal(err)
	}
	if err := st.TrustedFact("b", "2"); err == nil {
		t.Fatal("a field was added after the state was sent")
	}
}

func TestStateRefusesDuplicateKeys(t *testing.T) {
	st := NewState(RedactStrict)
	_ = st.Objective("1")
	if err := st.TrustedFact("objective", "2"); err == nil {
		t.Fatal("a key was silently overwritten")
	}
}

func TestEmptyStateIsNotAsked(t *testing.T) {
	j := &Fake{}
	_, note := AskAll(context.Background(), j, NewState(RedactStrict),
		map[string]Question{"q": Noul("?")})
	if note.Source != SourceError {
		t.Fatalf("source = %q", note.Source)
	}
	if len(j.Calls()) != 0 {
		t.Fatal("an empty state was sent")
	}
}

// --- configuration ---------------------------------------------------------

func TestAbsentConfigIsDisabledNotAnError(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("an absent %s was an error: %v", ConfigFile, err)
	}
	if cfg.Enabled {
		t.Fatal("the default configuration is enabled")
	}
	if cfg.Redact != RedactStrict {
		t.Fatalf("default redact = %q, want %q", cfg.Redact, RedactStrict)
	}
}

func TestUnknownConfigKeyIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "enabled: true\nredakt: strict\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("a misspelled key was accepted; a security setting that silently does nothing")
	}
}

// An enabled integration with no key exported builds, and the judge it builds
// is unavailable.
//
// This test used to assert that New refused. That refusal was the defect: New
// is reached by every command that opens a data directory, so a key that was
// not exported stopped `bcode eval` during configuration loading even for arms
// that never consult the judge. The credential is required at the boundary
// that builds a request instead; internal/judgment/credential_test.go covers
// that boundary, and the Available() assertion here is what keeps the rerank
// arm from running without its reranker.
func TestEnabledWithoutKeyBuildsButIsUnavailable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Model, cfg.APIKeyEnv = true, "jev-2026-05-01", "BC_TEST_ABSENT_KEY"
	os.Unsetenv("BC_TEST_ABSENT_KEY")
	j, err := New(cfg, Deps{})
	if err != nil {
		t.Fatalf("an enabled judge with no key was refused at construction: %v", err)
	}
	if j.Available() {
		t.Fatal("a judge with no key reported itself available")
	}
	if err := cfg.RequireCredential(); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("the credential boundary did not refuse: %v", err)
	}
}

func TestUnpinnedModelIsDetectable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Model = "jev-latest"
	if cfg.PinnedModel() {
		t.Fatal("an alias was reported as pinned")
	}
	cfg.Model = "jev-2026-05-01"
	if !cfg.PinnedModel() {
		t.Fatal("a dated snapshot was reported as unpinned")
	}
}

// --- the wire --------------------------------------------------------------

func TestWireShape(t *testing.T) {
	var got wireRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"model":"jev-2026-05-01","answers":{
			"rel":{"type":"noul","noul":0.91},
			"pick":{"type":"choice","choice":"b","confidence":0.8,"probabilities":{"a":0.2,"b":0.8}}},
			"usage":{"input_tokens":12,"output_tokens":3}}`))
	}))
	defer srv.Close()

	t.Setenv("BC_TEST_JUDGMENT_KEY", "sekret")
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Model, cfg.Endpoint = true, "jev-2026-05-01", srv.URL
	cfg.APIKeyEnv, cfg.Cache = "BC_TEST_JUDGMENT_KEY", false
	j, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}

	st := NewState(RedactStrict)
	_ = st.Objective("fix the limit")
	answers, note := AskAll(context.Background(), j, st, map[string]Question{
		"rel":  Noul("Relevant?"),
		"pick": Choice("Which?", map[string]string{"a": "A", "b": "B"}),
	})

	if auth != "Bearer sekret" {
		t.Fatalf("Authorization = %q", auth)
	}
	if got.Model != "jev-2026-05-01" {
		t.Fatalf("model = %q", got.Model)
	}
	if got.Questions["rel"].Type != "noul" || got.Questions["pick"].Type != "choice" {
		t.Fatalf("question types = %+v", got.Questions)
	}
	if !strings.Contains(string(got.State), "fix the limit") {
		t.Fatalf("state = %s", got.State)
	}
	if answers["rel"].Noul != 0.91 || !answers["rel"].Answered {
		t.Fatalf("noul = %+v", answers["rel"])
	}
	if answers["pick"].Choice != "b" || answers["pick"].Confidence != 0.8 {
		t.Fatalf("choice = %+v", answers["pick"])
	}
	if note.Usage.InputTokens != 12 {
		t.Fatalf("usage = %+v", note.Usage)
	}
}

func TestMissingProbabilityIsUnansweredNotZero(t *testing.T) {
	// A noul of zero means "certainly not". A response that omitted the field
	// means nothing at all, and confusing the two would make every dropped
	// answer read as a confident refusal.
	a := convert(KindNoul, wireAnswer{Type: "noul"})
	if a.Answered {
		t.Fatal("an omitted noul came back answered")
	}
	p := 0.0
	a = convert(KindNoul, wireAnswer{Type: "noul", Noul: &p})
	if !a.Answered || a.Noul != 0 {
		t.Fatalf("an explicit zero was not preserved: %+v", a)
	}
}

func TestNon200IsAFailureThatDoesNotEchoTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"echo":"the whole request including repository text"}`))
	}))
	defer srv.Close()

	t.Setenv("BC_TEST_JUDGMENT_KEY", "k")
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Model, cfg.Endpoint = true, "jev-2026-05-01", srv.URL
	cfg.APIKeyEnv, cfg.Cache = "BC_TEST_JUDGMENT_KEY", false
	j, _ := New(cfg, Deps{})

	st := NewState(RedactStrict)
	_ = st.Objective("x")
	_, note := AskAll(context.Background(), j, st, map[string]Question{
		"q": Noul("?"),
	})
	if note.Source != SourceUnavailable {
		t.Fatalf("source = %q", note.Source)
	}
	if strings.Contains(note.Detail, "repository text") {
		t.Fatalf("the error echoed the response body: %s", note.Detail)
	}
}

func TestJournalRecordsIntentBeforeTheRequestLeaves(t *testing.T) {
	var order []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "request")
		_, _ = w.Write([]byte(`{"answers":{"q":{"type":"noul","noul":0.5}},"usage":{}}`))
	}))
	defer srv.Close()

	var intent JournalIntent
	rec := recorderFunc(func(_ context.Context, in JournalIntent) (func(JournalOutcome), error) {
		order = append(order, "intent")
		intent = in
		return func(JournalOutcome) { order = append(order, "outcome") }, nil
	})

	t.Setenv("BC_TEST_JUDGMENT_KEY", "k")
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Model, cfg.Endpoint = true, "jev-2026-05-01", srv.URL
	cfg.APIKeyEnv, cfg.Cache = "BC_TEST_JUDGMENT_KEY", false
	j, _ := New(cfg, Deps{Recorder: rec})

	st := NewState(RedactStrict)
	_ = st.Objective("the objective text")
	_, _ = AskAll(context.Background(), j, st, map[string]Question{"q": Noul("?")})

	if strings.Join(order, ",") != "intent,request,outcome" {
		t.Fatalf("order = %v; the intent must be journalled before the request leaves", order)
	}
	if intent.StateDigest == "" || len(intent.StateDigest) != 64 {
		t.Fatalf("state digest = %q", intent.StateDigest)
	}
	if strings.Contains(intent.StateDigest, "objective") {
		t.Fatal("the journal carries the state rather than a digest of it")
	}
	if intent.RedactMode != string(RedactStrict) || intent.RepoText != 0 {
		t.Fatalf("intent = %+v", intent)
	}
}

type recorderFunc func(context.Context, JournalIntent) (func(JournalOutcome), error)

func (f recorderFunc) BeginJudgment(ctx context.Context, in JournalIntent) (func(JournalOutcome), error) {
	return f(ctx, in)
}

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- wire shape, all three primitives --------------------------------------

func TestWireShapeForEveryPrimitive(t *testing.T) {
	var got wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{
			"plain":{"type":"noul","noul":0.4},
			"explained":{"type":"noul","noul":0.7},
			"pick":{"type":"choice","choice":"b","confidence":0.8},
			"rate":{"type":"score","score":1.6,"confidence":0.9}},
			"usage":{"input_tokens":5,"output_tokens":1}}`))
	}))
	defer srv.Close()

	t.Setenv("BC_TEST_JUDGMENT_KEY", "sekret")
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Model, cfg.Endpoint = true, "jev-1.13.0", srv.URL
	cfg.APIKeyEnv, cfg.Cache = "BC_TEST_JUDGMENT_KEY", false
	j, err := New(cfg, Deps{})
	if err != nil {
		t.Fatal(err)
	}

	st := NewState(RedactStrict)
	if err := st.Objective("make the limit configurable"); err != nil {
		t.Fatal(err)
	}
	answers, _ := AskAll(context.Background(), j, st, map[string]Question{
		"plain":     Noul("Considering candidate `c0`: is it relevant?"),
		"explained": NoulTrueFalse("Is it urgent?", "it blocks work", "it can wait"),
		"pick":      Choice("Which team?", map[string]string{"a": "Billing", "b": "Platform"}),
		"rate":      Score("How severe?", []string{"cosmetic", "annoying", "breaking"}),
	})

	// Noul with no criteria must send none at all. TypeSafe documents the
	// field as an optional true/false pair; anything else is a shape the
	// service was never promised.
	if c := got.Questions["plain"].Criteria; c != nil {
		t.Errorf("a plain noul sent criteria %v; the proposition belongs in instructions", c)
	}
	if !strings.Contains(got.Questions["plain"].Instructions, "`c0`") {
		t.Error("the candidate is not named in the noul's instructions")
	}
	if got.Questions["plain"].Type != "noul" {
		t.Errorf("plain type = %q", got.Questions["plain"].Type)
	}

	explained, ok := got.Questions["explained"].Criteria.(map[string]any)
	if !ok || len(explained) != 2 || explained["true"] == nil || explained["false"] == nil {
		t.Errorf("noul criteria = %v, want exactly a true/false pair", got.Questions["explained"].Criteria)
	}

	if got.Questions["pick"].Type != "choice" {
		t.Errorf("choice type = %q", got.Questions["pick"].Type)
	}
	if opts, ok := got.Questions["pick"].Criteria.(map[string]any); !ok || len(opts) != 2 {
		t.Errorf("choice criteria = %v", got.Questions["pick"].Criteria)
	}

	if got.Questions["rate"].Type != "score" {
		t.Errorf("score type = %q", got.Questions["rate"].Type)
	}
	levels, ok := got.Questions["rate"].Criteria.([]any)
	if !ok || len(levels) != 3 {
		t.Errorf("score criteria = %v, want three ordered levels", got.Questions["rate"].Criteria)
	}

	if answers["plain"].Noul != 0.4 || answers["pick"].Choice != "b" || answers["rate"].Score != 1.6 {
		t.Errorf("answers = %+v", answers)
	}
}

// Every malformed combination the constructors cannot prevent must be refused
// before a request is built.
func TestMalformedPrimitivesNeverReachTheNetwork(t *testing.T) {
	for name, q := range map[string]Question{
		"noul with no proposition":   Noul("  "),
		"noul with half a pair":      NoulTrueFalse("?", "yes", ""),
		"choice with one option":     Choice("?", map[string]string{"only": "one"}),
		"choice with a blank option": Choice("?", map[string]string{"a": "A", "": "B"}),
		"score with one level":       Score("?", []string{"only"}),
		"score with a blank level":   Score("?", []string{"low", ""}),
		"the zero question":          {},
	} {
		t.Run(name, func(t *testing.T) {
			if err := q.Validate(); err == nil {
				t.Fatal("accepted")
			}
			j := &Fake{}
			st := NewState(RedactStrict)
			_ = st.Objective("x")
			_, note := AskAll(context.Background(), j, st, map[string]Question{"q": q})
			if len(j.Calls()) != 0 {
				t.Fatal("it reached the judge")
			}
			if note.Source != SourceRefused {
				t.Fatalf("source = %q", note.Source)
			}
		})
	}
}

// A noul whose criteria are exactly a true/false pair is the one valid form.
func TestNoulTrueFalseIsValid(t *testing.T) {
	if err := NoulTrueFalse("?", "yes means", "no means").Validate(); err != nil {
		t.Fatal(err)
	}
	if err := Noul("?").Validate(); err != nil {
		t.Fatal(err)
	}
}

// --- journalling ------------------------------------------------------------

// A cache hit is a decision the run relied on, so it appears in the trace. The
// earlier version returned before journalling, which meant a second run of the
// same task showed no judgments at all.
func TestCacheHitsAreJournalled(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"answers":{"q":{"type":"noul","noul":0.5}},"usage":{}}`))
	}))
	defer srv.Close()

	var sources []string
	rec := recorderFunc(func(_ context.Context, in JournalIntent) (func(JournalOutcome), error) {
		return func(out JournalOutcome) { sources = append(sources, out.Source) }, nil
	})
	cache := memCache{}

	t.Setenv("BC_TEST_JUDGMENT_KEY", "k")
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Model, cfg.Endpoint = true, "jev-1.13.0", srv.URL
	cfg.APIKeyEnv, cfg.Cache = "BC_TEST_JUDGMENT_KEY", true
	j, _ := New(cfg, Deps{Recorder: rec, Cache: cache})

	ask := func() {
		st := NewState(RedactStrict)
		_ = st.Objective("the same question twice")
		_, _ = AskAll(judgmentCtx(t), j, st, map[string]Question{"q": Noul("?")})
	}
	ask()
	ask()

	if calls != 1 {
		t.Fatalf("%d network call(s); the second should have hit the cache", calls)
	}
	if len(sources) != 2 {
		t.Fatalf("journalled %d entries, want 2: %v", len(sources), sources)
	}
	if sources[0] != string(SourceLive) || sources[1] != string(SourceCache) {
		t.Fatalf("sources = %v, want [live cache]", sources)
	}
}

// A judgment declined locally is a local event worth seeing in a trace: "no
// judgment was made here" and "one was made and came back unhelpful" lead an
// operator to different places.
func TestPreflightRefusalsAreJournalled(t *testing.T) {
	var got JournalOutcome
	var phase string
	rec := recorderFunc(func(_ context.Context, in JournalIntent) (func(JournalOutcome), error) {
		phase = in.Phase
		if !in.Refused {
			t.Error("a refusal was journalled without the refused marker")
		}
		return func(out JournalOutcome) { got = out }, nil
	})
	t.Setenv("BC_TEST_JUDGMENT_KEY", "k")
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Model, cfg.Cache = true, "jev-1.13.0", false
	cfg.APIKeyEnv = "BC_TEST_JUDGMENT_KEY"
	j, _ := New(cfg, Deps{Recorder: rec})

	st := NewState(RedactStrict)
	_ = st.Objective("x")
	ctx := WithPhase(context.Background(), "LOCALIZE")
	_, _ = AskAll(ctx, j, st, map[string]Question{"q": Choice("?", map[string]string{"a": "A"})})

	if got.Source != string(SourceRefused) {
		t.Fatalf("outcome source = %q", got.Source)
	}
	if phase != "LOCALIZE" {
		t.Fatalf("phase = %q; it was always empty before", phase)
	}
}

// The served model is recorded beside the requested one, so a benchmark can
// prove which model produced its numbers.
func TestServedModelIsJournalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"q":{"type":"noul","noul":0.5}},"usage":{}}`))
	}))
	defer srv.Close()

	var got JournalOutcome
	var requested string
	rec := recorderFunc(func(_ context.Context, in JournalIntent) (func(JournalOutcome), error) {
		requested = in.Model
		return func(out JournalOutcome) { got = out }, nil
	})
	t.Setenv("BC_TEST_JUDGMENT_KEY", "k")
	cfg := DefaultConfig()
	cfg.Enabled, cfg.Model, cfg.Endpoint = true, "jev-1.13.0", srv.URL
	cfg.APIKeyEnv, cfg.Cache = "BC_TEST_JUDGMENT_KEY", false
	j, _ := New(cfg, Deps{Recorder: rec})

	st := NewState(RedactStrict)
	_ = st.Objective("x")
	_, _ = AskAll(context.Background(), j, st, map[string]Question{"q": Noul("?")})

	if requested != "jev-1.13.0" || got.ModelServed != "jev-1.13.0" {
		t.Fatalf("requested %q, served %q", requested, got.ModelServed)
	}
}

type memCache map[string][]byte

func (m memCache) GetJudgment(key string) ([]byte, bool) { b, ok := m[key]; return b, ok }
func (m memCache) PutJudgment(key string, body []byte)   { m[key] = body }

func judgmentCtx(t *testing.T) context.Context {
	t.Helper()
	return WithTaskID(context.Background(), "t1")
}
