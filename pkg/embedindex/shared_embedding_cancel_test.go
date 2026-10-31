package embedindex

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

// A cancelled submitter's queued texts are never sent, while a batch already
// at the provider still delivers its vectors to its cancelled submitter.
func TestSharedEmbeddingNodeDropsCancelledQueuedTextsAndDeliversInFlight(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	release := make(chan struct{})
	var releaseOnce sync.Once
	prov := &defaultingProvider{batchSize: 1000, maxConcurrency: 1, started: make(chan int, 4), block: release}
	node := NewSharedEmbeddingNode(ctx, prov, 1, nil, EmbedPackerOptions{MinTexts: 1, MaxTexts: 8, MaxBytes: 1 << 20, MaxWait: time.Hour})
	defer func() {
		releaseOnce.Do(func() { close(release) })
		node.Close()
	}()

	type result struct {
		n   int
		err error
	}
	submit := func(ctx context.Context, texts ...string) <-chan result {
		out := make(chan result, 1)
		go func() {
			vecs, err := node.Submit(ctx, "embed_ontology_nodes", texts)
			out <- result{len(vecs), err}
		}()
		return out
	}

	inFlightCtx, cancelInFlight := context.WithCancel(ctx)
	defer cancelInFlight()
	inFlight := submit(inFlightCtx, "paid")
	require.Equal(t, 1, <-prov.started)

	queuedCtx, cancelQueued := context.WithCancel(ctx)
	defer cancelQueued()
	queued := submit(queuedCtx, "queued-1", "queued-2")
	require.Eventually(t, func() bool {
		return strings.Contains(collector.RenderSummary(), "ready_depth_peak=2")
	}, time.Second, time.Millisecond)

	cancelQueued()
	select {
	case got := <-queued:
		require.ErrorIs(t, got.err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("a cancelled queued request must return without waiting for a provider slot")
	}

	cancelInFlight()
	select {
	case got := <-inFlight:
		t.Fatalf("an in-flight request returned before its batch finished: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	got := <-inFlight
	require.NoError(t, got.err)
	require.Equal(t, 1, got.n)

	node.Close()
	require.Equal(t, []int{1}, prov.BatchSizes(), "cancelled queued texts must not reach the provider")
}
