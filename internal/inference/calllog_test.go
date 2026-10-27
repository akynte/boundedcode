package inference

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/store"
)

// The gateway's call log and the ledger carry what a benchmark attributes
// time and tokens to: time to the first token, the server's prefill and
// decode times, cached and reasoning tokens.
func TestGatewayCallLog(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := sseServer(t, http.StatusOK)
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m", DB: st.DB, TaskID: "t1", Source: "test", KeepLog: true}
	if _, _, err := g.Stream(ctx, "/v1/chat/completions", map[string]any{"messages": []any{}}, func(Delta) {}); err != nil {
		t.Fatal(err)
	}

	// A provider that reports reasoning tokens, not streamed.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":50,"completion_tokens":30,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":12}}}`)
	}))
	t.Cleanup(plain.Close)
	g.Client = NewClient(plain.URL, 0)
	if _, _, err := g.Forward(ctx, "/v1/chat/completions", map[string]any{"messages": []any{}}); err != nil {
		t.Fatal(err)
	}

	// An answer that is only a tool call still has a first token.
	tools := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c","type":"function","function":{"name":"ls","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3}}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(tools.Close)
	g.Client = NewClient(tools.URL, 0)
	passed := 0
	if _, _, err := g.Stream(ctx, "/v1/chat/completions", map[string]any{"messages": []any{}}, func(Delta) { passed++ }); err != nil {
		t.Fatal(err)
	}

	log := g.CallLog()
	if len(log) != 3 {
		t.Fatalf("calls: %+v", log)
	}
	s := log[0]
	if !s.Streamed || s.TTFTMS <= 0 || s.TTFTMS > s.TotalMS || s.Prompt != 120 || s.Cached != 100 || s.Completion != 9 || s.ReasoningText != "think" || s.Reasoning != 0 || s.Status != "ok" {
		t.Fatalf("streamed call: %+v", s)
	}
	if f := log[1]; f.Streamed || f.TTFTMS != 0 || f.Reasoning != 12 || f.ReasoningSource != "provider" || f.Cached != 40 {
		t.Fatalf("reported reasoning: %+v", f)
	}
	if tc := log[2]; tc.TTFTMS <= 0 || passed != 0 {
		t.Fatalf("tool-call answer: %+v, %d deltas passed on", tc, passed)
	}

	var reasoning int
	var ttft float64
	if err := st.DB.QueryRowContext(ctx, `SELECT SUM(reasoning_tokens), MAX(ttft_ms) FROM model_calls WHERE task_id = 't1'`).Scan(&reasoning, &ttft); err != nil {
		t.Fatal(err)
	}
	if reasoning != 12 || ttft <= 0 {
		t.Fatalf("ledger: reasoning %d ttft %v", reasoning, ttft)
	}

	// Without KeepLog nothing is kept in memory.
	g2 := &Gateway{Client: NewClient(plain.URL, 0), Model: "m"}
	_, _, _ = g2.Forward(ctx, "/v1/chat/completions", map[string]any{"messages": []any{}})
	if len(g2.CallLog()) != 0 {
		t.Fatal("call log kept without KeepLog")
	}
}

// A call with a purpose is recorded under it; a purpose from the sandbox
// that is not a plain word is not written to the ledger as given.
func TestGatewayPurpose(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`)
	}))
	t.Cleanup(srv.Close)
	g := &Gateway{Client: NewClient(srv.URL, 0), Model: "m", DB: st.DB, TaskID: "t", Source: "agent", KeepLog: true}
	for _, p := range []string{"", "condenser", "'; DROP TABLE x; --"} {
		if _, _, err := g.ForwardFor(ctx, p, "/v1/chat/completions", map[string]any{"messages": []any{}}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := st.DB.QueryContext(ctx, `SELECT source FROM model_calls ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		got = append(got, s)
	}
	rows.Close()
	if strings.Join(got, ",") != "agent,agent:condenser,agent:other" {
		t.Fatalf("sources = %v", got)
	}
	if log := g.CallLog(); log[1].Purpose != "condenser" {
		t.Fatalf("call log: %+v", log)
	}
}
