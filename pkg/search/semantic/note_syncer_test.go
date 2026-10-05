package semantic

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/stretchr/testify/require"
)

type stubDocIntelSource struct {
	sections []codeanchor.IntelDocSection
}

func (s stubDocIntelSource) IntelAnchors(ctx context.Context) ([]codeanchor.IntelAnchor, error) {
	return nil, nil
}

func (s stubDocIntelSource) IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error) {
	return s.sections, nil
}

func (s stubDocIntelSource) IntelEdges(ctx context.Context) ([]codeanchor.IntelEdge, error) {
	return nil, nil
}

type stubDocIntelSourceByPath struct {
	byPath map[string][]codeanchor.IntelDocSection
}

type recordingNoteWriteQueue struct {
	syncs []embeddings.NoteChunkSync
	store *embsqlite.Store
}

func (q *recordingNoteWriteQueue) SubmitNoteMeta(ctx context.Context, info embeddings.NoteFileInfo) error {
	if q.store != nil {
		return q.store.UpsertNoteMeta(ctx, info)
	}
	return nil
}

func (q *recordingNoteWriteQueue) SubmitNoteChunkSync(ctx context.Context, item embeddings.NoteChunkSync) error {
	q.syncs = append(q.syncs, item)
	if q.store != nil {
		return q.store.SyncNoteChunks(ctx, item)
	}
	return nil
}

func (q *recordingNoteWriteQueue) SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error {
	return nil
}

func (q *recordingNoteWriteQueue) SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error {
	return nil
}

func (q *recordingNoteWriteQueue) SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error {
	return nil
}

func (q *recordingNoteWriteQueue) FlushAndWait(context.Context) error {
	return nil
}

func (s stubDocIntelSourceByPath) IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error) {
	var sections []codeanchor.IntelDocSection
	for _, pathSections := range s.byPath {
		sections = append(sections, pathSections...)
	}
	return sections, nil
}

func (s stubDocIntelSourceByPath) IntelDocSectionsByPath(ctx context.Context, path string) ([]codeanchor.IntelDocSection, error) {
	return append([]codeanchor.IntelDocSection(nil), s.byPath[path]...), nil
}

type stubRawEligibility struct {
	typed map[string]bool
}

func (s stubRawEligibility) RawNoteEmbeddingEligibility(ctx context.Context, paths []string) (RawNoteEmbeddingEligibility, error) {
	out := RawNoteEmbeddingEligibility{RawEligible: map[string]bool{}}
	for _, path := range paths {
		out.RawEligible[path] = !s.typed[path]
		if s.typed[path] {
			out.TypedPaths = append(out.TypedPaths, path)
		}
	}
	return out, nil
}

