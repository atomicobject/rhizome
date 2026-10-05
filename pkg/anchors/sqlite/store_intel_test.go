package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	embeddingstypes "github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
)

func TestReplaceIntelCodeFile_ReplacesPerPath(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors1 := []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "Foo",
		Fingerprint: "fp1",
	}}
	edges1 := []codeanchor.IntelEdge{{SrcID: "a1", DstID: "x", Kind: "calls"}}
	fts1 := []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: "a1", Path: "src/a.go", Title: "Foo", Body: "body"}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/dep.go", []codeanchor.IntelAnchor{{
		AnchorID:    "x",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/dep.go",
		Symbol:      "DepX",
		Fingerprint: "fpx",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", anchors1, edges1, fts1))

	anchors2 := []codeanchor.IntelAnchor{{
		AnchorID:    "a2",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "Bar",
		Fingerprint: "fp2",
	}}
	edges2 := []codeanchor.IntelEdge{{SrcID: "a2", DstID: "y", Kind: "calls"}}
	fts2 := []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: "a2", Path: "src/a.go", Title: "Bar", Body: "new"}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/dep2.go", []codeanchor.IntelAnchor{{
		AnchorID:    "y",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/dep2.go",
		Symbol:      "DepY",
		Fingerprint: "fpy",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", anchors2, edges2, fts2))

	var anchorIDs []string
	rows, err := store.db.QueryContext(ctx, `SELECT anchor_id FROM intel_code_anchors WHERE path = ?`, "src/a.go")
	require.NoError(t, err)
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		anchorIDs = append(anchorIDs, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"a2"}, anchorIDs)

	var edgeCount int
	edgeCount = countIntelEdgesFromNode(t, ctx, store, "anchor", "a2")
	require.Equal(t, 1, edgeCount)
	edgeCount = countIntelEdgesFromNode(t, ctx, store, "anchor", "a1")
	require.Equal(t, 0, edgeCount)

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts_rowid WHERE item_id = 'a2'`).Scan(&edgeCount))
	require.Equal(t, 1, edgeCount)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts_rowid WHERE item_id = 'a1'`).Scan(&edgeCount))
	require.Equal(t, 0, edgeCount)
}

func TestRefs_NotReplacedOnParseError(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := Open(filepath.Join(tmp, "intel-refs-parse.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	pathRel := "src/app/bad.py"
	pathAbs := filepath.Join(tmp, pathRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(pathAbs), 0o755))

	initial := "def run():\n    target()\n"
	require.NoError(t, os.WriteFile(pathAbs, []byte(initial), 0o644))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, pathAbs, []byte(initial)))

	refsBefore, err := store.SymbolRefsByPaths(ctx, []string{pathRel})
	require.NoError(t, err)
	require.NotEmpty(t, refsBefore[pathRel])

	bad := "def run(:\n    target()\n"
	require.NoError(t, os.WriteFile(pathAbs, []byte(bad), 0o644))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, pathAbs, []byte(bad)))

	refsAfter, err := store.SymbolRefsByPaths(ctx, []string{pathRel})
	require.NoError(t, err)
	require.Equal(t, refsBefore[pathRel], refsAfter[pathRel])
}

func TestReplaceIntelDocSections_ReplacesPerPath(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-doc.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	sections1 := []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "notes/foo.md",
		Title:       "Foo",
		Level:       1,
		StartByte:   0,
		EndByte:     10,
		Content:     "foo content",
		Fingerprint: "fp1",
	}}
	mentions1 := []codeanchor.IntelEdge{{SrcID: "s1", DstID: "a1", Kind: "mentions"}}
	fts1 := []codeanchor.IntelFTSRow{{ItemType: "doc_section", ItemID: "s1", Path: "notes/foo.md", Title: "Foo", Body: "foo"}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/doc_targets.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/doc_targets.go",
		Symbol:      "DocTarget1",
		Fingerprint: "fpa1",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/foo.md", sections1, mentions1, fts1))

	sections2 := []codeanchor.IntelDocSection{{
		SectionID:   "s2",
		Path:        "notes/foo.md",
		Title:       "Bar",
		Level:       2,
		StartByte:   11,
		EndByte:     20,
		Content:     "bar content",
		Fingerprint: "fp2",
	}}
	mentions2 := []codeanchor.IntelEdge{{SrcID: "s2", DstID: "a2", Kind: "mentions"}}
	fts2 := []codeanchor.IntelFTSRow{{ItemType: "doc_section", ItemID: "s2", Path: "notes/foo.md", Title: "Bar", Body: "bar"}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/doc_targets2.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a2",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/doc_targets2.go",
		Symbol:      "DocTarget2",
		Fingerprint: "fpa2",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/foo.md", sections2, mentions2, fts2))

	var sectionIDs []string
	rows, err := store.db.QueryContext(ctx, `SELECT section_id FROM intel_doc_sections WHERE path = ?`, "notes/foo.md")
	require.NoError(t, err)
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		sectionIDs = append(sectionIDs, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"s2"}, sectionIDs)

	var edgeCount int
	edgeCount = countIntelEdgesFromNode(t, ctx, store, "doc_section", "s2")
	require.Equal(t, 1, edgeCount)
	edgeCount = countIntelEdgesFromNode(t, ctx, store, "doc_section", "s1")
	require.Equal(t, 0, edgeCount)

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts_rowid WHERE item_id = 's2'`).Scan(&edgeCount))
	require.Equal(t, 1, edgeCount)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts_rowid WHERE item_id = 's1'`).Scan(&edgeCount))
	require.Equal(t, 0, edgeCount)
}

func TestReplaceIntelCodeFile_RemovesEdgesPointingToRemovedAnchors(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-incoming-edges.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors1 := []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "Foo",
		Fingerprint: "fp1",
	}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", anchors1, nil, nil))

	anchorsB := []codeanchor.IntelAnchor{{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/b.go",
		Symbol:      "Caller",
		Fingerprint: "fpb1",
	}}
	edgesB := []codeanchor.IntelEdge{{SrcID: "b1", DstID: "a1", Kind: "calls"}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/b.go", anchorsB, edgesB, nil))

	count := countIntelEdgesByNodes(t, ctx, store, "anchor", "b1", "anchor", "a1", "calls")
	require.Equal(t, 1, count)
	removedRowID, ok, err := intelNodeRowID(ctx, store, "anchor", "a1")
	require.NoError(t, err)
	require.True(t, ok)

	anchors2 := []codeanchor.IntelAnchor{{
		AnchorID:    "a2",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "Foo",
		Fingerprint: "fp2",
	}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", anchors2, nil, nil))

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE dst_type = 'anchor' AND dst_row_id = ?`, removedRowID).Scan(&count))
	require.Equal(t, 0, count)
}

func TestReplaceIntelDocSections_RemovesEdgesPointingToRemovedSections(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-incoming-sections.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	sections1 := []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "notes/foo.md",
		Title:       "Foo",
		Level:       1,
		StartByte:   0,
		EndByte:     10,
		Content:     "foo content",
		Fingerprint: "fp1",
	}}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/foo.md", sections1, nil, nil))

	sectionsBar := []codeanchor.IntelDocSection{{
		SectionID:   "t1",
		Path:        "notes/bar.md",
		Title:       "Bar",
		Level:       1,
		StartByte:   0,
		EndByte:     10,
		Content:     "bar content",
		Fingerprint: "fpt1",
	}}
	edgesBar := []codeanchor.IntelEdge{{SrcID: "t1", DstID: "s1", Kind: "links"}}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/bar.md", sectionsBar, edgesBar, nil))

	count := countIntelEdgesByNodes(t, ctx, store, "doc_section", "t1", "doc_section", "s1", "links")
	require.Equal(t, 1, count)
	removedRowID, ok, err := intelNodeRowID(ctx, store, "doc_section", "s1")
	require.NoError(t, err)
	require.True(t, ok)

	sections2 := []codeanchor.IntelDocSection{{
		SectionID:   "s2",
		Path:        "notes/foo.md",
		Title:       "Foo2",
		Level:       1,
		StartByte:   0,
		EndByte:     10,
		Content:     "foo2 content",
		Fingerprint: "fp2",
	}}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/foo.md", sections2, nil, nil))

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE dst_type = 'doc_section' AND dst_row_id = ?`, removedRowID).Scan(&count))
	require.Equal(t, 0, count)
}

func TestDeleteIntelByPath_RemovesAll(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-delete.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors := []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "Foo",
		Fingerprint: "fp1",
	}}
	edges := []codeanchor.IntelEdge{{SrcID: "a1", DstID: "x", Kind: "calls"}}
	fts := []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: "a1", Path: "src/a.go", Title: "Foo", Body: "body"}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", anchors, edges, fts))

	sections := []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "notes/foo.md",
		Title:       "Foo",
		Level:       1,
		StartByte:   0,
		EndByte:     10,
		Content:     "foo content",
		Fingerprint: "fp1",
	}}
	mentions := []codeanchor.IntelEdge{{SrcID: "s1", DstID: "a1", Kind: "mentions"}}
	sectionsFTS := []codeanchor.IntelFTSRow{{ItemType: "doc_section", ItemID: "s1", Path: "notes/foo.md", Title: "Foo", Body: "body"}}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/foo.md", sections, mentions, sectionsFTS))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "src/a.go", []codeanchor.Rationale{{
		ID:          "r1",
		Path:        "src/a.go",
		Kind:        codeanchor.RationaleNote,
		Content:     "keep me fresh",
		StartLine:   1,
		EndLine:     1,
		Fingerprint: "rfp1",
	}}))
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"src/a.go": {{SrcPath: "src/a.go", OwnerFQN: "Foo", RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangGo, DstPkg: "pkg", DstName: "Gone", DstFQN: "pkg.Gone"}},
	}))
	require.NoError(t, store.ReplaceIntelImportRefsForPathsBatch(ctx, map[string][]codeanchor.ImportRefRow{"src/a.go": {{SrcPath: "src/a.go", Module: "pkg/gone"}}}))
	require.NoError(t, store.ReplaceIntelModuleDefsForPathsBatch(ctx, map[string][]codeanchor.ModuleDefRow{"src/a.go": {{SrcPath: "src/a.go", Lang: codeanchor.LangGo, Module: "pkg/source"}}}))

	require.NoError(t, store.DeleteIntelByPath(ctx, "src/a.go"))
	require.NoError(t, store.DeleteIntelByPath(ctx, "notes/foo.md"))

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_code_anchors`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_doc_sections`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts_rowid`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_rationale`).Scan(&count))
	require.Equal(t, 0, count)
	for _, table := range []string{"intel_symbol_refs", "intel_symbol_ref_files", "intel_symbol_ref_targets", "intel_import_refs", "intel_module_defs"} {
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestReplaceIntelChunks_ReplacesPerOwnerID(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-chunks.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	chunks1 := []codeanchor.IntelChunk{
		{
			ChunkID:     codeanchor.IntelChunkID("owner1", 0, "symbol"),
			OwnerID:     "owner1",
			OwnerType:   "anchor",
			Ord:         0,
			Granularity: "symbol",
			Breadcrumb:  "pkg > file.go > Func",
			Heading:     "Func",
			ContentHash: "hash1",
			StartByte:   0,
			EndByte:     100,
			UpdatedAt:   1000,
		},
		{
			ChunkID:     codeanchor.IntelChunkID("owner1", 1, "symbol"),
			OwnerID:     "owner1",
			OwnerType:   "anchor",
			Ord:         1,
			Granularity: "symbol",
			Breadcrumb:  "pkg > file.go > Func",
			Heading:     "Func (cont)",
			ContentHash: "hash2",
			StartByte:   100,
			EndByte:     200,
			UpdatedAt:   1000,
		},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"owner1"}, chunks1))

	got, err := store.IntelChunksByOwners(ctx, []string{"owner1"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "owner1", got[0].OwnerID)
	require.Equal(t, 0, got[0].Ord)
	require.Equal(t, 1, got[1].Ord)

	// Replace with different chunks.
	chunks2 := []codeanchor.IntelChunk{
		{
			ChunkID:     codeanchor.IntelChunkID("owner1", 0, "module"),
			OwnerID:     "owner1",
			OwnerType:   "anchor",
			Ord:         0,
			Granularity: "module",
			ContentHash: "hash3",
			StartByte:   0,
			EndByte:     50,
			UpdatedAt:   2000,
		},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"owner1"}, chunks2))

	got, err = store.IntelChunksByOwners(ctx, []string{"owner1"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "module", got[0].Granularity)
	require.Equal(t, int64(2000), got[0].UpdatedAt)
}

