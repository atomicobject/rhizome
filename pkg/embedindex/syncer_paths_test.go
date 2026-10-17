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

	"github.com/stretchr/testify/require"
)

func TestSyncerSyncPathsPrunesOnlyTargetPath(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "bar.go"), []byte("package bar\n"), 0o644))

	intel := &stubIntelSource{anchors: []codeanchor.IntelAnchor{
		{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp-a1", UpdatedAt: time.Now().Unix()},
		{AnchorID: "b1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/bar.go", Symbol: "Bar", FQN: "pkg.Bar", Fingerprint: "fp-b1", UpdatedAt: time.Now().Unix()},
	}}
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
		Intel:        intel,
		Root:         tempDir,
		Policy:       singleChunkPolicy{},
	}
	require.NoError(t, syncer.Sync(context.Background()))

	intel.anchors = []codeanchor.IntelAnchor{
		{AnchorID: "b1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/bar.go", Symbol: "Bar", FQN: "pkg.Bar", Fingerprint: "fp-b1", UpdatedAt: time.Now().Unix()},
	}
	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"pkg/foo.go"}))

	items, err := store.ListItems(context.Background())
	require.NoError(t, err)
	var have []string
	for _, item := range items {
		have = append(have, string(item.AnchorID))
	}
	require.Equal(t, []string{"b1"}, have)
}

func TestSyncerSyncPathsBatchesAcrossPaths(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "bar.go"), []byte("package bar\n"), 0o644))

	var anchorsList []codeanchor.IntelAnchor
	for i := 0; i < 3; i++ {
		anchorsList = append(anchorsList,
			codeanchor.IntelAnchor{
				AnchorID:    fmt.Sprintf("fa%d", i),
				Lang:        codeanchor.Lang("go"),
				Kind:        "func",
				Path:        "pkg/foo.go",
				Symbol:      fmt.Sprintf("Foo%d", i),
				FQN:         fmt.Sprintf("pkg.Foo%d", i),
				Fingerprint: fmt.Sprintf("fp-f-%d", i),
				UpdatedAt:   time.Now().Unix(),
			},
			codeanchor.IntelAnchor{
				AnchorID:    fmt.Sprintf("ba%d", i),
				Lang:        codeanchor.Lang("go"),
				Kind:        "func",
				Path:        "pkg/bar.go",
				Symbol:      fmt.Sprintf("Bar%d", i),
				FQN:         fmt.Sprintf("pkg.Bar%d", i),
				Fingerprint: fmt.Sprintf("fp-b-%d", i),
				UpdatedAt:   time.Now().Unix(),
			},
		)
	}

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
		Intel:        &stubIntelSource{anchors: anchorsList},
		Root:         tempDir,
		Policy:       singleChunkPolicy{},
		CodeEmbedPacker: &EmbedPackerOptions{
			MaxTexts: 6,
			MaxBytes: 1 << 20,
			MinTexts: 4,
			MaxWait:  200 * time.Millisecond,
		},
	}

	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"pkg/foo.go", "pkg/bar.go"}))
	require.Equal(t, int64(2), prov.calls.Load())
	require.ElementsMatch(t, []int{2, 4}, prov.BatchSizes())
}

