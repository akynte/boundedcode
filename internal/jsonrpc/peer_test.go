package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

func pair(t *testing.T) (*Peer, *Peer) {
	t.Helper()
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	a, b := New(ar, aw), New(br, bw)
	t.Cleanup(func() { aw.Close(); bw.Close() })
	return a, b
}

func TestCallsBothWaysAndNotify(t *testing.T) {
	a, b := pair(t)
	b.Handle("add", func(_ context.Context, p json.RawMessage) (any, error) {
		var v struct{ X, Y int }
		if err := json.Unmarshal(p, &v); err != nil {
			return nil, err
		}
		return v.X + v.Y, nil
	})
	a.Handle("fail", func(context.Context, json.RawMessage) (any, error) {
		return nil, &Error{Code: 7, Message: "custom"}
	})
	got := make(chan string, 1)
	a.OnNotify(func(m string, _ json.RawMessage) { got <- m })
	go a.Serve()
	go b.Serve()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var sum int
	if err := a.Call(ctx, "add", map[string]int{"X": 2, "Y": 40}, &sum); err != nil || sum != 42 {
		t.Fatalf("sum=%d err=%v", sum, err)
	}
	err := b.Call(ctx, "fail", nil, nil)
	var re *Error
	if !errors.As(err, &re) || re.Code != 7 {
		t.Fatalf("err = %v", err)
	}
	if err := b.Call(ctx, "nope", nil, nil); err == nil {
		t.Fatal("expected unknown method error")
	}
	if err := b.Notify("event", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m != "event" {
			t.Fatal(m)
		}
	case <-ctx.Done():
		t.Fatal("notification not received")
	}
}

func TestPendingCallsFailOnClose(t *testing.T) {
	ar, bw := io.Pipe()
	drain, aw := io.Pipe()
	a := New(ar, aw)
	go a.Serve()
	go func() { _, _ = io.Copy(io.Discard, drain) }()
	errc := make(chan error, 1)
	go func() { errc <- a.Call(context.Background(), "x", nil, nil) }()
	time.Sleep(50 * time.Millisecond)
	bw.Close() // EOF on a's reader
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("call not released on close")
	}
}