func TestReplaceIntelChunks_HandlesLargeKeepSet(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-chunks-large.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var ownerIDs []string
	var chunks []codeanchor.IntelChunk
	for i := 0; i < 300; i++ {
		ownerID := fmt.Sprintf("owner-%03d", i)
		ownerIDs = append(ownerIDs, ownerID)
		for ord := 0; ord < 2; ord++ {
			chunks = append(chunks, codeanchor.IntelChunk{
				ChunkID:     codeanchor.IntelChunkID(ownerID, ord, "symbol"),
				OwnerID:     ownerID,
				OwnerType:   "anchor",
				Ord:         ord,
				Granularity: "symbol",
				ContentHash: fmt.Sprintf("hash-%03d-%d", i, ord),
				UpdatedAt:   1000,
			})
		}
	}

	require.NoError(t, store.ReplaceIntelChunks(ctx, ownerIDs, chunks))
	got, err := store.IntelChunksByOwners(ctx, ownerIDs)
	require.NoError(t, err)
	require.Len(t, got, len(chunks))
	embeddings := make(map[string]embeddingstypes.Embedding, len(chunks))
	for _, chunk := range chunks {
		embeddings[chunk.ChunkID] = embeddingstypes.Embedding{1, 0, 0, 0}
	}
	require.NoError(t, store.UpsertEmbeddings(ctx, embeddings))
	moved := append([]codeanchor.IntelChunk(nil), chunks...)
	for i := range moved {
		moved[i].OwnerType = "doc_section"
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, ownerIDs, moved))
	partitioned, skipped, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, EmbeddingSearchFilters{OwnerTypes: []string{"doc_section"}})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, partitioned, 10, "the temp-table branch must move existing vectors into the new partition")
	stale, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, EmbeddingSearchFilters{OwnerTypes: []string{"anchor"}})
	require.NoError(t, err)
	require.Empty(t, stale)

	require.NoError(t, store.ReplaceIntelChunks(ctx, ownerIDs, chunks[:len(chunks)-1]))
	got, err = store.IntelChunksByOwners(ctx, ownerIDs)
	require.NoError(t, err)
	require.Len(t, got, len(chunks)-1)
}

func TestIntelChunks_ReturnsAllOrderedByOwnerAndOrd(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-chunks-all.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	chunks := []codeanchor.IntelChunk{
		{ChunkID: codeanchor.IntelChunkID("b", 1, "g"), OwnerID: "b", OwnerType: "anchor", Ord: 1, Granularity: "g", ContentHash: "h1"},
		{ChunkID: codeanchor.IntelChunkID("a", 0, "g"), OwnerID: "a", OwnerType: "doc_section", Ord: 0, Granularity: "g", ContentHash: "h2"},
		{ChunkID: codeanchor.IntelChunkID("b", 0, "g"), OwnerID: "b", OwnerType: "anchor", Ord: 0, Granularity: "g", ContentHash: "h3"},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"a", "b"}, chunks))

	got, err := store.IntelChunks(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "a", got[0].OwnerID)
	require.Equal(t, 0, got[0].Ord)
	require.Equal(t, "b", got[1].OwnerID)
	require.Equal(t, 0, got[1].Ord)
	require.Equal(t, "b", got[2].OwnerID)
	require.Equal(t, 1, got[2].Ord)
}

func TestReplaceIntelChunks_DedupesConflictingChunkRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-chunks-dedupe.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "owner1", Lang: codeanchor.LangTS, Kind: "function", Path: "src/app.ts", Symbol: "run", FQN: "src.app.run", Fingerprint: "fp", UpdatedAt: 1000},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/app.ts", anchors, nil, nil))

	chunks := []codeanchor.IntelChunk{
		{
			ChunkID:     codeanchor.IntelChunkID("owner1", 0, "symbol"),
			OwnerID:     "owner1",
			OwnerType:   "anchor",
			Ord:         0,
			Granularity: "symbol",
			ContentHash: "old-hash",
			StartByte:   0,
			EndByte:     10,
			UpdatedAt:   1000,
		},
		{
			ChunkID:     codeanchor.IntelChunkID("owner1", 0, "symbol"),
			OwnerID:     "owner1",
			OwnerType:   "anchor",
			Ord:         0,
			Granularity: "symbol",
			ContentHash: "new-hash",
			StartByte:   5,
			EndByte:     15,
			UpdatedAt:   2000,
		},
	}

	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"owner1", "owner1"}, chunks))

	got, err := store.IntelChunksByOwners(ctx, []string{"owner1"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "new-hash", got[0].ContentHash)
	require.Equal(t, int64(2000), got[0].UpdatedAt)
	require.Equal(t, int64(5), got[0].StartByte)
	require.Equal(t, int64(15), got[0].EndByte)
}

func TestReplaceIntelChunksByFamilyPreservesOtherFamilies(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-chunks-family.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	section := []codeanchor.IntelDocSection{{
		SectionID:   "section1",
		Path:        "docs/spec.md",
		Title:       "Spec",
		Level:       1,
		Content:     "Spec",
		Fingerprint: "section-fp",
		UpdatedAt:   1,
	}}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/spec.md", section, nil, nil))

	sectionChunk := codeanchor.IntelChunk{ChunkID: "section-chunk", OwnerID: "section1", OwnerType: "doc_section", ChunkFamily: codeanchor.IntelChunkFamilyAuthoredSection, Ord: 0, Granularity: "section", ContentHash: "section-hash", EndByte: 1}
	defaultChunk := codeanchor.IntelChunk{ChunkID: "default-chunk", OwnerID: "section1", OwnerType: "doc_section", ChunkFamily: codeanchor.IntelChunkFamilyDefault, Ord: 2000, Granularity: "fixture", ContentHash: "default-hash", EndByte: 1}
	require.NoError(t, store.ReplaceIntelChunksByFamily(ctx, []string{"section1"}, codeanchor.IntelChunkFamilyAuthoredSection, []codeanchor.IntelChunk{sectionChunk}))
	require.NoError(t, store.ReplaceIntelChunksByFamily(ctx, []string{"section1"}, codeanchor.IntelChunkFamilyDefault, []codeanchor.IntelChunk{defaultChunk}))

	updatedSection := sectionChunk
	updatedSection.ContentHash = "section-hash-2"
	require.NoError(t, store.ReplaceIntelChunksByFamily(ctx, []string{"section1"}, codeanchor.IntelChunkFamilyAuthoredSection, []codeanchor.IntelChunk{updatedSection}))

	got, err := store.IntelChunksByOwners(ctx, []string{"section1"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	byID := map[string]codeanchor.IntelChunk{}
	for _, chunk := range got {
		byID[chunk.ChunkID] = chunk
	}
	require.Equal(t, "section-hash-2", byID["section-chunk"].ContentHash)
	require.Equal(t, codeanchor.IntelChunkFamilyAuthoredSection, byID["section-chunk"].ChunkFamily)
	require.Equal(t, "default-hash", byID["default-chunk"].ContentHash)
	require.Equal(t, codeanchor.IntelChunkFamilyDefault, byID["default-chunk"].ChunkFamily)
}

func TestPackMetadata_GetAndSet(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "pack-meta.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Initially empty
	pm, err := store.GetPackMetadata(ctx)
	require.NoError(t, err)
	require.Empty(t, pm.ConfigHash)
	require.Empty(t, pm.ModelHash)
	require.Empty(t, pm.AlgoVersion)
	require.Empty(t, pm.IndexerVersion)

	// Set all fields
	pm = codeanchor.PackMetadata{
		ConfigHash:     "cfghash123",
		ModelHash:      "modelhash456",
		AlgoVersion:    "v2.0.0",
		IndexerVersion: "v1.3.1",
	}
	require.NoError(t, store.SetPackMetadata(ctx, pm))

	// Retrieve and verify
	got, err := store.GetPackMetadata(ctx)
	require.NoError(t, err)
	require.Equal(t, "cfghash123", got.ConfigHash)
	require.Equal(t, "modelhash456", got.ModelHash)
	require.Equal(t, "v2.0.0", got.AlgoVersion)
	require.Equal(t, "v1.3.1", got.IndexerVersion)

	// Partial update (only ConfigHash)
	pm2 := codeanchor.PackMetadata{ConfigHash: "newhash"}
	require.NoError(t, store.SetPackMetadata(ctx, pm2))

	got2, err := store.GetPackMetadata(ctx)
	require.NoError(t, err)
	require.Equal(t, "newhash", got2.ConfigHash)
	require.Equal(t, "modelhash456", got2.ModelHash) // unchanged
}

func TestScopeConfigHash_GetAndSet(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "scope-hash.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Initially empty
	hash, hasHash, err := store.GetScopeConfigHash(ctx)
	require.NoError(t, err)
	require.False(t, hasHash)
	require.Empty(t, hash)

	// Set hash
	require.NoError(t, store.SetScopeConfigHash(ctx, "abc123"))

	// Retrieve and verify
	hash, hasHash, err = store.GetScopeConfigHash(ctx)
	require.NoError(t, err)
	require.True(t, hasHash)
	require.Equal(t, "abc123", hash)

	// Update hash
	require.NoError(t, store.SetScopeConfigHash(ctx, "def456"))

	hash, hasHash, err = store.GetScopeConfigHash(ctx)
	require.NoError(t, err)
	require.True(t, hasHash)
	require.Equal(t, "def456", hash)
}

func TestIntelNoteIndexMetaAndTouchIntelNotePaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "note-meta.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO notes (path, title, mtime, content_hash, indexer_version)
		VALUES ('notes/a.md', 'A', 10, 'hash-a', 'vA'),
		       ('notes/b.md', 'B', 20, 'hash-b', 'vB')
	`)
	require.NoError(t, err)

	meta, err := store.IntelNoteIndexMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, "hash-a", meta["notes/a.md"].ContentHash)
	require.Equal(t, "vA", meta["notes/a.md"].IndexerVersion)
	require.Equal(t, int64(10), meta["notes/a.md"].Mtime)

	require.NoError(t, store.TouchIntelNotePaths(ctx, map[string]int64{
		"notes/a.md": 123,
		"notes/b.md": 456,
	}))
	mtimes, err := store.IntelNoteMtimes(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(123), mtimes["notes/a.md"])
	require.Equal(t, int64(456), mtimes["notes/b.md"])
}

func TestNoteIndexMeta(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "note-meta.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO notes (path, title, content_hash, indexer_version, mtime)
		VALUES ('note.md', 'Note', 'hash1', 'v1', 1234)
	`)
	require.NoError(t, err)

	hash, version, mtime, ok, err := store.NoteIndexMeta(ctx, "note.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "hash1", hash)
	require.Equal(t, "v1", version)
	require.Equal(t, int64(1234), mtime)
}

func TestTouchNoteMtimes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "touch-note-mtime.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO notes (path, title, content_hash, indexer_version, mtime)
		VALUES ('note1.md', 'Note 1', 'h1', 'v1', 10),
		       ('note2.md', 'Note 2', 'h2', 'v1', 20)
	`)
	require.NoError(t, err)

	require.NoError(t, store.TouchNoteMtimes(ctx, map[string]int64{
		"note1.md": 100,
	}))

	mtimes, err := store.IntelNoteMtimes(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(100), mtimes["note1.md"])
	require.Equal(t, int64(20), mtimes["note2.md"])
}

func TestReplaceIntentEmbeddingSnapshotRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intent_embeddings.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	row := intentstore.EmbeddingRecord{
		Intent:    "search",
		Exemplar:  "find relevant files",
		Embedding: embeddingstypes.Embedding{0.1, 0.2, 0.3},
	}
	require.NoError(t, store.ReplaceIntentEmbeddingSnapshot(ctx, intentstore.Snapshot{
		ProviderFingerprint: "provider-v1",
		Rows:                []intentstore.EmbeddingRecord{row},
	}))

	snapshot, err := store.IntentEmbeddingSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, "provider-v1", snapshot.ProviderFingerprint)
	found := snapshot.Rows
	require.NotEmpty(t, found)

	var got *intentstore.EmbeddingRecord
	for i := range found {
		if found[i].Intent == row.Intent && found[i].Exemplar == row.Exemplar {
			got = &found[i]
			break
		}
	}
	require.NotNil(t, got)
	require.Equal(t, len(row.Embedding), len(got.Embedding))
	for i := range row.Embedding {
		require.InDelta(t, row.Embedding[i], got.Embedding[i], 0.0001)
	}
	require.Equal(t, len(row.Embedding), got.Dimensions)
}

func TestReplaceIntentEmbeddingSnapshotRemovesStaleRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intent_embeddings_replace.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceIntentEmbeddingSnapshot(ctx, intentstore.Snapshot{
		ProviderFingerprint: "provider-v1",
		Rows: []intentstore.EmbeddingRecord{
			{Intent: "search", Exemplar: "keep", Embedding: embeddingstypes.Embedding{1, 0}, Dimensions: 2},
			{Intent: "search", Exemplar: "stale", Embedding: embeddingstypes.Embedding{0, 1}, Dimensions: 2},
		},
	}))
	require.NoError(t, store.ReplaceIntentEmbeddingSnapshot(ctx, intentstore.Snapshot{
		ProviderFingerprint: "provider-v2",
		Rows: []intentstore.EmbeddingRecord{
			{Intent: "search", Exemplar: "keep", Embedding: embeddingstypes.Embedding{0.5, 0.5}, Dimensions: 2},
		},
	}))

	snapshot, err := store.IntentEmbeddingSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, "provider-v2", snapshot.ProviderFingerprint)
	require.Len(t, snapshot.Rows, 1)
	require.Equal(t, "keep", snapshot.Rows[0].Exemplar)
}

func TestReplaceIntentEmbeddingSnapshotRollsBackRowsAndFingerprintTogether(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intent_embeddings_rollback.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	original := intentstore.Snapshot{
		ProviderFingerprint: "provider-v1",
		Rows: []intentstore.EmbeddingRecord{
			{Intent: "search", Exemplar: "original", Embedding: embeddingstypes.Embedding{1, 0}, Dimensions: 2},
		},
	}
	require.NoError(t, store.ReplaceIntentEmbeddingSnapshot(ctx, original))
	_, err = store.db.ExecContext(ctx, `
		CREATE TRIGGER fail_intent_fingerprint_update
		BEFORE UPDATE ON index_metadata
		WHEN OLD.key = 'intent_embeddings_provider_fingerprint'
		BEGIN
			SELECT RAISE(ABORT, 'fingerprint update failed');
		END;
	`)
	require.NoError(t, err)

	err = store.ReplaceIntentEmbeddingSnapshot(ctx, intentstore.Snapshot{
		ProviderFingerprint: "provider-v2",
		Rows: []intentstore.EmbeddingRecord{
			{Intent: "search", Exemplar: "replacement", Embedding: embeddingstypes.Embedding{0, 1}, Dimensions: 2},
		},
	})
	require.ErrorContains(t, err, "fingerprint update failed")

	snapshot, err := store.IntentEmbeddingSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, original.ProviderFingerprint, snapshot.ProviderFingerprint)
	require.Len(t, snapshot.Rows, 1)
	require.Equal(t, "original", snapshot.Rows[0].Exemplar)
}

func TestUpsertEmbeddings_SkipsMissingChunks(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "embeddings-missing.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	chunks := []codeanchor.IntelChunk{
		{
			ChunkID:     codeanchor.IntelChunkID("anchor1", 0, "symbol"),
			OwnerID:     "anchor1",
			OwnerType:   "anchor",
			Ord:         0,
			Granularity: "symbol",
			ContentHash: "hash1",
			StartByte:   0,
			EndByte:     100,
			UpdatedAt:   1000,
		},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"anchor1"}, chunks))

	embeddings := map[string]embeddingstypes.Embedding{
		codeanchor.IntelChunkID("anchor1", 0, "symbol"): {0.1, 0.2, 0.3},
		"missing-chunk": {0.9, 0.8, 0.7},
	}
	require.NoError(t, store.UpsertEmbeddings(ctx, embeddings))

	var count int
	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_embeddings`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	ids := []string{chunks[0].ChunkID, "missing-chunk"}
	published, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	expected := map[string]embeddingstypes.Embedding{chunks[0].ChunkID: embeddings[chunks[0].ChunkID]}
	require.Equal(t, expected, published)
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{chunks[0].ChunkID: nil, "missing-chunk": nil}))
	retained, err := store.EmbeddingsByChunkIDs(ctx, ids)
	require.NoError(t, err)
	require.Equal(t, expected, retained, "empty vectors must preserve existing bytes")
}

