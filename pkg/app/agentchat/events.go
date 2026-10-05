package agentchat

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/mattn/go-sqlite3"
)

const eventPersistAttempts = 3

func (s *Service) drainEvents(sessionID string, kind harness.Kind, live *liveSession) {
	defer live.drainOnce.Do(func() { close(live.drained) })
	for incoming := range live.session.Events() {
		if incoming.Kind == harness.EventApprovalRequested && incoming.RequestID != "" {
			s.mu.Lock()
			if incoming.Phase == harness.PhaseCancelled || incoming.Phase == harness.PhaseDeclined {
				delete(live.pending, incoming.RequestID)
			} else {
				live.pending[incoming.RequestID] = struct{}{}
			}
			s.mu.Unlock()
		}
		if incoming.SessionID != "" || incoming.TurnID != "" {
			_ = s.store.UpdateSessionHarness(s.ctx, sessionID, kind, incoming.SessionID, incoming.TurnID)
		}
		if incoming.Kind == harness.EventAssistantTextDelta {
			s.mu.Lock()
			if live.turn != nil {
				live.turn.assistant.WriteString(incoming.Text)
			}
			s.mu.Unlock()
		}
		if incoming.Kind == harness.EventTurnCompleted {
			if err := s.finalizeTurn(sessionID, live); err != nil {
				s.persistHarnessError(sessionID, kind, err)
			}
		}
		projected := eventFromHarness(sessionID, incoming)
		if incoming.Kind == harness.EventDiagnostic {
			projected.ID = -s.transientEventID.Add(1)
			projected.CreatedAt = time.Now().UTC()
			s.broker.Publish(projected)
		} else if stored, err := s.addEventWithRetry(s.ctx, projected); err == nil {
			if incoming.Kind == harness.EventAssistantTextDelta {
				stored.Type = "message_delta"
			}
			s.broker.Publish(stored)
		} else {
			s.handleEventPersistenceFailure(sessionID, live, incoming, err)
		}
	}

	s.mu.Lock()
	remaining := live.turn
	if s.live[sessionID] == live {
		delete(s.live, sessionID)
	}
	if remaining != nil {
		live.turn = nil
		live.running = false
		live.pending = make(map[string]struct{})
	}
	s.mu.Unlock()
	if remaining != nil && s.ctx.Err() == nil {
		s.persistHarnessError(sessionID, kind, harness.Phase(harness.ErrTransportClosed, errors.New("harness event stream closed during turn")))
	}
}

func (s *Service) finalizeTurn(sessionID string, live *liveSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if live.turn == nil {
		return nil
	}
	var err error
	if assistant := live.turn.assistant.String(); strings.TrimSpace(assistant) != "" {
		_, err = s.store.AddMessage(s.ctx, sessionID, "assistant", assistant)
	}
	live.turn = nil
	live.running = false
	live.pending = make(map[string]struct{})
	return err
}

func eventFromHarness(sessionID string, event harness.Event) Event {
	role := ""
	if event.Kind == harness.EventAssistantTextDelta {
		role = "assistant"
	}
	return Event{
		SessionID: sessionID,
		Type:      string(event.Kind),
		Role:      role,
		Content:   event.Text,
		ToolName:  event.Name,
		Result: &EventResult{
			TurnID: event.TurnID, ItemID: event.ItemID, RequestID: event.RequestID,
			Name: event.Name, Command: event.Command, Cwd: event.Cwd, Paths: event.Paths,
			Status: event.Status, Phase: event.Phase, Reason: event.Reason, AllowForSession: event.AllowForSession,
			Input: event.Input, Output: event.Output, Truncated: event.Truncated,
			ExitCode: event.ExitCode, Diff: event.Diff, Usage: EventUsage{
				InputTokens: event.Usage.InputTokens, CachedInputTokens: event.Usage.CachedInputTokens,
				OutputTokens: event.Usage.OutputTokens, ReasoningOutputTokens: event.Usage.ReasoningOutputTokens,
				TotalTokens: event.Usage.TotalTokens,
			},
		},
	}
}

