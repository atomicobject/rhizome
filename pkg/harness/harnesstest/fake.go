package harnesstest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/atomicobject/rhizome/pkg/harness"
)

type Harness struct {
	StatusResult   harness.Status
	StatusError    error
	Session        *Session
	StartError     error
	GenerateResult json.RawMessage
	GenerateError  error
	StartedWith    []harness.SessionOptions
	GeneratedWith  []harness.GenerateRequest
}

func (h *Harness) Status(context.Context) (harness.Status, error) {
	return h.StatusResult, h.StatusError
}

func (h *Harness) StartSession(_ context.Context, options harness.SessionOptions) (harness.Session, error) {
	h.StartedWith = append(h.StartedWith, options)
	if h.StartError != nil {
		return nil, h.StartError
	}
	if h.Session == nil {
		h.Session = NewSession()
	}
	return h.Session, nil
}

func (h *Harness) Generate(_ context.Context, request harness.GenerateRequest) (json.RawMessage, error) {
	h.GeneratedWith = append(h.GeneratedWith, request)
	return append(json.RawMessage(nil), h.GenerateResult...), h.GenerateError
}

type Turn struct {
	Prompt string
}

type Response struct {
	RequestID string
	Decision  harness.Decision
}

type Session struct {
	mu          sync.Mutex
	events      chan harness.Event
	Turns       []Turn
	Responses   []Response
	Interrupted bool
	Stopped     bool
	SendError   error
	SessionID   string
}

func NewSession() *Session {
	return &Session{events: make(chan harness.Event, 64)}
}

func (s *Session) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SessionID
}

func (s *Session) SendTurn(_ context.Context, prompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Turns = append(s.Turns, Turn{Prompt: prompt})
	return s.SendError
}

func (s *Session) Interrupt(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Interrupted = true
	return nil
}

func (s *Session) Respond(requestID string, decision harness.Decision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Responses = append(s.Responses, Response{RequestID: requestID, Decision: decision})
	return nil
}

func (s *Session) Events() <-chan harness.Event { return s.events }

func (s *Session) Emit(event harness.Event) { s.events <- event }

func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Stopped {
		s.Stopped = true
		close(s.events)
	}
	return nil
}
