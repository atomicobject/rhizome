package embedindex

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestDerivedCancellationCachesInFlightVectorsAndSkipsQueuedTexts(t *testing.T) {
	for _, kind := range []string{"notes", "code", "ontology"} {
		for _, route := range []string{"direct", "shared"} {
			t.Run(kind+"/"+route, func(t *testing.T) {
				info := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
				counted := &countingProvider{inner: embeddings.NewDeterministicProvider(info)}
				provider := &blockingFirstProvider{Provider: counted, started: make(chan struct{}), release: make(chan struct{})}
				var node *SharedEmbeddingNode
				opts := EmbedPackerOptions{MinTexts: 1, MaxTexts: 1, MaxWait: time.Millisecond}
				if route == "shared" {
					node = NewSharedEmbeddingNode(t.Context(), provider, 1, nil, opts)
					t.Cleanup(node.Close)
				}
				intel, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, intel.Close()) })
				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)
				done := make(chan error, 1)
				first, second := embeddings.NewChunkInput(0, "first", "T", "T"), embeddings.NewChunkInput(1, "second", "T", "T")
				var checkCache func()
				switch kind {
				case "notes":
					s := NoteSyncer{Provider: provider, ProviderInfo: info, EmbeddingCache: intel, EmbeddingNode: node, MaxConcurrent: 1, NoteEmbedPacker: &opts}
					plan := NotePlan{TotalWork: 2, tasks: []noteTask{{id: "note.md", payload: noteTaskPayload{embedChunks: []embeddings.ChunkInput{first, second}, embedTexts: []string{"first", "second"}}}}}
					go func() {
						var results []NoteDerivedResult
						err := s.ComputeDerived(ctx, plan, func(_ context.Context, result NoteDerivedResult) error { results = append(results, result); return nil })
						var incomplete *IncompleteDerivedError[NoteDerivedResult]
						if errors.As(err, &incomplete) {
							results = append(results, incomplete.Results...)
						}
						done <- errors.Join(err, s.CacheDerived(t.Context(), results...))
					}()
					checkCache = func() {
						cached, err := intel.EmbeddingCacheVectors(t.Context(), info.Provider, info.Model, 8, []string{first.Hash, second.Hash})
						require.NoError(t, err)
						require.Contains(t, cached, first.Hash)
						require.NotContains(t, cached, second.Hash)
					}
				case "code":
					index, err := embsqlite.OpenCode(t.Context(), embsqlite.File(filepath.Join(t.TempDir(), "code.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
					require.NoError(t, err)
					t.Cleanup(func() { require.NoError(t, index.Close()) })
					s := Syncer{Index: index, Provider: provider, EmbeddingNode: node, MaxConcurrent: 1, CodeEmbedPacker: &opts}
					plan := SyncPlan{TotalWork: 1, state: codePlanState{reuseCache: newCodeReuseCache()}, tasks: []codeTask{{id: "owner", payload: codeTaskPayload{chunks: []SemanticChunk{
						{Input: codeindex.ChunkInput{Index: 0, Hash: first.Hash}, Text: "first"},
						{Input: codeindex.ChunkInput{Index: 1, Hash: second.Hash}, Text: "second"},
					}}}}}
					go func() {
						var results []CodeDerivedResult
						err := s.ComputeDerived(ctx, plan, func(_ context.Context, result CodeDerivedResult) error { results = append(results, result); return nil })
						var incomplete *IncompleteDerivedError[CodeDerivedResult]
						if errors.As(err, &incomplete) {
							results = append(results, incomplete.Results...)
						}
						done <- errors.Join(err, s.CacheDerived(t.Context(), results...))
					}()
					checkCache = func() {
						vectors, err := index.EmbeddingByHashes(t.Context(), []string{first.Hash, second.Hash})
						require.NoError(t, err)
						require.Contains(t, vectors, first.Hash)
						require.NotContains(t, vectors, second.Hash)
					}
				case "ontology":
					s := OntologyNodeSyncer{Store: intel, Provider: provider, ProviderInfo: info, EmbeddingNode: node, MaxConcurrent: 1, BatchSize: 1}
					plan := OntologyDerivedPlan{ids: []string{"first", "second"}, texts: []string{"first", "second"}, textHashes: map[string]string{"first": first.Hash, "second": second.Hash}}
					go func() {
						result, err := s.ComputeDerived(ctx, plan)
						done <- errors.Join(err, s.CacheDerived(t.Context(), result))
					}()
					checkCache = func() {
						cached, err := intel.EmbeddingCacheVectors(t.Context(), info.Provider, info.Model, 8, []string{first.Hash, second.Hash})
						require.NoError(t, err)
						require.Contains(t, cached, first.Hash)
						require.NotContains(t, cached, second.Hash)
					}
				}
				<-provider.started
				cancel()
				close(provider.release)
				require.ErrorIs(t, <-done, context.Canceled)
				require.EqualValues(t, 1, counted.calls.Load(), "queued texts must not reach the provider after cancellation")
				checkCache()
			})
		}
	}
}
