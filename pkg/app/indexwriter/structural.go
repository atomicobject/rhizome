package indexwriter

import (
	"context"
	"fmt"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
)

// StructuralFinalize contains only structural dependency work. It is an
// ordered control because publication must observe its committed rows before
// provider tasks can prepare their call-sensitive inputs.
type StructuralFinalize struct {
	ChangedCodePaths   []string
	ChangedCallerPaths []string
	DefDeltas          anchors.DefDeltas
	AffectedAnchorIDs  []int64
	Complete           bool
	RecomputeScopes    bool
}

func (q *Writer) SubmitStructuralFinalize(ctx context.Context, work StructuralFinalize) error {
	work.ChangedCodePaths = append([]string(nil), work.ChangedCodePaths...)
	work.ChangedCallerPaths = append([]string(nil), work.ChangedCallerPaths...)
	work.AffectedAnchorIDs = append([]int64(nil), work.AffectedAnchorIDs...)
	cloned := anchors.DefDeltaAccumulator{}
	cloned.Add(work.DefDeltas)
	work.DefDeltas = cloned.Finalize()
	return submitErrorControl(ctx, q, func(q *Writer) error {
		if q.handlers.ApplyStructuralFinalize == nil {
			return fmt.Errorf("structural finalization queue not initialized")
		}
		return q.handlers.ApplyStructuralFinalize(q.ctx, work)
	})
}

// SubmitReverseIndexReset invalidates stale versions before source ingestion.
func (q *Writer) SubmitReverseIndexReset(ctx context.Context) error {
	return submitErrorControl(ctx, q, func(q *Writer) error {
		if q.handlers.ResetReverseIndex == nil {
			return fmt.Errorf("reverse index reset queue not initialized")
		}
		return q.handlers.ResetReverseIndex(q.ctx)
	})
}
