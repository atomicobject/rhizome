package embedindex

import (
	"context"

	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeidx "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	sqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestSyncerReembedsOnFingerprintVersionMismatch(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "pkg", "foo.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("package foo\n\nfunc DoThing() {}\n"), 0o644))

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/foo.go",
		Symbol:      "DoThing",
		FQN:         "foo.DoThing",
		Fingerprint: "fp1",
		UpdatedAt:   time.Now().Unix(),
	}

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
		Intel:        &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}},
		Root:         tempDir,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	prov.Reset()

	// Simulate an older index fingerprint version after a release. Use a fresh
	// Syncer so ensureReady runs again, matching a new process after upgrade.
	require.NoError(t, store.SetFingerprintVersion(context.Background(), 0))

	// Clear the embedding cache so we can verify the provider is actually called.
	// (Without this, the embedding would be found in cache and no API call needed.)
	require.NoError(t, store.ClearEmbeddingCache(context.Background()))

	upgradedSyncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}},
		Root:         tempDir,
	}

	require.NoError(t, upgradedSyncer.Sync(context.Background()))
	require.Greater(t, prov.calls.Load(), int64(0), "expected re-embed when fingerprint version changes")

	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, codeFingerprintVersion, meta.FingerprintVersion)
}

func TestSyncerCodeEmbedPackerBatchesAcrossItems(t *testing.T) {
	for _, tc := range []struct {
		name                                           string
		owners, chunksPerOwner, batchSize, concurrency int
		packer                                         *EmbedPackerOptions
		wantBatches                                    []int
	}{
		{"default packer", 31, 1, 100, 250, nil, nil},
		{"explicit single chunks", 6, 1, 0, 0, &EmbedPackerOptions{MaxTexts: 3, MaxBytes: 1 << 20, MinTexts: 3, MaxWait: 200 * time.Millisecond}, []int{3, 3}},
		{"explicit multiple chunks", 6, 2, 0, 0, &EmbedPackerOptions{MaxTexts: 6, MaxBytes: 1 << 20, MinTexts: 4, MaxWait: 200 * time.Millisecond}, []int{4, 4, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			filePath := filepath.Join(root, "pkg", "foo.go")
			require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
			require.NoError(t, os.WriteFile(filePath, []byte("package foo\n"), 0o644))
			anchorsList := make([]codeanchor.IntelAnchor, 0, tc.owners)
			for i := 0; i < tc.owners; i++ {
				anchorsList = append(anchorsList, codeanchor.IntelAnchor{AnchorID: fmt.Sprintf("a%d", i), Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: fmt.Sprintf("Fn%d", i), FQN: fmt.Sprintf("foo.Fn%d", i), Fingerprint: fmt.Sprintf("fp%d", i), UpdatedAt: time.Now().Unix()})
			}
			prov := &batchRecordingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
			store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(root, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			var policy SynthesisPolicy = singleChunkPolicy{}
			if tc.chunksPerOwner == 2 {
				policy = multiChunkPolicy{}
			}
			syncer := Syncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: &stubIntelSource{anchors: anchorsList}, Root: root, BatchSize: tc.batchSize, MaxConcurrent: tc.concurrency, Policy: policy, CodeEmbedPacker: tc.packer}
			require.NoError(t, syncer.Sync(ctx))
			require.Positive(t, prov.calls.Load())
			if tc.wantBatches != nil {
				require.Equal(t, tc.wantBatches, prov.BatchSizes())
			} else {
				require.Less(t, prov.calls.Load(), int64(tc.owners))
			}
			for _, anchor := range anchorsList {
				chunks, err := store.ItemChunks(ctx, codeidx.AnchorID(anchor.AnchorID))
				require.NoError(t, err)
				require.Len(t, chunks, tc.chunksPerOwner, anchor.AnchorID)
				for j, chunk := range chunks {
					text := "anchor:" + anchor.AnchorID
					if tc.chunksPerOwner == 2 {
						text += ":" + strconv.Itoa(j)
					}
					want, err := prov.inner.EmbedTexts(ctx, []string{text})
					require.NoError(t, err)
					require.Equal(t, want[0], chunk.Embedding, "owner=%s chunk=%d", anchor.AnchorID, j)
				}
			}
		})
	}
}

