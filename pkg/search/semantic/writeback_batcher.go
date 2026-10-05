package semantic

import (
	"context"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type writebackPayload[T any] struct {
	phase  string
	values T
	rows   int
	bytes  int
}

type writebackBatch[T any] struct {
	values    T
	rows      int
	bytes     int
	updatedAt time.Time
}

func (b *writebackBatch[T]) shouldFlush(policy WritebackFlushPolicy) bool {
	return b != nil && (b.rows >= policy.Rows || b.bytes >= policy.Bytes)
}

// writebackBatcher owns one lane's bounded queue and per-phase pending batches.
// Concrete adapters supply collection merging and the instrumented queue write.
// They install flush before starting run.
type writebackBatcher[T any] struct {
	ctx              context.Context
	drainCtx         context.Context
	cancel           context.CancelFunc
	policy           WritebackFlushPolicy
	pressure         *writebackPressureController
	submitWaitMetric string
	queueDepthMetric string
	merge            func(T, T) T
	flush            func(string, *writebackBatch[T]) error
	submitCh         chan writebackPayload[T]
	doneCh           chan struct{}
	mu               sync.Mutex
	err              error
}

func newWritebackBatcher[T any](ctx context.Context, capacity int, policy WritebackFlushPolicy, minimumRows int, metricPrefix string, merge func(T, T) T) *writebackBatcher[T] {
	ctx, cancel := context.WithCancel(ctx)
	return &writebackBatcher[T]{
		ctx:              ctx,
		drainCtx:         context.WithoutCancel(ctx),
		cancel:           cancel,
		policy:           policy,
		pressure:         newWritebackPressureController(policy, writebackRowsFloor(policy.Rows, minimumRows), writebackBytesFloor(policy.Bytes)),
		submitWaitMetric: metricPrefix + ".submit_wait",
		queueDepthMetric: metricPrefix + ".queue_depth",
		merge:            merge,
		submitCh:         make(chan writebackPayload[T], capacity),
		doneCh:           make(chan struct{}),
	}
}

func (b *writebackBatcher[T]) submit(ctx context.Context, payload writebackPayload[T]) error {
	started := time.Now()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		if err := b.Err(); err != nil {
			return err
		}
		return b.ctx.Err()
	case b.submitCh <- payload:
		indexingperf.ObserveLatency(ctx, b.submitWaitMetric, time.Since(started))
		indexingperf.SetGauge(ctx, b.queueDepthMetric, int64(len(b.submitCh)))
		return nil
	}
}

func (b *writebackBatcher[T]) Close() error {
	close(b.submitCh)
	<-b.doneCh
	return b.Err()
}

func (b *writebackBatcher[T]) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

func (b *writebackBatcher[T]) fail(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	b.mu.Unlock()
	b.cancel()
}

func (b *writebackBatcher[T]) run() {
	defer close(b.doneCh)
	ticker := time.NewTicker(b.policy.Idle)
	defer ticker.Stop()
	pending := make(map[string]*writebackBatch[T])
	flushAll := func() error {
		for phase, batch := range pending {
			if err := b.flush(phase, batch); err != nil {
				return err
			}
			delete(pending, phase)
		}
		return nil
	}
	addPayload := func(payload writebackPayload[T]) {
		batch := pending[payload.phase]
		if batch == nil {
			batch = &writebackBatch[T]{}
			pending[payload.phase] = batch
		}
		batch.values = b.merge(batch.values, payload.values)
		batch.rows += payload.rows
		batch.bytes += payload.bytes
		batch.updatedAt = time.Now()
	}
	drainAvailable := func() error {
		for {
			select {
			case payload, ok := <-b.submitCh:
				if !ok {
					return flushAll()
				}
				addPayload(payload)
			default:
				return flushAll()
			}
		}
	}
	for {
		select {
		case <-b.ctx.Done():
			if err := drainAvailable(); err != nil {
				b.fail(err)
			}
			return
		case payload, ok := <-b.submitCh:
			if !ok {
				if err := flushAll(); err != nil {
					b.fail(err)
				}
				return
			}
			addPayload(payload)
			batch := pending[payload.phase]
			effectivePolicy := b.pressure.effectivePolicy(len(b.submitCh), cap(b.submitCh))
			if batch.shouldFlush(effectivePolicy) {
				if err := b.flush(payload.phase, batch); err != nil {
					b.fail(err)
					return
				}
				delete(pending, payload.phase)
			}
		case now := <-ticker.C:
			for phase, batch := range pending {
				if now.Sub(batch.updatedAt) < b.policy.Idle {
					continue
				}
				if err := b.flush(phase, batch); err != nil {
					b.fail(err)
					return
				}
				delete(pending, phase)
			}
		}
	}
}