func TestNoteSyncerSkipsTypedNotesAndPrunesRawSurfaces(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	noteStore, err := embsqlite.Open(filepath.Join(t.TempDir(), "notes.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = noteStore.Close() })
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	now := time.Now().Unix()
	typed := "docs/typed.md"
	untyped := "docs/untyped.md"
	typedSection := codeanchor.IntelDocSection{SectionID: "typed-sec", Path: typed, Title: "Typed", Level: 1, Content: "typed prose", Fingerprint: "typed-fp", UpdatedAt: now}
	untypedSection := codeanchor.IntelDocSection{SectionID: "untyped-sec", Path: untyped, Title: "Untyped", Level: 1, Content: "untyped prose", Fingerprint: "untyped-fp", UpdatedAt: now}
	require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, typed, []codeanchor.IntelDocSection{typedSection}, nil, nil))
	require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, untyped, []codeanchor.IntelDocSection{untypedSection}, nil, nil))

	require.NoError(t, noteStore.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: embeddings.NoteID(typed), Path: typed, Title: "Typed", Mtime: time.Now()}))
	require.NoError(t, noteStore.UpsertNoteChunks(ctx, embeddings.NoteID(typed), []embeddings.ChunkInput{embeddings.NewChunkInput(0, "old typed raw", "Typed", "Typed")}, []embeddings.Embedding{{1, 0, 0, 0, 0, 0, 0, 0}}))
	require.NoError(t, intelStore.ReplaceIntelChunksByFamily(ctx, []string{"typed-sec"}, codeanchor.IntelChunkFamilyAuthoredSection, []codeanchor.IntelChunk{{
		ChunkID:     "typed-raw-chunk",
		OwnerID:     "typed-sec",
		OwnerType:   "doc_section",
		ChunkFamily: codeanchor.IntelChunkFamilyAuthoredSection,
		Ord:         0,
		Granularity: "section",
		ContentHash: "old",
		UpdatedAt:   now,
	}}))
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"typed-raw-chunk": {1, 0, 0, 0, 0, 0, 0, 0}}))

	syncer := NoteSyncer{
		Index:           noteStore,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{typed: {typedSection}, untyped: {untypedSection}}},
		ChunkWriter:     intelStore,
		EmbeddingWriter: intelStore,
		RawEligibility:  stubRawEligibility{typed: map[string]bool{typed: true}},
	}

	require.NoError(t, syncer.Sync(ctx))

	typedChunks, err := noteStore.NoteChunks(ctx, embeddings.NoteID(typed))
	require.NoError(t, err)
	require.Empty(t, typedChunks)
	untypedIntelChunks, err := intelStore.IntelChunksByOwners(ctx, []string{"untyped-sec"})
	require.NoError(t, err)
	require.NotEmpty(t, untypedIntelChunks)
	untypedEmbeddings, err := intelStore.EmbeddingsByChunkIDs(ctx, []string{untypedIntelChunks[0].ChunkID})
	require.NoError(t, err)
	require.NotEmpty(t, untypedEmbeddings)
	rawEmbeddings, err := intelStore.EmbeddingsByChunkIDs(ctx, []string{"typed-raw-chunk"})
	require.NoError(t, err)
	require.Empty(t, rawEmbeddings)
}

func TestOntologyRawNoteEligibilitySkipsAllNotesWhenOntologyReady(t *testing.T) {
	ctx := context.Background()
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })
	require.NoError(t, intelStore.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", MaterializationVersion: ontology.OntologyMaterializationVersion, LoadedAt: 1, Ready: true},
		Assessments: []semdb.OntologyNoteAssessmentRow{{
			NotePath:       "docs/typed.md",
			ResolvedType:   "ReferenceDoc",
			AssessmentJSON: `{"notePath":"docs/typed.md","resolvedType":"ReferenceDoc"}`,
			SchemaHash:     "schema",
			UpdatedAt:      1,
		}, {
			NotePath:       "docs/invalid.md",
			DeclaredType:   "MissingType",
			AssessmentJSON: `{"notePath":"docs/invalid.md","declaredType":"MissingType","issues":[{"code":"unknown_declared_type","message":"bad"}]}`,
			SchemaHash:     "schema",
			UpdatedAt:      1,
		}},
	}))

	eligibility, err := NewOntologyRawNoteEligibility(intelStore).RawNoteEmbeddingEligibility(ctx, []string{
		"docs/typed.md",
		"docs/untyped.md",
		"docs/invalid.md",
	})
	require.NoError(t, err)
	require.False(t, eligibility.RawEligible["docs/typed.md"])
	require.False(t, eligibility.RawEligible["docs/untyped.md"])
	require.False(t, eligibility.RawEligible["docs/invalid.md"])
}

func TestOntologyRawNoteEligibilityKeepsRawWhenOntologyUnavailable(t *testing.T) {
	ctx := context.Background()
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	eligibility, err := NewOntologyRawNoteEligibility(intelStore).RawNoteEmbeddingEligibility(ctx, []string{"docs/untyped.md"})
	require.NoError(t, err)
	require.True(t, eligibility.RawEligible["docs/untyped.md"])
}

