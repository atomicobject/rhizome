package embedindex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestNoteDerivedMetricsMeasureSinkFailures(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(t.Context(), collector), "embed_notes")
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	syncer := NoteSyncer{Provider: provider}
	plan := NotePlan{
		tasks: []noteTask{{id: "notes/test.md", payload: noteTaskPayload{
			embedChunks: []embeddings.ChunkInput{embeddings.NewChunkInput(0, "new content", "Test", "Test")},
			embedTexts:  []string{"new content"},
		}}},
		TotalWork: 1,
	}
	want := errors.New("synthetic sink failure")
	err := syncer.ComputeDerived(ctx, plan, func(context.Context, NoteDerivedResult) error {
		// Keep the synthetic sink above coarse Windows clock resolution.
		time.Sleep(25 * time.Millisecond)
		return want
	})
	require.ErrorIs(t, err, want)
	require.Contains(t, collector.RenderSummary(), "finalize_cum=")
}

func TestDerivedFinalizersAreBoundedByProviderConcurrency(t *testing.T) {
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	syncer := NoteSyncer{Provider: provider, MaxConcurrent: 2}
	plan := NotePlan{TotalWork: 64}
	for i := range 64 {
		text := fmt.Sprintf("content %d", i)
		plan.tasks = append(plan.tasks, noteTask{id: embeddings.NoteID(fmt.Sprintf("notes/%d.md", i)), payload: noteTaskPayload{
			embedChunks: []embeddings.ChunkInput{embeddings.NewChunkInput(0, text, "T", "T")},
			embedTexts:  []string{text},
		}})
	}
	var active, peak, seen atomic.Int64
	require.NoError(t, syncer.ComputeDerived(t.Context(), plan, func(context.Context, NoteDerivedResult) error {
		n := active.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		// A slow sink must stall the pipeline rather than accumulate owners.
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		seen.Add(1)
		return nil
	}))
	require.EqualValues(t, 64, seen.Load())
	require.LessOrEqual(t, peak.Load(), int64(2))
}

// The live runtime publishes plain-note results through PublishDerived. The
// vectors it records answer a later plan for the same text, in any note, so
// the provider never sees that text again.
func TestNoteDerivedPublicationCachesVectorsForLaterPlans(t *testing.T) {
	ctx := t.Context()
	notes, err := embsqlite.OpenNotes(ctx, embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	defer func() { require.NoError(t, notes.Close()) }()
	intel, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	defer intel.Close()
	info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	syncer := NoteSyncer{Index: notes, Provider: embeddings.NewDeterministicProvider(info), ProviderInfo: info, EmbeddingWriter: intel, EmbeddingCache: intel}
	chunk := embeddings.NewChunkInput(0, "Install the tools first.", "Guide", "Setup")
	plan := NotePlan{TotalWork: 1, tasks: []noteTask{{id: "notes/guide.md", payload: noteTaskPayload{
		info: embeddings.NoteFileInfo{ID: "notes/guide.md", Path: "notes/guide.md"}, embedChunks: []embeddings.ChunkInput{chunk}, embedTexts: []string{chunk.Text},
	}}}}
	var published embeddings.Embedding
	require.NoError(t, syncer.ComputeDerived(ctx, plan, func(ctx context.Context, result NoteDerivedResult) error {
		published = result.vectors[0]
		return syncer.PublishDerived(ctx, result)
	}))

	reuse, vectors, missing, _, err := syncer.planChunkUpdates(ctx, "notes/copy.md", []embeddings.ChunkInput{chunk}, nil)
	require.NoError(t, err)
	require.Empty(t, missing, "cached text must not be sent to the provider again")
	require.Equal(t, []embeddings.ChunkInput{stripChunkText(chunk)}, reuse)
	require.Equal(t, []embeddings.Embedding{published}, vectors)
}

// failingTextProvider fails any request containing fail; other requests wait
// briefly, so they are still in flight when the failure ends the pass.
type failingTextProvider struct {
	embeddings.Provider
	fail string
}

func (p failingTextProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	for _, text := range texts {
		if text == p.fail {
			return nil, errors.New("provider rejected text")
		}
	}
	time.Sleep(20 * time.Millisecond)
	return p.Provider.EmbedTexts(ctx, texts)
}

// When the provider fails one section of a note, the note is not published,
// but the sections it did embed are cached: the retry sends only the section
// that failed.
func TestNoteComputeFailureCachesEmbeddedSectionsOfIncompleteNotes(t *testing.T) {
	ctx := t.Context()
	notes, err := embsqlite.OpenNotes(ctx, embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	defer func() { require.NoError(t, notes.Close()) }()
	intel, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	defer intel.Close()
	info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	chunks := []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "Install the tools first.", "Guide", "Setup"),
		embeddings.NewChunkInput(1, "Run the index after edits.", "Guide", "Usage"),
	}
	syncer := NoteSyncer{
		Index: notes, ProviderInfo: info, EmbeddingWriter: intel, EmbeddingCache: intel, MaxConcurrent: 2,
		Provider:        failingTextProvider{Provider: embeddings.NewDeterministicProvider(info), fail: chunks[1].Text},
		NoteEmbedPacker: &EmbedPackerOptions{MaxTexts: 1, MinTexts: 1},
	}
	plan := NotePlan{TotalWork: 2, tasks: []noteTask{{id: "notes/guide.md", payload: noteTaskPayload{
		info: embeddings.NoteFileInfo{ID: "notes/guide.md", Path: "notes/guide.md"}, embedChunks: chunks, embedTexts: []string{chunks[0].Text, chunks[1].Text},
	}}}}
	require.ErrorContains(t, syncer.processTasksPipelined(ctx, plan, nil), "provider rejected text")

	_, _, missing, _, err := syncer.planChunkUpdates(ctx, "notes/guide.md", chunks, nil)
	require.NoError(t, err)
	require.Equal(t, []embeddings.ChunkInput{chunks[1]}, missing, "only the failed section is sent again")
}

