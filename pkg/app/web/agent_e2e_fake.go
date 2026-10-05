//go:build e2efake

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
)

func init() {
	agentHarnessOverride = newAgentE2EHarnesses
}

func newAgentE2EHarnesses() map[harness.Kind]harness.Harness {
	return map[harness.Kind]harness.Harness{
		harness.KindCodex:  &agentE2EHarness{kind: harness.KindCodex},
		harness.KindClaude: &agentE2EHarness{kind: harness.KindClaude},
	}
}

type agentE2EHarness struct {
	mu      sync.Mutex
	kind    harness.Kind
	started int
}

func (h *agentE2EHarness) Status(context.Context) (harness.Status, error) {
	return harness.Status{
		Kind: h.kind, Installed: true, LoggedIn: true, Version: "e2e-fake", Account: "fixture",
		Models: []harness.ModelOption{{ID: "fake", DisplayName: "Fake", Efforts: []string{"low"}, Default: true}},
		Capabilities: harness.Capabilities{
			SupportsAllowedTools: true, SupportsAllowForSession: true,
			PermissionModes: []harness.PermissionMode{harness.PermissionApprovalRequired},
		},
	}, nil
}

func (h *agentE2EHarness) StartSession(context.Context, harness.SessionOptions) (harness.Session, error) {
	h.mu.Lock()
	h.started++
	id := fmt.Sprintf("e2e-%s-%d", h.kind, h.started)
	h.mu.Unlock()
	return newAgentE2ESession(id), nil
}

func (h *agentE2EHarness) Generate(context.Context, harness.GenerateRequest) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

type agentE2EApproval struct {
	requestID string
	decision  harness.Decision
}

type agentE2ESession struct {
	mu        sync.Mutex
	id        string
	fake      *harnesstest.Session
	approvals chan agentE2EApproval
	interrupt chan struct{}
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
	turn      int
	active    int
	stopping  bool
}

func newAgentE2ESession(id string) *agentE2ESession {
	fake := harnesstest.NewSession()
	fake.SessionID = id
	return &agentE2ESession{
		id: id, fake: fake, approvals: make(chan agentE2EApproval, 4),
		interrupt: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}
}

func (s *agentE2ESession) ID() string { return s.id }

func (s *agentE2ESession) SendTurn(ctx context.Context, _ string) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return context.Canceled
	}
	s.active++
	s.turn++
	turnID := fmt.Sprintf("turn-%d", s.turn)
	requestID := fmt.Sprintf("approval-%d", s.turn)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		if s.stopping && s.active == 0 {
			close(s.done)
		}
		s.mu.Unlock()
	}()
	s.fake.Emit(harness.Event{Kind: harness.EventTurnStarted, SessionID: s.id, TurnID: turnID})
	s.fake.Emit(harness.Event{
		Kind: harness.EventApprovalRequested, SessionID: s.id, TurnID: turnID, RequestID: requestID,
		Name: "Run command", Command: "printf pong", Reason: "The fake harness needs approval to respond.",
		Input: json.RawMessage(`{"command":"printf pong"}`),
	})
	select {
	case response := <-s.approvals:
		if response.requestID != requestID || response.decision == harness.DecisionDeny {
			s.fake.Emit(harness.Event{Kind: harness.EventTurnCompleted, SessionID: s.id, TurnID: turnID, Status: "declined"})
			return nil
		}
		s.fake.Emit(harness.Event{Kind: harness.EventCommandExecution, SessionID: s.id, TurnID: turnID, ItemID: "command-1", Command: "printf pong", Phase: harness.PhaseStarted})
		exitCode := 0
		s.fake.Emit(harness.Event{Kind: harness.EventCommandExecution, SessionID: s.id, TurnID: turnID, ItemID: "command-1", Command: "printf pong", Phase: harness.PhaseCompleted, ExitCode: &exitCode, Output: json.RawMessage(`"pong"`)})
		s.fake.Emit(harness.Event{Kind: harness.EventAssistantTextDelta, SessionID: s.id, TurnID: turnID, Text: "pong"})
		s.fake.Emit(harness.Event{Kind: harness.EventTokenUsage, SessionID: s.id, TurnID: turnID, Usage: harness.TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}})
		s.fake.Emit(harness.Event{Kind: harness.EventTurnCompleted, SessionID: s.id, TurnID: turnID})
		return nil
	case <-s.interrupt:
		s.fake.Emit(harness.Event{Kind: harness.EventTurnCompleted, SessionID: s.id, TurnID: turnID, Status: "interrupted"})
		return nil
	case <-s.stop:
		return context.Canceled
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *agentE2ESession) Interrupt(ctx context.Context) error {
	if err := s.fake.Interrupt(ctx); err != nil {
		return err
	}
	select {
	case s.interrupt <- struct{}{}:
	default:
	}
	return nil
}

func (s *agentE2ESession) Respond(requestID string, decision harness.Decision) error {
	if err := s.fake.Respond(requestID, decision); err != nil {
		return err
	}
	s.approvals <- agentE2EApproval{requestID: requestID, decision: decision}
	return nil
}

func (s *agentE2ESession) Events() <-chan harness.Event { return s.fake.Events() }

func (s *agentE2ESession) Stop() error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopping = true
		close(s.stop)
		if s.active == 0 {
			close(s.done)
		}
		s.mu.Unlock()
		<-s.done
		_ = s.fake.Stop()
	})
	return nil
}