func TestUpsertEmbeddings_SupportsMixedDimensionsInOneCall(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "embeddings-mixed-dims.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	chunks := []codeanchor.IntelChunk{
		{
			ChunkID:     codeanchor.IntelChunkID("anchor1", 0, "symbol"),
			OwnerID:     "anchor1",
			OwnerType:   "anchor",
			Ord:         0,
			Granularity: "symbol",
			ContentHash: "hash1",
		},
		{
			ChunkID:     codeanchor.IntelChunkID("section1", 0, "section"),
			OwnerID:     "section1",
			OwnerType:   "doc_section",
			Ord:         0,
			Granularity: "section",
			ContentHash: "hash2",
		},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"anchor1", "section1"}, chunks))

	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		codeanchor.IntelChunkID("anchor1", 0, "symbol"):   {0.1, 0.2, 0.3, 0.4},
		codeanchor.IntelChunkID("section1", 0, "section"): {0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
	}))

	var count4 int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+intelVecTableName(4)).Scan(&count4))
	require.Equal(t, 1, count4)

	var count6 int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+intelVecTableName(6)).Scan(&count6))
	require.Equal(t, 1, count6)
}

func TestUpsertEmbeddings_BatchesBelowSQLiteVariableLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "embeddings-batch-limit.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "owner1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/service.go",
		Symbol:      "Run",
		Fingerprint: "fp1",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	chunks := make([]codeanchor.IntelChunk, 0, 205)
	rows := make(map[string]embeddingstypes.Embedding, 205)
	for i := 0; i < 205; i++ {
		chunkID := fmt.Sprintf("chunk-%03d", i)
		chunks = append(chunks, codeanchor.IntelChunk{
			ChunkID:     chunkID,
			OwnerID:     anchor.AnchorID,
			OwnerType:   "anchor",
			Ord:         i,
			Granularity: "symbol",
			ContentHash: fmt.Sprintf("hash-%03d", i),
			UpdatedAt:   1,
		})
		rows[chunkID] = embeddingstypes.Embedding{1, 0, 0, 0}
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchor.AnchorID}, chunks))

	require.NoError(t, store.UpsertEmbeddings(ctx, rows))

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_embeddings`).Scan(&count))
	require.Equal(t, 205, count)
}

func TestSearchEmbeddings_FindsSimilarChunks(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "search-embeddings.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Create intel anchors and doc sections first
	anchors1 := []codeanchor.IntelAnchor{
		{
			AnchorID:    "anchor1",
			Lang:        codeanchor.LangPy,
			Kind:        "function",
			Path:        "pkg/utils.py",
			Symbol:      "helper",
			FQN:         "pkg.utils.helper",
			StartByte:   0,
			EndByte:     100,
			StartLine:   1,
			EndLine:     10,
			Fingerprint: "fp1",
			UpdatedAt:   1000,
		},
	}
	anchors2 := []codeanchor.IntelAnchor{
		{
			AnchorID:    "anchor2",
			Lang:        codeanchor.LangPy,
			Kind:        "class",
			Path:        "pkg/models.py",
			Symbol:      "User",
			FQN:         "pkg.models.User",
			StartByte:   0,
			EndByte:     200,
			StartLine:   1,
			EndLine:     20,
			Fingerprint: "fp2",
			UpdatedAt:   1000,
		},
	}
	sections := []codeanchor.IntelDocSection{
		{
			SectionID:   "section1",
			Path:        "docs/README.md",
			Title:       "Overview",
			Level:       1,
			StartByte:   0,
			EndByte:     100,
			Content:     "This is the overview",
			Fingerprint: "fp3",
			UpdatedAt:   1000,
		},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/utils.py", anchors1, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/models.py", anchors2, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/README.md", sections, nil, nil))

	// Create chunks for each
	chunks := []codeanchor.IntelChunk{
		{ChunkID: "chunk1", OwnerID: "anchor1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
		{ChunkID: "chunk2", OwnerID: "anchor2", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h2"},
		{ChunkID: "chunk3", OwnerID: "section1", OwnerType: "doc_section", Ord: 0, Granularity: "section", ContentHash: "h3"},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"anchor1", "anchor2", "section1"}, chunks))

	// Create embeddings - chunk1 and chunk3 are similar, chunk2 is different
	embeddings := map[string]embeddingstypes.Embedding{
		"chunk1": {1.0, 0.0, 0.0, 0.0}, // unit vector along x
		"chunk2": {0.0, 1.0, 0.0, 0.0}, // unit vector along y (orthogonal)
		"chunk3": {0.9, 0.1, 0.0, 0.0}, // similar to chunk1
	}
	require.NoError(t, store.UpsertEmbeddings(ctx, embeddings))

	// Query with vector similar to chunk1 and chunk3
	query := embeddingstypes.Embedding{1.0, 0.0, 0.0, 0.0}
	results, skipped, err := store.SearchEmbeddings(ctx, query, 10, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, results, 3)

	// chunk1 should be first (exact match), chunk3 second (similar), chunk2 last (orthogonal)
	require.Equal(t, "chunk1", results[0].ChunkID)
	require.InDelta(t, 1.0, results[0].Score, 0.01)

	require.Equal(t, "chunk3", results[1].ChunkID)
	require.Greater(t, results[1].Score, 0.9)

	require.Equal(t, "chunk2", results[2].ChunkID)
	require.InDelta(t, 0.0, results[2].Score, 0.01)
}

func TestOntologyNodeEmbeddings_AreSidecarTrackedAndSearchable(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-embeddings.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := codeanchor.IntelOntologyNode{
		NodeID:                "node:decision",
		NotePath:              "docs/decisions/search.md",
		NodeRefJSON:           `{"notePath":"docs/decisions/search.md","nodeId":"decision","typeName":"Decision","kind":"SECTION"}`,
		NodeKind:              "SECTION",
		TypeName:              "Decision",
		Title:                 "Search decision",
		StartByte:             0,
		EndByte:               100,
		StructuralFingerprint: "node-fp",
		SchemaHash:            "schema-hash",
		UpdatedAt:             10,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{node},
		LinkDependencies: []codeanchor.IntelOntologyNodeLinkDependency{{
			SourceNotePath:         node.NotePath,
			NodeID:                 node.NodeID,
			TypeName:               node.TypeName,
			FieldName:              "owner",
			TargetInput:            "Alice",
			TargetInputNorm:        "alice",
			ResolvedTargetNotePath: "people/alice.md",
			ResolvedTargetTypeName: "Person",
			UpdatedAt:              1,
		}},
	}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "node-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: "node_body",
		Breadcrumb:  "docs > decisions > search",
		Heading:     "Search decision",
		ContentHash: "chunk-hash",
		StartByte:   0,
		EndByte:     100,
		UpdatedAt:   10,
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"node-chunk": {1, 0, 0, 0},
	}))
	state := codeanchor.IntelOntologyNodeEmbeddingState{
		ChunkID:                  "node-chunk",
		NodeID:                   node.NodeID,
		NotePath:                 node.NotePath,
		TypeName:                 node.TypeName,
		NodeKind:                 node.NodeKind,
		EmbeddingSchemaSignature: "schema-sig",
		NodeStructureFingerprint: node.StructuralFingerprint,
		SourceContentHash:        "source-hash",
		ChunkTextHash:            "text-hash",
		ChunkGranularity:         "node_body",
		Provider:                 "test",
		Model:                    "deterministic",
		UpdatedAt:                11,
	}
	require.NoError(t, store.UpsertOntologyNodeEmbeddingStates(ctx, []codeanchor.IntelOntologyNodeEmbeddingState{state}))

	results, skipped, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 5, EmbeddingSearchFilters{
		OwnerTypes:   []string{"ontology_node"},
		PathPrefixes: []string{"docs/decisions"},
		Granularity:  []string{"node_body"},
	})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, results, 1)
	require.Equal(t, "node-chunk", results[0].ChunkID)
	require.Equal(t, "ontology_node", results[0].OwnerType)
	require.Equal(t, node.NotePath, results[0].Path)

	nodes, err := store.OntologyNodesByIDs(ctx, []string{node.NodeID})
	require.NoError(t, err)
	require.Equal(t, node.NodeRefJSON, nodes[node.NodeID].NodeRefJSON)
	states, err := store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, []string{"node-chunk"})
	require.NoError(t, err)
	require.Equal(t, state.EmbeddingSchemaSignature, states["node-chunk"].EmbeddingSchemaSignature)

	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{node.NotePath}, nil))
	states, err = store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, []string{"node-chunk"})
	require.NoError(t, err)
	require.Empty(t, states)
	results, _, err = store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 5, EmbeddingSearchFilters{OwnerTypes: []string{"ontology_node"}})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestSearchEmbeddings_FiltersOntologyNodesByType(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-type-filter.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	specNode := codeanchor.IntelOntologyNode{
		NodeID:                "node:spec",
		NotePath:              "docs/spec.md",
		NodeRefJSON:           `{"notePath":"docs/spec.md","nodeId":"spec","typeName":"TechnicalSpec","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "TechnicalSpec",
		Title:                 "Spec",
		StructuralFingerprint: "spec-fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	refNode := codeanchor.IntelOntologyNode{
		NodeID:                "node:reference",
		NotePath:              "docs/reference.md",
		NodeRefJSON:           `{"notePath":"docs/reference.md","nodeId":"reference","typeName":"ReferenceDoc","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "ReferenceDoc",
		Title:                 "Reference",
		StructuralFingerprint: "ref-fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{specNode.NotePath, refNode.NotePath}, []codeanchor.IntelOntologyNode{specNode, refNode}))

	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "anchor1", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/app.go", Symbol: "Run", FQN: "pkg.Run", Fingerprint: "anchor-fp", UpdatedAt: 1},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/app.go", anchors, nil, nil))

	chunks := []codeanchor.IntelChunk{
		{ChunkID: "spec-chunk", OwnerID: specNode.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: "node_body", ContentHash: "spec-hash"},
		{ChunkID: "reference-chunk", OwnerID: refNode.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: "node_body", ContentHash: "ref-hash"},
		{ChunkID: "anchor-chunk", OwnerID: "anchor1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "anchor-hash"},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{specNode.NodeID, refNode.NodeID, "anchor1"}, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"spec-chunk":      {1, 0, 0, 0},
		"reference-chunk": {1, 0, 0, 0},
		"anchor-chunk":    {1, 0, 0, 0},
	}))

	results, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, EmbeddingSearchFilters{
		OntologyTypeNames: []string{"TechnicalSpec"},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "spec-chunk", results[0].ChunkID)
	require.Equal(t, specNode.NotePath, results[0].Path)

	results, _, err = store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, EmbeddingSearchFilters{
		OwnerTypes:        []string{"ontology_node"},
		OntologyTypeNames: []string{"ReferenceDoc", "TechnicalSpec"},
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.ElementsMatch(t, []string{"spec-chunk", "reference-chunk"}, []string{results[0].ChunkID, results[1].ChunkID})
}

func TestOntologyNodes_PersistLocatorFieldsAndLookupByFragment(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-locators.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := codeanchor.IntelOntologyNode{
		NodeID:        "node-story",
		NotePath:      "docs/spec.md",
		NodeRefJSON:   `{"notePath":"docs/spec.md","fragment":"^story-a","nodeId":"docs/spec.md#^story-a","typeName":"UserStory","kind":"EMBEDDED"}`,
		NodeKind:      "EMBEDDED",
		TypeName:      "UserStory",
		Title:         "Story A",
		SourceLocator: "docs/spec.md#^story-a",
		Fragment:      "^story-a",
		BlockID:       "story-a",
		DisplayLabel:  "Story A",
		LocatorStatus: "linkable",
		StartByte:     1,
		EndByte:       10,
		UpdatedAt:     123,
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))

	byLocator, err := store.OntologyNodesBySourceLocators(ctx, []string{"docs/spec.md#^story-a"})
	require.NoError(t, err)
	require.Equal(t, "node-story", byLocator["docs/spec.md#^story-a"].NodeID)
	require.Equal(t, "story-a", byLocator["docs/spec.md#^story-a"].BlockID)

	byFragment, err := store.OntologyNodesByNoteFragments(ctx, []string{"docs/spec.md#^story-a"})
	require.NoError(t, err)
	require.Equal(t, "node-story", byFragment["docs/spec.md#^story-a"].NodeID)

	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{node.NotePath}, nil))
	byLocator, err = store.OntologyNodesBySourceLocators(ctx, []string{"docs/spec.md#^story-a"})
	require.NoError(t, err)
	require.Empty(t, byLocator)
}

func TestReplaceOntologySnapshot_ClearsOntologyNodeCatalog(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-snapshot.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := codeanchor.IntelOntologyNode{
		NodeID:        "node-story",
		NotePath:      "docs/spec.md",
		NodeRefJSON:   `{"notePath":"docs/spec.md","fragment":"^story-a","nodeId":"docs/spec.md#^story-a","typeName":"UserStory","kind":"EMBEDDED"}`,
		NodeKind:      "EMBEDDED",
		TypeName:      "UserStory",
		SourceLocator: "docs/spec.md#^story-a",
		Fragment:      "^story-a",
		BlockID:       "story-a",
		StartByte:     1,
		EndByte:       10,
		UpdatedAt:     123,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{node},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{{
			NodeID:    node.NodeID,
			NotePath:  node.NotePath,
			TypeName:  node.TypeName,
			FieldName: "status",
			ValueKind: "string",
			ValueText: "active",
			ValueNorm: "active",
			UpdatedAt: 1,
		}},
	}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "node-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: "node_body",
		ContentHash: "hash",
		UpdatedAt:   1,
	}}))
	require.NoError(t, store.UpsertOntologyNodeEmbeddingStates(ctx, []codeanchor.IntelOntologyNodeEmbeddingState{{
		ChunkID:                  "node-chunk",
		NodeID:                   node.NodeID,
		NotePath:                 node.NotePath,
		TypeName:                 node.TypeName,
		NodeKind:                 node.NodeKind,
		EmbeddingSchemaSignature: "sig",
		ChunkTextHash:            "text",
		ChunkGranularity:         "node_body",
		UpdatedAt:                1,
	}}))
	_, err = store.db.ExecContext(ctx, `INSERT INTO ontology_node_field_value_dependencies
		(source_note_path, field_name, target_input_norm, resolved_target_note_path, updated_at)
		VALUES (?, 'owner', 'alice', 'people/alice.md', 1)`, node.NotePath)
	require.NoError(t, err)
	var dependencyCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_value_dependencies`).Scan(&dependencyCount))
	require.Equal(t, 1, dependencyCount)

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
	}))

	byLocator, err := store.OntologyNodesBySourceLocators(ctx, []string{"docs/spec.md#^story-a"})
	require.NoError(t, err)
	require.Empty(t, byLocator)
	states, err := store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, []string{"node-chunk"})
	require.NoError(t, err)
	require.Empty(t, states)
	var chunkCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_chunks WHERE chunk_id = ?`, "node-chunk").Scan(&chunkCount))
	require.Zero(t, chunkCount)
	var fieldValueCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_values`).Scan(&fieldValueCount))
	require.Zero(t, fieldValueCount)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_value_dependencies`).Scan(&dependencyCount))
	require.Zero(t, dependencyCount)
	sources, err := store.OntologyNodeLinkDependencySources(ctx, []string{"people/alice.md"}, []string{"alice"})
	require.NoError(t, err)
	require.Empty(t, sources)
}

func TestResetDomain_DropsOntologyNodeTables(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "reset-ontology-nodes.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := codeanchor.IntelOntologyNode{
		NodeID:                "node:story",
		NotePath:              "docs/spec.md",
		NodeRefJSON:           `{"notePath":"docs/spec.md","nodeId":"story","typeName":"Story","kind":"EMBEDDED"}`,
		NodeKind:              "EMBEDDED",
		TypeName:              "Story",
		StructuralFingerprint: "fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{node},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{{
			NodeID:    node.NodeID,
			NotePath:  node.NotePath,
			TypeName:  node.TypeName,
			FieldName: "status",
			ValueKind: "string",
			ValueText: "active",
			ValueNorm: "active",
			UpdatedAt: 1,
		}},
	}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: "node_body",
		ContentHash: "hash",
		UpdatedAt:   1,
	}}))
	require.NoError(t, store.UpsertOntologyNodeEmbeddingStates(ctx, []codeanchor.IntelOntologyNodeEmbeddingState{{
		ChunkID:                  "chunk",
		NodeID:                   node.NodeID,
		NotePath:                 node.NotePath,
		TypeName:                 node.TypeName,
		NodeKind:                 node.NodeKind,
		EmbeddingSchemaSignature: "sig",
		ChunkTextHash:            "text",
		ChunkGranularity:         "node_body",
		UpdatedAt:                1,
	}}))

	require.NoError(t, store.ResetDomain(ctx))
	for _, table := range []string{"ontology_nodes", "ontology_node_field_values", "ontology_node_field_value_dependencies", "ontology_node_embedding_state"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestReplaceOntologyNodeReadModel_ReplacesNodeScopedFieldValues(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-field-values.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	for _, name := range []string{
		"ontology_node_field_values", "ontology_node_field_value_dependencies",
		"idx_ontology_node_field_values_unique", "idx_ontology_node_field_values_norm",
		"idx_ontology_node_field_values_target_note",
		"idx_ontology_node_field_value_dependencies_unique",
		"idx_ontology_node_field_value_dependencies_target_input",
		"idx_ontology_node_field_value_dependencies_resolved_target",
	} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count))
		require.Equal(t, 1, count, name)
	}

	active := true
	nodeA := codeanchor.IntelOntologyNode{
		NodeID:      "node:a",
		NotePath:    "docs/a.md",
		NodeRefJSON: "{}",
		NodeKind:    "NOTE",
		TypeName:    "Spec",
		Title:       "A",
		UpdatedAt:   1,
	}
	nodeB := codeanchor.IntelOntologyNode{
		NodeID:      "node:b",
		NotePath:    "docs/b.md",
		NodeRefJSON: "{}",
		NodeKind:    "NOTE",
		TypeName:    "Spec",
		Title:       "B",
		UpdatedAt:   1,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{nodeA.NotePath, nodeB.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{nodeA, nodeB},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{
			{NodeID: nodeA.NodeID, NotePath: nodeA.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", ListOrdinal: 0, UpdatedAt: 1},
			{NodeID: nodeA.NodeID, NotePath: nodeA.NotePath, TypeName: "Spec", FieldName: "ready", ValueKind: "bool", ValueText: "true", ValueNorm: "true", ValueBool: &active, ListOrdinal: 0, UpdatedAt: 1},
			{NodeID: nodeB.NodeID, NotePath: nodeB.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Draft", ValueNorm: "draft", ListOrdinal: 0, UpdatedAt: 1},
		},
		LinkDependencies: []codeanchor.IntelOntologyNodeLinkDependency{
			{SourceNotePath: nodeA.NotePath, NodeID: nodeA.NodeID, TypeName: "Spec", FieldName: "owner", TargetInput: "ALICE", TargetInputNorm: "alice", ResolvedTargetNotePath: "people/alice.md", ResolvedTargetTypeName: "Person", UpdatedAt: 1},
			{SourceNotePath: nodeB.NotePath, NodeID: nodeB.NodeID, TypeName: "Spec", FieldName: "owner", TargetInput: "BOB", TargetInputNorm: "bob", ResolvedTargetNotePath: "people/bob.md", ResolvedTargetTypeName: "Person", UpdatedAt: 1},
		},
	}))

	rows, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, []string{nodeA.NodeID}, nil)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{nodeA.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{nodeA},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{
			{NodeID: nodeA.NodeID, NotePath: nodeA.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Complete", ValueNorm: "complete", ListOrdinal: 0, UpdatedAt: 2},
		},
	}))

	rows, err = store.OntologyNodeFieldValuesByNodeIDs(ctx, []string{nodeA.NodeID, nodeB.NodeID}, nil)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "complete", fieldValueNorm(t, rows, nodeA.NodeID, "status"))
	require.Equal(t, "draft", fieldValueNorm(t, rows, nodeB.NodeID, "status"))
	require.Empty(t, fieldValueNorm(t, rows, nodeA.NodeID, "ready"))

	sources, err := store.OntologyNodeLinkDependencySources(ctx, []string{"people/alice.md", "people/bob.md"}, []string{"alice", "bob"})
	require.NoError(t, err)
	require.Equal(t, []string{nodeB.NotePath}, sources)
}

func TestOntologyNodesByTypePlan_FiltersAndSortsFieldValues(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-query-plan.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	rankA := int64(2)
	rankB := int64(1)
	nodeA := codeanchor.IntelOntologyNode{NodeID: "node:a", NotePath: "docs/a.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Spec", Title: "A", UpdatedAt: 1}
	nodeB := codeanchor.IntelOntologyNode{NodeID: "node:b", NotePath: "docs/b.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Spec", Title: "B", UpdatedAt: 1}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{nodeA.NotePath, nodeB.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{nodeA, nodeB},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{
			{NodeID: nodeA.NodeID, NotePath: nodeA.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
			{NodeID: nodeA.NodeID, NotePath: nodeA.NotePath, TypeName: "Spec", FieldName: "rank", ValueKind: "int", ValueText: "2", ValueNorm: "2", ValueInt: &rankA, UpdatedAt: 1},
			{NodeID: nodeB.NodeID, NotePath: nodeB.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
			{NodeID: nodeB.NodeID, NotePath: nodeB.NotePath, TypeName: "Spec", FieldName: "rank", ValueKind: "int", ValueText: "1", ValueNorm: "1", ValueInt: &rankB, UpdatedAt: 1},
		},
	}))

	nodes, err := store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
		TypeNames: []string{"Spec"},
		Predicates: []codeanchor.OntologyFieldPredicate{{
			FieldName: "status",
			Op:        codeanchor.OntologyFieldOpEq,
			Values:    []codeanchor.IntelOntologyNodeFieldValue{{ValueNorm: "active"}},
		}},
		Sort:  []codeanchor.OntologyFieldSort{{FieldName: "rank", ValueKind: "int"}},
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, "node:b", nodes[0].NodeID)
	require.Equal(t, "node:a", nodes[1].NodeID)
}

func TestOntologyNodesByTypePlan_MultiTypeINQuery(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-query-plan-multi-type.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	rankSpec := int64(1)
	rankDecision := int64(2)
	specA := codeanchor.IntelOntologyNode{NodeID: "node:s", NotePath: "specs/a.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Spec", Title: "Spec A", UpdatedAt: 1}
	decisionA := codeanchor.IntelOntologyNode{NodeID: "node:d", NotePath: "decisions/a.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Decision", Title: "Decision A", UpdatedAt: 1}
	otherA := codeanchor.IntelOntologyNode{NodeID: "node:x", NotePath: "other/a.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "OtherType", Title: "Other", UpdatedAt: 1}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{specA.NotePath, decisionA.NotePath, otherA.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{specA, decisionA, otherA},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{
			{NodeID: specA.NodeID, NotePath: specA.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
			{NodeID: specA.NodeID, NotePath: specA.NotePath, TypeName: "Spec", FieldName: "rank", ValueKind: "int", ValueText: "1", ValueNorm: "1", ValueInt: &rankSpec, UpdatedAt: 1},
			{NodeID: decisionA.NodeID, NotePath: decisionA.NotePath, TypeName: "Decision", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
			{NodeID: decisionA.NodeID, NotePath: decisionA.NotePath, TypeName: "Decision", FieldName: "rank", ValueKind: "int", ValueText: "2", ValueNorm: "2", ValueInt: &rankDecision, UpdatedAt: 1},
			{NodeID: otherA.NodeID, NotePath: otherA.NotePath, TypeName: "OtherType", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
		},
	}))

	// Multi-type IN: returns Spec + Decision rows but excludes OtherType.
	rows, err := store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
		TypeNames: []string{"Spec", "Decision"},
		Predicates: []codeanchor.OntologyFieldPredicate{{
			FieldName: "status",
			Op:        codeanchor.OntologyFieldOpEq,
			Values:    []codeanchor.IntelOntologyNodeFieldValue{{ValueNorm: "active"}},
		}},
		Sort:  []codeanchor.OntologyFieldSort{{FieldName: "rank", ValueKind: "int"}},
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "node:s", rows[0].NodeID, "rank=1 (Spec) sorts before rank=2 (Decision)")
	require.Equal(t, "node:d", rows[1].NodeID)

	// Empty TypeNames returns empty.
	rows, err = store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
		TypeNames: nil,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestOntologyNodesByTypePlan_SortDeduplicatesRepeatedFieldRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-query-plan-duplicate-sort.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	early := int64(1)
	late := int64(9)
	mid := int64(5)
	nodeA := codeanchor.IntelOntologyNode{NodeID: "node:a", NotePath: "docs/a.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Spec", Title: "A", UpdatedAt: 1}
	nodeB := codeanchor.IntelOntologyNode{NodeID: "node:b", NotePath: "docs/b.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Spec", Title: "B", UpdatedAt: 1}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{nodeA.NotePath, nodeB.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{nodeA, nodeB},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{
			{NodeID: nodeA.NodeID, NotePath: nodeA.NotePath, TypeName: "Spec", FieldName: "rank", ValueKind: "int", ValueText: "9", ValueNorm: "9", ValueInt: &late, ListOrdinal: 0, UpdatedAt: 1},
			{NodeID: nodeA.NodeID, NotePath: nodeA.NotePath, TypeName: "Spec", FieldName: "rank", ValueKind: "int", ValueText: "1", ValueNorm: "1", ValueInt: &early, ListOrdinal: 1, UpdatedAt: 1},
			{NodeID: nodeB.NodeID, NotePath: nodeB.NotePath, TypeName: "Spec", FieldName: "rank", ValueKind: "int", ValueText: "5", ValueNorm: "5", ValueInt: &mid, UpdatedAt: 1},
		},
	}))

	nodes, err := store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
		TypeNames: []string{"Spec"},
		Sort:      []codeanchor.OntologyFieldSort{{FieldName: "rank", ValueKind: "int"}},
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, []string{"node:a", "node:b"}, []string{nodes[0].NodeID, nodes[1].NodeID})
}

func TestOntologyNodesByTypePlan_UsesIndexedFieldPredicateAtScale(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-query-plan-scale.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	const total = 1200
	notePaths := make([]string, 0, total)
	nodes := make([]codeanchor.IntelOntologyNode, 0, total)
	fields := make([]codeanchor.IntelOntologyNodeFieldValue, 0, total)
	for i := 0; i < total; i++ {
		notePath := fmt.Sprintf("docs/spec-%04d.md", i)
		nodeID := fmt.Sprintf("node:%04d", i)
		status := "draft"
		if i == 777 {
			status = "active"
		}
		notePaths = append(notePaths, notePath)
		nodes = append(nodes, codeanchor.IntelOntologyNode{
			NodeID:      nodeID,
			NotePath:    notePath,
			NodeRefJSON: "{}",
			NodeKind:    "NOTE",
			TypeName:    "Spec",
			Title:       notePath,
			UpdatedAt:   1,
		})
		fields = append(fields, codeanchor.IntelOntologyNodeFieldValue{
			NodeID:    nodeID,
			NotePath:  notePath,
			TypeName:  "Spec",
			FieldName: "status",
			ValueKind: "string",
			ValueText: status,
			ValueNorm: status,
			UpdatedAt: 1,
		})
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths:   notePaths,
		Nodes:       nodes,
		FieldValues: fields,
	}))

	plan := codeanchor.OntologyNodeQueryPlan{
		TypeNames: []string{"Spec"},
		Predicates: []codeanchor.OntologyFieldPredicate{{
			FieldName: "status",
			Op:        codeanchor.OntologyFieldOpEq,
			Values:    []codeanchor.IntelOntologyNodeFieldValue{{ValueNorm: "active"}},
		}},
		Limit: 10,
	}
	rows, err := store.OntologyNodesByTypePlan(ctx, plan)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "node:0777", rows[0].NodeID)

	query, args, err := ontologyNodesByTypePlanSQL(plan)
	require.NoError(t, err)
	planRows, err := store.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer planRows.Close()
	var planText string
	for planRows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, planRows.Scan(&id, &parent, &notUsed, &detail))
		planText += "\n" + detail
	}
	require.NoError(t, planRows.Err())
	require.Contains(t, planText, "idx_ontology_node_field_values_norm")
}

func TestOntologyNodesByTypePlan_ExistsRequiresNonEmptyValueAndUsesIndex(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-query-plan-exists.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	nodeWith := codeanchor.IntelOntologyNode{NodeID: "node:has", NotePath: "docs/has.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Spec", UpdatedAt: 1}
	nodeBlank := codeanchor.IntelOntologyNode{NodeID: "node:blank", NotePath: "docs/blank.md", NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: "Spec", UpdatedAt: 1}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{nodeWith.NotePath, nodeBlank.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{nodeWith, nodeBlank},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{
			{NodeID: nodeWith.NodeID, NotePath: nodeWith.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
			{NodeID: nodeBlank.NodeID, NotePath: nodeBlank.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "", ValueNorm: "", UpdatedAt: 1},
		},
	}))

	// exists must filter out blank-value rows.
	plan := codeanchor.OntologyNodeQueryPlan{
		TypeNames: []string{"Spec"},
		Predicates: []codeanchor.OntologyFieldPredicate{{
			FieldName: "status",
			Op:        codeanchor.OntologyFieldOpExists,
		}},
		Limit: 10,
	}
	rows, err := store.OntologyNodesByTypePlan(ctx, plan)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "node:has", rows[0].NodeID)

	// EXPLAIN QUERY PLAN must show idx_ontology_node_field_values_norm in use.
	query, args, err := ontologyNodesByTypePlanSQL(plan)
	require.NoError(t, err)
	planRows, err := store.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer planRows.Close()
	planText := readExplainPlan(t, planRows)
	require.Contains(t, planText, "idx_ontology_node_field_values_norm")
}

func TestOntologyNodesByTypePlan_ErrorsOnEmptyPredicateValues(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-query-plan-empty-vals.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for _, op := range []codeanchor.OntologyFieldOperator{
		codeanchor.OntologyFieldOpEq,
		codeanchor.OntologyFieldOpGT,
		codeanchor.OntologyFieldOpGTE,
		codeanchor.OntologyFieldOpLT,
		codeanchor.OntologyFieldOpLTE,
	} {
		_, err := store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
			TypeNames: []string{"Spec"},
			Predicates: []codeanchor.OntologyFieldPredicate{{
				FieldName: "status",
				Op:        op,
				Values:    nil,
			}},
			Limit: 10,
		})
		require.Error(t, err, "op=%s", op)
		require.Contains(t, err.Error(), "requires value", "op=%s", op)
	}
}

// TestOntologyNodesByTypePlan_TypedPredicatesUsePartialIndexes verifies that
// each typed-value predicate path generates SQL the SQLite planner can serve
// from the matching partial index. One sub-test per (column, partial index)
// pair; we only EXPLAIN the SQL shape, not run it against synthetic scale data.
func TestOntologyNodesByTypePlan_TypedPredicatesUsePartialIndexes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-typed-predicate-explain.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	boolValue, intValue, realValue := true, int64(5), 1.5
	dateValue, datetimeValue := "2026-05-06", "2026-05-06T00:00:00Z"
	cases := []struct {
		name  string
		op    codeanchor.OntologyFieldOperator
		value codeanchor.IntelOntologyNodeFieldValue
		index string
	}{
		{"bool", codeanchor.OntologyFieldOpEq, codeanchor.IntelOntologyNodeFieldValue{ValueBool: &boolValue}, "idx_ontology_node_field_values_bool"},
		{"int", codeanchor.OntologyFieldOpGTE, codeanchor.IntelOntologyNodeFieldValue{ValueInt: &intValue}, "idx_ontology_node_field_values_int"},
		{"real", codeanchor.OntologyFieldOpGT, codeanchor.IntelOntologyNodeFieldValue{ValueReal: &realValue}, "idx_ontology_node_field_values_real"},
		{"date", codeanchor.OntologyFieldOpLTE, codeanchor.IntelOntologyNodeFieldValue{ValueDate: &dateValue}, "idx_ontology_node_field_values_date"},
		{"datetime", codeanchor.OntologyFieldOpLT, codeanchor.IntelOntologyNodeFieldValue{ValueDateTime: &datetimeValue}, "idx_ontology_node_field_values_datetime"},
		{"target_node", codeanchor.OntologyFieldOpEq, codeanchor.IntelOntologyNodeFieldValue{TargetNodeID: "node:target"}, "idx_ontology_node_field_values_target"},
		{"target_note", codeanchor.OntologyFieldOpEq, codeanchor.IntelOntologyNodeFieldValue{TargetNotePath: "people/alice.md"}, "idx_ontology_node_field_values_target_note"},
		{"norm", codeanchor.OntologyFieldOpEq, codeanchor.IntelOntologyNodeFieldValue{ValueNorm: "active"}, "idx_ontology_node_field_values_norm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query, args, err := ontologyNodesByTypePlanSQL(codeanchor.OntologyNodeQueryPlan{
				TypeNames:  []string{"Spec"},
				Predicates: []codeanchor.OntologyFieldPredicate{{FieldName: "field", Op: tc.op, Values: []codeanchor.IntelOntologyNodeFieldValue{tc.value}}},
				Limit:      10,
			})
			require.NoError(t, err)
			rows, err := store.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
			require.NoError(t, err)
			defer rows.Close()
			require.Contains(t, readExplainPlan(t, rows), tc.index)
		})
	}
}

func TestOntologyNodesByTypePlan_RequiresPositiveLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-query-plan-limit.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for _, limit := range []int{0, -1, -100} {
		_, err := store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
			TypeNames: []string{"Spec"},
			Limit:     limit,
		})
		require.Error(t, err, "limit=%d", limit)
		require.Contains(t, err.Error(), "Limit")
	}
}

func TestOntologyNodeFieldValuesByNodeIDs_DoesNotMutateCallerFieldNames(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-field-values-fieldnames.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := codeanchor.IntelOntologyNode{
		NodeID:      "node:a",
		NotePath:    "docs/a.md",
		NodeRefJSON: "{}",
		NodeKind:    "NOTE",
		TypeName:    "Spec",
		UpdatedAt:   1,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{node},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{
			{NodeID: node.NodeID, NotePath: node.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
		},
	}))

	original := []string{"Status", "Title"}
	snapshot := append([]string(nil), original...)

	_, err = store.OntologyNodeFieldValuesByNodeIDs(ctx, []string{node.NodeID}, original)
	require.NoError(t, err)
	require.Equal(t, snapshot, original, "caller's fieldNames slice must not be mutated")
}

func TestReplaceOntologyNodeReadModel_SkipsInvalidFieldRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-field-rows-counter.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := codeanchor.IntelOntologyNode{
		NodeID:      "node:a",
		NotePath:    "docs/a.md",
		NodeRefJSON: "{}",
		NodeKind:    "NOTE",
		TypeName:    "Spec",
		UpdatedAt:   1,
	}
	// 1 valid row + 2 rows skipped by the empty-id guard.
	fieldValues := []codeanchor.IntelOntologyNodeFieldValue{
		{NodeID: node.NodeID, NotePath: node.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
		{NodeID: "", NotePath: node.NotePath, TypeName: "Spec", FieldName: "skipped", ValueKind: "string", ValueText: "x", ValueNorm: "x", UpdatedAt: 1},
		{NodeID: node.NodeID, NotePath: "", TypeName: "Spec", FieldName: "skipped2", ValueKind: "string", ValueText: "y", ValueNorm: "y", UpdatedAt: 1},
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths:   []string{node.NotePath},
		Nodes:       []codeanchor.IntelOntologyNode{node},
		FieldValues: fieldValues,
	}))

	var rowCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_values`).Scan(&rowCount))
	require.Equal(t, 1, rowCount, "only the valid row should be persisted")
}