func (s *Service) finishTurn(live *liveSession, turn *turnState) bool {
	s.mu.Lock()
	finished := false
	if live.turn == turn {
		live.turn = nil
		live.running = false
		live.pending = make(map[string]struct{})
		finished = true
	}
	s.mu.Unlock()
	return finished
}

func (s *Service) persistHarnessError(sessionID string, kind harness.Kind, err error) {
	phase, hint := harnessErrorDetails(err, kind, s.cachedHarnessStatus(kind))
	event := Event{
		SessionID: sessionID,
		Type:      string(harness.EventError),
		Content:   hint,
		Error:     err.Error(),
		Result:    &EventResult{Status: phase, Reason: hint},
	}
	stored, storeErr := s.addEventWithRetry(s.ctx, event)
	if storeErr != nil {
		s.publishPersistenceError(event, storeErr)
	} else {
		s.broker.Publish(stored)
	}
}

func (s *Service) addEventWithRetry(ctx context.Context, event Event) (Event, error) {
	s.eventWriteMu.Lock()
	defer s.eventWriteMu.Unlock()
	var err error
	for attempt := 1; attempt <= eventPersistAttempts; attempt++ {
		var stored Event
		stored, err = s.store.AddEvent(ctx, event)
		if err == nil {
			return stored, nil
		}
		log.Printf("agent chat: persist event session_id=%q type=%q attempt=%d/%d: %v", event.SessionID, event.Type, attempt, eventPersistAttempts, err)
		if attempt == eventPersistAttempts || !isTransientSQLiteError(err) {
			break
		}
		timer := time.NewTimer(time.Duration(attempt) * 25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Event{}, ctx.Err()
		case <-timer.C:
		}
	}
	return Event{}, err
}

func isTransientSQLiteError(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked)
}

func (s *Service) handleEventPersistenceFailure(sessionID string, live *liveSession, incoming harness.Event, err error) {
	if incoming.Kind == harness.EventApprovalRequested && incoming.RequestID != "" {
		if respondErr := live.session.Respond(incoming.RequestID, harness.DecisionDeny); respondErr != nil {
			log.Printf("agent chat: deny unpersisted approval session_id=%q request_id=%q: %v", sessionID, incoming.RequestID, respondErr)
		}
		s.mu.Lock()
		delete(live.pending, incoming.RequestID)
		s.mu.Unlock()
	}
	s.publishPersistenceError(eventFromHarness(sessionID, incoming), err)
}

func (s *Service) publishPersistenceError(failed Event, err error) {
	reason := fmt.Sprintf("Could not save %s event: %v", failed.Type, err)
	s.broker.Publish(Event{
		ID: -s.transientEventID.Add(1), SessionID: failed.SessionID,
		Type: string(harness.EventError), Content: reason, Error: err.Error(), CreatedAt: time.Now().UTC(),
		Result: &EventResult{Status: "persistence", Reason: reason},
	})
}

func harnessErrorDetails(err error, kind harness.Kind, status harness.Status) (string, string) {
	phase := "unknown"
	var phaseErr *harness.PhaseError
	if errors.As(err, &phaseErr) && phaseErr.Phase != nil {
		phase = strings.TrimPrefix(phaseErr.Phase.Error(), "harness ")
	}
	switch {
	case errors.Is(err, harness.ErrNotInstalled):
		return phase, "Install the " + string(kind) + " CLI and try again."
	case errors.Is(err, harness.ErrNotLoggedIn):
		if strings.TrimSpace(status.LoginHint) != "" {
			return phase, status.LoginHint
		}
		return phase, "Log in to the " + string(kind) + " CLI and try again."
	default:
		return phase, "The " + string(kind) + " harness failed during " + phase + "."
	}
}
