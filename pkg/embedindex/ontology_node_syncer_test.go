package embedindex

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"

	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func TestOntologyNodeSyncer_SkipsEmbeddingWhenSidecarStateMatches(t *testing.T) {
	root, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	countingStore := &countingOntologyNodeStore{Store: store}
	provider := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}),
	}
	syncer := OntologyNodeSyncer{
		Store:        countingStore,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:       schema,
		BatchSize:    100,
	}

	require.NoError(t, syncer.SyncProjections(context.Background(), projection))
	require.Greater(t, provider.calls.Load(), int64(0))
	require.Zero(t, countingStore.replaceNodes.Load())
	require.Greater(t, countingStore.replaceChunks.Load(), int64(0))

	provider.Reset()
	countingStore.Reset()
	require.NoError(t, syncer.SyncNotePaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, []string{"notes/specs/checkout.md"}, nil))
	require.Zero(t, provider.calls.Load())
	require.Zero(t, countingStore.replaceNodes.Load())
	require.Zero(t, countingStore.replaceChunks.Load())
}

func TestOntologyNodeSyncer_PreservesCatalogOwnedRows(t *testing.T) {
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	catalogModel, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)
	require.NotEmpty(t, catalogModel.Nodes)
	require.NotEmpty(t, catalogModel.FieldValues)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, catalogModel))

	countingStore := &countingOntologyNodeStore{Store: store}
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8})
	syncer := OntologyNodeSyncer{
		Store:        countingStore,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:       schema,
	}
	require.NoError(t, syncer.SyncProjections(ctx, projection))
	require.Zero(t, countingStore.replaceNodes.Load(), "semantic sync must not write ontology catalog rows")

	nodes, err := store.OntologyNodesByPaths(ctx, catalogModel.NotePaths)
	require.NoError(t, err)
	require.Len(t, nodes, len(catalogModel.Nodes))
	nodeIDs := make([]string, 0, len(catalogModel.Nodes))
	for _, node := range catalogModel.Nodes {
		nodeIDs = append(nodeIDs, node.NodeID)
	}
	fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.Len(t, fields, len(catalogModel.FieldValues))
}

func TestOntologyNodeSyncer_EmbedsFallbackUntypedNoteChunks(t *testing.T) {
	// The semantic syncer is responsible for chunks + embeddings of fallback
	// projections. The ontology_nodes NOTE row itself is owned by the catalog
	// sync (see TestBuildIntelOntologyNodeReadModel_UntypedGlobalSourceEmits...
	// in pkg/ontology); writing it here would re-issue per-path destructive
	// Replace and wipe sibling embedded children.
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["docs/specs/*.md"]) {
  summary: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "untyped.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`# Untyped

Search-only fallback prose.
`), 0o644))
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8})

	syncer := OntologyNodeSyncer{
		Store:        store,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:       schema,
	}
	require.NoError(t, syncer.SyncNotePaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, []string{"notes/untyped.md"}, nil))

	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/untyped.md")
	require.NoError(t, err)
	fallback, ok := ontology.AsFallbackNoteProjection(projection)
	require.True(t, ok)
	fallbackNodeID := ontology.OntologyNodeID(fallback.Ref)
	chunks, err := store.IntelChunksByOwners(context.Background(), []string{fallbackNodeID})
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	var bodyChunk string
	for _, chunk := range chunks {
		if chunk.Granularity == embeddings.GranularityOntologyNodeBody {
			bodyChunk = chunk.ChunkID
		}
	}
	require.NotEmpty(t, bodyChunk)
	vecs, err := store.EmbeddingsByChunkIDs(context.Background(), []string{bodyChunk})
	require.NoError(t, err)
	require.NotEmpty(t, vecs[bodyChunk])
}

func TestOntologyNodeSyncer_EmbedsChangedChunksConcurrentlyWithGate(t *testing.T) {
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := &concurrentRecordingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}),
		delay: 20 * time.Millisecond,
	}
	syncer := OntologyNodeSyncer{
		Store:         store,
		Provider:      provider,
		ProviderInfo:  embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:        schema,
		BatchSize:     1,
		MaxConcurrent: 4,
		EmbedGate:     make(chan struct{}, 2),
	}

	require.NoError(t, syncer.SyncProjections(context.Background(), projection))
	require.Greater(t, provider.calls.Load(), int64(1))
	require.Greater(t, provider.maxInflight.Load(), int64(1))
	require.LessOrEqual(t, provider.maxInflight.Load(), int64(2))
}

func TestOntologyNodeSyncer_UsesProviderDefaultBatchSize(t *testing.T) {
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := &defaultingProvider{batchSize: 1, maxConcurrency: 1}
	syncer := OntologyNodeSyncer{
		Store:        store,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:       schema,
	}

	require.NoError(t, syncer.SyncProjections(context.Background(), projection))
	sizes := provider.BatchSizes()
	require.Greater(t, len(sizes), 1)
	for _, size := range sizes {
		require.Equal(t, 1, size)
	}
	set, err := BuildOntologyNodeChunks(schema, projection, syncer.ProviderInfo, 0)
	require.NoError(t, err)
	ids := make([]string, 0, len(set.Chunks))
	for _, chunk := range set.Chunks {
		ids = append(ids, chunk.ChunkID)
	}
	vectors, err := store.EmbeddingsByChunkIDs(context.Background(), ids)
	require.NoError(t, err)
	require.Len(t, vectors, len(ids))
}

func setupOntologyNodeChunkFixture(t *testing.T) (string, *ontoschema.Schema, *ontology.NodeProjection) {
	t.Helper()
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["notes/specs/*.md"]) {
  status: String @field
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  storyId: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "specs", "checkout.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
status: draft
---
# Checkout Refresh

## User Stories

### Faster checkout
storyId:: US-1
Users need quicker checkout.
`), 0o644))

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/checkout.md")
	require.NoError(t, err)
	return root, schema, projection
}

type codeState struct {
	schemaSignature string
	textHash        string
}

type countingOntologyNodeStore struct {
	*semdb.Store
	replaceNodes  atomic.Int64
	replaceChunks atomic.Int64
}

func (s *countingOntologyNodeStore) ReplaceOntologyNodeReadModel(ctx context.Context, model readmodel.NodeReadModel) error {
	s.replaceNodes.Add(1)
	return s.Store.ReplaceOntologyNodeReadModel(ctx, model)
}

func (s *countingOntologyNodeStore) ReplaceIntelChunks(ctx context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error {
	s.replaceChunks.Add(1)
	return s.Store.ReplaceIntelChunks(ctx, ownerIDs, chunks)
}

func (s *countingOntologyNodeStore) Reset() {
	s.replaceNodes.Store(0)
	s.replaceChunks.Store(0)
}

type concurrentRecordingProvider struct {
	inner       embeddings.Provider
	delay       time.Duration
	calls       atomic.Int64
	inflight    atomic.Int64
	maxInflight atomic.Int64
}

func (p *concurrentRecordingProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *concurrentRecordingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.calls.Add(1)
	current := p.inflight.Add(1)
	for {
		max := p.maxInflight.Load()
		if current <= max || p.maxInflight.CompareAndSwap(max, current) {
			break
		}
	}
	defer p.inflight.Add(-1)
	if p.delay > 0 {
		timer := time.NewTimer(p.delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return p.inner.EmbedTexts(ctx, texts)
}
