package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestApplyOwnershipTransitions_CodeToNoteClearsCodeAndPublishesSource(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const path = "docs/release.HTML"
	seedOwnershipCodeAndNote(t, ctx, store, path)

	result, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path:   path,
		Target: OwnershipTargetNote,
		Note: &NoteSourceState{
			Title:             "Release",
			FormatID:          "html",
			ContentHash:       "html-source-hash",
			Mtime:             12,
			Size:              34,
			ProviderVersion:   "html-v1",
			ProjectionVersion: "root-v1",
			Status:            NoteProjectionStatusFatal,
			DiagnosticCode:    "invalid_utf8",
			DiagnosticDetail:  "unsupported source encoding",
			ObservedAt:        56,
		},
	}})
	require.NoError(t, err)
	// The fixture carries inbound note and anchor doc-links owned by other code
	// paths; retiring and rebuilding this path clears that source-owned evidence.
	require.Equal(t, []string{"src/anchor-linker.go", "src/other.go"}, result.AffectedSourcePaths)
	require.Empty(t, result.AffectedAnchorIDs)

	requireOwnershipCounts(t, ctx, store, path, ownershipCounts{
		Notes: 1, ProjectionState: 1,
	})
	var formatID, providerVersion, projectionVersion, status, diagnostic string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT n.format_id, p.provider_version, p.projection_version, p.status, p.diagnostic_code
		FROM notes n JOIN note_projection_state p ON p.note_id = n.id
		WHERE n.path = ?
	`, path).Scan(&formatID, &providerVersion, &projectionVersion, &status, &diagnostic))
	require.Equal(t, "html", formatID)
	require.Equal(t, "html-v1", providerVersion)
	require.Equal(t, "root-v1", projectionVersion)
	require.Equal(t, string(NoteProjectionStatusFatal), status)
	require.Equal(t, "invalid_utf8", diagnostic)
}

func TestApplyOwnershipTransitions_NoteToCodeAndUnownedClearNoteDomain(t *testing.T) {
	ctx := context.Background()
	for _, target := range []OwnershipTarget{OwnershipTargetCode, OwnershipTargetUnowned} {
		t.Run(string(target), func(t *testing.T) {
			store := newOwnershipTransitionStore(t)
			const path = "notes/transition.md"
			seedOwnershipCodeAndNote(t, ctx, store, path)

			_, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{Path: path, Target: target}})
			require.NoError(t, err)
			requireOwnershipCounts(t, ctx, store, path, ownershipCounts{})
		})
	}
}

func TestApplyOwnershipTransitions_NoteRetirementCleansUnclaimedAnchorsAndPreservesSharedAnchors(t *testing.T) {
	ctx := context.Background()
	for _, target := range []OwnershipTarget{OwnershipTargetCode, OwnershipTargetUnowned} {
		t.Run(string(target), func(t *testing.T) {
			store := newOwnershipTransitionStore(t)
			const path = "notes/anchor-owner.md"
			seedOwnershipCodeAndNote(t, ctx, store, path)
			orphanID, sharedID := seedOwnershipLegacyAnchors(t, ctx, store, path)

			result, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{Path: path, Target: target}})
			require.NoError(t, err)
			require.Equal(t, []int64{orphanID, sharedID}, result.AffectedAnchorIDs)

			requireOwnershipAnchorExists(t, ctx, store, orphanID, false)
			requireOwnershipAnchorExists(t, ctx, store, sharedID, true)
			var orphanScopes, sharedScopes, sharedClaims int
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM anchor_scopes WHERE anchor_id = ?`, orphanID).Scan(&orphanScopes))
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM anchor_scopes WHERE anchor_id = ?`, sharedID).Scan(&sharedScopes))
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM note_anchors WHERE anchor_id = ?`, sharedID).Scan(&sharedClaims))
			require.Zero(t, orphanScopes)
			require.Equal(t, 1, sharedScopes)
			require.Equal(t, 1, sharedClaims)
		})
	}
}

