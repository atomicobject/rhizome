package claude

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// turnState is one turn's terminal result. finish records the first result
// and closes done, so SendTurn and every concurrent Interrupt observe it.
type turnState struct {
	done chan struct{}
	once sync.Once
	err  error
}

func (t *turnState) finish(err error) {
	t.once.Do(func() {
		t.err = err
		close(t.done)
	})
}

func (s *session) clearTurn(turn *turnState) {
	s.stateMu.Lock()
	if s.turn == turn {
		s.running, s.started, s.turnID, s.turn = false, false, "", nil
	}
	s.stateMu.Unlock()
	// A turn that ended without a result (a failed send) still releases its
	// interrupt waiters, so none of them times out and stops a later turn.
	turn.finish(nil)
}

func (s *session) cancelTurn(ctxErr error, turn *turnState) error {
	interruptCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = s.sendInterrupt(interruptCtx, turn)
	cancel()
	timer := time.NewTimer(s.cancelTimeout)
	defer timer.Stop()
	select {
	case <-turn.done:
		// EOF fills done before it ends the lifetime; closure must win either way.
		if errors.Is(turn.err, harness.ErrTransportClosed) || s.lifetime.Err() != nil {
			return harness.Phase(harness.ErrTransportClosed, ctxErr)
		}
		return harness.Phase(harness.ErrTimeout, ctxErr)
	case <-s.lifetime.Done():
		return harness.Phase(harness.ErrTransportClosed, ctxErr)
	case <-timer.C:
		_ = s.Stop()
		return harness.Phase(harness.ErrTransportClosed, ctxErr)
	}
}