func TestOntologyRawNoteEligibilityKeepsRawWhenMaterializationVersionIsStale(t *testing.T) {
	ctx := context.Background()
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })
	require.NoError(t, intelStore.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		SchemaState: semdb.OntologySchemaState{
			SchemaHash:             "schema",
			NotesHash:              "notes",
			MaterializationVersion: ontology.OntologyMaterializationVersion - 1,
			LoadedAt:               1,
			Ready:                  true,
		},
		Assessments: []semdb.OntologyNoteAssessmentRow{{
			NotePath:       "docs/typed.md",
			ResolvedType:   "ReferenceDoc",
			AssessmentJSON: `{"notePath":"docs/typed.md","resolvedType":"ReferenceDoc"}`,
			SchemaHash:     "schema",
			UpdatedAt:      1,
		}},
	}))

	eligibility, err := NewOntologyRawNoteEligibility(intelStore).RawNoteEmbeddingEligibility(ctx, []string{"docs/typed.md"})
	require.NoError(t, err)
	require.True(t, eligibility.RawEligible["docs/typed.md"])
	require.Empty(t, eligibility.TypedPaths)
}

func TestNoteSyncerSyncPathsUnchangedPrefixShrinkPrunesStaleChunks(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	notePath := "notes/shrinks.md"
	now := time.Now().Unix()
	sections := []codeanchor.IntelDocSection{
		{
			SectionID:   "section-1",
			Path:        notePath,
			Title:       "First",
			Level:       2,
			Content:     "unchanged prefix",
			Fingerprint: "fp-1",
			UpdatedAt:   now,
		},
		{
			SectionID:   "section-2",
			Path:        notePath,
			Title:       "Second",
			Level:       2,
			Content:     "removed suffix",
			Fingerprint: "fp-2",
			UpdatedAt:   now,
		},
	}
	originalSections := append([]codeanchor.IntelDocSection(nil), sections...)

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := embsqlite.Open(filepath.Join(tempDir, "notes.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	source := stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{notePath: sections}}
	syncer := NoteSyncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        source,
		WriteQueue:   &recordingNoteWriteQueue{store: store},
	}

	require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
	chunks, err := store.NoteChunks(ctx, embeddings.NoteID(notePath))
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	firstVector := append(embeddings.Embedding(nil), chunks[0].Embedding...)
	secondVector := append(embeddings.Embedding(nil), chunks[1].Embedding...)
	require.NotEmpty(t, firstVector)
	require.NotEmpty(t, secondVector)
	require.NotEqual(t, firstVector, secondVector)
	queue := syncer.WriteQueue.(*recordingNoteWriteQueue)
	require.Len(t, queue.syncs, 1)
	require.Equal(t, embeddings.NoteID(notePath), queue.syncs[0].NoteID)
	require.Equal(t, []int{0, 1}, queue.syncs[0].KeepIndices)
	require.Len(t, queue.syncs[0].Chunks, 2)
	require.Len(t, queue.syncs[0].Embeddings, 2)
	sections[1].Content = "changed suffix"
	sections[1].Fingerprint = "fp-2-changed"
	sections[1].UpdatedAt = now + 1
	require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
	chunks, err = store.NoteChunks(ctx, embeddings.NoteID(notePath))
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, firstVector, chunks[0].Embedding)
	require.NotEqual(t, secondVector, chunks[1].Embedding)

	source.byPath[notePath] = sections[:1]
	require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))

	chunks, err = store.NoteChunks(ctx, embeddings.NoteID(notePath))
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, 0, chunks[0].Index)
	require.Equal(t, "First", chunks[0].Heading)
	require.Equal(t, firstVector, chunks[0].Embedding)

	t.Run("unified store", func(t *testing.T) {
		collector := indexingperf.New()
		ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "embed_notes")
		idx, err := embsqlite.Open(filepath.Join(t.TempDir(), "notes.db"), prov.Dimensions())
		require.NoError(t, err)
		t.Cleanup(func() { _ = idx.Close() })
		reused := embeddings.Embedding{9, 8, 7, 6, 5, 4, 3, 2}
		firstChunk := buildSectionChunks("shrinks", originalSections, 0)[0]
		require.NoError(t, idx.CacheEmbedding(ctx, firstChunk.Hash, reused))
		intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = intelStore.Close() })
		require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, notePath, originalSections, nil, nil))
		source := stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{notePath: append([]codeanchor.IntelDocSection(nil), originalSections...)}}
		syncer := NoteSyncer{Index: idx, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: source, ChunkWriter: intelStore, EmbeddingWriter: intelStore}
		require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
		summary := collector.RenderSummary()
		require.Contains(t, summary, "reuse_hits=1")
		persisted, err := intelStore.IntelChunksByOwners(ctx, []string{"section-1", "section-2"})
		require.NoError(t, err)
		require.Len(t, persisted, 2)
		ids := []string{persisted[0].ChunkID, persisted[1].ChunkID}
		vectors, err := intelStore.EmbeddingsByChunkIDs(ctx, ids)
		require.NoError(t, err)
		require.Equal(t, reused, vectors[ids[0]])
		require.NotEmpty(t, vectors[ids[1]])
		require.NotEqual(t, vectors[ids[0]], vectors[ids[1]])
		first := append(embeddings.Embedding(nil), vectors[ids[0]]...)
		second := append(embeddings.Embedding(nil), vectors[ids[1]]...)
		changed := append([]codeanchor.IntelDocSection(nil), originalSections...)
		changed[1] = sections[1]
		source.byPath[notePath] = changed
		require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, notePath, changed, nil, nil))
		require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
		vectors, err = intelStore.EmbeddingsByChunkIDs(ctx, ids)
		require.NoError(t, err)
		require.Equal(t, first, vectors[ids[0]])
		require.NotEqual(t, second, vectors[ids[1]])
		source.byPath[notePath] = sections[:1]
		require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, notePath, sections[:1], nil, nil))
		require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
		persisted, err = intelStore.IntelChunksByOwners(ctx, []string{"section-1", "section-2"})
		require.NoError(t, err)
		require.Len(t, persisted, 1)
		require.Equal(t, "section-1", persisted[0].OwnerID)
		vectors, err = intelStore.EmbeddingsByChunkIDs(ctx, ids)
		require.NoError(t, err)
		require.Equal(t, first, vectors[ids[0]])
		require.NotContains(t, vectors, ids[1])
	})
}

