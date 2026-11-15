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
	err := s.persistAndPublish(s.ctx, event)
	if err != nil {
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
	beforeStart := running && !live.turn.started
	if beforeStart {
		live.turn.interrupted = true
		live.turn.cancel()
	}
	s.mu.Unlock()
	if !running {
		return fmt.Errorf("%w: no turn is running", ErrConflict)
	}
	if beforeStart {
		// sendTurn records the interrupt and the turn's end, in that order.
		return nil
	}
	// ponytail: a Stop in the microseconds between started and the harness
	// registering its turn gets the harness's "no running turn" error.
	if err := live.session.Interrupt(ctx); err != nil {
		return err
	}
	event := Event{
		SessionID: sessionID,
		Type:      "interrupt-requested",
		Result:    &EventResult{Status: "requested"},
	}
	err := s.persistAndPublish(s.ctx, event)
	if err != nil {
		s.publishPersistenceError(event, err)
	}
	return err
}

func (s *Service) sendTurn(session Session, live *liveSession, turn *turnState, content string) {
	defer turn.cancel()
	err := live.session.SetModel(turn.ctx, turn.model, turn.effort)
	s.mu.Lock()
	interrupted := turn.interrupted
	turn.started = !interrupted
	s.mu.Unlock()
	if interrupted {
		s.endInterruptedTurn(session.ID, live, turn)
		return
	}
	if err == nil {
		err = live.session.SendTurn(turn.ctx, content)
	}
	if err != nil {
		if s.finishTurn(session.ID, live, turn) && s.ctx.Err() == nil {
			s.persistHarnessError(session.ID, session.Harness, err)
		}
	}
}

// endInterruptedTurn ends a turn stopped before its harness turn started,
// with the events a harness interrupt would leave.
func (s *Service) endInterruptedTurn(sessionID string, live *liveSession, turn *turnState) {
	if !s.finishTurn(sessionID, live, turn) || s.ctx.Err() != nil {
		return
	}
	// Its own turn id, so a client gathering this turn's reply does not
	// collect every earlier turn's text.
	turnID := fmt.Sprintf("stopped-%d", time.Now().UnixNano())
	for _, event := range []Event{
		{SessionID: sessionID, Type: "interrupt-requested", Result: &EventResult{Status: "requested", TurnID: turnID}},
		{SessionID: sessionID, Type: string(harness.EventTurnCompleted), Result: &EventResult{Status: "interrupted", TurnID: turnID}},
	} {
		if err := s.persistAndPublish(s.ctx, event); err != nil {
			s.publishPersistenceError(event, err)
		}
	}
}
