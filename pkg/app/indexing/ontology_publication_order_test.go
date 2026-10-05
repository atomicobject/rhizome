package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// TestQueuedOntologyPublicationDrainsInitialBodyWriteBeforeCatalogRefresh
// keeps a late catalog refresh from deleting the chunk parent of an in-flight
// ontology body embedding state.
func TestQueuedOntologyPublicationDrainsInitialBodyWriteBeforeCatalogRefresh(t *testing.T) {
	// Let the test runner own the deadline: Windows SQLite initialization can
	// exceed a second before the publication ordering under test even starts.
	ctx := t.Context()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Decision @node(paths: ["notes/*.md"]) {
  summary: String
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "decision.md"), []byte("# Decision\n\nDurable body evidence.\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "publication-order", Path: root, Links: obsidian.LinkTypeBoth}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(ctx, vaultDef, &obsidian.Note{}, schema, "notes/decision.md")
	require.NoError(t, err)
	catalog, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)
	require.NotEmpty(t, catalog.Nodes)

	q := indexwriter.New(ctx, indexwriter.Handlers{
		ApplyIntelChunks:           store.ReplaceIntelChunks,
		ApplyIntelEmbeddings:       store.UpsertEmbeddings,
		ApplyOntologyNodeReadModel: store.ReplaceOntologyNodeReadModel,
		ApplyOntologyNodeStates:    store.UpsertOntologyNodeEmbeddingStates,
	})
	t.Cleanup(func() { _ = q.StopAndWait() })
	require.NoError(t, q.SubmitOntologyNodeReadModel(ctx, catalog))
	require.NoError(t, q.FlushAndWait(ctx))

	set, err := semantic.BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}, 1)
	require.NoError(t, err)
	require.NotEmpty(t, set.Chunks)
	ownerIDs := ontologyPublicationOwnerIDs(set.Nodes)
	require.NoError(t, store.ReplaceIntelChunks(ctx, ownerIDs, set.Chunks))

	provider := &publicationOrderBlockingProvider{
		inner:   embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}),
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseProvider := func() { releaseOnce.Do(func() { close(provider.release) }) }

	syncer := semantic.OntologyNodeSyncer{
		Store:        ontologyBodyQueuedStore{readStore: store, queue: q},
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8},
		Schema:       schema,
	}
	streaming := &unifiedStreamingCoordinator{
		ctx:    ctx,
		noteCh: make(chan noteStreamingBatch, 1),
		syncNoteBodyPaths: func(stageCtx context.Context, changedPaths, _ []string, _ bool) error {
			return syncer.SyncProjections(stageCtx, projection)
		},
	}
	streaming.Start()
	finalizer := &unifiedBatchFinalizer{writeQueue: q, streaming: streaming}
	t.Cleanup(func() {
		releaseProvider()
		_ = finalizer.CloseStreaming()
	})

	require.NoError(t, streaming.SubmitNotePaths(ctx, catalog.NotePaths))
	select {
	case <-provider.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	// The provider stays in flight until draining closes the input channel.
	// At this point the sole body worker is blocked in EmbedTexts and the input
	// queue is empty, so this receive observes closure without stealing work.
	providerReleased := make(chan struct{})
	go func() {
		defer close(providerReleased)
		select {
		case <-streaming.noteCh:
		case <-ctx.Done():
		}
		releaseProvider()
	}()
	require.NoError(t, finalizer.DrainStreamingWrites(ctx))
	<-providerReleased

	chunkIDs := ontologyPublicationChunkIDs(set.Chunks)
	states, err := store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, chunkIDs)
	require.NoError(t, err)
	require.Len(t, states, len(chunkIDs), "the drain must persist every body state before catalog replacement")
	require.Equal(t, 1, provider.CallCount(), "the initial body write must complete before catalog replacement")

	// This models the final catalog refresh removing the now-stale projection.
	// Once the body stream is drained, cascading that completed state is safe.
	require.NoError(t, q.SubmitOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{NotePaths: catalog.NotePaths}))
	require.NoError(t, q.FlushAndWait(ctx))
	chunks, err := store.IntelChunksByOwners(ctx, ownerIDs)
	require.NoError(t, err)
	require.Empty(t, chunks)
	states, err = store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, chunkIDs)
	require.NoError(t, err)
	require.Empty(t, states)

	// The final refresh then installs the current catalog and regenerates the
	// current body surface through the same queued store.
	require.NoError(t, q.SubmitOntologyNodeReadModel(ctx, catalog))
	require.NoError(t, q.FlushAndWait(ctx))
	finalProvider := &publicationOrderCountingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}),
	}
	finalSyncer := semantic.OntologyNodeSyncer{
		Store:        ontologyBodyQueuedStore{readStore: store, queue: q},
		Provider:     finalProvider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8},
		Schema:       schema,
	}
	require.NoError(t, finalSyncer.SyncProjections(ctx, projection))
	require.NoError(t, q.FlushAndWait(ctx))
	chunks, err = store.IntelChunksByOwners(ctx, ownerIDs)
	require.NoError(t, err)
	require.Len(t, chunks, len(set.Chunks))
	states, err = store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, chunkIDs)
	require.NoError(t, err)
	require.Len(t, states, len(chunkIDs))
	require.Equal(t, 1, finalProvider.CallCount(), "the final body refresh must embed the reinstalled current chunk")
	results, _, err := store.SearchEmbeddings(ctx, embeddings.Embedding{1, 0, 0, 0, 0, 0, 0, 0}, len(chunkIDs), semdb.EmbeddingSearchFilters{OwnerTypes: []string{semantic.OntologyNodeOwnerType}})
	require.NoError(t, err)
	require.Len(t, results, len(chunkIDs), "every current ontology chunk must have an embedding")
}

func TestDrainStreamingWritesPropagatesBodyFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	wantErr := errors.New("body write failed")
	streaming := &unifiedStreamingCoordinator{
		ctx:    ctx,
		noteCh: make(chan noteStreamingBatch, 1),
		syncNoteBodyPaths: func(context.Context, []string, []string, bool) error {
			return wantErr
		},
	}
	streaming.Start()
	q := indexwriter.New(ctx, indexwriter.Handlers{})
	t.Cleanup(func() { _ = q.StopAndWait() })
	finalizer := &unifiedBatchFinalizer{writeQueue: q, streaming: streaming}
	require.NoError(t, streaming.SubmitNotePaths(ctx, []string{"notes/decision.md"}))
	require.ErrorIs(t, finalizer.DrainStreamingWrites(ctx), wantErr)
}

func TestDrainStreamingWritesPropagatesCancellation(t *testing.T) {
	t.Run("before drain starts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		streaming := &unifiedStreamingCoordinator{
			ctx:    ctx,
			noteCh: make(chan noteStreamingBatch, 1),
		}
		streaming.Start()
		q := indexwriter.New(ctx, indexwriter.Handlers{})
		t.Cleanup(func() {
			cancel()
			_ = q.StopAndWait()
		})
		finalizer := &unifiedBatchFinalizer{writeQueue: q, streaming: streaming}
		require.NoError(t, streaming.SubmitNotePaths(ctx, []string{"notes/decision.md"}))
		cancel()
		require.ErrorIs(t, finalizer.DrainStreamingWrites(ctx), context.Canceled)
	})

	t.Run("during queued body flush", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		bodyStarted := make(chan struct{}, 1)
		bodyRelease := make(chan struct{})
		writeStarted := make(chan struct{}, 1)
		writeRelease := make(chan struct{})
		var releaseBodyOnce sync.Once
		var releaseWriteOnce sync.Once
		releaseBody := func() { releaseBodyOnce.Do(func() { close(bodyRelease) }) }
		releaseWrite := func() { releaseWriteOnce.Do(func() { close(writeRelease) }) }
		streaming := &unifiedStreamingCoordinator{
			ctx:    ctx,
			noteCh: make(chan noteStreamingBatch, 1),
			syncNoteBodyPaths: func(context.Context, []string, []string, bool) error {
				bodyStarted <- struct{}{}
				<-bodyRelease
				return nil
			},
		}
		streaming.Start()
		cfg := indexwriter.DefaultConfig()
		cfg.DefaultPolicy.Idle = time.Hour
		cfg.PollInterval = time.Hour
		q := indexwriter.NewWithConfig(ctx, indexwriter.Handlers{
			ApplyNoteIndexBatch: func(context.Context, []codeanchor.NoteIndexWork) error {
				writeStarted <- struct{}{}
				<-writeRelease
				return nil
			},
		}, cfg)
		t.Cleanup(func() {
			releaseBody()
			releaseWrite()
			cancel()
			_ = q.StopAndWait()
		})
		finalizer := &unifiedBatchFinalizer{writeQueue: q, streaming: streaming}
		require.NoError(t, streaming.SubmitNotePaths(ctx, []string{"notes/decision.md"}))
		select {
		case <-bodyStarted:
		case <-time.After(time.Second):
			t.Fatal("body worker did not start")
		}
		require.NoError(t, q.SubmitNoteIndexWork(ctx, codeanchor.NoteIndexWork{Path: "notes/decision.md"}))
		drained := make(chan error, 1)
		go func() { drained <- finalizer.DrainStreamingWrites(ctx) }()
		releaseBody()
		select {
		case <-writeStarted:
		case <-time.After(time.Second):
			t.Fatal("queued body write did not start")
		}
		cancel()
		releaseWrite()
		select {
		case err := <-drained:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("stream drain did not return after cancellation")
		}
	})
}

func ontologyPublicationOwnerIDs(nodes []codeanchor.IntelOntologyNode) []string {
	ownerIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ownerIDs = append(ownerIDs, node.NodeID)
	}
	return ownerIDs
}

func ontologyPublicationChunkIDs(chunks []codeanchor.IntelChunk) []string {
	chunkIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunkIDs = append(chunkIDs, chunk.ChunkID)
	}
	return chunkIDs
}

type publicationOrderBlockingProvider struct {
	inner   embeddings.Provider
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (p *publicationOrderBlockingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	select {
	case p.entered <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.release:
		return p.inner.EmbedTexts(ctx, texts)
	}
}

func (p *publicationOrderBlockingProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *publicationOrderBlockingProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type publicationOrderCountingProvider struct {
	inner embeddings.Provider
	mu    sync.Mutex
	calls int
}

func (p *publicationOrderCountingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.inner.EmbedTexts(ctx, texts)
}

func (p *publicationOrderCountingProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *publicationOrderCountingProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
