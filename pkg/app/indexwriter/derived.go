package indexwriter

import (
	"context"
	"fmt"
	anchors "github.com/atomicobject/rhizome/pkg/anchors"
)

// SubmitDerivedDirty durably records obligations before ownership changes.
func (q *Writer) SubmitDerivedDirty(ctx context.Context, scopes []anchors.DerivedScope) ([]anchors.DerivedWork, error) {
	if len(scopes) == 0 {
		return nil, nil
	}
	cloned := append([]anchors.DerivedScope(nil), scopes...)
	return submitControl(ctx, q, func(q *Writer) ([]anchors.DerivedWork, error) {
		if q.handlers.MarkDerivedDirty == nil {
			return nil, fmt.Errorf("derived dirty queue not initialized")
		}
		return q.handlers.MarkDerivedDirty(ctxWithPhaseOp(q.ctx, indexQueueDefaultPhaseLabel, "intel.derived_work"), cloned)
	}, false)
}

// SubmitActivateDerived makes exact generations available after structural writes.
func (q *Writer) SubmitActivateDerived(ctx context.Context, work []anchors.DerivedWork) error {
	if len(work) == 0 {
		return nil
	}
	cloned := append([]anchors.DerivedWork(nil), work...)
	return submitErrorControl(ctx, q, func(q *Writer) error {
		if q.handlers.ActivateDerivedWork == nil {
			return fmt.Errorf("derived activation queue not initialized")
		}
		return q.handlers.ActivateDerivedWork(ctxWithPhaseOp(q.ctx, indexQueueDefaultPhaseLabel, "intel.derived_work"), cloned)
	})
}

// SubmitDerivedAcknowledgement clears exact completed generations after writes.
func (q *Writer) SubmitDerivedAcknowledgement(ctx context.Context, work []anchors.DerivedWork) error {
	if len(work) == 0 {
		return nil
	}
	cloned := append([]anchors.DerivedWork(nil), work...)
	return submitErrorControl(ctx, q, func(q *Writer) error {
		if q.handlers.AckDerivedWork == nil {
			return fmt.Errorf("derived acknowledgement queue not initialized")
		}
		return q.handlers.AckDerivedWork(ctxWithPhaseOp(q.ctx, indexQueueDefaultPhaseLabel, "intel.derived_work"), cloned)
	})
}
