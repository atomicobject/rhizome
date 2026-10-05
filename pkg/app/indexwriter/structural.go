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
type queueStructuralFinalize struct {
	barrier uint64
	work    StructuralFinalize
	ack     chan error
}

func (q *Writer) SubmitStructuralFinalize(ctx context.Context, work StructuralFinalize) error {
	ack := make(chan error, 1)
	work.ChangedCodePaths = append([]string(nil), work.ChangedCodePaths...)
	work.ChangedCallerPaths = append([]string(nil), work.ChangedCallerPaths...)
	work.AffectedAnchorIDs = append([]int64(nil), work.AffectedAnchorIDs...)
	cloned := anchors.DefDeltaAccumulator{}
	cloned.Add(work.DefDeltas)
	work.DefDeltas = cloned.Finalize()
	if err := q.sendControl(ctx, queueStructuralFinalize{barrier: q.currentSubmitBarrier(), work: work, ack: ack}); err != nil {
		return err
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-q.doneCh:
		select {
		case err := <-ack:
			return err
		default:
		}
		if err := q.Err(); err != nil {
			return err
		}
		return fmt.Errorf("structural finalization queue stopped")
	}
}
