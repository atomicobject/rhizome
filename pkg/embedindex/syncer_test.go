package embedindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"strings"

	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeidx "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	sqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestFileSystemLoaderRejectsEscapedPaths(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "inside.go"), []byte("inside"), 0o644))

	sibling := filepath.Join(parent, "root-sibling")
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, "secret.go"), []byte("outside"), 0o644))

	loader := FileSystemLoader(root)
	got, err := loader("inside.go")
	require.NoError(t, err)
	require.Equal(t, []byte("inside"), got)

	_, err = loader("../" + filepath.Base(sibling) + "/secret.go")
	require.Error(t, err)

	_, err = loader(filepath.Join(sibling, "secret.go"))
	require.Error(t, err)
}

func TestSyncerIndexesAnchorsAndChunks(t *testing.T) {
	t.Run("default policy removes stored field owner without fallback", func(t *testing.T) {
		ctx := context.Background()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "model.go"), []byte("package pkg\ntype Model struct { Count int }\n"), 0o644))
		prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
		store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(root, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		intelStore, err := semdb.Open(filepath.Join(root, "intel.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = intelStore.Close() })
		anchor := codeanchor.IntelAnchor{AnchorID: "field-1", Lang: codeanchor.LangGo, Kind: "field", Path: "pkg/model.go", Symbol: "Count", FQN: "pkg.Model.Count", Signature: "Count int", Fingerprint: "fp-field", UpdatedAt: time.Now().Unix()}
		intel := &storedChunkIntelSource{stubIntelSource: &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}}, store: intelStore}
		syncer := Syncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: intel, Root: root, Policy: singleChunkPolicy{}, ChunkWriter: intelStore, EmbeddingWriter: intelStore}
		require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
		stale, err := intelStore.IntelChunksByOwners(ctx, []string{"field-1"})
		require.NoError(t, err)
		require.Len(t, stale, 1)
		syncer.Policy = DefaultSynthesisPolicy(PolicyOptions{Budget: DefaultChunkBudget()})
		meta, ok, err := store.Metadata(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		intel.anchors[0].UpdatedAt = meta.SourceHighWater.Unix() + 1
		prov.Reset()
		require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
		chunks, err := store.ItemChunks(ctx, "field-1")
		require.NoError(t, err)
		require.Empty(t, chunks, "suppressed field must not use fallback chunk")
		chunksIntel, err := intelStore.IntelChunksByOwners(ctx, []string{"field-1"})
		require.NoError(t, err)
		require.Empty(t, chunksIntel)
		require.Zero(t, prov.calls.Load(), "cleanup should not embed a field")
	})
	t.Run("unchanged prefix removes stale extra chunk", func(t *testing.T) {
		ctx := context.Background()
		prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
		store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(t.TempDir(), "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		anchor := codeanchor.IntelAnchor{AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp", UpdatedAt: time.Now().Unix()}
		syncer := Syncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}}, Policy: multiChunkPolicy{}}
		require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
		before, err := store.ItemChunks(ctx, "a1")
		require.NoError(t, err)
		require.Len(t, before, 2)
		syncer.Policy = firstChunkOnlyPolicy{}
		prov.Reset()
		require.NoError(t, syncer.SyncPaths(ctx, []string{anchor.Path}))
		after, err := store.ItemChunks(ctx, "a1")
		require.NoError(t, err)
		require.Len(t, after, 1)
		require.Equal(t, 0, after[0].Index)
		require.Equal(t, before[0].Embedding, after[0].Embedding)
		require.Zero(t, prov.calls.Load())
	})

	for _, unified := range []bool{false, true} {
		name := "legacy"
		if unified {
			name = "unified"
		}
		t.Run(name+" reuses primary and embeds secondary", func(t *testing.T) {
			ctx := context.Background()
			prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
			store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(t.TempDir(), "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			reused := embeddings.Embedding{9, 8, 7, 6, 5, 4, 3, 2}
			require.NoError(t, store.CacheEmbedding(ctx, "h0-a1", reused))
			anchor := codeanchor.IntelAnchor{AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "func", Path: "pkg/foo.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp", UpdatedAt: time.Now().Unix()}
			syncer := Syncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: &stubIntelSource{anchors: []codeanchor.IntelAnchor{anchor}}, Policy: multiChunkPolicy{}}
			var intelStore *semdb.Store
			if unified {
				intelStore, err = semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = intelStore.Close() })
				syncer.ChunkWriter = intelStore
				syncer.EmbeddingWriter = intelStore
			}
			require.NoError(t, syncer.Sync(ctx))
			require.Equal(t, int64(1), prov.calls.Load())
			newVecs, err := prov.inner.EmbedTexts(ctx, []string{"anchor:a1:1"})
			require.NoError(t, err)
			if unified {
				chunks, err := intelStore.IntelChunksByOwners(ctx, []string{"a1"})
				require.NoError(t, err)
				require.Len(t, chunks, 2)
				ids := []string{chunks[0].ChunkID, chunks[1].ChunkID}
				vectors, err := intelStore.EmbeddingsByChunkIDs(ctx, ids)
				require.NoError(t, err)
				require.Equal(t, []int{0, 1}, []int{chunks[0].Ord, chunks[1].Ord})
				require.Equal(t, reused, vectors[ids[0]])
				require.Equal(t, newVecs[0], vectors[ids[1]])
			} else {
				chunks, err := store.ItemChunks(ctx, "a1")
				require.NoError(t, err)
				require.Len(t, chunks, 2)
				require.Equal(t, []int{0, 1}, []int{chunks[0].Index, chunks[1].Index})
				require.Equal(t, reused, chunks[0].Embedding)
				require.Equal(t, newVecs[0], chunks[1].Embedding)
				itemVec, _, ok, err := store.GetItemEmbedding(ctx, "a1")
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, reused, itemVec)
			}
		})
	}

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "pkg", "foo.go")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "package foo\n\n// test doc\nfunc DoThing() string {\n    return \"ok\"\n}\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	module := codeanchor.IntelAnchor{
		AnchorID:    "m1",
		Lang:        codeanchor.Lang("go"),
		Kind:        "module",
		Path:        "pkg/foo.go",
		Symbol:      "foo.go",
		Fingerprint: "fp-file",
		StartLine:   1,
		EndLine:     1,
		UpdatedAt:   time.Now().Unix(),
	}
	anchor := codeanchor.IntelAnchor{
		AnchorID:   "a1",
		Lang:       codeanchor.Lang("go"),
		Kind:       "func",
		Path:       "pkg/foo.go",
		Symbol:     "DoThing",
		FQN:        "foo.DoThing",
		Signature:  "func DoThing() string",
		DocComment: "test doc",
		StartLine:  3,
		EndLine:    5,
		UpdatedAt:  time.Now().Unix(),
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	storePath := filepath.Join(tempDir, "code.db")
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(storePath), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        &stubIntelSource{anchors: []codeanchor.IntelAnchor{module, anchor}},
		Root:         tempDir,
	}

	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if prov.calls.Load() == 0 {
		t.Fatalf("expected provider to be called on first sync")
	}

	items, chunks, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if items == 0 || chunks == 0 {
		t.Fatalf("expected items and chunks to be indexed, got %d items %d chunks", items, chunks)
	}

	storedChunks, err := store.ItemChunks(context.Background(), codeidx.AnchorID(anchor.AnchorID))
	if err != nil {
		t.Fatalf("fetch chunks: %v", err)
	}
	if len(storedChunks) < 1 {
		t.Fatalf("expected at least one chunk, got %d", len(storedChunks))
	}

	// Second pass should detect unchanged anchors/chunks and avoid re-embedding.
	prov.Reset()
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("sync second: %v", err)
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("expected no provider calls on second sync, got %d", prov.calls.Load())
	}
}

