package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func (s *session) Stop() error {
	s.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		s.stateMu.Lock()
		s.stopped = true
		pending := make([]pendingApproval, 0, len(s.pending))
		for id, approval := range s.pending {
			pending = append(pending, approval)
			delete(s.pending, id)
		}
		s.stateMu.Unlock()
		denialCtx, stopDenials := context.WithTimeout(ctx, s.shutdownTimeout/2)
		for _, approval := range pending {
			if denialCtx.Err() != nil {
				break
			}
			_ = s.sendApprovalDuringStop(denialCtx, approval)
		}
		stopDenials()
		s.cancel()
		s.closeErr = s.transport.Close(ctx)
		waitReader(ctx, &s.reader)
		s.stream.Close()
	})
	return s.closeErr
}

func (s *session) fail(err error) {
	s.stateMu.Lock()
	if !s.stopped {
		s.stopped = true
		s.terminalErr = err
	}
	done, turnID, sessionID := s.turnDone, s.turnID, s.threadID
	clear(s.pending)
	s.stateMu.Unlock()
	if done != nil {
		select {
		case done <- harness.Phase(harness.ErrTransportClosed, err):
		default:
		}
	}
	s.stream.Emit(harness.Event{Kind: harness.EventError, Phase: harness.PhaseFailed, SessionID: sessionID, TurnID: turnID, Text: err.Error()})
	s.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	_ = s.transport.Close(ctx)
	cancel()
	s.stream.Abort()
}

func (s *session) notificationIsStale(message rpcMessage) bool {
	method := message.Method
	if method != "turn/started" && method != "turn/completed" && method != "error" && !strings.HasPrefix(method, "item/") {
		return false
	}
	var params notificationEnvelope
	if json.Unmarshal(message.Params, &params) != nil {
		return false
	}
	turnID := params.TurnID
	if turnID == "" {
		turnID = params.Turn.ID
	}
	s.stateMu.Lock()
	running, current, last := s.running, s.turnID, s.lastTurnID
	s.stateMu.Unlock()
	if !running {
		return method != "error" || turnID != ""
	}
	if turnID == "" {
		return false
	}
	if current == "" {
		// The turn/start response may not have been processed yet; only a
		// notification for the previous turn is known to be stale.
		return turnID == last
	}
	return turnID != current
}

// preferTerminal replaces a bare closed-transport error with the cause the
// read loop recorded (decode failure, protocol error) when one exists.
func (s *session) preferTerminal(err error) error {
	if !errors.Is(err, harness.ErrTransportClosed) {
		return err
	}
	s.stateMu.Lock()
	terminal := s.terminalErr
	s.stateMu.Unlock()
	if terminal == nil {
		return err
	}
	return harness.Phase(harness.ErrTransportClosed, terminal)
}

func (s *session) transportError() error {
	s.stateMu.Lock()
	err := s.terminalErr
	s.stateMu.Unlock()
	if err == nil {
		err = errors.New("session stopped")
	}
	return harness.Phase(harness.ErrTransportClosed, err)
}

func waitReader(ctx context.Context, reader interface{ Wait() }) {
	done := make(chan struct{})
	go func() {
		reader.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
