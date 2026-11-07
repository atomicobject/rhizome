package agentchat

import (
	"context"
	"fmt"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func (s *Service) RespondToApproval(sessionID, requestID string, decision harness.Decision) error {
	switch decision {
	case harness.DecisionAllow, harness.DecisionDeny, harness.DecisionAllowForSession:
	default:
		return fmt.Errorf("invalid approval decision %q", decision)
	}
	// Claim the request before answering it, so a turn ending meanwhile does
	// not also record it as cancelled and a second answer gets ErrNotFound.
	s.mu.Lock()
	live := s.live[sessionID]
	pending := false
	var turn *turnState
	if live != nil {
		if _, pending = live.pending[requestID]; pending {
			delete(live.pending, requestID)
			turn = live.turn
		}
	}
	s.mu.Unlock()
	if live == nil || !pending {
		return ErrNotFound
	}
	if err := live.session.Respond(requestID, decision); err != nil {
		s.mu.Lock()
		restored := live.turn == turn
		if restored {
			live.pending[requestID] = struct{}{}
		}
		s.mu.Unlock()
		if !restored {
			s.cancelApprovals(sessionID, map[string]struct{}{requestID: {}})
		}
		return err
	}
	event := Event{
		SessionID: sessionID,
		Type:      "approval-decision",
		Result:    &EventResult{RequestID: requestID, Status: string(decision)},
	}
	stored, err := s.addEventWithRetry(s.ctx, event)
	if err == nil {
		s.broker.Publish(stored)
	} else {
		// The vendor already accepted the decision; publish it transiently so the
		// browser disables the card even though the durable record failed.
		transient := event
		transient.ID = -s.transientEventID.Add(1)
		transient.CreatedAt = time.Now().UTC()
		transient.Result = &EventResult{RequestID: requestID, Status: string(decision), Phase: "unpersisted"}
		s.broker.Publish(transient)
		s.publishPersistenceError(event, err)
	}
	return err
}

func (s *Service) Interrupt(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	live := s.live[sessionID]
	running := live != nil && live.running
	s.mu.Unlock()
	if !running {
		return fmt.Errorf("%w: no turn is running", ErrConflict)
	}
	if err := live.session.Interrupt(ctx); err != nil {
		return err
	}
	event := Event{
		SessionID: sessionID,
		Type:      "interrupt-requested",
		Result:    &EventResult{Status: "requested"},
	}
	stored, err := s.addEventWithRetry(s.ctx, event)
	if err == nil {
		s.broker.Publish(stored)
	} else {
		s.publishPersistenceError(event, err)
	}
	return err
}
