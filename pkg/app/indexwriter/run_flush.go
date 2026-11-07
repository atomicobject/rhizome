package indexwriter

import "time"

type queueState struct {
	codeKind       payloadKind
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

// flushBefore flushes every kind ahead of k in durable flush order.
func (s *queueState) flushBefore(q *Writer, k payloadKind) error {
	for _, kind := range s.kinds {
		if kind == k {
			return nil
		}
		if err := kind.flush(q, time.Time{}, false); err != nil {
			return err
		}
	}
	return nil
}
func (s *queueState) flushExpired(q *Writer, now time.Time) error {
	codeHot := q.cfg.codePriorityActive(now, s.lastCodeAt, s.codeKind.pending())
	// A kind whose idle timer expired must not commit ahead of pending earlier
	// kinds it may depend on, so those flush in full first.
	last := -1
	for i, kind := range s.kinds {
		if kind.due(now, codeHot) {
			last = i
		}
	}
	for i, kind := range s.kinds {
		at := now
		if i < last {
			at = time.Time{}
		}
		if err := kind.flush(q, at, codeHot); err != nil {
			return err
		}
	}
	return nil
}
