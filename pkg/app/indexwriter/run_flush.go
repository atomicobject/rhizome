package indexwriter

import "time"

type queueState struct {
	kinds          []payloadKind
	pendingCtrls   []pendingControl
	holdingForCtrl bool
	drainingToCtrl bool
	lastCodeAt     time.Time
}

func (s *queueState) shouldFlushNow(rows, bytes int, policy flushPolicy, allowDuringHold bool, allowDuringDrain bool) bool {
	if s.holdingForCtrl && !allowDuringHold || s.drainingToCtrl && !allowDuringDrain {
		return false
	}
	return rows >= policy.Rows || bytes >= policy.Bytes
}
func (s *queueState) hasPendingPayloads() bool {
	for _, kind := range s.kinds {
		if kind.pending() > 0 {
			return true
		}
	}
	return false
}
func (s *queueState) flushAll(q *Writer) error {
	for _, kind := range s.kinds {
		if err := kind.flush(q, time.Time{}, false); err != nil {
			return err
		}
	}
	q.snapshotQueueDepths(q.ctx)
	return nil
}
func (s *queueState) flushExpired(q *Writer, now time.Time) error {
	codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, s.kinds[0].pending())
	for _, kind := range s.kinds {
		if err := kind.flush(q, now, codeHot); err != nil {
			return err
		}
	}
	return nil
}
