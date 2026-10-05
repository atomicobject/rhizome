package semantic

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

func embedTextsWithProvider(ctx context.Context, provider embeddings.Provider, texts []string, batchSize, maxConcurrent int, globalSem chan struct{}) ([]embeddings.Embedding, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if batchSize <= 0 {
		batchSize = EffectiveBatchSize(provider, 0)
	}
	if maxConcurrent <= 0 {
		maxConcurrent = EffectiveMaxConcurrent(provider, 0)
	}

	type batchResult struct {
		start int
		vecs  []embeddings.Embedding
		err   error
	}

	resultCh := make(chan batchResult, (len(texts)+batchSize-1)/batchSize)
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var inflight atomic.Int64

	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batchTexts := append([]string(nil), texts[start:end]...)
		wg.Add(1)
		go func(start int, batchTexts []string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				return
			default:
			}

			gateStarted := time.Now()
			if globalSem != nil {
				select {
				case <-ctx.Done():
					return
				case globalSem <- struct{}{}:
				}
				defer func() { <-globalSem }()
			}
			indexingperf.ObserveLatency(ctx, "embed.gate_wait", time.Since(gateStarted))

			active := inflight.Add(1)
			indexingperf.SetGauge(ctx, "provider.inflight", active)
			defer func() {
				remaining := inflight.Add(-1)
				indexingperf.SetGauge(ctx, "provider.inflight", remaining)
			}()

			totalBytes := 0
			for _, text := range batchTexts {
				totalBytes += len(text)
			}
			indexingperf.AddCount(ctx, "provider.calls", 1)
			indexingperf.AddCount(ctx, "provider.texts", int64(len(batchTexts)))
			indexingperf.AddCount(ctx, "provider.logical_texts", int64(len(batchTexts)))
			indexingperf.AddBytes(ctx, "provider.text", int64(totalBytes))
			if cap := EffectiveBatchSize(provider, 0); cap > 0 {
				indexingperf.ObserveSample(ctx, "provider.cap_texts", int64(cap))
			}
			if batchSize > 0 {
				indexingperf.ObserveSample(ctx, "provider.dispatch_floor_texts", int64(batchSize))
			}
			indexingperf.ObserveSample(ctx, "provider.capacity", int64(maxConcurrent))
			indexingperf.ObserveSample(ctx, "provider.batch_texts", int64(len(batchTexts)))
			indexingperf.ObserveSample(ctx, "provider.batch_bytes", int64(totalBytes))
			indexingperf.ObserveSample(ctx, "provider.input_texts", int64(len(batchTexts)))
			indexingperf.ObserveSample(ctx, "provider.input_bytes", int64(totalBytes))
			if batchSize > 0 {
				indexingperf.ObserveSample(ctx, "provider.batch_fill_ratio", int64(len(batchTexts)*100/batchSize))
			}

			callStarted := time.Now()
			vecs, err := provider.EmbedTexts(ctx, batchTexts)
			callDur := time.Since(callStarted)
			indexingperf.ObserveLatency(ctx, "provider.latency", callDur)
			indexingperf.ObserveInterval(ctx, "provider.latency", callStarted, callDur)
			if err == nil && len(vecs) != len(batchTexts) {
				err = fmt.Errorf("embed: unexpected vector count %d for %d texts", len(vecs), len(batchTexts))
			}
			if err != nil {
				cancel()
				resultCh <- batchResult{start: start, vecs: vecs, err: err}
				return
			}

			select {
			case <-ctx.Done():
			case resultCh <- batchResult{start: start, vecs: vecs, err: err}:
			}
		}(start, batchTexts)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	out := make([]embeddings.Embedding, len(texts))
	var firstErr error
	for result := range resultCh {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
				cancel()
			}
			continue
		}
		copy(out[result.start:], result.vecs)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
