package indexing

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	codeemb "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSubmitStaleCodeEmbeddingCleanup_ZeroCodeOwnersQueuesAllStoredPaths(t *testing.T) {
	t.Parallel()

	store, err := codeemb.Open(filepath.Join(t.TempDir(), "code.db"), 8)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	require.NoError(t, store.UpsertItemMetaBatch(context.Background(), []codeindex.Item{
		{AnchorID: "a1", Lang: "go", Kind: "function", Path: "src/removed.go", Symbol: "Removed", FQN: "src.Removed", Fingerprint: "removed", UpdatedAt: now},
		{AnchorID: "a2", Lang: "go", Kind: "function", Path: "src/other.go", Symbol: "Other", FQN: "src.Other", Fingerprint: "other", UpdatedAt: now},
	}))

	streaming := &unifiedStreamingCoordinator{
		ctx:          context.Background(),
		seenCodePath: make(map[string]struct{}),
		codeCh:       make(chan codeStreamingBatch, 1),
	}
	require.NoError(t, submitStaleCodeEmbeddingCleanup(context.Background(), streaming, store, nil))

	select {
	case batch := <-streaming.codeCh:
		require.Equal(t, []string{"src/other.go", "src/removed.go"}, batch.indexedPaths)
		require.True(t, batch.prepareIndexedPaths)
	case <-time.After(time.Second):
		t.Fatal("expected stale code cleanup batch")
	}
}
