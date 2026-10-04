package jsonrpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
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

// rawPeer returns a served peer, the writer feeding its input, and a scanner
// over its output lines.
func rawPeer(t *testing.T) (p *Peer, in *io.PipeWriter, out *bufio.Scanner) {
	t.Helper()
	pr, pw := io.Pipe()
	or, ow := io.Pipe()
	p = New(pr, ow)
	t.Cleanup(func() { pw.Close(); or.Close() })
	go p.Serve()
	return p, pw, bufio.NewScanner(or)
}

// startCall issues a call in the background and returns the id it was sent
// with (read from the output stream) and the eventual error.
func startCall(t *testing.T, p *Peer, out *bufio.Scanner, ctx context.Context, v any) (int64, <-chan error) {
	t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- p.Call(ctx, "m", nil, v) }()
	if !out.Scan() {
		t.Fatal("request not written")
	}
	var m message
	if err := json.Unmarshal(out.Bytes(), &m); err != nil || m.ID == nil {
		t.Fatalf("bad request line %q: %v", out.Bytes(), err)
	}
	return *m.ID, errc
}

func waitErr(t *testing.T, errc <-chan error) error {
	t.Helper()
	select {
	case err := <-errc:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("call hung")
		return nil
	}
}

func TestGarbageAndUnknownIDAreIgnored(t *testing.T) {
	p, in, out := rawPeer(t)
	var got struct{ OK bool }
	id, errc := startCall(t, p, out, context.Background(), &got)
	for _, l := range []string{
		"not json at all",
		`{"jsonrpc":"2.0","id":"abc","result":1}`,
		`{"jsonrpc":"2.0","id":99999,"result":{"ok":false}}`,
		`{"jsonrpc":"2.0","result":{"ok":false}}`,
		`[1,2,3]`, `null`, `5`, `{"method":7}`,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"OK":true}}`, id),
	} {
		fmt.Fprintln(in, l)
	}
	if err := waitErr(t, errc); err != nil || !got.OK {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestMalformedResponseFailsCall(t *testing.T) {
	cases := map[string]string{
		"result type": `{"jsonrpc":"2.0","id":%d,"result":"not an object"}`,
		"error type":  `{"jsonrpc":"2.0","id":%d,"error":"boom"}`,
	}
	for name, tmpl := range cases {
		t.Run(name, func(t *testing.T) {
			p, in, out := rawPeer(t)
			var v struct{ OK bool }
			id, errc := startCall(t, p, out, context.Background(), &v)
			fmt.Fprintf(in, tmpl+"\n", id)
			if err := waitErr(t, errc); !errors.Is(err, ErrMalformed) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestPartialLineThenEOF(t *testing.T) {
	p, in, out := rawPeer(t)
	id, errc := startCall(t, p, out, context.Background(), nil)
	fmt.Fprintf(in, `{"jsonrpc":"2.0","id":%d,"res`, id)
	in.Close()
	if err := waitErr(t, errc); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	if err := p.Call(context.Background(), "after", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("call after close: %v", err)
	}
}

func TestCallDeadline(t *testing.T) {
	p, _, out := rawPeer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, errc := startCall(t, p, out, ctx, nil)
	if err := waitErr(t, errc); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	p.mu.Lock()
	n := len(p.pending)
	p.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d pending calls leaked", n)
	}
}

// TestPeerProcessExitsMidCall: the remote reads the request and dies without
// answering; the call must be released with ErrClosed.
func TestPeerProcessExitsMidCall(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	cmd := exec.Command("sh", "-c", "read line; exit 3")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := New(stdout, stdin)
	go p.Serve()
	errc := make(chan error, 1)
	go func() { errc <- p.Call(context.Background(), "x", nil, nil) }()
	if err := waitErr(t, errc); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	_ = cmd.Wait()
}

// TestCallRacingClose hammers Call while the stream closes: every call must
// return (none may register after the pending drain and hang).
func TestCallRacingClose(t *testing.T) {
	for range 50 {
		pr, pw := io.Pipe()
		p := New(pr, io.Discard)
		go p.Serve()
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := p.Call(ctx, "x", nil, nil); errors.Is(err, context.DeadlineExceeded) {
					t.Error("call hung across close")
				}
			}()
		}
		pw.Close()
		wg.Wait()
	}
}

func FuzzServe(f *testing.F) {
	for _, s := range []string{
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":1,"message":"x"}}`,
		`{"jsonrpc":"2.0","method":"event","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"m","params":[]}`,
		"garbage\n{\"id\":", `{"id":1e400}`, `{"error":null,"id":1}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p := New(bytes.NewReader(data), io.Discard)
		p.Handle("m", func(context.Context, json.RawMessage) (any, error) { return 1, nil })
		p.OnNotify(func(string, json.RawMessage) {})
		errc := make(chan error, 1)
		go func() { errc <- p.Call(context.Background(), "x", nil, nil) }()
		p.Serve()
		select {
		case <-errc:
		case <-time.After(5 * time.Second):
			t.Fatal("call not released after EOF")
		}
	})
}
