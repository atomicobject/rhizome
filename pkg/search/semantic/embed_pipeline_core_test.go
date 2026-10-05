package semantic

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

type coreTestPrepared struct {
	id    string
	seq   int
	texts []string
}

type coreTestJob struct {
	id   string
	text string
}

func (j coreTestJob) pipelineOwnerKey() string { return j.id }
func (j coreTestJob) pipelineText() string     { return j.text }
func (j coreTestJob) pipelineBytes() int       { return len(j.text) }

type coreTestOwner struct {
	id      string
	seq     int
	readyAt time.Time
	pending int
	vecs    []embeddings.Embedding
}

func (o *coreTestOwner) pipelineReadyAt() time.Time { return o.readyAt }
func (o *coreTestOwner) pipelinePending() int       { return o.pending }
func (o *coreTestOwner) pipelineSetPending(n int)   { o.pending = n }

func TestEmbedPipelineCoreBatchesAcrossOwners(t *testing.T) {
	t.Parallel()

	prov := &slowBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
		delay: 10 * time.Millisecond,
	}
	preparedCh := make(chan coreTestPrepared, 8)
	var (
		mu       sync.Mutex
		finished []string
	)
	core := newEmbedPipelineCoreWithNode(
		context.Background(),
		prov,
		nil,
		2,
		nil,
		EmbedPackerOptions{MinTexts: 3, MaxTexts: 3, MaxBytes: 1 << 20, MaxWait: time.Second},
		preparedCh,
		func(prepared coreTestPrepared) (string, *coreTestOwner, []coreTestJob) {
			owner := &coreTestOwner{id: prepared.id, seq: prepared.seq, readyAt: time.Now()}
			jobs := make([]coreTestJob, 0, len(prepared.texts))
			for _, text := range prepared.texts {
				jobs = append(jobs, coreTestJob{id: prepared.id, text: text})
			}
			return prepared.id, owner, jobs
		},
		func(owner *coreTestOwner, _ coreTestJob, vec embeddings.Embedding) error {
			owner.vecs = append(owner.vecs, vec)
			return nil
		},
		func(owner *coreTestOwner) error {
			mu.Lock()
			finished = append(finished, owner.id)
			mu.Unlock()
			return nil
		},
	)

	errCh := make(chan error, 1)
	go func() { errCh <- core.run() }()

	preparedCh <- coreTestPrepared{id: "a", texts: []string{"a1"}}
	preparedCh <- coreTestPrepared{id: "b", texts: []string{"b1"}}
	preparedCh <- coreTestPrepared{id: "c", texts: []string{"c1"}}
	close(preparedCh)

	require.NoError(t, <-errCh)
	require.Equal(t, []int{3}, prov.BatchSizes())
	require.ElementsMatch(t, []string{"a", "b", "c"}, finished)
}

func TestEmbedPipelineCoreFlushesTailWhenPreparedCloses(t *testing.T) {
	t.Parallel()

	prov := &slowBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
		delay: 0,
	}
	preparedCh := make(chan coreTestPrepared, 4)
	core := newEmbedPipelineCoreWithNode(
		context.Background(),
		prov,
		nil,
		1,
		nil,
		EmbedPackerOptions{MinTexts: 4, MaxTexts: 8, MaxBytes: 1 << 20, MaxWait: time.Hour},
		preparedCh,
		func(prepared coreTestPrepared) (string, *coreTestOwner, []coreTestJob) {
			owner := &coreTestOwner{id: prepared.id, seq: prepared.seq, readyAt: time.Now()}
			jobs := make([]coreTestJob, 0, len(prepared.texts))
			for _, text := range prepared.texts {
				jobs = append(jobs, coreTestJob{id: prepared.id, text: text})
			}
			return prepared.id, owner, jobs
		},
		func(owner *coreTestOwner, _ coreTestJob, vec embeddings.Embedding) error {
			owner.vecs = append(owner.vecs, vec)
			return nil
		},
		func(*coreTestOwner) error { return nil },
	)

	errCh := make(chan error, 1)
	go func() { errCh <- core.run() }()

	preparedCh <- coreTestPrepared{id: "a", texts: []string{"a1"}}
	preparedCh <- coreTestPrepared{id: "b", texts: []string{"b1"}}
	close(preparedCh)

	require.NoError(t, <-errCh)
	require.Equal(t, []int{2}, prov.BatchSizes())
}

