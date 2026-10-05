package semantic

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type preparedPipelineWork interface {
	pipelineProviderCalls() int
}

func runPreparedPipeline[Task any, Prepared preparedPipelineWork, Owner any](
	ctx context.Context,
	tasks []Task,
	totalWork int,
	workerCount int,
	prepareMetric string,
	enqueueMetric string,
	preparedCh chan Prepared,
	runPipeline func(func(Owner) error) error,
	prepare func(context.Context, Task) (Prepared, error),
	finalize func(Owner) error,
	onProgress func(done, total int),
) error {
	if len(tasks) == 0 {
		close(preparedCh)
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}

	var (
		prepWG      sync.WaitGroup
		totalQueued atomic.Int64
		doneQueued  atomic.Int64
		errOnce     sync.Once
		firstErr    error
	)
	setErr := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}
	reportProgress := func(totalDelta, doneDelta int64) {
		if onProgress == nil {
			return
		}
		if totalDelta != 0 {
			totalQueued.Add(totalDelta)
		}
		if doneDelta != 0 {
			doneQueued.Add(doneDelta)
		}
		total := totalWork
		if queued := int(totalQueued.Load()); queued > total {
			total = queued
		}
		done := int(doneQueued.Load())
		if done == 0 && doneDelta == 0 && totalWork > 0 {
			return
		}
		onProgress(done, total)
	}

	pipelineErrCh := make(chan error, 1)
	go func() {
		pipelineErrCh <- runPipeline(func(owner Owner) error {
			if err := finalize(owner); err != nil {
				setErr(err)
				return err
			}
			if doneWork, ok := any(owner).(interface{ pipelineDoneCalls() int }); ok {
				reportProgress(0, int64(doneWork.pipelineDoneCalls()))
			}
			return nil
		})
	}()

	taskCh := make(chan Task, workerCount*2)
	for i := 0; i < workerCount; i++ {
		prepWG.Add(1)
		go func() {
			defer prepWG.Done()
			for task := range taskCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				indexingperf.AddCount(ctx, "node.prepare.in", 1)
				started := time.Now()
				prepared, err := prepare(ctx, task)
				if prepareMetric != "" {
					indexingperf.ObserveLatency(ctx, prepareMetric, time.Since(started))
				}
				indexingperf.ObserveLatency(ctx, "node.prepare.busy", time.Since(started))
				if err != nil {
					setErr(err)
					return
				}
				reportProgress(int64(prepared.pipelineProviderCalls()), 0)
				enqueueStarted := time.Now()
				select {
				case <-ctx.Done():
					return
				case preparedCh <- prepared:
					if enqueueMetric != "" {
						indexingperf.ObserveLatency(ctx, enqueueMetric, time.Since(enqueueStarted))
					}
					indexingperf.ObserveLatency(ctx, "node.prepare.blocked", time.Since(enqueueStarted))
					indexingperf.AddCount(ctx, "node.prepare.out", 1)
				}
			}
		}()
	}

enqueueLoop:
	for _, task := range tasks {
		if firstErr != nil {
			break
		}
		select {
		case <-ctx.Done():
			break enqueueLoop
		case taskCh <- task:
		}
	}
	close(taskCh)
	prepWG.Wait()
	close(preparedCh)

	if err := <-pipelineErrCh; err != nil && !errors.Is(err, context.Canceled) {
		setErr(err)
	}
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