func TestOntologyDerivedComputeDoesNotPublishChunksOrVectors(t *testing.T) {
	ctx := context.Background()
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	defer store.Close()
	model, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))
	info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	s := OntologyNodeSyncer{Store: store, Provider: embeddings.NewDeterministicProvider(info), ProviderInfo: info, Schema: schema}
	plan, err := s.PrepareProjections(ctx, projection)
	require.NoError(t, err)
	require.NotEmpty(t, plan.texts)
	result, err := s.ComputeDerived(ctx, plan)
	require.NoError(t, err)
	chunks, err := store.IntelChunksByOwners(ctx, plan.owners)
	require.NoError(t, err)
	require.Empty(t, chunks)
	vecs, err := store.EmbeddingsByChunkIDs(ctx, plan.ids)
	require.NoError(t, err)
	require.Empty(t, vecs)
	require.NoError(t, s.PublishDerived(ctx, result))
	chunks, err = store.IntelChunksByOwners(ctx, plan.owners)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	vecs, err = store.EmbeddingsByChunkIDs(ctx, plan.ids)
	require.NoError(t, err)
	require.Len(t, vecs, len(plan.ids))
}

// blockingFirstProvider holds its first call until release closes, then
// answers it, as a provider that finishes after the pass was cancelled.
type blockingFirstProvider struct {
	embeddings.Provider
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (p *blockingFirstProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	first := false
	p.once.Do(func() { first = true; close(p.started) })
	if first {
		<-p.release
	}
	return p.Provider.EmbedTexts(context.Background(), texts)
}

// Cancelling a plain-note pass while its batch is at the shared node's
// provider must still cache the vectors that batch returns.
func TestNoteCancelledPassCachesSharedNodeInFlightVectors(t *testing.T) {
	notes, err := embsqlite.OpenNotes(t.Context(), embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	defer func() { require.NoError(t, notes.Close()) }()
	intel, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	defer intel.Close()
	info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	prov := &blockingFirstProvider{Provider: embeddings.NewDeterministicProvider(info), started: make(chan struct{}), release: make(chan struct{})}
	node := NewSharedEmbeddingNode(context.Background(), prov, 1, nil, EmbedPackerOptions{MinTexts: 1, MaxTexts: 8, MaxBytes: 1 << 20, MaxWait: time.Millisecond})
	defer node.Close()
	chunks := []embeddings.ChunkInput{embeddings.NewChunkInput(0, "Install the tools first.", "Guide", "Setup")}
	syncer := NoteSyncer{
		Index: notes, ProviderInfo: info, EmbeddingWriter: intel, EmbeddingCache: intel, MaxConcurrent: 1,
		Provider: prov, EmbeddingNode: node,
	}
	plan := NotePlan{TotalWork: 1, tasks: []noteTask{{id: "notes/guide.md", payload: noteTaskPayload{
		info: embeddings.NoteFileInfo{ID: "notes/guide.md", Path: "notes/guide.md"}, embedChunks: chunks, embedTexts: []string{chunks[0].Text},
	}}}}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- syncer.processTasksPipelined(ctx, plan, nil) }()
	<-prov.started
	cancel() // another lane failed while the batch is at the provider
	close(prov.release)
	require.Error(t, <-done)

	_, _, missing, _, err := syncer.planChunkUpdates(t.Context(), "notes/guide.md", chunks, nil)
	require.NoError(t, err)
	require.Empty(t, missing, "the vector the provider returned after cancellation is cached")
}