func TestEmbedPipelineCoreSharedNodeOwnsProviderGate(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx, cancel := context.WithTimeout(indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "embed_code"), time.Second)
	defer cancel()

	releaseProvider := make(chan struct{})
	var releaseProviderOnce sync.Once
	prov := &defaultingProvider{
		batchSize:      4,
		maxConcurrency: 1,
		started:        make(chan int, 1),
		block:          releaseProvider,
	}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	node := NewSharedEmbeddingNode(ctx, prov, 1, gate, EmbedPackerOptions{MinTexts: 1, MaxTexts: 4, MaxBytes: 1 << 20, MaxWait: time.Hour})
	t.Cleanup(func() {
		releaseProviderOnce.Do(func() { close(releaseProvider) })
		select {
		case <-gate:
		default:
		}
		node.Close()
	})

	preparedCh := make(chan coreTestPrepared, 1)
	core := newEmbedPipelineCoreWithNode(
		ctx,
		prov,
		node,
		1,
		gate,
		EmbedPackerOptions{MinTexts: 1, MaxTexts: 4, MaxBytes: 1 << 20, MaxWait: time.Hour},
		preparedCh,
		func(prepared coreTestPrepared) (string, *coreTestOwner, []coreTestJob) {
			owner := &coreTestOwner{id: prepared.id, seq: prepared.seq, readyAt: time.Now()}
			jobs := make([]coreTestJob, 0, len(prepared.texts))
			for _, text := range prepared.texts {
				jobs = append(jobs, coreTestJob{id: prepared.id, text: text})
			}
			return prepared.id, owner, jobs
		},
		func(owner *coreTestOwner, _ coreTestJob, vec embeddings.Embedding) error {
			owner.vecs = append(owner.vecs, vec)
			return nil
		},
		func(*coreTestOwner) error { return nil },
	)

	errCh := make(chan error, 1)
	go func() { errCh <- core.run() }()
	preparedCh <- coreTestPrepared{id: "a", texts: []string{"a1"}}
	close(preparedCh)

	require.Never(t, func() bool {
		select {
		case <-prov.started:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 5*time.Millisecond)
	blocked := collector.RenderWindow(time.Second)
	require.NotContains(t, blocked, "provider_inflight=1", "the outer pipeline must not count work waiting at a shared node")

	<-gate
	require.Equal(t, 1, <-prov.started)
	active := collector.RenderWindow(time.Second)
	require.Contains(t, active, "provider_inflight=1", "the shared node counts the physical provider call after the gate")
	releaseProviderOnce.Do(func() { close(releaseProvider) })
	require.NoError(t, <-errCh)
	require.Equal(t, []int{1}, prov.BatchSizes())
}

func TestEmbedPipelineCoreDispatchesAtMinTextsWithoutWaitingForMaxTexts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prov := &slowBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
		delay: 40 * time.Millisecond,
	}
	preparedCh := make(chan coreTestPrepared, 8)
	core := newEmbedPipelineCoreWithNode(
		ctx,
		prov,
		nil,
		2,
		nil,
		EmbedPackerOptions{MinTexts: 2, MaxTexts: 3, MaxBytes: 1 << 20, MaxWait: time.Hour},
		preparedCh,
		func(prepared coreTestPrepared) (string, *coreTestOwner, []coreTestJob) {
			owner := &coreTestOwner{id: prepared.id, seq: prepared.seq, readyAt: time.Now()}
			jobs := make([]coreTestJob, 0, len(prepared.texts))
			for _, text := range prepared.texts {
				jobs = append(jobs, coreTestJob{id: prepared.id, text: text})
			}
			return prepared.id, owner, jobs
		},
		func(owner *coreTestOwner, _ coreTestJob, vec embeddings.Embedding) error {
			owner.vecs = append(owner.vecs, vec)
			return nil
		},
		func(*coreTestOwner) error { return nil },
	)

	errCh := make(chan error, 1)
	go func() { errCh <- core.run() }()

	preparedCh <- coreTestPrepared{id: "a", texts: []string{"a1"}}
	preparedCh <- coreTestPrepared{id: "b", texts: []string{"b1"}}

	require.Eventually(t, func() bool {
		sizes := prov.BatchSizes()
		return len(sizes) > 0 && sizes[0] == 2
	}, 200*time.Millisecond, 10*time.Millisecond)

	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

