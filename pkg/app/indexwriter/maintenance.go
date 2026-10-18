package indexwriter

import "context"

// SubmitMaintenance runs post-index database upkeep after earlier writes drain.
// The callback uses the writer's context and completes before the next write.
func (q *Writer) SubmitMaintenance(ctx context.Context, run func(context.Context) error) error {
	return submitErrorControl(ctx, q, func(q *Writer) error { return run(q.ctx) })
}
