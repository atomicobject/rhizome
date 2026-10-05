package sqlite

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestIndexedCodeNoteLinksForFileNormalizesBoundsAndOrders(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-context.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for _, link := range []codeanchor.DocLink{
		{SrcType: "code", SrcPath: "src/a.go", DstKind: "note", DstPath: "notes/z.md", Label: "z"},
		{SrcType: "code", SrcPath: "src/a.go", DstKind: "note", DstPath: "notes/a.md", Label: "a", Snippet: "literal persisted snippet"},
		{SrcType: "code", SrcPath: "src/a.go", DstKind: "anchor", DstPath: "notes/ignored.md"},
		{SrcType: "code", SrcPath: "src/b.go", DstKind: "note", DstPath: "notes/b.md"},
	} {
		require.NoError(t, store.ReplaceDocLinksForPath(ctx, link.SrcPath, appendLinkForSource(t, store, ctx, link)))
	}

	got, err := store.IndexedCodeNoteLinksForFile(ctx, `.\src\a.go`, 1)
	require.NoError(t, err)
	require.Equal(t, []codeanchor.IndexedCodeNoteLink{{
		CodePath: "src/a.go",
		NotePath: "notes/a.md",
		Label:    "a",
		Snippet:  "literal persisted snippet",
	}}, got)

	_, err = store.IndexedCodeNoteLinksForFile(ctx, "../outside.go", 10)
	require.Error(t, err)
}

