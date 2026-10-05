package claude

import (
	"context"
	"errors"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func (s *session) Stop() error {
	s.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		s.stateMu.Lock()
		s.stopped = true
		pending := s.pending
		s.pending = make(map[string]pendingApproval)
		s.stateMu.Unlock()
		denialCtx, stopDenials := context.WithTimeout(ctx, s.shutdownTimeout/2)
		for id, approval := range pending {
			if denialCtx.Err() != nil {
				break
			}
			_ = s.sendApprovalDuringStop(denialCtx, id, approval.request)
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
		s.stopped, s.terminalErr = true, err
	}
	done, turnID, sessionID := s.turnDone, s.turnID, s.sessionID
	s.pending = make(map[string]pendingApproval)
	s.stateMu.Unlock()
	if done != nil {
		signalTurn(done, harness.Phase(harness.ErrTransportClosed, err))
	}
	s.stream.Emit(harness.Event{Kind: harness.EventError, Phase: harness.PhaseFailed, SessionID: sessionID, TurnID: turnID, Text: err.Error()})
	s.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	_ = s.transport.Close(ctx)
	cancel()
	s.stream.Abort()
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
