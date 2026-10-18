package embedindex

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/stretchr/testify/require"
)

// Embedding the required Index port deliberately omits optional batch methods.
// Both fallback lookups must be admitted before either finishes.
type parallelNoteHashIndex struct {
	embeddings.Index
	arrivals atomic.Int64
	release  chan struct{}
}

func (i *parallelNoteHashIndex) ChunkHashes(ctx context.Context, id embeddings.NoteID) (map[int]string, error) {
	if i.arrivals.Add(1) == 2 {
		close(i.release)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-i.release:
		return nil, nil
	}
}

func TestNotePlanParallelFallbackHashLookups(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := embsqlite.OpenNotes(ctx, embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	index := &parallelNoteHashIndex{Index: store, release: make(chan struct{})}
	syncer := NoteSyncer{
		Index: index, Provider: provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Dimensions: 8},
		Intel: stubDocIntelSource{sections: []codeanchor.IntelDocSection{
			{SectionID: "first", Path: "first.md", Title: "First", Content: "First authored source", UpdatedAt: 1},
			{SectionID: "second", Path: "second.md", Title: "Second", Content: "Second authored source", UpdatedAt: 1},
		}},
		PlanConcurrent: 2,
	}
	plan, err := syncer.Plan(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, plan.TotalWork)
	require.Equal(t, int64(2), index.arrivals.Load())
	var actual []string
	for _, task := range plan.tasks {
		actual = append(actual, string(task.id))
	}
	require.ElementsMatch(t, []string{"first.md", "second.md"}, actual)
}