func TestApplyOwnershipTransitions_InvalidatesGlobalNoteMetadataFreshness(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const path = "notes/freshness.md"
	seedOwnershipCodeAndNote(t, ctx, store, path)

	before, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.True(t, before.Ready)
	require.NotEmpty(t, before.NotesHash)

	_, err = store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{Path: path, Target: OwnershipTargetUnowned}})
	require.NoError(t, err)

	after, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.False(t, after.Ready)
	require.Empty(t, after.NotesHash)
	require.Empty(t, after.RawNotesHash)
	require.Zero(t, after.LoadedAt)
	rows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestApplyOwnershipTransitions_RenamePairIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const oldPath = "notes/old.md"
	const newPath = "docs/new.htm"
	seedOwnershipCodeAndNote(t, ctx, store, oldPath)

	_, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{
		{Path: oldPath, Target: OwnershipTargetUnowned},
		{Path: newPath, Target: OwnershipTargetNote, Note: &NoteSourceState{
			FormatID: "html", ContentHash: "new", ProviderVersion: "v1", ProjectionVersion: "p1",
			Status: NoteProjectionStatusCurrent, ObservedAt: 1,
		}},
	})
	require.NoError(t, err)
	requireOwnershipCounts(t, ctx, store, oldPath, ownershipCounts{})
	requireOwnershipCounts(t, ctx, store, newPath, ownershipCounts{Notes: 1, ProjectionState: 1})
}

func TestApplyOwnershipTransitions_RejectsInvalidBatchWithoutWrites(t *testing.T) {
	ctx := context.Background()
	const path = "notes/source.md"
	validNote := func(status NoteProjectionStatus, code, detail string) *NoteSourceState {
		return &NoteSourceState{
			FormatID: "html", ContentHash: "source", ProviderVersion: "html-v1", ProjectionVersion: "root-v1",
			Status: status, DiagnosticCode: code, DiagnosticDetail: detail, ObservedAt: 1,
		}
	}
	for name, transitions := range map[string][]OwnershipTransition{
		"duplicate normalized path": {
			{Path: path, Target: OwnershipTargetCode},
			{Path: "./" + path, Target: OwnershipTargetUnowned},
		},
		"windows drive path":                {{Path: `C:\outside\source.html`, Target: OwnershipTargetCode}},
		"fatal without blocking diagnostic": {{Path: path, Target: OwnershipTargetNote, Note: validNote(NoteProjectionStatusFatal, "", "")}},
		"current retaining diagnostic":      {{Path: path, Target: OwnershipTargetNote, Note: validNote(NoteProjectionStatusCurrent, "parse_error", "must not persist")}},
		"stale retaining diagnostic":        {{Path: path, Target: OwnershipTargetNote, Note: validNote(NoteProjectionStatusStale, "parse_error", "must not persist")}},
		"stale without source hash": {{Path: path, Target: OwnershipTargetNote, Note: &NoteSourceState{
			FormatID: "html", ProviderVersion: "html-v1", ProjectionVersion: "root-v1", Status: NoteProjectionStatusStale, ObservedAt: 1,
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			store := newOwnershipTransitionStore(t)
			seedOwnershipCodeAndNote(t, ctx, store, path)

			_, err := store.ApplyOwnershipTransitions(ctx, transitions)
			require.Error(t, err)
			requireOwnershipCounts(t, ctx, store, path, seededOwnershipCounts())
		})
	}
}

func TestApplyOwnershipTransitions_AllowsStaleSourceWithoutDiagnostic(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)

	_, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "./notes//discovered.html", Target: OwnershipTargetNote,
		Note: &NoteSourceState{
			FormatID: "html", ContentHash: "source", ProviderVersion: "html-v1", ProjectionVersion: "root-v1",
			Status: NoteProjectionStatusStale, ObservedAt: 1,
		},
	}})
	require.NoError(t, err)
	requireOwnershipCounts(t, ctx, store, "notes/discovered.html", ownershipCounts{Notes: 1, ProjectionState: 1})
}

