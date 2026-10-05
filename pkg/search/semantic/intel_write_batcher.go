package semantic

import (
	"context"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

const (
	defaultIntelEmbeddingFlushSize     = 1024
	defaultIntelEmbeddingFlushInterval = 100 * time.Millisecond
)

type intelEmbeddingBatchWriterOptions struct {
	Label         string
	FlushSize     int
	FlushInterval time.Duration
	OnError       func(error)
	OnProgress    func(format string, args ...any)
}

// batchedIntelEmbeddingWriter coalesces high-volume intel embedding writes into
// larger transactions. Indexing paths should submit chunk embeddings here
// instead of calling EmbeddingWriter per task/item.
type batchedIntelEmbeddingWriter struct {
	ctx      context.Context
	writer   EmbeddingWriter
	label    string
	maxBatch int
	interval time.Duration
	onError  func(error)
	progress func(format string, args ...any)

	submitCh chan map[string]embeddings.Embedding
	doneCh   chan struct{}

	mu  sync.Mutex
	err error

	flushes   int
	submitted int
	flushed   int
	maxFlush  int
}

func intelWritebackMetricPrefix(label string) string {
	switch label {
	case "notes":
		return "noteembed.writeback.intel"
	case "code":
		return "codeembed.writeback.intel"
	default:
		return ""
	}
}

func newBatchedIntelEmbeddingWriter(ctx context.Context, writer EmbeddingWriter, opts intelEmbeddingBatchWriterOptions) *batchedIntelEmbeddingWriter {
	if writer == nil {
		return nil
	}
	flushSize := opts.FlushSize
	if flushSize <= 0 {
		flushSize = defaultIntelEmbeddingFlushSize
	}
	flushInterval := opts.FlushInterval
	if flushInterval <= 0 {
		flushInterval = defaultIntelEmbeddingFlushInterval
	}
	w := &batchedIntelEmbeddingWriter{
		ctx:      ctx,
		writer:   writer,
		label:    opts.Label,
		maxBatch: flushSize,
		interval: flushInterval,
		onError:  opts.OnError,
		progress: opts.OnProgress,
		submitCh: make(chan map[string]embeddings.Embedding, 32),
		doneCh:   make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *batchedIntelEmbeddingWriter) Submit(rows map[string]embeddings.Embedding) error {
	if w == nil || len(rows) == 0 {
		return nil
	}
	if err := w.Err(); err != nil {
		return err
	}
	clone := make(map[string]embeddings.Embedding, len(rows))
	for chunkID, vec := range rows {
		if len(vec) == 0 {
			continue
		}
		cp := make(embeddings.Embedding, len(vec))
		copy(cp, vec)
		clone[chunkID] = cp
	}
	if len(clone) == 0 {
		return nil
	}
	started := time.Now()
	select {
	case <-w.ctx.Done():
		if err := w.Err(); err != nil {
			return err
		}
		return w.ctx.Err()
	case <-w.doneCh:
		if err := w.Err(); err != nil {
			return err
		}
		return context.Canceled
	case w.submitCh <- clone:
		if prefix := intelWritebackMetricPrefix(w.label); prefix != "" {
			indexingperf.ObserveLatency(w.ctx, prefix+".submit_wait", time.Since(started))
		}
		return nil
	}
}

func (w *batchedIntelEmbeddingWriter) Close() error {
	if w == nil {
		return nil
	}
	close(w.submitCh)
	<-w.doneCh
	return w.Err()
}

func (w *batchedIntelEmbeddingWriter) Err() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *batchedIntelEmbeddingWriter) run() {
	defer close(w.doneCh)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	pending := make(map[string]embeddings.Embedding)
	flushCtx := w.ctx
	ctxDone := w.ctx.Done()
	for {
		select {
		case <-ctxDone:
			// Workers may already have submitted rows before noticing cancellation.
			// Keep draining until Close() closes submitCh, then flush with a context
			// that is detached from cancellation so completed embedding work lands.
			ctxDone = nil
			flushCtx = context.WithoutCancel(w.ctx)
		case rows, ok := <-w.submitCh:
			if !ok {
				if err := w.flushAll(flushCtx, pending); err != nil {
					w.fail(err)
					return
				}
				w.report()
				return
			}
			w.submitted += len(rows)
			for chunkID, vec := range rows {
				pending[chunkID] = vec
			}
			if prefix := intelWritebackMetricPrefix(w.label); prefix != "" {
				indexingperf.SetGauge(w.ctx, prefix+".queue_depth", int64(len(pending)))
			}
			for len(pending) >= w.maxBatch {
				if err := w.flushNext(flushCtx, pending, w.maxBatch); err != nil {
					w.fail(err)
					return
				}
			}
		case <-ticker.C:
			if err := w.flushAll(flushCtx, pending); err != nil {
				w.fail(err)
				return
			}
		}
	}
}

func (w *batchedIntelEmbeddingWriter) flushAll(ctx context.Context, pending map[string]embeddings.Embedding) error {
	for len(pending) > 0 {
		if err := w.flushNext(ctx, pending, w.maxBatch); err != nil {
			return err
		}
	}
	return nil
}

func (w *batchedIntelEmbeddingWriter) flushNext(ctx context.Context, pending map[string]embeddings.Embedding, limit int) error {
	if len(pending) == 0 {
		return nil
	}
	if ctx.Err() != nil {
		ctx = context.WithoutCancel(ctx)
	}
	if limit <= 0 || limit > len(pending) {
		limit = len(pending)
	}
	batch := make(map[string]embeddings.Embedding, limit)
	for chunkID, vec := range pending {
		batch[chunkID] = vec
		delete(pending, chunkID)
		if len(batch) >= limit {
			break
		}
	}
	if len(batch) == 0 {
		return nil
	}
	if prefix := intelWritebackMetricPrefix(w.label); prefix != "" {
		indexingperf.AddCount(ctx, prefix+".flush.count", 1)
		indexingperf.ObserveSample(ctx, prefix+".batch_rows", int64(len(batch)))
		indexingperf.ObserveSample(ctx, prefix+".batch_bytes", int64(estimateIntelEmbeddingRows(batch)))
	}
	started := time.Now()
	if err := w.writer.UpsertEmbeddings(ctx, batch); err != nil {
		return err
	}
	if prefix := intelWritebackMetricPrefix(w.label); prefix != "" {
		indexingperf.ObserveLatency(ctx, prefix+".flush", time.Since(started))
		indexingperf.SetGauge(ctx, prefix+".queue_depth", int64(len(pending)))
	}
	w.flushes++
	w.flushed += len(batch)
	if len(batch) > w.maxFlush {
		w.maxFlush = len(batch)
	}
	return nil
}

func (w *batchedIntelEmbeddingWriter) fail(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	if w.err == nil {
		w.err = err
	}
	w.mu.Unlock()
	if w.onError != nil {
		w.onError(err)
	}
}

func (w *batchedIntelEmbeddingWriter) report() {
	if w == nil || w.progress == nil || w.flushed == 0 {
		return
	}
	avg := 0
	if w.flushes > 0 {
		avg = w.flushed / w.flushes
	}
	if w.label != "" {
		w.progress("[index] intel-batch %s flushes=%d submitted=%d persisted=%d avg_flush=%d max_flush=%d",
			w.label, w.flushes, w.submitted, w.flushed, avg, w.maxFlush)
		return
	}
	w.progress("[index] intel-batch flushes=%d submitted=%d persisted=%d avg_flush=%d max_flush=%d",
		w.flushes, w.submitted, w.flushed, avg, w.maxFlush)
}
