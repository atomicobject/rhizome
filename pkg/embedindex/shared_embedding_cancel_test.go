package embedindex

import (
	"context"
	"fmt"
	"runtime"
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

// Another lane can hold the shared provider gate; a batch whose request was
// cancelled while it waited for the gate is not sent.
func TestSharedEmbeddingNodeDropsBatchCancelledWhileWaitingForGate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	gate := make(chan struct{}, 1)
	gate <- struct{}{} // another lane holds the gate
	prov := &defaultingProvider{batchSize: 1000, maxConcurrency: 1, started: make(chan int, 4)}
	node := NewSharedEmbeddingNode(ctx, prov, 1, gate, EmbedPackerOptions{MinTexts: 1, MaxTexts: 8, MaxBytes: 1 << 20, MaxWait: time.Millisecond})
	defer node.Close()

	reqCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := node.Submit(reqCtx, "embed_ontology_nodes", []string{"waiting"})
		done <- err
	}()
	// Let the batch leave the queue and block on the gate, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-gate // the other lane releases the gate
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled request did not return")
	}
	node.Close()
	require.Empty(t, prov.BatchSizes(), "a batch cancelled while waiting for the gate must not reach the provider")
}

// A cold ontology pass with many slices must not start a goroutine per slice
// while the shared node is busy.
func TestOntologyComputeDerivedBoundsSliceGoroutines(t *testing.T) {
	release := make(chan struct{})
	prov := &defaultingProvider{batchSize: 1000, maxConcurrency: 1, started: make(chan int, 1024), block: release}
	node := NewSharedEmbeddingNode(context.Background(), prov, 1, nil, EmbedPackerOptions{MinTexts: 1, MaxTexts: 64, MaxBytes: 1 << 20, MaxWait: time.Millisecond})
	defer node.Close()

	const slices = 100
	plan := OntologyDerivedPlan{textHashes: map[string]string{}}
	for i := range slices * ontologyEmbedSliceTexts {
		id := fmt.Sprintf("chunk-%d", i)
		plan.ids = append(plan.ids, id)
		plan.texts = append(plan.texts, id)
	}
	before := runtime.NumGoroutine()
	done := make(chan error, 1)
	go func() {
		_, err := OntologyNodeSyncer{EmbeddingNode: node}.ComputeDerived(context.Background(), plan)
		done <- err
	}()
	<-prov.started
	time.Sleep(50 * time.Millisecond)
	grown := runtime.NumGoroutine() - before
	close(release)
	require.NoError(t, <-done)
	require.Less(t, grown, ontologyEmbedNodeInFlightSlices+16, "slice goroutines must stay bounded while the node is busy")
}