func TestApplyOwnershipTransitions_PersistsFatalUnreadableSourceWithoutContentHash(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const path = "notes/unreadable.html"

	_, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: path, Target: OwnershipTargetNote,
		Note: &NoteSourceState{
			FormatID: "html", ProviderVersion: "html-v1", ProjectionVersion: "root-v1",
			Status: NoteProjectionStatusFatal, DiagnosticCode: "unreadable_source", DiagnosticDetail: "permission denied", ObservedAt: 1,
		},
	}})
	require.NoError(t, err)
	var contentHash, sourceHash, status string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT n.content_hash, p.source_content_hash, p.status
		FROM notes n JOIN note_projection_state p ON p.note_id = n.id
		WHERE n.path = ?
	`, path).Scan(&contentHash, &sourceHash, &status))
	require.Empty(t, contentHash)
	require.Empty(t, sourceHash)
	require.Equal(t, string(NoteProjectionStatusFatal), status)
}

func TestApplyOwnershipTransitions_ReportsEveryAffectedSourceWithoutDeletingIt(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const targetPath = "notes/target.md"
	seedOwnershipCodeAndNote(t, ctx, store, targetPath)
	for _, dependency := range []struct {
		source string
		field  string
	}{
		{source: "notes/z-source.md", field: "related"},
		{source: "notes/a-source.md", field: "references"},
		{source: "notes/a-source.md", field: "mentions"},
		{source: targetPath, field: "self-reference"},
	} {
		_, err := store.db.ExecContext(ctx, `
			INSERT INTO ontology_node_field_value_dependencies(
				source_note_path, field_name, target_input_norm, resolved_target_note_path, updated_at
			) VALUES (?, ?, ?, ?, 1)
		`, dependency.source, dependency.field, dependency.field, targetPath)
		require.NoError(t, err)
	}
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/graph-source.md", GraphDocEdgeKindWikilink, []string{targetPath}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/z-source.md", GraphDocEdgeKindWikilink, []string{targetPath}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "src/a-code.go", []codeanchor.DocLink{{
		SrcType: "code", SrcPath: "src/a-code.go", DstKind: "note", DstPath: targetPath,
	}}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "notes/a-source.md", []codeanchor.DocLink{{
		SrcType: "note", SrcPath: "notes/a-source.md", DstKind: "note", DstPath: targetPath,
	}}))

	result, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{Path: targetPath, Target: OwnershipTargetUnowned}})
	require.NoError(t, err)
	require.Equal(t, []string{
		"notes/a-source.md",
		"notes/graph-source.md",
		"notes/z-source.md",
		"src/a-code.go",
		"src/anchor-linker.go",
		"src/other.go",
	}, result.AffectedSourcePaths)
	var remaining int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM ontology_node_field_value_dependencies
		WHERE resolved_target_note_path = ?
	`, targetPath).Scan(&remaining))
	require.Equal(t, 3, remaining)
}

func TestApplyOwnershipTransitions_RollsBackWhenPublishingSourceFails(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const path = "notes/rollback.md"
	seedOwnershipCodeAndNote(t, ctx, store, path)
	require.NoError(t, createOwnershipAbortTrigger(ctx, store, "before_projection_insert", `
		CREATE TRIGGER before_projection_insert
		BEFORE INSERT ON note_projection_state
		BEGIN SELECT RAISE(ABORT, 'injected projection failure'); END;
	`))

	_, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: path, Target: OwnershipTargetNote, Note: &NoteSourceState{
			FormatID: "markdown", ContentHash: "new", ProviderVersion: "v1", ProjectionVersion: "p1",
			Status: NoteProjectionStatusCurrent, ObservedAt: 2,
		},
	}})
	require.ErrorContains(t, err, "injected projection failure")
	requireOwnershipCounts(t, ctx, store, path, seededOwnershipCounts())
}

func TestApplyOwnershipTransitions_RollsBackWhenCodeCleanupFails(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const path = "notes/cleanup.md"
	seedOwnershipCodeAndNote(t, ctx, store, path)
	require.NoError(t, createOwnershipAbortTrigger(ctx, store, "before_file_delete", `
		CREATE TRIGGER before_file_delete
		BEFORE DELETE ON files
		BEGIN SELECT RAISE(ABORT, 'injected code cleanup failure'); END;
	`))

	_, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{Path: path, Target: OwnershipTargetUnowned}})
	require.ErrorContains(t, err, "injected code cleanup failure")
	requireOwnershipCounts(t, ctx, store, path, seededOwnershipCounts())
}

func newOwnershipTransitionStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(currentSchemaTestDBPath(t, "ownership-transition.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedOwnershipCodeAndNote(t *testing.T, ctx context.Context, store *Store, path string) {
	t.Helper()
	identifier := strings.NewReplacer("/", "_", ".", "_").Replace(path)
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes", RawNotesHash: "raw", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{{
			Path: path, Title: "Old", ContentHash: "old-note", Mtime: 1, Size: 2, FormatID: "markdown",
			Projection: NoteProjectionState{ProviderVersion: "md-v1", ProjectionVersion: "root-v1", SourceContentHash: "old-note", Status: NoteProjectionStatusCurrent, UpdatedAt: 1},
		}},
		PropertyValues: []NotePropertyValueRow{{NotePath: path, PropertyName: "kind", Source: NotePropertySourceFrontmatter, ValueText: "old", ValueNorm: "old", ValueKind: NotePropertyValueString}},
		Tags:           []NoteTagRow{{NotePath: path, TagNorm: "old"}},
		WikilinkEdges:  []GraphDocEdgeRow{{SrcPath: path, DstPath: "notes/other.md", Kind: GraphDocEdgeKindWikilink}},
	}))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, path, []codeanchor.IntelDocSection{{
		SectionID: "section:" + path, Path: path, Title: "Old", Level: 1, Content: "old", Fingerprint: "old",
	}}, nil, nil))
	node := codeanchor.IntelOntologyNode{NodeID: "node:" + path, NotePath: path, NodeRefJSON: fmt.Sprintf(`{"notePath":%q}`, path), NodeKind: "NOTE", TypeName: "Old", SourceLocator: path, UpdatedAt: 1}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{NotePaths: []string{path}, Nodes: []codeanchor.IntelOntologyNode{node}}))
	require.NoError(t, store.ApplyOntologyDelta(ctx, OntologyDelta{EdgeSources: []string{path}, Edges: []OntologyEdgeRow{{SrcPath: path, SrcNodeID: node.NodeID, RelationName: "related", DstPath: "notes/other.md", UpdatedAt: 1}}}))
	anchorID := "anchor:" + path
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{{AnchorID: anchorID, Lang: codeanchor.LangGo, Kind: "func", Path: path, Symbol: "Old", FQN: "old." + identifier, Fingerprint: "old"}}, nil, nil))
	const otherCodePath = "src/ownership-external.go"
	otherAnchorID := "anchor:ownership-external:" + identifier
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, otherCodePath, []codeanchor.IntelAnchor{{
		AnchorID: otherAnchorID, Lang: codeanchor.LangGo, Kind: "func", Path: otherCodePath, Symbol: "External", FQN: "external." + identifier, Fingerprint: "external",
	}}, []codeanchor.IntelEdge{{SrcID: otherAnchorID, DstID: anchorID, Kind: "calls"}}, nil))
	_, err := store.db.ExecContext(ctx, `INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime) VALUES (?, 'go', 'old', 'v1', 'ok', 1)`, path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO symbols(lang, kind, file, pkg, name, fqn) VALUES ('go', 'func', ?, 'old', 'Old', ?)`, path, "old."+identifier)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, path, []codeanchor.DocLink{{SrcType: "code", SrcPath: path, DstKind: "note", DstPath: "notes/other.md"}}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "src/other.go", []codeanchor.DocLink{{SrcType: "code", SrcPath: "src/other.go", DstKind: "note", DstPath: path}}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "src/anchor-linker.go", []codeanchor.DocLink{{SrcType: "code", SrcPath: "src/anchor-linker.go", DstKind: "anchor", DstID: anchorID}}))
	_, err = store.db.ExecContext(ctx, `INSERT INTO graph_doc_scores(doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at) VALUES (?, 'note', 1, 1, 'ownership', 1, 1, 1)`, path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO graph_anchor_scores(anchor_id, pagerank, updated_at) VALUES (?, 1, 1)`, anchorID)
	require.NoError(t, err)

	var nodeRowID, otherAnchorRowID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM ontology_nodes WHERE node_id = ?`, node.NodeID).Scan(&nodeRowID))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM intel_code_anchors WHERE anchor_id = ?`, otherAnchorID).Scan(&otherAnchorRowID))
	chunkID := "ontology-chunk:" + path
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_chunks(chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity, content_hash, start_byte, end_byte, updated_at)
		VALUES (?, ?, ?, 'ontology_node', 'default', 0, 'node', 'ownership', 0, 0, 1)
	`, chunkID, node.NodeID, nodeRowID)
	require.NoError(t, err)
	var chunkRowID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM intel_chunks WHERE chunk_id = ?`, chunkID).Scan(&chunkRowID))
	_, err = store.db.ExecContext(ctx, `INSERT INTO intel_embeddings(chunk_row_id, chunk_id, norm, dimensions, created_at) VALUES (?, ?, 1, 1, 1)`, chunkRowID, chunkID)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO ontology_node_embedding_state(
			chunk_id, node_id, note_path, type_name, node_kind,
			embedding_schema_signature, node_structure_fingerprint, source_content_hash,
			chunk_text_hash, chunk_granularity, provider, model, updated_at
		) VALUES (?, ?, ?, 'Old', 'NOTE', 'schema', 'structure', 'source', 'text', 'node', 'provider', 'model', 1)
	`, chunkID, node.NodeID, path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `PRAGMA ignore_check_constraints = ON`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_edges(src_type, src_row_id, dst_type, dst_row_id, kind, meta_json)
		VALUES ('ontology_node', ?, 'anchor', ?, 'ontology_link', NULL), ('anchor', ?, 'ontology_node', ?, 'ontology_link_back', NULL)
	`, nodeRowID, otherAnchorRowID, otherAnchorRowID, nodeRowID)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `PRAGMA ignore_check_constraints = OFF`)
	require.NoError(t, err)
}