func TestIndexedCodeNoteLinksForSubtreeScopesEitherEndpoint(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-subtree.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	linksBySource := map[string][]codeanchor.DocLink{
		"cmd/in.go": {
			{SrcType: "code", SrcPath: "cmd/in.go", DstKind: "note", DstPath: "notes/out.md"},
		},
		"src/out.go": {
			{SrcType: "code", SrcPath: "src/out.go", DstKind: "note", DstPath: "docs/specs/in.md"},
			{SrcType: "code", SrcPath: "src/out.go", DstKind: "note", DstPath: "docs/specsmanship/no.md"},
		},
	}
	for source, links := range linksBySource {
		require.NoError(t, store.ReplaceDocLinksForPath(ctx, source, links))
	}

	got, err := store.IndexedCodeNoteLinksForSubtree(ctx, `./docs\specs/`, 10)
	require.NoError(t, err)
	require.Equal(t, []codeanchor.IndexedCodeNoteLink{{
		CodePath: "src/out.go",
		NotePath: "docs/specs/in.md",
	}}, got)

	got, err = store.IndexedCodeNoteLinksForSubtree(ctx, "src", 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "src/out.go", got[0].CodePath)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.IndexedCodeNoteLinksForSubtree(cancelled, "src", 10)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIndexedRationaleForFileAndSubtreeNormalizeOrderBoundAndCancel(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-rationale.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for i := 0; i < maxIndexedContextRationale+1; i++ {
		path := "src/pkg/a.go"
		if i == maxIndexedContextRationale {
			path = "src/pkgish/collision.go"
		}
		require.NoError(t, store.ReplaceRationaleForPath(ctx, path, appendIndexedRationale(t, store, ctx, path, codeanchor.Rationale{
			ID:          fmt.Sprintf("r-%03d", i),
			Path:        path,
			SymbolFQN:   "pkg.Run",
			Kind:        codeanchor.RationaleWhy,
			Content:     fmt.Sprintf("why %03d", i),
			StartLine:   int64(maxIndexedContextRationale - i + 1),
			EndLine:     int64(maxIndexedContextRationale - i + 1),
			Fingerprint: fmt.Sprintf("fp-%03d", i),
		})))
	}

	exact, err := store.IndexedRationaleForFile(ctx, `.\src\pkg\a.go`, 1)
	require.NoError(t, err)
	require.Len(t, exact, 1)
	require.Equal(t, "r-099", exact[0].ID)
	require.Equal(t, codeanchor.RationaleWhy, exact[0].Kind)
	require.Equal(t, "why 099", exact[0].Content)
	require.Equal(t, int64(maxIndexedContextRationale-99+1), exact[0].StartLine)

	subtree, err := store.IndexedRationaleForSubtree(ctx, "src/pkg", 1000)
	require.NoError(t, err)
	require.Len(t, subtree, maxIndexedContextRationale)
	for _, rationale := range subtree {
		require.Equal(t, "src/pkg/a.go", rationale.CodePath)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.IndexedRationaleForSubtree(cancelled, "src/pkg", 10)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIndexedCodeEdgesForFileAndSubtreeScopeOrderBoundAndCancel(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-edges.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	source := codeanchor.IntelAnchor{
		AnchorID: "source", Lang: codeanchor.LangGo, Kind: "function",
		Path: "src/pkg/a.go", Symbol: "Run", FQN: "pkg.Run", Fingerprint: "source",
	}
	edges := make([]codeanchor.IntelEdge, 0, maxIndexedContextEdges+3)
	for i := 0; i < maxIndexedContextEdges+1; i++ {
		path := fmt.Sprintf("dep/%03d.go", i)
		anchorID := fmt.Sprintf("target-%03d", i)
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{{
			AnchorID: anchorID, Lang: codeanchor.LangGo, Kind: "function",
			Path: path, Symbol: "Target", FQN: "dep.Target", Fingerprint: anchorID,
		}}, nil, nil))
		edges = append(edges, codeanchor.IntelEdge{SrcID: source.AnchorID, DstID: anchorID, Kind: "calls"})
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, source.Path, []codeanchor.IntelAnchor{source}, edges, nil))

	collision := codeanchor.IntelAnchor{
		AnchorID: "collision", Lang: codeanchor.LangGo, Kind: "function",
		Path: "src/pkgish/no.go", Symbol: "No", FQN: "pkgish.No", Fingerprint: "collision",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, collision.Path, []codeanchor.IntelAnchor{collision},
		[]codeanchor.IntelEdge{{SrcID: collision.AnchorID, DstID: "target-000", Kind: "calls"}}, nil))

	// These edge kinds are deliberately excluded from startup code context.
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, source.Path, []codeanchor.IntelAnchor{source},
		append(edges,
			codeanchor.IntelEdge{SrcID: source.AnchorID, DstID: "target-000", Kind: "defines"},
			codeanchor.IntelEdge{SrcID: source.AnchorID, DstID: "target-001", Kind: "mentions"},
		), nil))

	exact, err := store.IndexedCodeEdgesForFile(ctx, `.\src\pkg\a.go`, 1)
	require.NoError(t, err)
	require.Equal(t, []codeanchor.IndexedCodeEdge{{
		SourcePath: "src/pkg/a.go",
		TargetPath: "dep/000.go",
		Kind:       "calls",
		Weight:     1,
	}}, exact)

	subtree, err := store.IndexedCodeEdgesForSubtree(ctx, "src/pkg", 1000)
	require.NoError(t, err)
	require.Len(t, subtree, maxIndexedContextEdges)
	for _, edge := range subtree {
		require.Equal(t, "src/pkg/a.go", edge.SourcePath)
		require.Equal(t, "calls", edge.Kind)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.IndexedCodeEdgesForSubtree(cancelled, "src/pkg", 10)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIndexedOntologySummaryUsesOnlyPublicPersistedTypesAndBoundsExamples(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-ontology.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for _, path := range []string{"notes/a.md", "notes/b.md", "notes/c.md"} {
		_, err := store.db.ExecContext(ctx, `INSERT INTO notes(path) VALUES (?)`, path)
		require.NoError(t, err)
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO ontology_note_types(note_path, type_name, schema_hash, updated_at) VALUES
		  ('notes/a.md', 'Project', 'schema', 1),
		  ('notes/b.md', 'Project', 'schema', 1)
	`)
	require.NoError(t, err)
	require.NoError(t, store.UpsertOntologySchemaState(ctx, OntologySchemaState{
		SchemaHash:             "schema",
		NotesHash:              "notes",
		MaterializationVersion: 7,
		LoadedAt:               123,
		Ready:                  true,
	}))

	got, err := store.IndexedOntologySummary(ctx, 10, 1)
	require.NoError(t, err)
	require.True(t, got.Available)
	require.True(t, got.Ready)
	require.Equal(t, "schema", got.SchemaHash)
	require.Equal(t, 7, got.MaterializationVersion)
	require.Equal(t, 3, got.TotalNotes)
	require.Equal(t, 2, got.TypedNotes)
	require.Equal(t, 1, got.UntypedNotes)
	require.Equal(t, []codeanchor.IndexedOntologyTypeCount{{
		TypeName: "Project",
		Count:    2,
		Examples: []string{"notes/a.md"},
	}}, got.TypeCounts)
}

func TestIndexedNoteMetadataForPathIsExactNormalizedAndCancelable(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-note.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO notes(path, title, mtime, size, indexed_at) VALUES
		  ('docs/spec.md', 'Startup spec', 10, 200, 30),
		  ('docs/spec.md-old', 'Collision', 11, 201, 31)
	`)
	require.NoError(t, err)

	got, err := store.IndexedNoteMetadataForPath(ctx, `.\docs\spec.md`)
	require.NoError(t, err)
	require.Equal(t, &codeanchor.IndexedNoteMetadata{
		Path: "docs/spec.md", Title: "Startup spec", Mtime: 10, Size: 200, IndexedAt: 30,
	}, got)
	missing, err := store.IndexedNoteMetadataForPath(ctx, "docs/missing.md")
	require.NoError(t, err)
	require.Nil(t, missing)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.IndexedNoteMetadataForPath(cancelled, "docs/spec.md")
	require.ErrorIs(t, err, context.Canceled)
}

func TestIndexedGraphSummaryAggregatesAndLimitsWithoutMaterializingRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-graph.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceGraphDocScores(ctx, []GraphDocScore{
		{DocPath: "docs/a.md", DocType: "note", Authority: 3, Community: "alpha", Inbound: 0, Outbound: 0, UpdatedAt: 1},
		{DocPath: "pkg/a.go", DocType: "code", Authority: 2, Community: "alpha", Inbound: 1, Outbound: 1, UpdatedAt: 1},
		{DocPath: "docs/b.md", DocType: "note", Authority: 4, Community: "beta", Inbound: 0, Outbound: 0, UpdatedAt: 1},
	}))
	got, err := store.IndexedGraphSummary(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 3, got.DocumentCount)
	require.Equal(t, 2, got.NoteCount)
	require.Equal(t, 2, got.OrphanCount)
	require.Equal(t, []codeanchor.IndexedGraphCommunity{{
		ID: "alpha", DocumentCount: 2, NoteCount: 1, TopPath: "docs/a.md",
	}}, got.Communities)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.IndexedGraphSummary(cancelled, 10)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIndexedGraphSummaryRequiresPersistedGraphScoreEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-graph-empty.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = store.IndexedGraphSummary(ctx, 10)
	require.ErrorIs(t, err, codeanchor.ErrIndexedGraphSummaryMissing)
}