func TestReplaceOntologyNodeReadModel_FullReplaceClearsEmptyBuild(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-full-replace-empty.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := codeanchor.IntelOntologyNode{
		NodeID:      "node:a",
		NotePath:    "docs/a.md",
		NodeRefJSON: "{}",
		NodeKind:    "NOTE",
		TypeName:    "Spec",
		UpdatedAt:   1,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{node},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{
			{NodeID: node.NodeID, NotePath: node.NotePath, TypeName: "Spec", FieldName: "status", ValueKind: "string", ValueText: "Active", ValueNorm: "active", UpdatedAt: 1},
		},
	}))

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{FullReplace: true}))

	for _, table := range []string{"ontology_nodes", "ontology_node_field_values", "ontology_node_field_value_dependencies"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count))
		require.Equal(t, 0, count, table)
	}
}

func fieldValueNorm(t *testing.T, rows []codeanchor.IntelOntologyNodeFieldValue, nodeID, fieldName string) string {
	t.Helper()
	for _, row := range rows {
		if row.NodeID == nodeID && row.FieldName == fieldName {
			return row.ValueNorm
		}
	}
	return ""
}

// readExplainPlan scans EXPLAIN QUERY PLAN result rows into a single concatenated detail string.
func readExplainPlan(t *testing.T, rows *sql.Rows) string {
	t.Helper()
	var planText string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		planText += "\n" + detail
	}
	require.NoError(t, rows.Err())
	return planText
}

