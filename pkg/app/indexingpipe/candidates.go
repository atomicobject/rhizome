package indexingpipe

import (
	"context"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// CandidateProcessOptions configures bounded reading and processing of an
// already discovered candidate slice. It intentionally has no discovery or
// classification inputs: callers own those decisions before this stage.
type CandidateProcessOptions struct {
	WorkerCount   int
	QueueCapacity int
	Progress      *ProgressCallbacks
	// OnReadError can persist source-only failure state without a second read.
	// Returning nil retains ProcessFiles' read-failure completion behavior.
	OnReadError func(context.Context, FileCandidate, error) error
}

// ProcessCandidates reads and processes an already-discovered candidate slice
// through a bounded worker queue. Candidates are admitted in input order;
// OnDiscovered runs in that order, while OnCompleted follows worker completion.
//
// A skipped candidate and an unreadable candidate complete without invoking
// process, matching ProcessFiles. A process error cancels the remaining work
// and is returned. OnReadError may handle a read failure without a second
// filesystem read; a handler error cancels and is returned. The input slice
// and its candidate values are never mutated.
func ProcessCandidates(
	ctx context.Context,
	candidates []FileCandidate,
	opts CandidateProcessOptions,
	shouldRead func(FileCandidate) bool,
	onSkip func(FileCandidate),
	process func(FilePayload) error,
) error {
	if process == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	workerCount := opts.WorkerCount
	if workerCount <= 0 {
		workerCount = runtime.GOMAXPROCS(0)
		if workerCount < 1 {
			workerCount = 1
		}
	}
	queueCapacity := opts.QueueCapacity
	if queueCapacity <= 0 {
		queueCapacity = workerCount * 4
	}

	// Candidates move by value through the bounded queue. Only the worker-local
	// value receives queue timing, so this stage adds O(queue+workers) memory and
	// leaves the caller's run-owned slice untouched.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	candidateCh := make(chan FileCandidate, queueCapacity)
	errCh := make(chan error, 1)
	var workers sync.WaitGroup
	var activeWorkers atomic.Int64
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for candidate := range candidateCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				active := activeWorkers.Add(1)
				indexingperf.SetGauge(ctx, "codeindex.active_workers", active)
				workerStarted := time.Now()
				if !candidate.enqueuedAt.IsZero() {
					indexingperf.ObserveLatency(ctx, "codeindex.file_queue_wait", time.Since(candidate.enqueuedAt))
				}

				readStarted := time.Now()
				content, err := os.ReadFile(candidate.AbsPath)
				indexingperf.ObserveLatency(ctx, "fs.read_latency", time.Since(readStarted))
				if err != nil {
					if opts.OnReadError != nil {
						if handlerErr := opts.OnReadError(ctx, candidate, err); handlerErr != nil {
							finishCandidateWorker(ctx, &activeWorkers, candidateCh, workerStarted)
							select {
							case errCh <- handlerErr:
							default:
							}
							cancel()
							return
						}
					}
					finishCandidateWorker(ctx, &activeWorkers, candidateCh, workerStarted)
					reportCandidateCompleted(opts.Progress, candidate)
					continue
				}
				indexingperf.AddCount(ctx, "fs.read", 1)
				indexingperf.AddBytes(ctx, "fs.read", int64(len(content)))

				if err := process(FilePayload{Candidate: candidate, Content: content}); err != nil {
					finishCandidateWorker(ctx, &activeWorkers, candidateCh, workerStarted)
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}

				finishCandidateWorker(ctx, &activeWorkers, candidateCh, workerStarted)
				indexingperf.AddCount(ctx, "fs.completed", 1)
				reportCandidateCompleted(opts.Progress, candidate)
			}
		}()
	}

	var enqueueErr error
enqueue:
	for _, candidate := range candidates {
		select {
		case <-ctx.Done():
			enqueueErr = ctx.Err()
			break enqueue
		default:
		}

		indexingperf.AddCount(ctx, "fs.discovered", 1)
		if opts.Progress != nil && opts.Progress.OnDiscovered != nil {
			opts.Progress.OnDiscovered(candidate)
		}
		if shouldRead != nil && !shouldRead(candidate) {
			if onSkip != nil {
				onSkip(candidate)
			}
			indexingperf.AddCount(ctx, "fs.completed", 1)
			reportCandidateCompleted(opts.Progress, candidate)
			continue
		}

		candidate.enqueuedAt = time.Now()
		enqueueStarted := time.Now()
		select {
		case <-ctx.Done():
			enqueueErr = ctx.Err()
			break enqueue
		case candidateCh <- candidate:
			indexingperf.ObserveLatency(ctx, "codeindex.file_enqueue_wait", time.Since(enqueueStarted))
			indexingperf.SetGauge(ctx, "codeindex.file_queue_depth", int64(len(candidateCh)))
		}
	}
	close(candidateCh)
	workers.Wait()

	select {
	case err := <-errCh:
		return err
	default:
	}
	if enqueueErr != nil {
		return enqueueErr
	}
	return ctx.Err()
}

func finishCandidateWorker(
	ctx context.Context,
	activeWorkers *atomic.Int64,
	candidateCh chan FileCandidate,
	workerStarted time.Time,
) {
	indexingperf.ObserveLatency(ctx, "codeindex.worker_service", time.Since(workerStarted))
	active := activeWorkers.Add(-1)
	indexingperf.SetGauge(ctx, "codeindex.active_workers", active)
	indexingperf.SetGauge(ctx, "codeindex.file_queue_depth", int64(len(candidateCh)))
}

func reportCandidateCompleted(progress *ProgressCallbacks, candidate FileCandidate) {
	if progress != nil && progress.OnCompleted != nil {
		progress.OnCompleted(candidate)
	}
}