func seedOwnershipLegacyAnchors(t *testing.T, ctx context.Context, store *Store, ownerPath string) (orphanID, sharedID int64) {
	t.Helper()
	const sharedNotePath = "notes/shared-anchor-owner.md"
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO notes(path, title, content_hash, indexer_version, mtime, size, indexed_at, note_key_full, note_key_base, path_len, first_segment_norm, format_id)
		VALUES (?, 'Shared anchor owner', 'shared', '', 1, 1, 1, 'notes/shared-anchor-owner', 'shared-anchor-owner', 28, 'notes', 'markdown')
	`, sharedNotePath)
	require.NoError(t, err)

	for _, label := range []string{"orphan-transition-anchor", "shared-transition-anchor"} {
		_, err := store.db.ExecContext(ctx, `INSERT INTO anchors(label, kind, base_member) VALUES (?, 'function', 0)`, label)
		require.NoError(t, err)
	}
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = 'orphan-transition-anchor'`).Scan(&orphanID))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = 'shared-transition-anchor'`).Scan(&sharedID))

	var ownerNoteID, sharedNoteID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM notes WHERE path = ?`, ownerPath).Scan(&ownerNoteID))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM notes WHERE path = ?`, sharedNotePath).Scan(&sharedNoteID))
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO note_anchors(note_id, anchor_id) VALUES (?, ?), (?, ?), (?, ?);
		INSERT INTO anchor_scopes(anchor_id, call_file) VALUES (?, 'pkg/orphan.go'), (?, 'pkg/shared.go')
	`, ownerNoteID, orphanID, ownerNoteID, sharedID, sharedNoteID, sharedID, orphanID, sharedID)
	require.NoError(t, err)
	return orphanID, sharedID
}

