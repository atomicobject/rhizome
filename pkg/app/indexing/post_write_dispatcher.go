package indexing

import (
	"context"
	"errors"
	"fmt"
	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

const postWriteDispatcherBuffer = 128

var errPostWriteDispatcherFull = errors.New("post-write dispatcher queue full")
var errPostWriteDispatcherClosed = errors.New("post-write dispatcher closed")

type postWriteDispatch struct {
	codeBatch      []codeanchor.CodeIndexWork
	noteScopePaths []string
}

type postWriteDispatcher struct {
	ctx           context.Context
	codeSubmitter *asyncCodeBatchSubmitter
	streaming     *unifiedStreamingCoordinator
	fail          func(error)

	ch        chan postWriteDispatch
	closeOnce sync.Once
	wg        sync.WaitGroup

	mu     sync.Mutex
	err    error
	closed bool
}

func newPostWriteDispatcher(
	ctx context.Context,
	codeSubmitter *asyncCodeBatchSubmitter,
	streaming *unifiedStreamingCoordinator,
	fail func(error),
) *postWriteDispatcher {
	if codeSubmitter == nil && streaming == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	d := &postWriteDispatcher{
		ctx:           ctx,
		codeSubmitter: codeSubmitter,
		streaming:     streaming,
		fail:          fail,
		ch:            make(chan postWriteDispatch, postWriteDispatcherBuffer),
	}
	d.wg.Add(1)
	go d.run()
	return d
}

func (d *postWriteDispatcher) run() {
	defer d.wg.Done()
	for item := range d.ch {
		var err error
		switch {
		case len(item.codeBatch) > 0:
			err = d.codeSubmitter.Enqueue(d.ctx, item.codeBatch)
		case len(item.noteScopePaths) > 0:
			err = d.streaming.TrySubmitScopeNotePaths(d.ctx, item.noteScopePaths)
		}
		if err != nil {
			d.setErr(err)
			if d.fail != nil {
				d.fail(err)
			}
			return
		}
	}
}

func (d *postWriteDispatcher) EnqueueCodeBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	if d == nil || len(batch) == 0 {
		return nil
	}
	if err := d.Err(); err != nil {
		return err
	}
	batchCopy := append([]codeanchor.CodeIndexWork(nil), batch...)
	return d.enqueue(ctx, postWriteDispatch{codeBatch: batchCopy})
}

func (d *postWriteDispatcher) EnqueueNoteIndexBatch(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
	if d == nil || len(batch) == 0 {
		return nil
	}
	paths := make([]string, 0, len(batch))
	for _, work := range batch {
		if work.Path != "" {
			paths = append(paths, work.Path)
		}
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	if err := d.Err(); err != nil {
		return err
	}
	return d.enqueue(ctx, postWriteDispatch{noteScopePaths: paths})
}

func (d *postWriteDispatcher) enqueue(ctx context.Context, item postWriteDispatch) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := d.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errPostWriteDispatcherClosed
	}
	select {
	case <-d.ctx.Done():
		if d.err != nil {
			return d.err
		}
		return d.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case d.ch <- item:
		return nil
	default:
		return fmt.Errorf("%w (depth=%d capacity=%d)", errPostWriteDispatcherFull, len(d.ch), cap(d.ch))
	}
}

func (d *postWriteDispatcher) Close() error {
	if d == nil {
		return nil
	}
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		close(d.ch)
		d.mu.Unlock()
	})
	d.wg.Wait()
	return d.Err()
}

func (d *postWriteDispatcher) Err() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

func (d *postWriteDispatcher) setErr(err error) {
	if err == nil || d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err == nil {
		d.err = err
	}
}
