package codeintel

import (
	"context"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

const asyncSemanticSubmitterBuffer = 256

type AsyncCodeSemanticSubmitter struct {
	ctx       context.Context
	submitter CodeSemanticBatchSubmitter
	fail      func(error)

	ch        chan codeanchor.CodeIndexWork
	closeOnce sync.Once
	wg        sync.WaitGroup

	mu  sync.Mutex
	err error
}

func NewAsyncCodeSemanticSubmitter(
	ctx context.Context,
	submitter CodeSemanticBatchSubmitter,
	fail func(error),
) *AsyncCodeSemanticSubmitter {
	if submitter == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s := &AsyncCodeSemanticSubmitter{
		ctx:       ctx,
		submitter: submitter,
		fail:      fail,
		ch:        make(chan codeanchor.CodeIndexWork, asyncSemanticSubmitterBuffer),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *AsyncCodeSemanticSubmitter) SubmitPreparedCodeBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	if s == nil || len(batch) == 0 {
		return nil
	}
	for _, work := range batch {
		if err := s.SubmitCodeIndexWork(ctx, work); err != nil {
			return err
		}
	}
	return nil
}

func (s *AsyncCodeSemanticSubmitter) SubmitCodeIndexWork(ctx context.Context, work codeanchor.CodeIndexWork) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Err(); err != nil {
		return err
	}
	select {
	case <-s.ctx.Done():
		if err := s.Err(); err != nil {
			return err
		}
		return s.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case s.ch <- work:
		indexingperf.SetGauge(ctx, "codeindex.semantic_queue_depth", int64(len(s.ch)))
		return nil
	}
}

func (s *AsyncCodeSemanticSubmitter) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		close(s.ch)
	})
	s.wg.Wait()
	return s.Err()
}

func (s *AsyncCodeSemanticSubmitter) Err() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *AsyncCodeSemanticSubmitter) run() {
	defer s.wg.Done()

	batch := make([]codeanchor.CodeIndexWork, 0, indexWriterBatchCap)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timerActive := false

	stopTimer := func() {
		if !timerActive {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerActive = false
	}
	defer stopTimer()

	resetTimer := func() {
		stopTimer()
		timer.Reset(indexWriterFlushIdle)
		timerActive = true
	}

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		payload := append([]codeanchor.CodeIndexWork(nil), batch...)
		batch = batch[:0]
		return s.submitter.SubmitPreparedCodeBatch(s.ctx, payload)
	}

	fail := func(err error) {
		if err == nil {
			return
		}
		s.setErr(err)
		if s.fail != nil {
			s.fail(err)
		}
	}

	for {
		var timerCh <-chan time.Time
		if timerActive {
			timerCh = timer.C
		}
		select {
		case <-s.ctx.Done():
			return
		case work, ok := <-s.ch:
			if !ok {
				if err := flush(); err != nil {
					fail(err)
				}
				return
			}
			batch = append(batch, work)
			if len(batch) >= indexWriterBatchCap {
				stopTimer()
				if err := flush(); err != nil {
					fail(err)
					return
				}
				continue
			}
			resetTimer()
		case <-timerCh:
			timerActive = false
			if err := flush(); err != nil {
				fail(err)
				return
			}
		}
	}
}

func (s *AsyncCodeSemanticSubmitter) setErr(err error) {
	if err == nil || s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}
