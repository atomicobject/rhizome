package indexwriter

import (
	"context"
	"fmt"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
)

type queueDerived struct {
	barrier     uint64
	scopes      []anchors.DerivedScope
	work        []anchors.DerivedWork
	activate    bool
	acknowledge bool
	ack         chan derivedAck
}
type derivedAck struct {
	work []anchors.DerivedWork
	err  error
}

// SubmitDerivedDirty is an ordered control, not a batched payload. Its
// returned generations are durable before any following ownership control.
func (q *Writer) SubmitDerivedDirty(ctx context.Context, scopes []anchors.DerivedScope) ([]anchors.DerivedWork, error) {
	if len(scopes) == 0 {
		return nil, nil
	}
	ack := make(chan derivedAck, 1)
	cmd := queueDerived{barrier: q.currentSubmitBarrier(), scopes: append([]anchors.DerivedScope(nil), scopes...), ack: ack}
	if err := q.sendControl(ctx, cmd); err != nil {
		return nil, err
	}
	return q.waitDerived(ctx, ack)
}

// SubmitActivateDerived makes exact generations available for provider work
// only after previously submitted structural writes have become durable.
func (q *Writer) SubmitActivateDerived(ctx context.Context, work []anchors.DerivedWork) error {
	if len(work) == 0 {
		return nil
	}
	ack := make(chan derivedAck, 1)
	cmd := queueDerived{barrier: q.currentSubmitBarrier(), work: append([]anchors.DerivedWork(nil), work...), activate: true, ack: ack}
	if err := q.sendControl(ctx, cmd); err != nil {
		return err
	}
	_, err := q.waitDerived(ctx, ack)
	return err
}

// SubmitDerivedAcknowledgement clears only exact completed provider generations.
// It drains destination writes first so a failed publication cannot lose debt.
func (q *Writer) SubmitDerivedAcknowledgement(ctx context.Context, work []anchors.DerivedWork) error {
	if len(work) == 0 {
		return nil
	}
	ack := make(chan derivedAck, 1)
	cmd := queueDerived{barrier: q.currentSubmitBarrier(), work: append([]anchors.DerivedWork(nil), work...), acknowledge: true, ack: ack}
	if err := q.sendControl(ctx, cmd); err != nil {
		return err
	}
	_, err := q.waitDerived(ctx, ack)
	return err
}

func (q *Writer) waitDerived(ctx context.Context, ack <-chan derivedAck) ([]anchors.DerivedWork, error) {
	select {
	case result := <-ack:
		return result.work, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-q.doneCh:
		select {
		case result := <-ack:
			return result.work, result.err
		default:
		}
		if err := q.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("derived control queue stopped")
	}
}
func (q *Writer) applyDerived(cmd *queueDerived) derivedAck {
	ctx := ctxWithPhaseOp(q.ctx, indexQueueDefaultPhaseLabel, "intel.derived_work")
	if cmd.acknowledge {
		if q.handlers.AckDerivedWork == nil {
			return derivedAck{err: fmt.Errorf("derived acknowledgement queue not initialized")}
		}
		return derivedAck{err: q.handlers.AckDerivedWork(ctx, cmd.work)}
	}
	if cmd.activate {
		if q.handlers.ActivateDerivedWork == nil {
			return derivedAck{err: fmt.Errorf("derived activation queue not initialized")}
		}
		return derivedAck{err: q.handlers.ActivateDerivedWork(ctx, cmd.work)}
	}
	if q.handlers.MarkDerivedDirty == nil {
		return derivedAck{err: fmt.Errorf("derived dirty queue not initialized")}
	}
	work, err := q.handlers.MarkDerivedDirty(ctx, cmd.scopes)
	return derivedAck{work: work, err: err}
}