func TestSearchEmbeddings_MigratesAndSyncsVecMirror(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "search-vec-mirror.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "a1", Lang: codeanchor.LangPy, Kind: "function", Path: "pkg/foo.py", Symbol: "foo", FQN: "pkg.foo", Fingerprint: "fp1", UpdatedAt: 1000},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/foo.py", anchors, nil, nil))
	chunks := []codeanchor.IntelChunk{
		{ChunkID: "c1", OwnerID: "a1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"a1"}, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"c1": {1, 0, 0, 0},
	}))

	_, _, err = store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, EmbeddingSearchFilters{})
	require.NoError(t, err)

	vecTable := intelVecTableName(4)
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+vecTable).Scan(&count))
	require.Equal(t, 1, count)

	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"c1": {0.9, 0.1, 0, 0},
	}))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+vecTable).Scan(&count))
	require.Equal(t, 1, count)
	var vectorHex string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT hex(v.embedding) FROM `+vecTable+` v JOIN intel_chunks c ON c.id = v.chunk_id WHERE c.chunk_id = 'c1'`).Scan(&vectorHex))
	require.Equal(t, fmt.Sprintf("%X", embedToBytes(embeddingstypes.Embedding{0.9, 0.1, 0, 0})), vectorHex)
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{"c1": {0.9, 0.1, 0, 0}}))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+vecTable).Scan(&count))
	require.Equal(t, 1, count)
}

func TestSearchEmbeddings_UsesVecMirrorAsSourceOfTruth(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "search-vec-source.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "a1", Lang: codeanchor.LangPy, Kind: "function", Path: "pkg/foo.py", Symbol: "foo", FQN: "pkg.foo", Fingerprint: "fp1", UpdatedAt: 1000},
		{AnchorID: "a2", Lang: codeanchor.LangPy, Kind: "function", Path: "pkg/bar.py", Symbol: "bar", FQN: "pkg.bar", Fingerprint: "fp2", UpdatedAt: 1000},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/foo.py", anchors[:1], nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/bar.py", anchors[1:], nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"a1", "a2"}, []codeanchor.IntelChunk{
		{ChunkID: "c1", OwnerID: "a1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
		{ChunkID: "c2", OwnerID: "a2", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h2"},
	}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"c1": {1, 0, 0, 0},
		"c2": {0, 1, 0, 0},
	}))

	query := embeddingstypes.Embedding{0.95, 0.05, 0, 0}
	before, _, err := store.SearchEmbeddings(ctx, query, 2, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Len(t, before, 2)
	require.Equal(t, "c1", before[0].ChunkID)

	vecTable := intelVecTableName(4)
	var chunkRowID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM intel_chunks WHERE chunk_id = ?`, "c1").Scan(&chunkRowID))
	_, err = store.db.ExecContext(ctx, `
		UPDATE `+vecTable+`
		SET embedding = ?
		WHERE chunk_id = ?
	`, embedToBytes(embeddingstypes.Embedding{-1, 0, 0, 0}), chunkRowID)
	require.NoError(t, err)

	after, _, err := store.SearchEmbeddings(ctx, query, 2, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Len(t, after, 2)
	require.Equal(t, "c2", after[0].ChunkID)
}

