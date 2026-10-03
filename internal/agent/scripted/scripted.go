// Package scripted is a deterministic agent.Runtime for tests and dry runs:
// each Send applies the next scripted step to the workspace.
package scripted

import (
	"context"
	"fmt"
	"sync"

	"github.com/akynte/boundedcode/internal/agent"
)

// Step mutates the workspace and returns the agent's final message.
type Step func(workspace, message string) (string, error)

// Runtime replays Steps in order across sessions (resumes continue the script).
type Runtime struct {
	mu       sync.Mutex
	Steps    []Step
	next     int
	Messages []string // every message received, for assertions
	Opens    int
	Resumes  int
	Requests []agent.OpenRequest
}

// Name implements agent.Runtime.
func (r *Runtime) Name() string { return "scripted" }

// Open implements agent.Runtime.
func (r *Runtime) Open(_ context.Context, req agent.OpenRequest) (agent.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Opens++
	r.Requests = append(r.Requests, req)
	id, resumed := req.SessionID, req.SessionID != ""
	if resumed {
		r.Resumes++
	} else {
		id = fmt.Sprintf("scripted-%d", r.Opens)
	}
	return &session{rt: r, id: id, resumed: resumed, ws: req.Workspace}, nil
}

type session struct {
	rt      *Runtime
	id      string
	resumed bool
	ws      string
}

func (s *session) ID() string    { return s.id }
func (s *session) Resumed() bool { return s.resumed }

func (s *session) Send(_ context.Context, message string) (agent.Result, error) {
	s.rt.mu.Lock()
	s.rt.Messages = append(s.rt.Messages, message)
	if s.rt.next >= len(s.rt.Steps) {
		s.rt.mu.Unlock()
		return agent.Result{Status: "finished", FinalMessage: "no more scripted steps"}, nil
	}
	step := s.rt.Steps[s.rt.next]
	s.rt.next++
	s.rt.mu.Unlock()
	final, err := step(s.ws, message)
	if err != nil {
		// A failing step is an agent outcome, not a transport error.
		return agent.Result{Status: "error", Error: err.Error()}, nil //nolint:nilerr // see above
	}
	return agent.Result{Status: "finished", FinalMessage: final, EventsNew: 3}, nil
}

func (s *session) Condense(context.Context) error  { return nil }
func (s *session) Interrupt(context.Context) error { return nil }
func (s *session) State(context.Context) (agent.State, error) {
	return agent.State{Status: "idle", ConversationID: s.id}, nil
}
func (s *session) Close() error { return nil }
