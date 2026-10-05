package indexing

import (
	"context"
	"errors"
	"fmt"
	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

const postCodeSubmitterBuffer = 64

var errPostCodeSubmitterFull = errors.New("post-code semantic submitter queue full")

type asyncCodeBatchSubmitter struct {
	ctx    context.Context
	submit func(context.Context, []codeanchor.CodeIndexWork) error
	fail   func(error)

	ch        chan []codeanchor.CodeIndexWork
	closeOnce sync.Once
	wg        sync.WaitGroup

	mu  sync.Mutex
	err error
}

func newAsyncCodeBatchSubmitter(
	ctx context.Context,
	submit func(context.Context, []codeanchor.CodeIndexWork) error,
	fail func(error),
) *asyncCodeBatchSubmitter {
	if submit == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s := &asyncCodeBatchSubmitter{
		ctx:    ctx,
		submit: submit,
		fail:   fail,
		ch:     make(chan []codeanchor.CodeIndexWork, postCodeSubmitterBuffer),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *asyncCodeBatchSubmitter) run() {
	defer s.wg.Done()
	for batch := range s.ch {
		if len(batch) == 0 {
			continue
		}
		if err := s.submit(s.ctx, batch); err != nil {
			s.setErr(err)
			if s.fail != nil {
				s.fail(err)
			}
			return
		}
	}
}

func (s *asyncCodeBatchSubmitter) Enqueue(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	if s == nil || len(batch) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Err(); err != nil {
		return err
	}
	batchCopy := append([]codeanchor.CodeIndexWork(nil), batch...)
	select {
	case <-s.ctx.Done():
		if err := s.Err(); err != nil {
			return err
		}
		return s.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case s.ch <- batchCopy:
		return nil
	default:
		return fmt.Errorf("%w (depth=%d capacity=%d)", errPostCodeSubmitterFull, len(s.ch), cap(s.ch))
	}
}

func (s *asyncCodeBatchSubmitter) Submit(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	if s == nil || len(batch) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Err(); err != nil {
		return err
	}
	batchCopy := append([]codeanchor.CodeIndexWork(nil), batch...)
	select {
	case <-s.ctx.Done():
		if err := s.Err(); err != nil {
			return err
		}
		return s.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case s.ch <- batchCopy:
		return nil
	}
}

func (s *asyncCodeBatchSubmitter) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		close(s.ch)
	})
	s.wg.Wait()
	return s.Err()
}

func (s *asyncCodeBatchSubmitter) Err() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *asyncCodeBatchSubmitter) setErr(err error) {
	if err == nil || s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}
