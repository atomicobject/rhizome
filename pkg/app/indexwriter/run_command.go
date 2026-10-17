package indexwriter

import (
	"fmt"
	"time"
)

func (s *queueState) handleCommand(q *Writer, raw any) bool {
	switch cmd := raw.(type) {
	case queuedPayload:
		cmd.value = normalizePayload(cmd.value)
		for _, kind := range s.kinds {
			matched, err := kind.offer(q, s, cmd, time.Now())
			if err != nil {
				q.fail(err)
				return false
			}
			if matched {
				q.snapshotQueueDepths(q.ctx)
				return true
			}
		}
		q.fail(fmt.Errorf("unsupported queue payload %T", cmd.value))
		return false
	case pendingControl:
		s.pendingCtrls = append(s.pendingCtrls, cmd)
		return true
	default:
		q.fail(fmt.Errorf("unsupported queue command %T", raw))
		return false
	}
}