func TestSearchEmbeddings_RequiresCurrentEmbeddingMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "search-vec-no-intel-join.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "a1", Lang: codeanchor.LangPy, Kind: "function", Path: "pkg/foo.py", Symbol: "foo", FQN: "pkg.foo", Fingerprint: "fp1", UpdatedAt: 1000},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/foo.py", anchors, nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"a1"}, []codeanchor.IntelChunk{
		{ChunkID: "c1", OwnerID: "a1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
	}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"c1": {1, 0, 0, 0},
	}))

	query := embeddingstypes.Embedding{1, 0, 0, 0}
	before, _, err := store.SearchEmbeddings(ctx, query, 5, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Equal(t, "c1", before[0].ChunkID)

	vecTable := intelVecTableName(4)
	_, err = store.db.ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_`+vecTable+`_del`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `DELETE FROM intel_embeddings WHERE chunk_id = ?`, "c1")
	require.NoError(t, err)

	after, _, err := store.SearchEmbeddings(ctx, query, 5, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Empty(t, after, "retained vector bytes do not authorize a chunk without current embedding metadata")
}

func TestSearchEmbeddings_CascadeDeletesEmbeddings(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "cascade.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Create anchor, chunk, and embedding
	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "owner1", Lang: codeanchor.LangPy, Kind: "function", Path: "test.py", Symbol: "test", Fingerprint: "fp1"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "test.py", anchors, nil, nil))

	chunks := []codeanchor.IntelChunk{
		{ChunkID: "c1", OwnerID: "owner1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"owner1"}, chunks))

	embeddings := map[string]embeddingstypes.Embedding{"c1": {1.0, 0.0, 0.0, 0.0}}
	require.NoError(t, store.UpsertEmbeddings(ctx, embeddings))

	// Verify embedding exists
	query := embeddingstypes.Embedding{1.0, 0.0, 0.0, 0.0}
	results, _, err := store.SearchEmbeddings(ctx, query, 10, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Delete chunk - should cascade to embedding
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"owner1"}, nil))

	// Verify embedding is gone (check directly in intel_embeddings table)
	var count int
	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_embeddings WHERE chunk_id = 'c1'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "embedding should be deleted when chunk is deleted")
}

func TestIntelAnchorIDsByGoMethodNameInPkg_MatchesFunctionsAndMethods(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "go-symbol-lookup.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Mix of functions and methods in the same package.
	anchors := []codeanchor.IntelAnchor{
		{
			AnchorID:    "f1",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/coderefs/parser.go",
			Symbol:      "DetectLanguage",
			FQN:         "github.com/atomicobject/rhizome/pkg/vault/coderefs.DetectLanguage",
			Fingerprint: "fp1",
		},
		{
			AnchorID:    "m1",
			Lang:        codeanchor.LangGo,
			Kind:        "method",
			Path:        "pkg/coderefs/index.go",
			Symbol:      "Add",
			FQN:         "github.com/atomicobject/rhizome/pkg/vault/coderefs.Index.Add",
			Fingerprint: "fp2",
		},
		{
			AnchorID:    "f2",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/other/other.go",
			Symbol:      "DetectLanguage",
			FQN:         "github.com/atomicobject/rhizome/pkg/other.DetectLanguage",
			Fingerprint: "fp3",
		},
		{
			AnchorID: "m2", Lang: codeanchor.LangGo, Kind: "method", Path: "pkg/other/method.go",
			Symbol: "Add", FQN: "github.com/atomicobject/rhizome/pkg/other.Index.Add", Fingerprint: "fp4",
		},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/coderefs/parser.go", []codeanchor.IntelAnchor{anchors[0]}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/coderefs/index.go", []codeanchor.IntelAnchor{anchors[1]}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/other/other.go", []codeanchor.IntelAnchor{anchors[2]}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/other/method.go", []codeanchor.IntelAnchor{anchors[3]}, nil, nil))

	// Should find the function by exact FQN match.
	ids, err := store.IntelAnchorIDsByGoMethodNameInPkg(ctx, "github.com/atomicobject/rhizome/pkg/vault/coderefs", "DetectLanguage", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"f1"}, ids)

	// Should find the method by LIKE pattern match.
	ids, err = store.IntelAnchorIDsByGoMethodNameInPkg(ctx, "github.com/atomicobject/rhizome/pkg/vault/coderefs", "Add", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"m1"}, ids)

	// Should not find function from different package.
	ids, err = store.IntelAnchorIDsByGoMethodNameInPkg(ctx, "github.com/atomicobject/rhizome/pkg/vault/coderefs", "OtherFunc", 10)
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestIntelAnchorsByFQNsLimited_ReturnsSuffixMatches(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-fqn-limited.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors := []codeanchor.IntelAnchor{
		{
			AnchorID:    "a1",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/a.go",
			Symbol:      "Bar",
			FQN:         "acme.foo.Bar",
			Fingerprint: "fp1",
		},
		{
			AnchorID:    "a2",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/b.go",
			Symbol:      "Bar",
			FQN:         "acme.foo.Bar",
			Fingerprint: "fp2",
		},
		{
			AnchorID:    "b1",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/c.go",
			Symbol:      "Baz",
			FQN:         "acme.foo.Baz",
			Fingerprint: "fp3",
		},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/a.go", []codeanchor.IntelAnchor{anchors[0]}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/b.go", []codeanchor.IntelAnchor{anchors[1]}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/c.go", []codeanchor.IntelAnchor{anchors[2]}, nil, nil))

	found, err := store.IntelAnchorsByFQNsLimited(ctx, []string{"foo.Bar", "acme.foo.Baz"}, 1)
	require.NoError(t, err)

	require.Len(t, found["foo.Bar"], 1)
	require.Equal(t, "a1", found["foo.Bar"][0].AnchorID)

	require.Len(t, found["acme.foo.Baz"], 1)
	require.Equal(t, "b1", found["acme.foo.Baz"][0].AnchorID)
}

func TestIntelAnchorIDsByFQNsAndLang_ExactAndSuffixMatches(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-fqn-ids.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "go-exact", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/a.go", Symbol: "Bar", FQN: "acme.foo.Bar", Fingerprint: "fp1"},
		{AnchorID: "go-other", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/b.go", Symbol: "Baz", FQN: "acme.foo.Baz", Fingerprint: "fp2"},
		{AnchorID: "py-same-suffix", Lang: codeanchor.LangPy, Kind: "function", Path: "pkg/c.py", Symbol: "Bar", FQN: "py.acme.foo.Bar", Fingerprint: "fp3"},
	}
	for _, anchor := range anchors {
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
		require.NoError(t, seedSymbolForAnchor(ctx, store, anchor))
	}

	got, err := store.IntelAnchorIDsByFQNsAndLang(ctx, string(codeanchor.LangGo), []string{
		"acme.foo.Bar",
		"foo.Bar",
		"acme.foo.Baz",
		"missing.Symbol",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"go-exact"}, got["acme.foo.Bar"])
	require.Equal(t, []string{"go-exact"}, got["foo.Bar"])
	require.Equal(t, []string{"go-other"}, got["acme.foo.Baz"])
	require.NotContains(t, got, "missing.Symbol")
}

func TestIntelAnchorIDsByFQNsAndLang_BatchesExactAndSuffixLookups(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-fqn-batch.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	inputs := make([]string, 0, 450)
	for i := 0; i < 450; i++ {
		fqn := fmt.Sprintf("acme.pkg%03d.Task%03d", i, i)
		path := fmt.Sprintf("pkg/%03d.go", i)
		anchor := codeanchor.IntelAnchor{
			AnchorID:    fmt.Sprintf("a%03d", i),
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        path,
			Symbol:      fmt.Sprintf("Task%03d", i),
			FQN:         fqn,
			Fingerprint: fmt.Sprintf("fp%03d", i),
		}
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{anchor}, nil, nil))
		require.NoError(t, seedSymbolForAnchor(ctx, store, anchor))
		if i%2 == 0 {
			inputs = append(inputs, fqn)
			continue
		}
		inputs = append(inputs, fmt.Sprintf("pkg%03d.Task%03d", i, i))
	}

	got, err := store.IntelAnchorIDsByFQNsAndLang(ctx, string(codeanchor.LangGo), inputs)
	require.NoError(t, err)
	require.Len(t, got, len(inputs))
	require.Equal(t, []string{"a000"}, got["acme.pkg000.Task000"])
	require.Equal(t, []string{"a001"}, got["pkg001.Task001"])
	require.Equal(t, []string{"a449"}, got["pkg449.Task449"])
}

func seedSymbolForAnchor(ctx context.Context, store *Store, anchor codeanchor.IntelAnchor) error {
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn, fqn_reversed)
		VALUES (?, ?, ?, '', ?, ?, ?)
	`, string(anchor.Lang), anchor.Kind, anchor.Path, anchor.Symbol, anchor.FQN, codeanchor.ReverseString(anchor.FQN))
	return err
}

