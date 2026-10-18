package indexwriter

import (
	"context"
	"fmt"
)

type controlResult[T any] struct {
	value T
	err   error
}

type pendingControl interface {
	barrier() uint64
	run(*Writer) error
	complete(error)
	closesWriter() bool
}

type orderedControl[T any] struct {
	sequence uint64
	apply    func(*Writer) (T, error)
	result   T
	ack      chan controlResult[T]
	close    bool
}

func (c *orderedControl[T]) barrier() uint64    { return c.sequence }
func (c *orderedControl[T]) closesWriter() bool { return c.close }
func (c *orderedControl[T]) run(q *Writer) error {
	if c.apply == nil {
		return nil
	}
	var err error
	c.result, err = c.apply(q)
	return err
}
func (c *orderedControl[T]) complete(err error) { c.ack <- controlResult[T]{value: c.result, err: err} }

func awaitControl[T any](ctx context.Context, q *Writer, ack <-chan controlResult[T]) (T, error) {
	select {
	case result := <-ack:
		return result.value, result.err
	default:
	}
	var waitErr error
	select {
	case result := <-ack:
		return result.value, result.err
	case <-ctx.Done():
		waitErr = ctx.Err()
	case <-q.doneCh:
		waitErr = q.Err()
		if waitErr == nil {
			waitErr = fmt.Errorf("index write queue stopped before control completed")
		}
	}
	// A committed result takes precedence when acknowledgement and cancellation
	// become ready together, including cancellation immediately after commit.
	select {
	case result := <-ack:
		return result.value, result.err
	default:
	}
	var zero T
	return zero, waitErr
}
func submitControl[T any](ctx context.Context, q *Writer, apply func(*Writer) (T, error), close bool) (T, error) {
	command := &orderedControl[T]{sequence: q.currentSubmitBarrier(), apply: apply, ack: make(chan controlResult[T], 1), close: close}
	if err := q.sendControl(ctx, command); err != nil {
		var zero T
		return zero, err
	}
	return awaitControl(ctx, q, command.ack)
}
func submitErrorControl(ctx context.Context, q *Writer, apply func(*Writer) error) error {
	_, err := submitControl(ctx, q, func(q *Writer) (struct{}, error) { return struct{}{}, apply(q) }, false)
	return err
}