func TestNoteSyncerBatchesIntelEmbeddingWritesAcrossNotes(t *testing.T) {
	tempDir := t.TempDir()

	const noteCount = 48
	sections := make([]codeanchor.IntelDocSection, 0, noteCount)
	now := time.Now().Unix()
	for i := 0; i < noteCount; i++ {
		path := filepath.ToSlash(filepath.Join("notes", "n"+strconv.Itoa(i)+".md"))
		sections = append(sections, codeanchor.IntelDocSection{
			SectionID:   "s" + strconv.Itoa(i),
			Path:        path,
			Title:       "Section",
			Level:       2,
			Content:     "Hello " + strconv.Itoa(i),
			Fingerprint: "fp-" + strconv.Itoa(i),
			UpdatedAt:   now,
		})
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := embsqlite.Open(filepath.Join(tempDir, "notes.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	writer := &recordingEmbeddingWriter{}
	syncer := NoteSyncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           stubDocIntelSource{sections: sections},
		BatchSize:       100,
		MaxConcurrent:   32,
		EmbeddingWriter: writer,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.Less(t, writer.CallCount(), noteCount, "intel writes should batch across notes")
	require.Equal(t, noteCount, writer.StoredCount())
}

func TestNoteSyncerNoteEmbedPackerBatchesAcrossNotes(t *testing.T) {
	for _, tc := range []struct {
		name                                               string
		noteCount, sectionsPerNote, batchSize, concurrency int
		packer                                             *EmbedPackerOptions
	}{
		{"default packer", 50, 1, 100, 250, nil},
		{"explicit packer", 40, 3, 8, 12, &EmbedPackerOptions{MinTexts: 12, MaxTexts: 64, MaxBytes: 96 << 10, MaxWait: 50 * time.Millisecond}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			sections := make([]codeanchor.IntelDocSection, 0, tc.noteCount*tc.sectionsPerNote)
			now := time.Now().Unix()
			for i := 0; i < tc.noteCount; i++ {
				path := filepath.ToSlash(filepath.Join("notes", "n"+strconv.Itoa(i)+".md"))
				for j := 0; j < tc.sectionsPerNote; j++ {
					sections = append(sections, codeanchor.IntelDocSection{SectionID: "s" + strconv.Itoa(i) + "-" + strconv.Itoa(j), Path: path, Title: "Section " + strconv.Itoa(j), Level: 2, Content: "Hello " + strconv.Itoa(i) + " " + strconv.Itoa(j), Fingerprint: "fp-" + strconv.Itoa(i) + "-" + strconv.Itoa(j), UpdatedAt: now})
				}
			}
			prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
			store, err := embsqlite.Open(filepath.Join(t.TempDir(), "notes.db"), prov.Dimensions())
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			syncer := NoteSyncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: stubDocIntelSource{sections: sections}, BatchSize: tc.batchSize, MaxConcurrent: tc.concurrency, NoteEmbedPacker: tc.packer}
			require.NoError(t, syncer.Sync(ctx))
			require.Positive(t, prov.calls.Load())
			require.Less(t, prov.calls.Load(), int64(tc.noteCount), "provider should batch across notes")
			for i := 0; i < tc.noteCount; i++ {
				path := filepath.ToSlash(filepath.Join("notes", "n"+strconv.Itoa(i)+".md"))
				chunks, err := store.NoteChunks(ctx, embeddings.NoteID(path))
				require.NoError(t, err)
				require.Len(t, chunks, tc.sectionsPerNote, path)
				for j, chunk := range chunks {
					require.Equal(t, j, chunk.Index, path)
					require.Equal(t, "Section "+strconv.Itoa(j), chunk.Heading, path)
					text := "# n" + strconv.Itoa(i) + "\n\n## Section " + strconv.Itoa(j) + "\n\nHello " + strconv.Itoa(i) + " " + strconv.Itoa(j)
					want, err := prov.inner.EmbedTexts(ctx, []string{text})
					require.NoError(t, err)
					require.Equal(t, want[0], chunk.Embedding, path)
				}
			}
		})
	}
}

func TestNoteSyncerReturnsIntelEmbeddingFlushError(t *testing.T) {
	tempDir := t.TempDir()

	sections := []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "notes/n1.md",
		Title:       "Section",
		Level:       2,
		Content:     "Hello",
		Fingerprint: "fp-1",
		UpdatedAt:   time.Now().Unix(),
	}}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := embsqlite.Open(filepath.Join(tempDir, "notes.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := NoteSyncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           stubDocIntelSource{sections: sections},
		BatchSize:       100,
		MaxConcurrent:   4,
		EmbeddingWriter: &recordingEmbeddingWriter{failCall: 1, failErr: errors.New("boom")},
	}

	err = syncer.Sync(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
}

func TestNoteSyncerSyncPathsBatchesAcrossPaths(t *testing.T) {
	tempDir := t.TempDir()

	sectionsByPath := map[string][]codeanchor.IntelDocSection{
		"notes/a.md": {{
			SectionID:   "a-1",
			Path:        "notes/a.md",
			Title:       "A",
			Level:       2,
			Content:     "hello a",
			Fingerprint: "fp-a",
			UpdatedAt:   time.Now().Unix(),
		}},
		"notes/b.md": {{
			SectionID:   "b-1",
			Path:        "notes/b.md",
			Title:       "B",
			Level:       2,
			Content:     "hello b",
			Fingerprint: "fp-b",
			UpdatedAt:   time.Now().Unix(),
		}},
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := embsqlite.Open(filepath.Join(tempDir, "notes.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := NoteSyncer{
		Index:         store,
		Provider:      prov,
		ProviderInfo:  embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:         stubDocIntelSourceByPath{byPath: sectionsByPath},
		BatchSize:     16,
		MaxConcurrent: 8,
		NoteEmbedPacker: &EmbedPackerOptions{
			MinTexts: 2,
			MaxTexts: 16,
			MaxBytes: 32 << 10,
			MaxWait:  10 * time.Millisecond,
		},
	}

	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"notes/a.md", "notes/b.md"}))
	require.EqualValues(t, 1, prov.calls.Load())
	for path, sections := range sectionsByPath {
		chunks, err := store.NoteChunks(context.Background(), embeddings.NoteID(path))
		require.NoError(t, err)
		require.Len(t, chunks, 1, path)
		require.Equal(t, sections[0].Title, chunks[0].Heading)
		require.Len(t, chunks[0].Embedding, prov.Dimensions())
	}
}