func TestSyncerPlanDoesNotDeadlock(t *testing.T) {
	tempDir := t.TempDir()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	anchors := make([]codeanchor.IntelAnchor, 0, 4)
	for i := 0; i < 4; i++ {
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

	syncer := Syncer{
		Index:          store,
		Provider:       prov,
		ProviderInfo:   embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:          &stubIntelSource{anchors: anchors},
		PlanConcurrent: 1,
		Policy:         singleChunkPolicy{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = syncer.Plan(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("plan hung waiting for workers")
	}
	require.NoError(t, err)
}

func TestSyncerPlanPassesRelatedDocsIntoAnchorChunks(t *testing.T) {
	tempDir := t.TempDir()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(tempDir, "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "func",
		Path:        "pkg/story.go",
		Symbol:      "ResolveStory",
		FQN:         "pkg.ResolveStory",
		Signature:   "func ResolveStory()",
		DocComment:  "Resolves a user story into structured answer context.",
		Fingerprint: "fp-1",
		UpdatedAt:   time.Now().Unix(),
	}
	intel := &stubIntelSource{
		anchors: []codeanchor.IntelAnchor{anchor},
		docLinks: map[string][]codeanchor.DocLink{
			"a1": {{SrcPath: "docs/specs/user-story-retrieval.md", Label: "User Story Retrieval"}, {SrcPath: "docs/specs/user-story-retrieval.md", Label: "User Story Retrieval"}, {SrcPath: "docs/specs/process.md"}},
		},
	}
	syncer := Syncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        intel,
	}

	plan, err := syncer.Plan(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, plan.tasks)

	var text string
	for _, task := range plan.tasks {
		if task.id == codeidx.AnchorID("a1") && len(task.payload.chunks) > 0 {
			text = task.payload.chunks[0].Text
			break
		}
	}
	require.Contains(t, text, "Related docs:")
	require.Contains(t, text, "User Story Retrieval (docs/specs/user-story-retrieval.md)")
	require.Contains(t, text, "process (docs/specs/process.md)")
	require.Equal(t, 1, strings.Count(text, "User Story Retrieval (docs/specs/user-story-retrieval.md)"))
	require.Less(t, strings.Index(text, "User Story Retrieval (docs/specs/user-story-retrieval.md)"), strings.Index(text, "process (docs/specs/process.md)"))
}
