// Package jsonrpc implements a symmetric, newline-delimited JSON-RPC 2.0 peer
// over a byte stream (ADR-0004). Either side may issue requests.
package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Handler serves an incoming request. Returning an *Error controls the code.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// NotifyHandler receives an incoming notification.
type NotifyHandler func(method string, params json.RawMessage)

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// ErrClosed is returned for calls on a closed peer.
var ErrClosed = errors.New("jsonrpc: peer closed")

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// envelope is the lenient first-pass decoding of an incoming line: fields
// that may be malformed stay raw so a response with a usable id can still
// fail its call instead of leaving it waiting forever.
type envelope struct {
	ID     json.RawMessage `json:"id"`
	Method json.RawMessage `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// ErrMalformed wraps a response that matched a pending call but could not
// be decoded.
var ErrMalformed = errors.New("jsonrpc: malformed response")

type response struct {
	result json.RawMessage
	err    error
}

// Peer is one end of the connection.
type Peer struct {
	r        io.Reader
	w        io.Writer
	wmu      sync.Mutex
	nextID   atomic.Int64
	mu       sync.Mutex
	pending  map[int64]chan response
	closed   bool // set under mu once pending calls have been released
	handlers map[string]Handler
	notify   NotifyHandler
	done     chan struct{}
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	log      *slog.Logger
}

// New creates a peer. Register handlers before calling Serve.
func New(r io.Reader, w io.Writer) *Peer {
	ctx, cancel := context.WithCancel(context.Background())
	return &Peer{r: r, w: w, pending: map[int64]chan response{}, handlers: map[string]Handler{},
		done: make(chan struct{}), ctx: ctx, cancel: cancel, log: slog.New(slog.DiscardHandler)}
}

// SetLogger receives debug diagnostics about dropped input. Call before Serve.
func (p *Peer) SetLogger(l *slog.Logger) {
	if l != nil {
		p.log = l
	}
}

// Handle registers a request handler.
func (p *Peer) Handle(method string, h Handler) { p.handlers[method] = h }

// OnNotify sets the notification handler (called synchronously, in order).
func (p *Peer) OnNotify(h NotifyHandler) { p.notify = h }

// Done is closed when the stream ends.
func (p *Peer) Done() <-chan struct{} { return p.done }

// Err returns the read error that ended the stream (nil on clean EOF).
func (p *Peer) Err() error {
	<-p.done
	return p.err
}

// Serve reads until EOF or error. In-flight request handlers are cancelled
// and awaited before Serve returns, so no goroutines outlive it.
func (p *Peer) Serve() {
	defer func() {
		p.cancel()
		p.wg.Wait()
		p.mu.Lock()
		p.closed = true
		for id, ch := range p.pending {
			ch <- response{err: ErrClosed}
			delete(p.pending, id)
		}
		p.mu.Unlock()
		close(p.done)
	}()
	sc := bufio.NewScanner(p.r)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		if line := sc.Bytes(); len(line) > 0 {
			p.dispatch(line)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		p.err = err
	}
}

// dispatch handles one input line. Garbage is tolerated (the adapter logs to
// stderr, never to the protocol stream) but reported at debug level.
func (p *Peer) dispatch(line []byte) {
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		p.log.Debug("jsonrpc: dropping undecodable line", "err", err, "line", clip(line))
		return
	}
	var id *int64
	if len(e.ID) > 0 && string(e.ID) != "null" {
		var n int64
		if err := json.Unmarshal(e.ID, &n); err != nil {
			p.log.Debug("jsonrpc: dropping message with non-integer id", "id", clip(e.ID))
			return
		}
		id = &n
	}
	if len(e.Method) > 0 && string(e.Method) != "null" {
		var method string
		if err := json.Unmarshal(e.Method, &method); err != nil || method == "" {
			p.log.Debug("jsonrpc: dropping message with invalid method", "method", clip(e.Method))
			if id != nil {
				p.wg.Add(1)
				go func() {
					defer p.wg.Done()
					_ = p.write(message{ID: id, Error: &Error{Code: -32600, Message: "invalid method"}})
				}()
			}
			return
		}
		if id != nil {
			p.wg.Add(1)
			go p.serveRequest(message{ID: id, Method: method, Params: e.Params})
		} else if p.notify != nil {
			p.notify(method, e.Params)
		}
		return
	}
	if id == nil {
		p.log.Debug("jsonrpc: dropping message without id or method", "line", clip(line))
		return
	}
	p.mu.Lock()
	ch, ok := p.pending[*id]
	delete(p.pending, *id)
	p.mu.Unlock()
	if !ok {
		p.log.Debug("jsonrpc: dropping response for unknown id", "id", *id)
		return
	}
	switch {
	case len(e.Error) > 0 && string(e.Error) != "null":
		var re Error
		if err := json.Unmarshal(e.Error, &re); err != nil {
			ch <- response{err: fmt.Errorf("%w: error object: %w", ErrMalformed, err)}
		} else {
			ch <- response{err: &re}
		}
	default:
		ch <- response{result: e.Result}
	}
}

func clip(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}

func (p *Peer) serveRequest(m message) {
	defer p.wg.Done()
	h, ok := p.handlers[m.Method]
	reply := message{JSONRPC: "2.0", ID: m.ID}
	if !ok {
		reply.Error = &Error{Code: -32601, Message: "unknown method " + m.Method}
	} else {
		res, err := h(p.ctx, m.Params)
		if err != nil {
			var re *Error
			if errors.As(err, &re) {
				reply.Error = re
			} else {
				reply.Error = &Error{Code: -32000, Message: err.Error()}
			}
		} else {
			b, err := json.Marshal(res)
			if err != nil {
				reply.Error = &Error{Code: -32603, Message: "encode result: " + err.Error()}
			} else {
				reply.Result = b
			}
		}
	}
	_ = p.write(reply)
}

func (p *Peer) write(m message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err = p.w.Write(b)
	return err
}

// Call sends a request and waits for the response, decoding into out (may be nil).
func (p *Peer) Call(ctx context.Context, method string, params, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	id := p.nextID.Add(1)
	ch := make(chan response, 1)
	// Register and check for shutdown atomically: Serve drains pending under
	// the same lock, so a call can never slip in after the drain and hang.
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	p.pending[id] = ch
	p.mu.Unlock()
	if err := p.write(message{ID: &id, Method: method, Params: raw}); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return fmt.Errorf("jsonrpc write %s: %w", method, err)
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if out != nil && len(r.result) > 0 {
			if err := json.Unmarshal(r.result, out); err != nil {
				return fmt.Errorf("%w: %s result: %w", ErrMalformed, method, err)
			}
		}
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return ctx.Err()
	}
}

// Notify sends a notification.
func (p *Peer) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return p.write(message{Method: method, Params: raw})
}