func TestCallAnchorsByPaths_ReturnsCalleesWithLimits(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "call-anchors.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	callee1 := codeanchor.IntelAnchor{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep.go",
		Symbol:      "Target",
		FQN:         "pkg.dep.Target",
		Fingerprint: "fp-b1",
	}
	callee2 := codeanchor.IntelAnchor{
		AnchorID:    "b2",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep.go",
		Symbol:      "Target",
		FQN:         "pkg.dep.Target",
		Fingerprint: "fp-b2",
	}
	callee3 := codeanchor.IntelAnchor{
		AnchorID:    "c1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep2.go",
		Symbol:      "Other",
		FQN:         "pkg.dep.Other",
		Fingerprint: "fp-c1",
	}
	caller := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/one.go",
		Symbol:      "Caller",
		FQN:         "pkg.one.Caller",
		Fingerprint: "fp-a1",
	}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/dep.go", []codeanchor.IntelAnchor{callee1, callee2}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/dep2.go", []codeanchor.IntelAnchor{callee3}, nil, nil))

	edges := []codeanchor.IntelEdge{
		{SrcID: "a1", DstID: "b1", Kind: "calls"},
		{SrcID: "a1", DstID: "b2", Kind: "calls"},
		{SrcID: "a1", DstID: "c1", Kind: "calls"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/one.go", []codeanchor.IntelAnchor{caller}, edges, nil))

	var callCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE kind = 'calls'`).Scan(&callCount))
	require.Equal(t, 3, callCount)

	var joinCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls' AND caller.path = 'pkg/one.go'
	`).Scan(&joinCount))
	require.Equal(t, 3, joinCount)

	found, err := store.CallAnchorsByPaths(ctx, []string{"pkg/one.go"}, 10, 1)
	require.NoError(t, err)

	anchors := found["pkg/one.go"]
	require.Len(t, anchors, 2)
	require.ElementsMatch(t, []string{"b1", "c1"}, []string{anchors[0].AnchorID, anchors[1].AnchorID})

	limited, err := store.CallAnchorsByPaths(ctx, []string{"pkg/one.go"}, 1, 2)
	require.NoError(t, err)
	require.Len(t, limited["pkg/one.go"], 1)
}

func TestCallEdgeSuffixSeedsByLangs_ReturnsAuthoritativeAnchorSeeds(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "call-edge-suffix-seeds.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	pyAnchor := codeanchor.IntelAnchor{
		AnchorID:    "py1",
		Lang:        codeanchor.LangPy,
		Kind:        "function",
		Path:        "src/app/callee.py",
		Symbol:      "new_func",
		FQN:         "root.pkg.callee.new_func",
		Fingerprint: "fp-py1",
	}
	goAnchor := codeanchor.IntelAnchor{
		AnchorID:    "go1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/service.go",
		Symbol:      "Run",
		FQN:         "github.com/acme/service.Run",
		Fingerprint: "fp-go1",
	}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, pyAnchor.Path, []codeanchor.IntelAnchor{pyAnchor}, nil, nil))
	require.NoError(t, seedSymbolForAnchor(ctx, store, pyAnchor))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, goAnchor.Path, []codeanchor.IntelAnchor{goAnchor}, nil, nil))
	require.NoError(t, seedSymbolForAnchor(ctx, store, goAnchor))

	seeds, err := store.CallEdgeSuffixSeedsByLangs(ctx, []codeanchor.Lang{codeanchor.LangPy})
	require.NoError(t, err)
	require.Equal(t, []codeanchor.CallEdgeSuffixSeed{{
		Lang:     codeanchor.LangPy,
		FQN:      pyAnchor.FQN,
		AnchorID: pyAnchor.AnchorID,
	}}, seeds)
}

func TestIntelAnchorsByPaths_GroupsByNormalizedPath(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel-anchors-by-paths.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	anchorA := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/one.go",
		Symbol:      "One",
		FQN:         "pkg.one.One",
		Fingerprint: "fp-a1",
	}
	anchorB := codeanchor.IntelAnchor{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/two.go",
		Symbol:      "Two",
		FQN:         "pkg.two.Two",
		Fingerprint: "fp-b1",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/one.go", []codeanchor.IntelAnchor{anchorA}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/two.go", []codeanchor.IntelAnchor{anchorB}, nil, nil))

	found, err := store.IntelAnchorsByPaths(ctx, []string{"./pkg/one.go", "pkg/two.go", "pkg/one.go"})
	require.NoError(t, err)
	require.Len(t, found, 2)
	require.Len(t, found["pkg/one.go"], 1)
	require.Len(t, found["pkg/two.go"], 1)
	require.Equal(t, "a1", found["pkg/one.go"][0].AnchorID)
	require.Equal(t, "b1", found["pkg/two.go"][0].AnchorID)
}

func TestCallAnchorsByCallerIDs_ReturnsCalleesForCaller(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "call-anchors-by-caller.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	callee1 := codeanchor.IntelAnchor{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep.go",
		Symbol:      "Target",
		FQN:         "pkg.dep.Target",
		Fingerprint: "fp-b1",
	}
	callee2 := codeanchor.IntelAnchor{
		AnchorID:    "c1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep2.go",
		Symbol:      "Other",
		FQN:         "pkg.dep.Other",
		Fingerprint: "fp-c1",
	}
	caller1 := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/one.go",
		Symbol:      "Caller",
		FQN:         "pkg.one.Caller",
		Fingerprint: "fp-a1",
	}
	caller2 := codeanchor.IntelAnchor{
		AnchorID:    "a2",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/one.go",
		Symbol:      "OtherCaller",
		FQN:         "pkg.one.OtherCaller",
		Fingerprint: "fp-a2",
	}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/dep.go", []codeanchor.IntelAnchor{callee1}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/dep2.go", []codeanchor.IntelAnchor{callee2}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/one.go", []codeanchor.IntelAnchor{caller1, caller2}, []codeanchor.IntelEdge{
		{SrcID: "a1", DstID: "b1", Kind: "calls"},
		{SrcID: "a2", DstID: "c1", Kind: "calls"},
	}, nil))

	found, err := store.CallAnchorsByCallerIDs(ctx, []string{"a1"}, 10, 2)
	require.NoError(t, err)

	anchors := found["a1"]
	require.Len(t, anchors, 1)
	require.Equal(t, "b1", anchors[0].AnchorID)
}

