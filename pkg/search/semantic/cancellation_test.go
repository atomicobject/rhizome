package semantic

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/stretchr/testify/require"
)

// A cancelled indexing job must stop embedding work where it is, not at the
// end of the cycle (SPEC-0104 US3).
func TestNoteSyncerSyncPathsStopsOnCancellation(t *testing.T) {
	tempDir := t.TempDir()
	notePath := "notes/cancelled.md"
	sections := []codeanchor.IntelDocSection{{
		SectionID:   "section-1",
		Path:        notePath,
		Title:       "First",
		Level:       2,
		Content:     "content",
		Fingerprint: "fp-1",
		UpdatedAt:   time.Now().Unix(),
	}}

	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := embsqlite.Open(filepath.Join(tempDir, "notes.db"), provider.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := NoteSyncer{
		Index:        store,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{notePath: sections}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, syncer.SyncPaths(ctx, []string{notePath}), context.Canceled)

	chunks, err := store.NoteChunks(context.Background(), embeddings.NoteID(notePath))
	require.NoError(t, err)
	require.Empty(t, chunks, "a cancelled sync must not write embedding rows")
}

func TestCodeSyncerSyncStopsOnCancellation(t *testing.T) {
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	syncer := Syncer{Provider: provider}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)
}
