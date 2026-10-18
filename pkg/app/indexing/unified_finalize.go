package indexing

import (
	"context"
	"errors"
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type unifiedBatchFinalizer struct {
	writeQueue             *indexwriter.Writer
	asyncSemanticSubmitter *codeintel.AsyncCodeSemanticSubmitter
	streaming              *unifiedStreamingCoordinator
	semanticCancel         context.CancelFunc
	semanticResources      *unifiedSemanticResources
	waitFinalOntology      func() error

	closeAsyncOnce     sync.Once
	closeAsyncErr      error
	closeStreamingOnce sync.Once
	closeStreamingErr  error
	waitOntologyOnce   sync.Once
	waitOntologyErr    error
	detachWriterOnce   sync.Once
	closeWriterOnce    sync.Once
	closeWriterErr     error
	cancelSemanticOnce sync.Once
	closeSemanticOnce  sync.Once
	writerClosed       bool
}

func (f *unifiedBatchFinalizer) CloseAsyncSemanticSubmitter() error {
	if f == nil {
		return nil
	}
	f.closeAsyncOnce.Do(func() {
		if f.asyncSemanticSubmitter != nil {
			f.closeAsyncErr = f.asyncSemanticSubmitter.Close()
		}
	})
	return f.closeAsyncErr
}

func (f *unifiedBatchFinalizer) FlushQueue(ctx context.Context) error {
	if f == nil || f.writeQueue == nil || f.writerClosed {
		return nil
	}
	return f.writeQueue.FlushAndWait(ctx)
}

func (f *unifiedBatchFinalizer) CloseStreaming() error {
	if f == nil {
		return nil
	}
	f.closeStreamingOnce.Do(func() {
		if f.streaming != nil {
			f.closeStreamingErr = f.streaming.CloseAndWait()
		}
	})
	return f.closeStreamingErr
}

// DrainStreamingWrites orders body writes before catalog replacement on success.
// Cancellation abandons queued work; durable obligations retain recovery debt.
func (f *unifiedBatchFinalizer) DrainStreamingWrites(ctx context.Context) (err error) {
	done := indexingperf.StartSpan(ctx, "streaming_durability_barrier")
	defer func() { done(err) }()
	if err := f.CloseStreaming(); err != nil {
		return err
	}
	if ctx == nil {
		return f.FlushQueue(ctx)
	}
	return errors.Join(f.FlushQueue(ctx), ctx.Err())
}

func (f *unifiedBatchFinalizer) WaitFinalOntology() error {
	if f == nil {
		return nil
	}
	f.waitOntologyOnce.Do(func() {
		if f.waitFinalOntology != nil {
			f.waitOntologyErr = f.waitFinalOntology()
		}
	})
	return f.waitOntologyErr
}

func (f *unifiedBatchFinalizer) DetachWriterCallbacks() {
	if f == nil {
		return
	}
	f.detachWriterOnce.Do(func() {
		if f.writeQueue != nil {
			f.writeQueue.SetAfterCodeIndexBatch(nil)
		}
	})
}

func (f *unifiedBatchFinalizer) CloseWriter() error {
	if f == nil {
		return nil
	}
	f.closeWriterOnce.Do(func() {
		f.DetachWriterCallbacks()
		if f.writeQueue != nil {
			f.closeWriterErr = f.writeQueue.Close()
		}
		f.writerClosed = true
	})
	return f.closeWriterErr
}

func (f *unifiedBatchFinalizer) CancelSemanticContext() {
	if f == nil {
		return
	}
	f.cancelSemanticOnce.Do(func() {
		if f.semanticCancel != nil {
			f.semanticCancel()
		}
	})
}

func (f *unifiedBatchFinalizer) CloseSemanticResources() {
	if f == nil {
		return
	}
	f.closeSemanticOnce.Do(func() {
		if f.semanticResources != nil {
			f.semanticResources.close()
		}
	})
}

func (f *unifiedBatchFinalizer) Shutdown(ctx context.Context) error {
	if f == nil {
		return nil
	}
	var err error
	capture := func(next error) {
		if next != nil {
			err = errors.Join(err, next)
		}
	}

	capture(f.CloseAsyncSemanticSubmitter())
	capture(f.FlushQueue(ctx))
	capture(f.CloseStreaming())
	capture(f.WaitFinalOntology())
	capture(f.FlushQueue(ctx))
	f.DetachWriterCallbacks()
	capture(f.CloseWriter())
	f.CancelSemanticContext()
	f.CloseSemanticResources()
	return err
}
