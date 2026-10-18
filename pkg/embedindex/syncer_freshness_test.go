package embedindex

import (
	"context"

	"os"
	"path/filepath"

	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	sqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	"github.com/stretchr/testify/require"
)

func TestSyncerEmbedWithoutSyncMarkLeavesLastSyncUnsetUntilMarked(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           &stubIntelSource{anchors: []codeanchor.IntelAnchor{{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/file.go", Symbol: "Fn", FQN: "pkg.Fn", Fingerprint: "fp-1", UpdatedAt: time.Now().Unix()}}},
		PlanConcurrent:  1,
		Policy:          singleChunkPolicy{},
		EmbeddingWriter: noopEmbeddingWriter{},
	}

	plan, err := syncer.Plan(context.Background())
	require.NoError(t, err)
	require.NotZero(t, plan.TaskCount())

	require.NoError(t, syncer.EmbedWithoutSyncMark(context.Background(), plan))

	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, meta.LastSync.IsZero())
	require.True(t, meta.SourceHighWater.IsZero())

	require.NoError(t, syncer.MarkLastSync(context.Background()))

	meta, ok, err = store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, meta.LastSync.IsZero())
	require.False(t, meta.SourceHighWater.IsZero())
}

func TestSyncerFullSyncUsesSourceHighWaterForSkip(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "file.go"), []byte("package pkg\n"), 0o644))
	ctx := context.Background()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/file.go", Symbol: "Fn", FQN: "pkg.Fn", Fingerprint: "fp-1", UpdatedAt: 100}
	syncer := Syncer{
		Index:          store,
		Provider:       prov,
		ProviderInfo:   embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:          &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}},
		Root:           tempDir,
		PlanConcurrent: 1,
		Policy:         singleChunkPolicy{},
	}
	require.NoError(t, syncer.Sync(ctx))
	prov.Reset()

	require.NoError(t, store.UpdateLastSync(ctx, time.Unix(10_000, 0)))
	anchor.Fingerprint = "fp-2"
	anchor.UpdatedAt = 150
	syncer.Intel = &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}}

	plan, err := syncer.Plan(ctx)
	require.NoError(t, err)
	require.False(t, plan.ShouldSkipEmbed(), "future diagnostic last_sync must not hide newer source rows")
	require.NoError(t, syncer.Embed(ctx, plan))

	meta, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(150), meta.SourceHighWater.Unix())
}

func TestSyncerSyncPathsDoesNotMarkLastSync(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "file.go"), []byte("package pkg\n"), 0o644))

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel: &stubIntelSource{anchors: []codeanchor.IntelAnchor{{
			AnchorID:    "a1",
			Lang:        codeanchor.Lang("go"),
			Kind:        "func",
			Path:        "pkg/file.go",
			Symbol:      "Fn",
			FQN:         "pkg.Fn",
			Fingerprint: "fp-1",
			UpdatedAt:   time.Now().Unix(),
		}}},
		Root:   tempDir,
		Policy: singleChunkPolicy{},
	}

	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"pkg/file.go"}))

	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, meta.LastSync.IsZero())
}