func TestNoteSyncerSyncPathsDeletesMissingNote(t *testing.T) {
	tempDir := t.TempDir()

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := embsqlite.Open(filepath.Join(tempDir, "notes.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "test",
		Model:      "deterministic",
		Dimensions: prov.Dimensions(),
	}))
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "notes/missing.md",
		Path:  "notes/missing.md",
		Title: "Missing",
		Mtime: time.Now(),
	}))

	syncer := NoteSyncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{}},
	}

	require.NoError(t, syncer.SyncPaths(ctx, []string{"notes/missing.md"}))
	notes, err := store.ListNotes(ctx)
	require.NoError(t, err)
	require.Empty(t, notes)
}

func TestNoteSyncerSyncPathsDoesNotMarkLastSync(t *testing.T) {
	tempDir := t.TempDir()

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := embsqlite.Open(filepath.Join(tempDir, "notes.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := NoteSyncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel: stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{
			"notes/a.md": {{
				SectionID:   "a-1",
				Path:        "notes/a.md",
				Title:       "A",
				Level:       2,
				Content:     "hello a",
				Fingerprint: "fp-a",
				UpdatedAt:   time.Now().Unix(),
			}},
		}},
	}

	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"notes/a.md"}))

	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, meta.LastSync.IsZero())
}

