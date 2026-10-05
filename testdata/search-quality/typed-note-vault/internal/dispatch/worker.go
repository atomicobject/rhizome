package dispatch

import "context"

type Worker struct {
	Queue *Queue
	Store OperationStore
}

func (w *Worker) SyncTechnician(ctx context.Context, operations []Operation, upload func(context.Context, Operation) error) error {
	for _, op := range operations {
		if err := w.Store.Save(op); err != nil {
			return err
		}
		w.Queue.Enqueue(op)
	}
	return w.Queue.Drain(ctx, upload)
}