func TestEmbedPipelineCoreTargetsOpenSlotsAfterWarmup(t *testing.T) {
	t.Parallel()

	prov := &slowBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
		delay: 100 * time.Millisecond,
	}
	preparedCh := make(chan coreTestPrepared, 8)
	core := newEmbedPipelineCoreWithNode(
		context.Background(),
		prov,
		nil,
		16,
		nil,
		EmbedPackerOptions{
			MinTexts:         2,
			AdaptiveMinTexts: 2,
			MaxTexts:         4,
			MaxBytes:         1 << 20,
			MaxWait:          time.Hour,
		},
		preparedCh,
		func(prepared coreTestPrepared) (string, *coreTestOwner, []coreTestJob) {
			owner := &coreTestOwner{id: prepared.id, seq: prepared.seq, readyAt: time.Now()}
			jobs := make([]coreTestJob, 0, len(prepared.texts))
			for _, text := range prepared.texts {
				jobs = append(jobs, coreTestJob{id: prepared.id, text: text})
			}
			return prepared.id, owner, jobs
		},
		func(owner *coreTestOwner, _ coreTestJob, vec embeddings.Embedding) error {
			owner.vecs = append(owner.vecs, vec)
			return nil
		},
		func(*coreTestOwner) error { return nil },
	)

	errCh := make(chan error, 1)
	go func() { errCh <- core.run() }()

	for i := 0; i < 4; i++ {
		preparedCh <- coreTestPrepared{id: string(rune('a' + i)), texts: []string{"x", "y"}}
	}
	require.Eventually(t, func() bool {
		return len(prov.BatchSizes()) == 4
	}, 200*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []int{2, 2, 2, 2}, prov.BatchSizes())

	preparedCh <- coreTestPrepared{id: "e", texts: []string{"x", "y"}}
	require.Eventually(t, func() bool {
		return len(prov.BatchSizes()) == 5
	}, 200*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, []int{2, 2, 2, 2, 2}, prov.BatchSizes())

	preparedCh <- coreTestPrepared{id: "f", texts: []string{"x", "y"}}
	close(preparedCh)
	require.NoError(t, <-errCh)
	require.Equal(t, []int{2, 2, 2, 2, 2, 2}, prov.BatchSizes())
}

func TestEmbedPipelineCoreKeepsOwnerStateForDuplicateKeys(t *testing.T) {
	t.Parallel()

	prov := &slowBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
		delay: 40 * time.Millisecond,
	}
	preparedCh := make(chan coreTestPrepared, 4)
	var (
		mu       sync.Mutex
		finished []int
	)
	core := newEmbedPipelineCoreWithNode(
		context.Background(),
		prov,
		nil,
		1,
		nil,
		EmbedPackerOptions{MinTexts: 1, MaxTexts: 1, MaxBytes: 1 << 20, MaxWait: time.Second},
		preparedCh,
		func(prepared coreTestPrepared) (string, *coreTestOwner, []coreTestJob) {
			owner := &coreTestOwner{id: prepared.id, seq: prepared.seq, readyAt: time.Now()}
			jobs := make([]coreTestJob, 0, len(prepared.texts))
			for _, text := range prepared.texts {
				jobs = append(jobs, coreTestJob{id: prepared.id, text: text})
			}
			return prepared.id, owner, jobs
		},
		func(owner *coreTestOwner, _ coreTestJob, vec embeddings.Embedding) error {
			owner.vecs = append(owner.vecs, vec)
			return nil
		},
		func(owner *coreTestOwner) error {
			mu.Lock()
			finished = append(finished, owner.seq)
			mu.Unlock()
			return nil
		},
	)

	errCh := make(chan error, 1)
	go func() { errCh <- core.run() }()

	preparedCh <- coreTestPrepared{id: "a", seq: 1, texts: []string{"a1"}}
	preparedCh <- coreTestPrepared{id: "a", seq: 2, texts: nil}
	close(preparedCh)

	require.NoError(t, <-errCh)
	require.ElementsMatch(t, []int{1, 2}, finished)
}