func requireOwnershipAnchorExists(t *testing.T, ctx context.Context, store *Store, anchorID int64, want bool) {
	t.Helper()
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM anchors WHERE id = ?`, anchorID).Scan(&count))
	if want {
		require.Equal(t, 1, count)
		return
	}
	require.Zero(t, count)
}

type ownershipCounts struct {
	Notes, ProjectionState, Files, Symbols, CodeAnchors, DocSections, DocLinks, InboundAnchorDocLinks, GraphEdges, GraphDocScores, GraphAnchorScores, OntologyNodes, OntologyChunks, OntologyEmbeddings, OntologyEmbeddingStates, OntologyIntelEdges int
}

func seededOwnershipCounts() ownershipCounts {
	return ownershipCounts{
		Notes: 1, ProjectionState: 1, Files: 1, Symbols: 1, CodeAnchors: 1,
		DocSections: 1, DocLinks: 2, InboundAnchorDocLinks: 1, GraphEdges: 1, GraphDocScores: 1, GraphAnchorScores: 1,
		OntologyNodes: 1, OntologyChunks: 1, OntologyEmbeddings: 1, OntologyEmbeddingStates: 1, OntologyIntelEdges: 2,
	}
}

func requireOwnershipCounts(t *testing.T, ctx context.Context, store *Store, path string, want ownershipCounts) {
	t.Helper()
	queries := map[string]struct {
		query string
		want  int
	}{
		"notes":                     {`SELECT COUNT(*) FROM notes WHERE path = ?`, want.Notes},
		"projection":                {`SELECT COUNT(*) FROM note_projection_state p JOIN notes n ON n.id = p.note_id WHERE n.path = ?`, want.ProjectionState},
		"files":                     {`SELECT COUNT(*) FROM files WHERE path = ?`, want.Files},
		"symbols":                   {`SELECT COUNT(*) FROM symbols WHERE file = ?`, want.Symbols},
		"code anchors":              {`SELECT COUNT(*) FROM intel_code_anchors WHERE path = ?`, want.CodeAnchors},
		"doc sections":              {`SELECT COUNT(*) FROM intel_doc_sections WHERE path = ?`, want.DocSections},
		"doc links":                 {`SELECT COUNT(*) FROM doc_links WHERE src_path = ? OR (dst_kind = 'note' AND dst_path = ?)`, want.DocLinks},
		"inbound anchor doc links":  {`SELECT COUNT(*) FROM doc_links WHERE dst_kind = 'anchor' AND dst_id = 'anchor:' || ?`, want.InboundAnchorDocLinks},
		"graph edges":               {`SELECT COUNT(*) FROM graph_doc_edges WHERE src_path = ? OR dst_path = ?`, want.GraphEdges},
		"graph doc scores":          {`SELECT COUNT(*) FROM graph_doc_scores WHERE doc_path = ?`, want.GraphDocScores},
		"graph anchor scores":       {`SELECT COUNT(*) FROM graph_anchor_scores WHERE anchor_id = 'anchor:' || ?`, want.GraphAnchorScores},
		"ontology nodes":            {`SELECT COUNT(*) FROM ontology_nodes WHERE note_path = ?`, want.OntologyNodes},
		"ontology chunks":           {`SELECT COUNT(*) FROM intel_chunks WHERE owner_type = 'ontology_node' AND owner_id = 'node:' || ?`, want.OntologyChunks},
		"ontology embeddings":       {`SELECT COUNT(*) FROM intel_embeddings WHERE chunk_id = 'ontology-chunk:' || ?`, want.OntologyEmbeddings},
		"ontology embedding states": {`SELECT COUNT(*) FROM ontology_node_embedding_state WHERE node_id = 'node:' || ?`, want.OntologyEmbeddingStates},
		"ontology intel edges":      {`SELECT COUNT(*) FROM intel_edges WHERE kind IN ('ontology_link', 'ontology_link_back')`, want.OntologyIntelEdges},
	}
	for name, check := range queries {
		var got int
		args := make([]any, strings.Count(check.query, "?"))
		for i := range args {
			args[i] = path
		}
		require.NoError(t, store.db.QueryRowContext(ctx, check.query, args...).Scan(&got), name)
		require.Equal(t, check.want, got, name)
	}
}

func createOwnershipAbortTrigger(ctx context.Context, store *Store, name, statement string) error {
	_, err := store.db.ExecContext(ctx, statement)
	return err
}