func TestSyncerPlanPrefetchesReuseCacheAndPrepUsesMemory(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\n"), 0o644))

	intel := &stubIntelSource{anchors: []codeanchor.IntelAnchor{{
		AnchorID:    "fa",
		Lang:        codeanchor.Lang("go"),
		Kind:        "func",
		Path:        "pkg/foo.go",
		Symbol:      "Foo",
		FQN:         "pkg.Foo",
		Fingerprint: "fp-fa",
		UpdatedAt:   time.Now().Unix(),
	}}}
	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	seedVec, err := prov.inner.EmbedTexts(context.Background(), []string{"shared-text"})
	require.NoError(t, err)
	require.NoError(t, store.CacheEmbedding(context.Background(), "shared-hash", seedVec[0]))

	tracked := &countingEmbeddingLookupIndex{Index: store}
	syncer := Syncer{
		Index:        tracked,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        intel,
		Root:         tempDir,
		Policy:       sharedHashChunkPolicy{hash: "shared-hash", text: "shared-text"},
	}

	prepared, err := syncer.PreparePathsEarly(context.Background(), []string{"pkg/foo.go"})
	require.NoError(t, err)
	plan, err := syncer.PlanPreparedPaths(context.Background(), prepared)
	require.NoError(t, err)
	require.Positive(t, tracked.batchCalls.Load())
	require.Zero(t, tracked.pointCalls.Load())
	require.NoError(t, syncer.EmbedWithoutSyncMark(context.Background(), plan))
	require.Zero(t, tracked.pointCalls.Load())
	require.Zero(t, prov.calls.Load())
	chunks, err := store.ItemChunks(context.Background(), "fa")
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, seedVec[0], chunks[0].Embedding)
	t.Run("multiple owners use one batch lookup", func(t *testing.T) {
		ctx := context.Background()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "foo.go"), []byte("package foo\n"), 0o644))
		prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
		store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(root, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		var anchors []codeanchor.IntelAnchor
		want := map[codeidx.AnchorID]embeddings.Embedding{}
		for i := 0; i < 4; i++ {
			id := codeidx.AnchorID("a" + strconv.Itoa(i))
			anchors = append(anchors, codeanchor.IntelAnchor{AnchorID: string(id), Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: "Fn" + strconv.Itoa(i), FQN: "pkg.Fn" + strconv.Itoa(i), Fingerprint: "fp-" + strconv.Itoa(i), UpdatedAt: time.Now().Unix()})
			vecs, err := prov.inner.EmbedTexts(ctx, []string{"anchor:" + string(id)})
			require.NoError(t, err)
			want[id] = vecs[0]
			require.NoError(t, store.CacheEmbedding(ctx, "h-"+string(id), vecs[0]))
		}
		tracked := &countingEmbeddingLookupIndex{Index: store}
		syncer := Syncer{Index: tracked, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: &stubIntelSource{anchors: anchors}, Root: root, Policy: singleChunkPolicy{}}
		plan, err := syncer.Plan(ctx)
		require.NoError(t, err)
		require.Positive(t, tracked.batchCalls.Load())
		require.GreaterOrEqual(t, tracked.batchHashes.Load(), int64(4))
		require.NoError(t, syncer.EmbedWithoutSyncMark(ctx, plan))
		require.Zero(t, tracked.pointCalls.Load())
		require.Zero(t, prov.calls.Load())
		for id, vector := range want {
			chunks, err := store.ItemChunks(ctx, id)
			require.NoError(t, err)
			require.Len(t, chunks, 1)
			require.Equal(t, vector, chunks[0].Embedding)
		}
	})

	t.Run("completed owner warms later owner", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "pkg"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, "pkg", "foo.go"), []byte("package foo\n"), 0o644))

		intel := &stubIntelSource{anchors: []codeanchor.IntelAnchor{
			{AnchorID: "a1", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp-a1", UpdatedAt: time.Now().Unix()},
			{AnchorID: "a2", Lang: codeanchor.Lang("go"), Kind: "func", Path: "pkg/foo.go", Symbol: "Bar", FQN: "pkg.Bar", Fingerprint: "fp-a2", UpdatedAt: time.Now().Unix()},
		}}
		prov := &countingProvider{
			inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
		}
		store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		tracked := &countingEmbeddingLookupIndex{Index: store}
		syncer := Syncer{
			Index:        tracked,
			Provider:     prov,
			ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
			Intel:        intel,
			Root:         tempDir,
			Policy:       sharedHashChunkPolicy{hash: "shared-hash", text: "shared-text"},
		}

		syncer.MaxConcurrent = 1
		ctx := context.Background()
		plan, err := syncer.Plan(ctx)
		require.NoError(t, err)
		require.Len(t, plan.tasks, 2)
		first, second := plan, plan
		first.tasks = plan.tasks[:1]
		second.tasks = plan.tasks[1:]
		require.NoError(t, syncer.EmbedWithoutSyncMark(ctx, first))
		require.EqualValues(t, 1, prov.calls.Load(), "first owner must complete its provider request")
		require.NoError(t, syncer.EmbedWithoutSyncMark(ctx, second))
		require.EqualValues(t, 1, prov.calls.Load(), "second owner should reuse first completed embedding")
		require.Zero(t, tracked.pointCalls.Load())
		want, err := prov.inner.EmbedTexts(ctx, []string{"shared-text"})
		require.NoError(t, err)
		for _, id := range []codeidx.AnchorID{"a1", "a2"} {
			chunks, err := store.ItemChunks(ctx, id)
			require.NoError(t, err)
			require.Len(t, chunks, 1)
			require.Equal(t, want[0], chunks[0].Embedding)
		}
	})
}