func TestCallerAnchorsByCalleeIDs_ReturnsCallers(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "caller-anchors.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	callee := codeanchor.IntelAnchor{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep.go",
		Symbol:      "Target",
		FQN:         "pkg.dep.Target",
		Fingerprint: "fp-b1",
	}
	caller := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/one.go",
		Symbol:      "Caller",
		FQN:         "pkg.one.Caller",
		Fingerprint: "fp-a1",
	}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/dep.go", []codeanchor.IntelAnchor{callee}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/one.go", []codeanchor.IntelAnchor{caller}, []codeanchor.IntelEdge{
		{SrcID: "a1", DstID: "b1", Kind: "calls"},
	}, nil))

	found, err := store.CallerAnchorsByCalleeIDs(ctx, []string{"b1"}, 5)
	require.NoError(t, err)
	require.Len(t, found["b1"], 1)
	require.Equal(t, "a1", found["b1"][0].AnchorID)
}

func TestGraphDocCodeEdges_ReturnsAllCodeToCodeEdgeKinds(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "graph-code-edges.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Create anchors in different files
	anchorsA := []codeanchor.IntelAnchor{
		{AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/a.go", Symbol: "FuncA", FQN: "pkg.a.FuncA", Fingerprint: "fp1"},
		{AnchorID: "a2", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/a.go", Symbol: "FuncA2", FQN: "pkg.a.FuncA2", Fingerprint: "fp1b"},
	}
	anchorsB := []codeanchor.IntelAnchor{
		{AnchorID: "b1", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/b.go", Symbol: "FuncB", FQN: "pkg.b.FuncB", Fingerprint: "fp2"},
		{AnchorID: "b2", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/b.go", Symbol: "FuncB2", FQN: "pkg.b.FuncB2", Fingerprint: "fp2b"},
	}
	anchorsC := []codeanchor.IntelAnchor{
		{AnchorID: "c1", Lang: codeanchor.LangPy, Kind: "module", Path: "lib/c.py", Symbol: "c", FQN: "lib.c", Fingerprint: "fp3"},
	}
	anchorsTest := []codeanchor.IntelAnchor{
		{AnchorID: "t1", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/a_test.go", Symbol: "TestFuncA", FQN: "pkg.a.TestFuncA", Fingerprint: "fp4"},
	}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/a.go", anchorsA, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/b.go", anchorsB, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "lib/c.py", anchorsC, nil, nil))

	// Create edges of different kinds: calls, imports, tests
	// Also add defines and mentions edges that should be excluded
	edgesTest := []codeanchor.IntelEdge{
		{SrcID: "t1", DstID: "a1", Kind: "tests"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/a_test.go", anchorsTest, edgesTest, nil))

	// Add calls edges: b1 -> a1 and b2 -> a2 (two functions in b.go calling functions in a.go)
	// This should result in aggregated weight of 2 at the file level
	edgesB := []codeanchor.IntelEdge{
		{SrcID: "b1", DstID: "a1", Kind: "calls"},
		{SrcID: "b2", DstID: "a2", Kind: "calls"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/b.go", anchorsB, edgesB, nil))

	// Add imports edge: c1 -> a1 (simulating Python import)
	edgesC := []codeanchor.IntelEdge{
		{SrcID: "c1", DstID: "a1", Kind: "imports"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "lib/c.py", anchorsC, edgesC, nil))

	// Add defines edge (should be excluded) - need another anchor that defines something
	anchorsD := []codeanchor.IntelAnchor{
		{AnchorID: "d1", Lang: codeanchor.LangGo, Kind: "struct", Path: "pkg/d.go", Symbol: "MyStruct", FQN: "pkg.d.MyStruct", Fingerprint: "fp5"},
		{AnchorID: "d2", Lang: codeanchor.LangGo, Kind: "method", Path: "pkg/d.go", Symbol: "Method", FQN: "pkg.d.MyStruct.Method", Fingerprint: "fp6"},
	}
	edgesD := []codeanchor.IntelEdge{
		{SrcID: "d1", DstID: "d2", Kind: "defines"}, // struct defines method - should be excluded
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/d.go", anchorsD, edgesD, nil))

	// Get all code-to-code edges
	edges, err := store.GraphDocCodeEdges(ctx)
	require.NoError(t, err)

	// Verify we get the right edges
	// Should have: calls (b->a), imports (c->a), tests (test->a)
	// Should NOT have: defines (d->d)
	require.Len(t, edges, 3)

	// Build a map for easier verification
	edgeMap := make(map[string]GraphDocEdge)
	for _, e := range edges {
		key := e.SrcPath + "->" + e.DstPath + ":" + e.Kind
		edgeMap[key] = e
	}

	// Verify calls edge (should have weight 2 from duplicate edges)
	callsEdge, ok := edgeMap["pkg/b.go->pkg/a.go:calls"]
	require.True(t, ok, "expected calls edge from pkg/b.go to pkg/a.go")
	require.Equal(t, 2, callsEdge.Weight, "expected aggregated weight of 2 for duplicate calls edges")

	// Verify imports edge
	_, ok = edgeMap["lib/c.py->pkg/a.go:imports"]
	require.True(t, ok, "expected imports edge from lib/c.py to pkg/a.go")

	// Verify tests edge
	_, ok = edgeMap["pkg/a_test.go->pkg/a.go:tests"]
	require.True(t, ok, "expected tests edge from pkg/a_test.go to pkg/a.go")

	// Verify defines edge is NOT present
	_, ok = edgeMap["pkg/d.go->pkg/d.go:defines"]
	require.False(t, ok, "defines edge should be excluded")
}

func TestModuleAnchorIDsByModuleSuffix(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "suffix.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Create module anchors with different paths.
	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "cache-id", Lang: codeanchor.LangPy, Kind: "module", Path: "backend/src/app/core/cache.py", Symbol: "cache.py", Fingerprint: "fp1"},
		{AnchorID: "events-id", Lang: codeanchor.LangPy, Kind: "module", Path: "backend/src/app/core/events.py", Symbol: "events.py", Fingerprint: "fp2"},
		{AnchorID: "service-id", Lang: codeanchor.LangPy, Kind: "module", Path: "backend/src/app/domain/service.py", Symbol: "service.py", Fingerprint: "fp3"},
		{AnchorID: "init-id", Lang: codeanchor.LangPy, Kind: "module", Path: "backend/src/app/utils/__init__.py", Symbol: "__init__.py", Fingerprint: "fp4"},
	}
	for _, a := range anchors {
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, a.Path, []codeanchor.IntelAnchor{a}, nil, nil))
	}

	// Test suffix matching.
	tests := []struct {
		modules  []string
		expected map[string]string
	}{
		{
			modules:  []string{"app.core.cache"},
			expected: map[string]string{"app.core.cache": "cache-id"},
		},
		{
			modules:  []string{"app.core.cache", "app.core.events"},
			expected: map[string]string{"app.core.cache": "cache-id", "app.core.events": "events-id"},
		},
		{
			modules:  []string{"app.domain.service"},
			expected: map[string]string{"app.domain.service": "service-id"},
		},
		{
			// Package module (__init__.py).
			modules:  []string{"app.utils"},
			expected: map[string]string{"app.utils": "init-id"},
		},
		{
			// Non-existent module.
			modules:  []string{"app.nonexistent"},
			expected: map[string]string{},
		},
		{
			// Mix of existing and non-existing.
			modules:  []string{"app.core.cache", "app.nonexistent"},
			expected: map[string]string{"app.core.cache": "cache-id"},
		},
	}

	for _, tt := range tests {
		result, err := store.ModuleAnchorIDsByModuleSuffix(ctx, tt.modules)
		require.NoError(t, err, "modules=%v", tt.modules)
		require.Equal(t, tt.expected, result, "modules=%v", tt.modules)
	}
}

func TestModuleAnchorIDsByModuleSuffix_EdgeCases(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "suffix-edge.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Create anchors with tricky paths.
	anchors := []codeanchor.IntelAnchor{
		// Deep nested path.
		{AnchorID: "deep-id", Lang: codeanchor.LangPy, Kind: "module", Path: "a/b/c/d/e/f/g.py", Symbol: "g.py", Fingerprint: "fp1"},
		// Path that could be confused with another (partial match).
		{AnchorID: "cache-id", Lang: codeanchor.LangPy, Kind: "module", Path: "src/cache.py", Symbol: "cache.py", Fingerprint: "fp2"},
		{AnchorID: "redis-cache-id", Lang: codeanchor.LangPy, Kind: "module", Path: "src/redis/cache.py", Symbol: "cache.py", Fingerprint: "fp3"},
	}
	for _, a := range anchors {
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, a.Path, []codeanchor.IntelAnchor{a}, nil, nil))
	}

	// Deep path should resolve correctly.
	result, err := store.ModuleAnchorIDsByModuleSuffix(ctx, []string{"d.e.f.g"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"d.e.f.g": "deep-id"}, result)

	// Partial module name should match the right file.
	result, err = store.ModuleAnchorIDsByModuleSuffix(ctx, []string{"cache"})
	require.NoError(t, err)
	// Should match "src/cache.py" not "src/redis/cache.py" (first match wins).
	require.Len(t, result, 1)
	require.Contains(t, result, "cache")

	// Full path should match the nested one.
	result, err = store.ModuleAnchorIDsByModuleSuffix(ctx, []string{"redis.cache"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"redis.cache": "redis-cache-id"}, result)
}

func TestUpsertIntelCallEdgesForPath_DeletesTypeRefEdges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "type_ref_delete.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Create anchors in different files.
	typeAnchor := codeanchor.IntelAnchor{
		AnchorID:    "t1",
		Lang:        codeanchor.LangTS,
		Kind:        "interface",
		Path:        "models/user.ts",
		Symbol:      "User",
		FQN:         "models.user.User",
		Fingerprint: "fp-t1",
	}
	funcAnchor := codeanchor.IntelAnchor{
		AnchorID:    "f1",
		Lang:        codeanchor.LangTS,
		Kind:        "function",
		Path:        "services/user.ts",
		Symbol:      "getUser",
		FQN:         "services.user.getUser",
		Fingerprint: "fp-f1",
	}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "models/user.ts", []codeanchor.IntelAnchor{typeAnchor}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "services/user.ts", []codeanchor.IntelAnchor{funcAnchor}, nil, nil))

	// Add type_ref edge from function to type.
	edges := []codeanchor.IntelEdge{
		{SrcID: "f1", DstID: "t1", Kind: "type_ref"},
	}
	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, "services/user.ts", edges))

	// Verify edge exists.
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE kind = 'type_ref'`).Scan(&count))
	require.Equal(t, 1, count, "expected 1 type_ref edge")

	// Now upsert with no edges - the old type_ref edge should be deleted.
	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, "services/user.ts", nil))

	// Verify edge is deleted.
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE kind = 'type_ref'`).Scan(&count))
	require.Equal(t, 0, count, "expected type_ref edge to be deleted on re-upsert")
}

func TestCallEdgesStaleLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "call_edges_stale.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{
		Path: "src/todo.py",
		Lang: codeanchor.LangPy,
		Hash: "hash-a1",
	}))

	stale, err := store.StaleCallEdgePaths(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, stale)

	winPath := "src\\todo.py"
	require.NoError(t, store.MarkCallEdgesStale(ctx, []string{winPath}))
	stale, err = store.StaleCallEdgePaths(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"src/todo.py"}, stale)

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, winPath, nil))
	stale, err = store.StaleCallEdgePaths(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, stale)
}