func TestIndexedSubtreeReadersTreatNormalizedRootAsWholeVault(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "indexed-root.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "pkg/start.go", []codeanchor.DocLink{{
		SrcPath: "pkg/start.go", SrcType: "code", DstPath: "docs/start.md", DstKind: "note",
	}}))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "pkg/start.go", []codeanchor.Rationale{{
		ID: "root-rationale", Path: "pkg/start.go", Kind: codeanchor.RationaleWhy, Content: "root evidence",
	}}))
	source := codeanchor.IntelAnchor{
		AnchorID: "root-source", Lang: codeanchor.LangGo, Kind: "function",
		Path: "pkg/start.go", Symbol: "Start", FQN: "pkg.Start", Fingerprint: "root-source",
	}
	target := codeanchor.IntelAnchor{
		AnchorID: "root-target", Lang: codeanchor.LangGo, Kind: "function",
		Path: "pkg/store.go", Symbol: "Open", FQN: "pkg.Open", Fingerprint: "root-target",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, target.Path, []codeanchor.IntelAnchor{target}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, source.Path, []codeanchor.IntelAnchor{source}, []codeanchor.IntelEdge{{
		SrcID: source.AnchorID, DstID: target.AnchorID, Kind: "calls",
	}}, nil))

	links, err := store.IndexedCodeNoteLinksForSubtree(ctx, "", 10)
	require.NoError(t, err)
	require.Len(t, links, 1)
	rationales, err := store.IndexedRationaleForSubtree(ctx, "", 10)
	require.NoError(t, err)
	require.Len(t, rationales, 1)
	edges, err := store.IndexedCodeEdgesForSubtree(ctx, "", 10)
	require.NoError(t, err)
	require.Len(t, edges, 1)

	_, err = store.IndexedCodeNoteLinksForSubtree(ctx, "../outside", 10)
	require.Error(t, err)
}

func appendLinkForSource(t *testing.T, store *Store, ctx context.Context, link codeanchor.DocLink) []codeanchor.DocLink {
	t.Helper()
	existing, err := store.DocLinksFromCodePath(ctx, link.SrcPath, 100)
	require.NoError(t, err)
	return append(existing, link)
}

func appendIndexedRationale(t *testing.T, store *Store, ctx context.Context, path string, rationale codeanchor.Rationale) []codeanchor.Rationale {
	t.Helper()
	existing, err := store.RationaleForPath(ctx, path)
	require.NoError(t, err)
	return append(existing, rationale)
}