func TestSyncerPiggybacksItemEmbeddingOnChunkBatch(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "pkg", "foo.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("package foo\n"), 0o644))

	prov := &batchRecordingProvider{
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
			Path:        "pkg/foo.go",
			Symbol:      "DoThing",
			FQN:         "foo.DoThing",
			Fingerprint: "fp1",
			UpdatedAt:   time.Now().Unix(),
		}}},
		Root:      tempDir,
		Policy:    multiChunkPolicy{},
		BatchSize: 16,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.Equal(t, int64(1), prov.calls.Load(), "expected one provider call when item embedding reuses chunk 0")
	require.Equal(t, []int{2}, prov.BatchSizes())

	itemVec, _, ok, err := store.GetItemEmbedding(context.Background(), codeidx.AnchorID("a1"))
	require.NoError(t, err)
	require.True(t, ok)
	chunks, err := store.ItemChunks(context.Background(), "a1")
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, chunks[0].Embedding, itemVec)
}

func TestSyncerUsesIntelChunkStatesWhenEmbeddingWriterEnabled(t *testing.T) {
	tempDir := t.TempDir()

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/foo.go",
		Symbol:      "DoThing",
		FQN:         "foo.DoThing",
		Fingerprint: "fp1",
		UpdatedAt:   time.Now().Unix(),
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	intelStore, err := semdb.Open(filepath.Join(tempDir, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	intel := &storedChunkIntelSource{
		stubIntelSource: &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}},
		store:           intelStore,
	}
	syncer := Syncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           intel,
		Policy:          singleChunkPolicy{},
		ChunkWriter:     intelStore,
		EmbeddingWriter: intelStore,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.Greater(t, prov.calls.Load(), int64(0), "expected provider to be called on first sync")

	hashes, err := store.ChunkHashes(context.Background(), codeidx.AnchorID(anchor.AnchorID))
	require.NoError(t, err)
	require.Len(t, hashes, 0, "expected no legacy chunk hashes when EmbeddingWriter is set")
	chunks, err := intelStore.IntelChunksByOwners(context.Background(), []string{anchor.AnchorID})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	vectors, err := intelStore.EmbeddingsByChunkIDs(context.Background(), []string{chunks[0].ChunkID})
	require.NoError(t, err)
	require.Len(t, vectors[chunks[0].ChunkID], prov.Dimensions())
	states, err := intel.IntelChunkEmbeddingStates(context.Background(), []string{anchor.AnchorID})
	require.NoError(t, err)
	require.Equal(t, chunks[0].ContentHash, states[anchor.AnchorID][chunks[0].Ord])

	prov.Reset()
	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	intel.anchors[0].UpdatedAt = meta.SourceHighWater.Unix() + 1
	require.Greater(t, intel.anchors[0].UpdatedAt, meta.SourceHighWater.Unix())
	plan, err := syncer.Plan(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, plan.TotalWork)
	// TotalWork excludes cache hits, so discarded chunk states would still plan zero work.
	require.Equal(t, 0, plan.CacheHits, "unchanged Intel chunk states must leave no task planned")
	stateCallsBefore := intel.stateCalls.Load()

	require.NoError(t, syncer.Sync(context.Background()))
	require.Greater(t, intel.stateCalls.Load(), stateCallsBefore, "second sync must read persisted Intel chunk states")
	require.Equal(t, int64(0), prov.calls.Load(), "expected intel chunk hashes to skip re-embedding")
	retained, err := intelStore.EmbeddingsByChunkIDs(context.Background(), []string{chunks[0].ChunkID})
	require.NoError(t, err)
	require.Equal(t, vectors[chunks[0].ChunkID], retained[chunks[0].ChunkID])
}
