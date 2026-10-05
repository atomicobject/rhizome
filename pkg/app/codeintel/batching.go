package codeintel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

const (
	maxIndexWorkers      = 32
	indexWriterBatchCap  = 500
	indexWriterFlushIdle = 75 * time.Millisecond
)

func ClampIndexWorkers(workers int) int {
	if workers < 2 {
		return 2
	}
	if workers > maxIndexWorkers/2 {
		workers = maxIndexWorkers / 2
	}
	workers *= 2
	if workers > maxIndexWorkers {
		return maxIndexWorkers
	}
	return workers
}

func RunAdaptiveBatchWriter[T any](
	ctx context.Context,
	cancel context.CancelFunc,
	in <-chan T,
	errCh chan<- error,
	apply func([]T) error,
) {
	batch := make([]T, 0, indexWriterBatchCap)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timerActive := false

	sendErr := func(err error) {
		if err == nil {
			return
		}
		select {
		case errCh <- err:
		default:
		}
	}

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
		err := apply(batch)
		batch = batch[:0]
		return err
	}

	fail := func(err error) {
		sendErr(err)
		if cancel != nil {
			cancel()
		}
	}

	for {
		var timerCh <-chan time.Time
		if timerActive {
			timerCh = timer.C
		}
		select {
		case <-ctx.Done():
			_ = flush()
			return
		case item, ok := <-in:
			if !ok {
				sendErr(flush())
				return
			}
			batch = append(batch, item)
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

func HashContentBytes(content []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(content))
}
