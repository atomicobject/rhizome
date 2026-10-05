package dispatch

import "context"

type Operation struct {
	ID                string
	AssignmentVersion int
	TerminalFailure   bool
}

type Queue struct {
	pending     []Operation
	quarantined []Operation
}

func (q *Queue) Enqueue(op Operation) {
	q.pending = append(q.pending, op)
}

func (q *Queue) Drain(ctx context.Context, upload func(context.Context, Operation) error) error {
	for len(q.pending) > 0 {
		op := q.pending[0]
		if op.TerminalFailure {
			q.Quarantine(op)
			q.pending = q.pending[1:]
			continue
		}
		if err := upload(ctx, op); err != nil {
			return err
		}
		q.pending = q.pending[1:]
	}
	return nil
}

func (q *Queue) Quarantine(op Operation) {
	q.quarantined = append(q.quarantined, op)
}
