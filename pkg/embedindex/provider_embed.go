package embedindex

import (
	"context"
	"fmt"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func embedProviderBatch(ctx context.Context, provider embeddings.Provider, texts []string, batchSize int, gate chan struct{}) ([]embeddings.Embedding, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	texts = append([]string(nil), texts...)
	gateStarted := time.Now()
	if gate != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case gate <- struct{}{}:
		}
		defer func() { <-gate }()
	}
	indexingperf.ObserveLatency(ctx, indexingperf.EmbedGateWait, time.Since(gateStarted))
	indexingperf.SetGauge(ctx, indexingperf.ProviderInflight, 1)
	defer indexingperf.SetGauge(ctx, indexingperf.ProviderInflight, 0)

	totalBytes := 0
	for _, text := range texts {
		totalBytes += len(text)
	}
	indexingperf.AddCount(ctx, indexingperf.ProviderCalls, 1)
	indexingperf.AddCount(ctx, indexingperf.ProviderTexts, int64(len(texts)))
	indexingperf.AddCount(ctx, indexingperf.ProviderLogicalTexts, int64(len(texts)))
	indexingperf.AddBytes(ctx, indexingperf.ProviderTextBytes, int64(totalBytes))
	if cap := EffectiveBatchSize(provider, 0); cap > 0 {
		indexingperf.ObserveSample(ctx, indexingperf.ProviderCapTexts, int64(cap))
	}
	if batchSize > 0 {
		indexingperf.ObserveSample(ctx, indexingperf.ProviderDispatchFloorTexts, int64(batchSize))
	}
	indexingperf.ObserveSample(ctx, indexingperf.ProviderCapacity, 1)
	indexingperf.ObserveSample(ctx, indexingperf.ProviderBatchTexts, int64(len(texts)))
	indexingperf.ObserveSample(ctx, indexingperf.ProviderBatchBytes, int64(totalBytes))
	indexingperf.ObserveSample(ctx, indexingperf.ProviderInputTexts, int64(len(texts)))
	indexingperf.ObserveSample(ctx, indexingperf.ProviderInputBytes, int64(totalBytes))
	if batchSize > 0 {
		indexingperf.ObserveSample(ctx, indexingperf.ProviderBatchFillRatio, int64(len(texts)*100/batchSize))
	}

	callStarted := time.Now()
	vecs, err := provider.EmbedTexts(ctx, texts)
	callDur := time.Since(callStarted)
	indexingperf.ObserveLatency(ctx, indexingperf.ProviderLatency, callDur)
	indexingperf.ObserveInterval(ctx, indexingperf.ProviderLatency, callStarted, callDur)
	if err != nil {
		return nil, err
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("embed: unexpected vector count %d for %d texts", len(vecs), len(texts))
	}
	// Completed provider work remains usable even if cancellation raced its return.
	return vecs, nil
}
