package embedindex

import (
	"context"

	"path/filepath"

	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestNoteSyncerSkipsTypedNotesAndPrunesRawSurfaces(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	noteStore, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
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
