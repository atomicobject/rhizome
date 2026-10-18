package embedindex

import (
	"context"
	"errors"
	"fmt"

	"path/filepath"

	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	sqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	"github.com/stretchr/testify/require"
)

func TestSyncerBatchesIntelEmbeddingWritesAcrossItems(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	anchors := make([]codeanchor.IntelAnchor, 0, 48)
	for i := 0; i < 48; i++ {
		anchors = append(anchors, codeanchor.IntelAnchor{
			AnchorID:    fmt.Sprintf("a%d", i),
			Lang:        codeanchor.Lang("go"),
			Kind:        "func",
			Path:        fmt.Sprintf("pkg/file%d.go", i),
			Symbol:      fmt.Sprintf("Fn%d", i),
			FQN:         fmt.Sprintf("pkg.Fn%d", i),
			Fingerprint: fmt.Sprintf("fp-%d", i),
			UpdatedAt:   now,
		})
	}
	writer := &recordingEmbeddingWriter{}
	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           &stubIntelSource{anchors: anchors},
		PlanConcurrent:  1,
		Policy:          singleChunkPolicy{},
		EmbeddingWriter: writer,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.Less(t, writer.CallCount(), len(anchors), "intel writes should batch across items")
	require.Equal(t, len(anchors), writer.StoredCount())
}

func TestSyncerEmbedWithoutSyncMarkFlushesQueuedWritesOnce(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	anchors := []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/file.go",
		Symbol:      "Fn",
		FQN:         "pkg.Fn",
		Fingerprint: "fp-1",
		UpdatedAt:   now,
	}}
	queue := &countingSyncWriteQueue{}
	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           &stubIntelSource{anchors: anchors},
		Root:            tempDir,
		Policy:          singleChunkPolicy{},
		ChunkWriter:     noopChunkWriter{},
		EmbeddingWriter: noopEmbeddingWriter{},
		WriteQueue:      queue,
	}

	plan, err := syncer.Plan(context.Background())
	require.NoError(t, err)
	require.NoError(t, syncer.EmbedWithoutSyncMark(context.Background(), plan))
	require.EqualValues(t, 1, queue.flushes.Load())
	require.Zero(t, queue.singleItemCalls.Load())
	require.Greater(t, queue.batchItemCalls.Load(), int64(0))
	require.Zero(t, queue.singleChunkCalls.Load())
	require.Greater(t, queue.intelCalls.Load(), int64(0))
}

func TestSyncerDrainsQueuedWritebackOnFinalizeError(t *testing.T) {
	tempDir := t.TempDir()

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	anchors := []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/file.go",
		Symbol:      "A",
		FQN:         "pkg.A",
		Fingerprint: "fp-1",
		UpdatedAt:   now,
	}}
	queue := &countingSyncWriteQueue{}
	syncer := Syncer{
		Index:          failDeleteChunksIndex{Index: store},
		Provider:       prov,
		ProviderInfo:   embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:          &stubIntelSource{anchors: anchors},
		Root:           tempDir,
		BatchSize:      1,
		MaxConcurrent:  1,
		PlanConcurrent: 1,
		Policy:         singleChunkPolicy{},
		WriteQueue:     queue,
	}

	err = syncer.Sync(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "delete chunks failed after writeback")
	require.EqualValues(t, 1, queue.flushes.Load())
	require.Greater(t, queue.batchChunkCalls.Load(), int64(0))
	require.Greater(t, queue.batchItemCalls.Load(), int64(0))
}

func TestSyncerReturnsIntelEmbeddingFlushError(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	writer := &recordingEmbeddingWriter{failCall: 1, failErr: errors.New("boom")}
	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           &stubIntelSource{anchors: []codeanchor.IntelAnchor{{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/file.go", Symbol: "Fn", FQN: "pkg.Fn", Fingerprint: "fp-1", UpdatedAt: time.Now().Unix()}}},
		PlanConcurrent:  1,
		Policy:          singleChunkPolicy{},
		EmbeddingWriter: writer,
	}

	err = syncer.Sync(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
}