func TestNoteSyncerFullSyncUsesSourceHighWaterForSkip(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := embsqlite.Open(filepath.Join(tempDir, "notes.db"), prov.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sections := []codeanchor.IntelDocSection{{
		SectionID:   "a-1",
		Path:        "notes/a.md",
		Title:       "A",
		Level:       1,
		Content:     "first body",
		Fingerprint: "fp-a",
		UpdatedAt:   100,
	}}
	syncer := NoteSyncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        stubDocIntelSource{sections: sections},
	}
	require.NoError(t, syncer.Sync(ctx))
	prov.Reset()

	require.NoError(t, store.UpdateLastSync(ctx, time.Unix(10_000, 0)))
	syncer.Intel = stubDocIntelSource{sections: []codeanchor.IntelDocSection{{
		SectionID:   "a-1",
		Path:        "notes/a.md",
		Title:       "A",
		Level:       1,
		Content:     "changed body after high-water",
		Fingerprint: "fp-a2",
		UpdatedAt:   150,
	}}}

	require.NoError(t, syncer.Sync(ctx))
	require.Greater(t, prov.calls.Load(), int64(0), "future diagnostic last_sync must not hide newer source rows")

	meta, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(150), meta.SourceHighWater.Unix())
}

func TestNoteSyncerFullSyncPrunesStaleExtraWithoutReembeddingCurrent(t *testing.T) {
	ctx := context.Background()
	store, err := embsqlite.Open(filepath.Join(t.TempDir(), "notes.db"), 8)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
	sections := []codeanchor.IntelDocSection{{
		SectionID: "current-1", Path: "notes/current.md", Title: "Current", Level: 1,
		Content: "current body", Fingerprint: "current-fp", UpdatedAt: 100,
	}}
	syncer := NoteSyncer{
		Index: store, Provider: provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        stubDocIntelSource{sections: sections},
	}
	require.NoError(t, syncer.Sync(ctx))
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "notes/stale.md", Path: "notes/stale.md", Title: "Stale", Mtime: time.Unix(99, 0)}))
	provider.Reset()

	require.NoError(t, syncer.Sync(ctx))
	require.Zero(t, provider.calls.Load(), "unchanged current source must not be re-embedded")
	notes, err := store.ListNotes(ctx)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	require.Equal(t, "notes/current.md", notes[0].Path)
}
