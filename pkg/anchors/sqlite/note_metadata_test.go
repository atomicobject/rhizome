package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestReplaceNoteMetadataSnapshotAndQueryHelpers(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/project.md", Title: "Project", ContentHash: "a", Mtime: 10, Size: 100},
			{Path: "notes/decision.md", Title: "Decision", ContentHash: "b", Mtime: 11, Size: 120},
		},
		PropertyValues: []NotePropertyValueRow{
			{NotePath: "notes/project.md", PropertyName: "type", Source: NotePropertySourceFrontmatter, ValueText: "Project", ValueNorm: "project", ValueKind: NotePropertyValueString},
			{NotePath: "notes/project.md", PropertyName: "status", Source: NotePropertySourceFrontmatter, ValueText: "active", ValueNorm: "active", ValueKind: NotePropertyValueString},
			{NotePath: "notes/decision.md", PropertyName: "type", Source: NotePropertySourceFrontmatter, ValueText: "Decision", ValueNorm: "decision", ValueKind: NotePropertyValueString},
		},
		Tags: []NoteTagRow{
			{NotePath: "notes/project.md", TagNorm: "topic/project"},
			{NotePath: "notes/decision.md", TagNorm: "topic/project/decision"},
		},
		WikilinkEdges: []GraphDocEdgeRow{
			{SrcPath: "notes/project.md", DstPath: "notes/decision.md", Kind: "wikilink"},
			{SrcPath: "notes/project.md", DstPath: "notes/decision.md", Kind: "mdlink"},
			{SrcPath: "notes/project.md", DstPath: "notes/decision.md", Kind: NoteLinkKind("mdlink", "heading")},
		},
	})
	require.NoError(t, err)

	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.True(t, state.Ready)
	require.Equal(t, int64(42), state.LoadedAt)

	rows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	projects, err := store.CurrentNotePathsByPropertyValue(ctx, "type", "project", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/project.md"}, projects)

	tagged, err := store.CurrentNotePathsByTag(ctx, "topic/project")
	require.NoError(t, err)
	require.Equal(t, []string{"notes/decision.md", "notes/project.md"}, tagged)

	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "notes/project.md", edges[0].SrcPath)
	require.Equal(t, "notes/decision.md", edges[0].DstPath)

	detailedEdges, err := store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	require.Len(t, detailedEdges, 3)

	byKind := make(map[string]GraphDocEdge, len(detailedEdges))
	for _, edge := range detailedEdges {
		byKind[edge.Kind] = edge
	}

	wiki := byKind["wikilink"]
	require.Equal(t, EdgeConfidenceExtracted, wiki.Confidence)
	require.InDelta(t, 1.0, wiki.ConfidenceScore, 0.0001)

	md := byKind["mdlink"]
	require.Equal(t, EdgeConfidenceExtracted, md.Confidence)
	require.InDelta(t, 0.9, md.ConfidenceScore, 0.0001)

	noteLink := byKind[NoteLinkKind("mdlink", "heading")]
	require.Equal(t, EdgeConfidenceExtracted, noteLink.Confidence)
	require.InDelta(t, 0.9, noteLink.ConfidenceScore, 0.0001)
}

func TestNoteFactQueriesShareReadTransactionSnapshot(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "metadata.db")
	reader, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	writer, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, writer.Close()) })

	snapshot := func(title, value, tag string) NoteMetadataSnapshot {
		return NoteMetadataSnapshot{
			State: NoteMetadataState{NotesHash: title, LoadedAt: 1, Ready: true},
			Notes: []NoteMetadataRow{{
				Path: "notes/example.md", Title: title, ContentHash: title, Mtime: 1, Size: 1,
			}},
			PropertyValues: []NotePropertyValueRow{{
				NotePath: "notes/example.md", PropertyName: "summary", Source: NotePropertySourceFrontmatter,
				ValueText: value, ValueNorm: value, ValueKind: NotePropertyValueString,
			}},
			Tags: []NoteTagRow{{NotePath: "notes/example.md", TagNorm: tag}},
		}
	}
	require.NoError(t, writer.ReplaceNoteMetadataSnapshot(ctx, snapshot("generation-a", "a", "a")))
	reader.db.SetMaxOpenConns(1)
	conn, err := reader.db.Conn(ctx)
	require.NoError(t, err)
	intercepted := false
	var writeErr error
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, name, _ string, database string) int {
			if op == sqlite3.SQLITE_READ && name == "note_property_values" && database == "main" && !intercepted {
				intercepted = true
				writeErr = writer.ReplaceNoteMetadataSnapshot(ctx, snapshot("generation-b", "b", "b"))
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())

	prior, err := reader.DurableNoteFactsByPaths(ctx, []string{"notes/example.md"})
	require.NoError(t, err)
	require.True(t, intercepted)
	require.NoError(t, writeErr)
	require.Equal(t, "generation-a", prior.MetadataRows["notes/example.md"].Title)
	require.Len(t, prior.PropertyValues, 1)
	require.Equal(t, "a", prior.PropertyValues[0].ValueText)
	require.Equal(t, "a", prior.Tags[0].TagNorm)

	current, err := reader.DurableNoteFactsByPaths(ctx, []string{"notes/example.md"})
	require.NoError(t, err)
	require.Equal(t, "generation-b", current.MetadataRows["notes/example.md"].Title)
	require.Equal(t, "b", current.PropertyValues[0].ValueText)
	require.Equal(t, "b", current.Tags[0].TagNorm)
}

func TestDurableNoteFactsByPathsReleasesCanceledTransaction(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "ready", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{{Path: "notes/example.md", Title: "Example", ContentHash: "hash", Mtime: 1, Size: 1}},
	}))

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	facts, err := store.DurableNoteFactsByPaths(canceled, []string{"notes/example.md"})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, facts.MetadataRows)

	facts, err = store.DurableNoteFactsByPaths(ctx, []string{"notes/example.md"})
	require.NoError(t, err)
	require.Equal(t, "Example", facts.MetadataRows["notes/example.md"].Title)
}

func TestReplaceNoteMetadataSnapshot_RemovesStaleIndexedNotes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "first", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/keep.md", Title: "Keep", ContentHash: "a", Mtime: 10, Size: 1},
			{Path: "notes/stale.md", Title: "Stale", ContentHash: "b", Mtime: 11, Size: 1},
		},
	}))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "second", LoadedAt: 2, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/keep.md", Title: "Keep", ContentHash: "a2", Mtime: 12, Size: 2},
		},
	}))

	rows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "notes/keep.md", rows[0].Path)
}

func TestAllNoteMetadataPathsIncludesRowsWhenStateIsUnready(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{LoadedAt: 1, Ready: false},
		Notes: []NoteMetadataRow{{Path: "stale.md", IndexedAt: 1}},
	}))

	current, err := store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.Empty(t, current)
	all, err := store.AllNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"stale.md"}, all)
}

func TestDurableNoteMetadataRowsByPathsReadsUnreadyRowsWithCanonicalInputs(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{LoadedAt: 11, Ready: false},
		Notes: []NoteMetadataRow{{Path: "notes/published.html", ContentHash: "html-hash", Mtime: 4, Size: 12, FormatID: "html"}},
	}))

	rows, err := store.DurableNoteMetadataRowsByPaths(ctx, []string{"notes/published.html"})
	require.NoError(t, err)
	require.Equal(t, "html-hash", rows["notes/published.html"].ContentHash)

	_, err = store.DurableNoteMetadataRowsByPaths(ctx, []string{"./notes/published.html"})
	require.ErrorContains(t, err, "canonical and vault-relative")
	_, err = store.DurableNoteMetadataRowsByPaths(ctx, []string{"notes/published.html", "notes/published.html"})
	require.ErrorContains(t, err, "repeated")
}

func TestNoteProjectionState_SnapshotAndDeltaReplacementRetainsFatalSource(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "projection-state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "first", RawNotesHash: "raw-first", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{
			{
				Path:        "notes/current.md",
				Title:       "Current",
				ContentHash: "current-hash",
				Mtime:       1,
				Size:        10,
				FormatID:    "markdown",
				Projection: NoteProjectionState{
					ProviderVersion:   "markdown-v1",
					ProjectionVersion: "projection-v1",
					SourceContentHash: "current-hash",
					Status:            NoteProjectionStatusCurrent,
					UpdatedAt:         1,
				},
			},
			{Path: "notes/deleted.md", Title: "Deleted", ContentHash: "deleted-hash", Mtime: 1, Size: 10},
		},
		PropertyValues: []NotePropertyValueRow{{
			NotePath: "notes/current.md", PropertyName: "type", Source: NotePropertySourceFrontmatter,
			ValueText: "Project", ValueNorm: "project", ValueKind: NotePropertyValueString,
		}},
	}))

	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/current.md", "notes/deleted.md"})
	require.NoError(t, err)
	require.Equal(t, "markdown", rows["notes/current.md"].FormatID)
	require.Equal(t, NoteProjectionStatusCurrent, rows["notes/current.md"].Projection.Status)
	require.Equal(t, "markdown", rows["notes/deleted.md"].FormatID, "zero-value provenance defaults to Markdown")
	require.Equal(t, NoteProjectionStatusStale, rows["notes/deleted.md"].Projection.Status)

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "snapshot-replaced", RawNotesHash: "raw-snapshot-replaced", LoadedAt: 2, Ready: true},
		Notes: []NoteMetadataRow{{
			Path:        "notes/current.md",
			Title:       "Current",
			ContentHash: "stale-hash",
			Mtime:       2,
			Size:        11,
			FormatID:    "markdown",
			Projection: NoteProjectionState{
				Status:    NoteProjectionStatusStale,
				UpdatedAt: 2,
			},
		}},
		PropertyValues: []NotePropertyValueRow{{
			NotePath: "notes/current.md", PropertyName: "type", Source: NotePropertySourceFrontmatter,
			ValueText: "Project", ValueNorm: "project", ValueKind: NotePropertyValueString,
		}},
	}))
	rows, err = store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/current.md", "notes/deleted.md"})
	require.NoError(t, err)
	require.Len(t, rows, 1, "snapshot replacement removes stale source and projection state together")
	require.Equal(t, NoteProjectionStatusStale, rows["notes/current.md"].Projection.Status)

	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State: NoteMetadataState{NotesHash: "second", RawNotesHash: "raw-second", LoadedAt: 3, Ready: true},
		Notes: []NoteMetadataRow{{
			Path:        "notes/current.md",
			Title:       "Current",
			ContentHash: "fatal-hash",
			Mtime:       3,
			Size:        11,
			FormatID:    "html",
			Projection: NoteProjectionState{
				ProviderVersion:   "html-v1",
				ProjectionVersion: "projection-v1",
				SourceContentHash: "fatal-hash",
				Status:            NoteProjectionStatusFatal,
				DiagnosticCode:    "invalid_utf8",
				DiagnosticDetail:  "source is not valid UTF-8",
				UpdatedAt:         3,
			},
		}},
		DeletedPaths: []string{"notes/deleted.md"},
	}))

	rows, err = store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/current.md", "notes/deleted.md"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	fatal := rows["notes/current.md"]
	require.Equal(t, "html", fatal.FormatID)
	require.Equal(t, "fatal-hash", fatal.ContentHash, "fatal projection keeps current source identity")
	require.Equal(t, NoteProjectionStatusFatal, fatal.Projection.Status)
	require.Equal(t, "invalid_utf8", fatal.Projection.DiagnosticCode)
	require.Equal(t, "source is not valid UTF-8", fatal.Projection.DiagnosticDetail)

	paths, err := store.CurrentNotePathsByPropertyValue(ctx, "type", "project", 0)
	require.NoError(t, err)
	require.Empty(t, paths, "changed fatal sources replace stale derived rows in the same delta")
}

func TestNoteProjectionState_RejectsDiagnosticStatusMismatch(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		write func(*Store, NoteProjectionState) error
		state NoteProjectionState
		want  string
	}{
		{
			name: "snapshot fatal requires diagnostic code and detail",
			write: func(store *Store, state NoteProjectionState) error {
				return store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
					State: NoteMetadataState{NotesHash: "snapshot", LoadedAt: 1, Ready: true},
					Notes: []NoteMetadataRow{{Path: "notes/fatal.md", ContentHash: "hash", Projection: state}},
				})
			},
			state: NoteProjectionState{Status: NoteProjectionStatusFatal, DiagnosticCode: "invalid_utf8"},
			want:  "fatal projections require non-empty diagnostic code and detail",
		},
		{
			name: "delta current rejects blocking diagnostic",
			write: func(store *Store, state NoteProjectionState) error {
				return store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
					State: NoteMetadataState{NotesHash: "delta", LoadedAt: 1, Ready: true},
					Notes: []NoteMetadataRow{{Path: "notes/current.md", ContentHash: "hash", Projection: state}},
				})
			},
			state: NoteProjectionState{Status: NoteProjectionStatusCurrent, DiagnosticCode: "invalid_utf8", DiagnosticDetail: "not valid UTF-8"},
			want:  "current projections must not retain a blocking diagnostic",
		},
		{
			name: "delta stale rejects blocking diagnostic detail",
			write: func(store *Store, state NoteProjectionState) error {
				return store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
					State: NoteMetadataState{NotesHash: "delta", LoadedAt: 1, Ready: true},
					Notes: []NoteMetadataRow{{Path: "notes/stale.md", ContentHash: "hash", Projection: state}},
				})
			},
			state: NoteProjectionState{Status: NoteProjectionStatusStale, DiagnosticDetail: "stale details are not diagnostics"},
			want:  "stale projections must not retain a blocking diagnostic",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := Open(currentSchemaTestDBPath(t, "projection-state.db"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })

			err = tc.write(store, tc.state)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestNoteProjectionState_RejectsCurrentAndFatalProvenanceMismatch(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		write func(*Store, NoteProjectionState) error
		state NoteProjectionState
		want  string
	}{
		{
			name: "snapshot current requires provider version",
			write: func(store *Store, state NoteProjectionState) error {
				return store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
					State: NoteMetadataState{NotesHash: "snapshot", LoadedAt: 1, Ready: true},
					Notes: []NoteMetadataRow{{Path: "notes/current.md", ContentHash: "hash", Projection: state}},
				})
			},
			state: NoteProjectionState{ProjectionVersion: "projection-v1", SourceContentHash: "hash", Status: NoteProjectionStatusCurrent},
			want:  "current projections require non-empty provider and projection versions",
		},
		{
			name: "delta current requires matching source hash",
			write: func(store *Store, state NoteProjectionState) error {
				return store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
					State: NoteMetadataState{NotesHash: "delta", LoadedAt: 1, Ready: true},
					Notes: []NoteMetadataRow{{Path: "notes/current.md", ContentHash: "hash", Projection: state}},
				})
			},
			state: NoteProjectionState{ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", SourceContentHash: "other-hash", Status: NoteProjectionStatusCurrent},
			want:  "current projection source hash must match the note content hash",
		},
		{
			name: "delta current requires non-empty source hash",
			write: func(store *Store, state NoteProjectionState) error {
				return store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
					State: NoteMetadataState{NotesHash: "delta", LoadedAt: 1, Ready: true},
					Notes: []NoteMetadataRow{{Path: "notes/current.md", ContentHash: "hash", Projection: state}},
				})
			},
			state: NoteProjectionState{ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", Status: NoteProjectionStatusCurrent},
			want:  "current projection source hash must match the note content hash",
		},
		{
			name: "snapshot fatal requires provider and projection versions",
			write: func(store *Store, state NoteProjectionState) error {
				return store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
					State: NoteMetadataState{NotesHash: "snapshot", LoadedAt: 1, Ready: true},
					Notes: []NoteMetadataRow{{Path: "notes/fatal.md", ContentHash: "hash", Projection: state}},
				})
			},
			state: NoteProjectionState{DiagnosticCode: "invalid_utf8", DiagnosticDetail: "not valid UTF-8", SourceContentHash: "hash", Status: NoteProjectionStatusFatal},
			want:  "fatal projections require non-empty provider and projection versions",
		},
		{
			name: "delta fatal source hash must match when source is available",
			write: func(store *Store, state NoteProjectionState) error {
				return store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
					State: NoteMetadataState{NotesHash: "delta", LoadedAt: 1, Ready: true},
					Notes: []NoteMetadataRow{{Path: "notes/fatal.md", ContentHash: "hash", Projection: state}},
				})
			},
			state: NoteProjectionState{ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", SourceContentHash: "other-hash", DiagnosticCode: "invalid_utf8", DiagnosticDetail: "not valid UTF-8", Status: NoteProjectionStatusFatal},
			want:  "fatal projection source hash must match the note content hash when source is available",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := Open(currentSchemaTestDBPath(t, "projection-state.db"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })

			err = tc.write(store, tc.state)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestNoteProjectionStateSchema_RejectsInvalidDiagnosticStatuses(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "projection-state-constraints.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO notes(path, content_hash, indexed_at)
		VALUES ('notes/raw.sql', 'hash', 1)
	`)
	require.NoError(t, err)

	cases := []struct {
		name, status, provider, projection, sourceHash, code, detail string
	}{
		{name: "fatal missing detail", status: "fatal", provider: "provider-v1", projection: "projection-v1", sourceHash: "hash", code: "invalid_utf8"},
		{name: "fatal whitespace detail", status: "fatal", provider: "provider-v1", projection: "projection-v1", sourceHash: "hash", code: "invalid_utf8", detail: "  "},
		{name: "current diagnostic", status: "current", provider: "provider-v1", projection: "projection-v1", sourceHash: "hash", code: "invalid_utf8", detail: "not valid UTF-8"},
		{name: "stale diagnostic", status: "stale", provider: "provider-v1", projection: "projection-v1", sourceHash: "hash", detail: "stale details"},
		{name: "current missing provider version", status: "current", projection: "projection-v1", sourceHash: "hash"},
		{name: "current missing source hash", status: "current", provider: "provider-v1", projection: "projection-v1"},
		{name: "fatal missing provider version", status: "fatal", projection: "projection-v1", sourceHash: "hash", code: "invalid_utf8", detail: "not valid UTF-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.db.ExecContext(ctx, `
				INSERT INTO note_projection_state(
					note_id, provider_version, projection_version, source_content_hash,
					status, diagnostic_code, diagnostic_detail
				)
				SELECT id, ?, ?, ?, ?, ?, ? FROM notes WHERE path = 'notes/raw.sql'
			`, tc.provider, tc.projection, tc.sourceHash, tc.status, tc.code, tc.detail)
			require.Error(t, err)
		})
	}
	for _, status := range []string{"current", "fatal", "stale"} {
		code, detail := "", ""
		if status == "fatal" {
			code, detail = "invalid_utf8", "not valid UTF-8"
		}
		_, err := store.db.ExecContext(ctx, `INSERT INTO note_projection_state
			(note_id, provider_version, projection_version, source_content_hash, status, diagnostic_code, diagnostic_detail)
			SELECT id, 'provider-v1', 'projection-v1', 'hash', ?, ?, ? FROM notes WHERE path = 'notes/raw.sql'`, status, code, detail)
		require.NoError(t, err)
		_, err = store.db.ExecContext(ctx, `DELETE FROM note_projection_state`)
		require.NoError(t, err)
	}
}

func TestNoteFormatID_CanonicalizesAcrossMetadataAndOwnershipWrites(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "format-id.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "snapshot", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{{
			Path: "notes/snapshot.html", ContentHash: "snapshot", FormatID: " HTML ",
		}},
	}))
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/snapshot.html"})
	require.NoError(t, err)
	require.Equal(t, "html", rows["notes/snapshot.html"].FormatID)

	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State: NoteMetadataState{NotesHash: "delta", LoadedAt: 2, Ready: true},
		Notes: []NoteMetadataRow{{
			Path: "notes/delta.md", ContentHash: "delta", FormatID: "MARKDOWN",
		}},
	}))
	rows, err = store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/delta.md"})
	require.NoError(t, err)
	require.Equal(t, "markdown", rows["notes/delta.md"].FormatID)

	_, err = store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path:   "notes/transition.html",
		Target: OwnershipTargetNote,
		Note: &NoteSourceState{
			FormatID: "HTML", ContentHash: "transition", ProviderVersion: "html-v1", ProjectionVersion: "root-v1",
			Status: NoteProjectionStatusCurrent, ObservedAt: 3,
		},
	}})
	require.NoError(t, err)
	var formatID string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT format_id FROM notes WHERE path = 'notes/transition.html'`).Scan(&formatID))
	require.Equal(t, "html", formatID)
}

func TestNoteFormatIDSchema_RejectsNonCanonicalRawSQL(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "format-id-constraints.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	for _, tc := range []struct {
		name     string
		formatID string
	}{
		{name: "empty", formatID: ""},
		{name: "uppercase", formatID: "HTML"},
		{name: "leading whitespace", formatID: " html"},
		{name: "trailing whitespace", formatID: "html "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.db.ExecContext(ctx, `
				INSERT INTO notes(path, content_hash, indexed_at, format_id)
				VALUES (?, 'hash', 1, ?)
			`, "notes/"+tc.name+".html", tc.formatID)
			require.Error(t, err)
		})
	}
}

func TestNoteFragmentTargets_SnapshotAndDeltaReplacement(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "markdown-targets.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "first", RawNotesHash: "raw-first", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/original.md", Title: "Original", ContentHash: "a", Mtime: 1, Size: 1},
			{Path: "notes/deleted.md", Title: "Deleted", ContentHash: "b", Mtime: 1, Size: 1},
		},
		FragmentTargets: []NoteFragmentTargetRow{
			{NotePath: "notes/original.md", Kind: NoteFragmentTargetHeading, Target: "Repeat", TargetNorm: "repeat", Ordinal: 1},
			{NotePath: "notes/original.md", Kind: NoteFragmentTargetHeading, Target: "Repeat", TargetNorm: "repeat", Ordinal: 2},
			{NotePath: "notes/original.md", Kind: NoteFragmentTargetBlock, Target: "CaseID", TargetNorm: "CaseID", Ordinal: 1},
			{NotePath: "notes/original.md", Kind: NoteFragmentTargetBlock, Target: "caseid", TargetNorm: "caseid", Ordinal: 1},
			{NotePath: "notes/deleted.md", Kind: NoteFragmentTargetHeading, Target: "Gone", TargetNorm: "gone", Ordinal: 1},
		},
	}))

	duplicates, err := store.CurrentNoteFragmentTargets(ctx, []string{"notes/original.md"}, NoteFragmentTargetHeading, "repeat")
	require.NoError(t, err)
	require.Len(t, duplicates, 2, "duplicate headings must remain separate rows")
	require.Equal(t, []int{1, 2}, []int{duplicates[0].Ordinal, duplicates[1].Ordinal})

	upperBlock, err := store.CurrentNoteFragmentTargets(ctx, []string{"notes/original.md"}, NoteFragmentTargetBlock, "CaseID")
	require.NoError(t, err)
	require.Len(t, upperBlock, 1)
	require.Equal(t, "CaseID", upperBlock[0].Target)
	lowerBlock, err := store.CurrentNoteFragmentTargets(ctx, []string{"notes/original.md"}, NoteFragmentTargetBlock, "caseid")
	require.NoError(t, err)
	require.Len(t, lowerBlock, 1, "block target lookup must remain case-sensitive")
	require.Equal(t, "caseid", lowerBlock[0].Target)

	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State: NoteMetadataState{NotesHash: "second", RawNotesHash: "raw-second", LoadedAt: 2, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/renamed.md", Title: "Renamed", ContentHash: "c", Mtime: 2, Size: 1},
		},
		FragmentTargets: []NoteFragmentTargetRow{
			{NotePath: "notes/renamed.md", Kind: NoteFragmentTargetHeading, Target: "Current", TargetNorm: "current", Ordinal: 1},
		},
		DeletedPaths: []string{"notes/original.md", "notes/deleted.md"},
	}))

	oldTargets, err := store.CurrentNoteFragmentTargets(ctx, []string{"notes/original.md", "notes/deleted.md"}, "", "")
	require.NoError(t, err)
	require.Empty(t, oldTargets, "delete/rename tombstones must remove old target rows")
	newTargets, err := store.CurrentNoteFragmentTargets(ctx, []string{"notes/renamed.md"}, "", "")
	require.NoError(t, err)
	require.Len(t, newTargets, 1)
	require.Positive(t, newTargets[0].NoteID)
	require.Equal(t, "notes/renamed.md", newTargets[0].NotePath)
	require.Equal(t, NoteFragmentTargetHeading, newTargets[0].Kind)
	require.Equal(t, "Current", newTargets[0].Target)
	require.Equal(t, "current", newTargets[0].TargetNorm)
	require.Equal(t, 1, newTargets[0].Ordinal)

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "third", RawNotesHash: "raw-third", LoadedAt: 3, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/renamed.md", Title: "Renamed", ContentHash: "d", Mtime: 3, Size: 1},
		},
	}))
	targets, err := store.CurrentNoteFragmentTargets(ctx, nil, "", "")
	require.NoError(t, err)
	require.Empty(t, targets, "snapshot replacement must clear stale target rows")
}

func TestProjectionDiagnosticsAndSearchRegionsPublishAtomically(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "projection-facts.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	note := NoteMetadataRow{
		Path: "notes/prototype.html", Title: "Prototype", ContentHash: "source-a", Mtime: 1, Size: 20,
		Projection: NoteProjectionState{ProviderVersion: "html-v1", ProjectionVersion: "facts-v1", SourceContentHash: "source-a", Status: NoteProjectionStatusCurrent},
	}
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State:           NoteMetadataState{NotesHash: "one", RawNotesHash: "raw-one", LoadedAt: 1, Ready: true},
		Notes:           []NoteMetadataRow{note},
		FragmentTargets: []NoteFragmentTargetRow{{NotePath: note.Path, Kind: NoteFragmentTargetElementID, Target: "chart", TargetNorm: "chart", Ordinal: 1}},
		ProjectionDiagnostics: []NoteProjectionDiagnosticRow{{
			NotePath: note.Path, Ordinal: 0, Code: "metadata_shape", Category: "metadata", Message: "metadata is not an object",
			RangePresent: true, StartByte: 3, EndByte: 9, AffectedOperation: "metadata_read",
		}},
		SearchRegions: []NoteSearchRegionRow{{
			NotePath: note.Path, Ordinal: 0, Origin: "authored", Kind: "visible", Text: "Quarterly results", MediaType: "text/html",
			RangePresent: true, StartByte: 10, EndByte: 19,
		}},
	}))

	facts, err := store.DurableNoteFactsByPaths(ctx, []string{note.Path})
	require.NoError(t, err)
	require.Len(t, facts.FragmentTargets, 1)
	require.Equal(t, NoteFragmentTargetElementID, facts.FragmentTargets[0].Kind)
	require.Equal(t, []NoteProjectionDiagnosticRow{{
		NoteID: facts.MetadataRows[note.Path].NoteID, NotePath: note.Path, Ordinal: 0, Code: "metadata_shape", Category: "metadata", Message: "metadata is not an object",
		RangePresent: true, StartByte: 3, EndByte: 9, AffectedOperation: "metadata_read",
	}}, facts.ProjectionDiagnostics)
	require.Len(t, facts.SearchRegions, 1)
	require.Equal(t, "Quarterly results", facts.SearchRegions[0].Text)
	require.True(t, facts.SearchRegions[0].RangePresent)
	require.Equal(t, 10, facts.SearchRegions[0].StartByte)
	require.Equal(t, 19, facts.SearchRegions[0].EndByte)

	note.ContentHash = "source-b"
	note.Projection = NoteProjectionState{
		ProviderVersion: "html-v1", ProjectionVersion: "facts-v1", SourceContentHash: "source-b", Status: NoteProjectionStatusFatal,
		DiagnosticCode: "invalid_utf8", DiagnosticDetail: "source is not UTF-8",
	}
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State: NoteMetadataState{NotesHash: "two", RawNotesHash: "raw-two", LoadedAt: 2, Ready: true},
		Notes: []NoteMetadataRow{note},
		ProjectionDiagnostics: []NoteProjectionDiagnosticRow{{
			NotePath: note.Path, Ordinal: 0, Code: "invalid_utf8", Category: "source", Message: "source is not UTF-8", Blocking: true, AffectedOperation: "projection",
		}},
	}))
	facts, err = store.DurableNoteFactsByPaths(ctx, []string{note.Path})
	require.NoError(t, err)
	require.Empty(t, facts.FragmentTargets)
	require.Empty(t, facts.SearchRegions)
	require.Len(t, facts.ProjectionDiagnostics, 1)
	require.True(t, facts.ProjectionDiagnostics[0].Blocking)
	require.Equal(t, facts.MetadataRows[note.Path].Projection.DiagnosticCode, facts.ProjectionDiagnostics[0].Code)
}

func TestApplyNoteMetadataDelta_PersistsLargeBatchedWrites(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	propertyRows := make([]NotePropertyValueRow, 0, 180)
	tags := make([]NoteTagRow, 0, 180)
	edges := make([]GraphDocEdgeRow, 0, 180)
	for i := range 180 {
		name := fmt.Sprintf("field_%03d", i)
		value := fmt.Sprintf("value_%03d", i)
		propertyRows = append(propertyRows, NotePropertyValueRow{
			NotePath:     "notes/project.md",
			PropertyName: name,
			Source:       NotePropertySourceFrontmatter,
			ValueText:    value,
			ValueNorm:    value,
			ValueKind:    NotePropertyValueString,
		})
		tags = append(tags, NoteTagRow{
			NotePath: "notes/project.md",
			TagNorm:  fmt.Sprintf("topic/%03d", i),
		})
		edges = append(edges, GraphDocEdgeRow{
			SrcPath: "notes/project.md",
			DstPath: fmt.Sprintf("notes/target-%03d.md", i),
			Kind:    NoteLinkKind("mdlink", "heading"),
		})
	}

	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State: NoteMetadataState{NotesHash: "notes-hash", RawNotesHash: "raw-notes-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/project.md", Title: "Project", ContentHash: "hash", Mtime: 10, Size: 100},
		},
		PropertyValues: propertyRows,
		Tags:           tags,
		WikilinkEdges:  edges,
	}))

	gotProperties, err := store.CurrentNotePropertyValues(ctx, []string{"notes/project.md"}, nil, 0)
	require.NoError(t, err)
	require.Len(t, gotProperties, 180)

	gotTags, err := store.CurrentNoteTags(ctx, []string{"notes/project.md"})
	require.NoError(t, err)
	require.Len(t, gotTags, 180)

	gotEdges, err := store.GraphDocNoteLinkEdges(ctx)
	require.NoError(t, err)
	require.Len(t, gotEdges, 180)

	detailedEdges, err := store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	require.Len(t, detailedEdges, 180)
	require.Equal(t, EdgeConfidenceExtracted, detailedEdges[0].Confidence)
	require.InDelta(t, 0.9, detailedEdges[0].ConfidenceScore, 0.0001)
}

func TestApplyNoteMetadataDelta_BatchesLargePathDeletes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "before", RawNotesHash: "raw-before", LoadedAt: 1, Ready: true},
	}))

	deletedPaths := make([]string, 0, sqliteValuesBatchMaxParams*40)
	for i := range sqliteValuesBatchMaxParams * 40 {
		deletedPaths = append(deletedPaths, fmt.Sprintf("notes/deleted-%05d.md", i))
	}
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State:        NoteMetadataState{NotesHash: "after", RawNotesHash: "raw-after", LoadedAt: 2, Ready: true},
		DeletedPaths: deletedPaths,
	}))

	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, "after", state.NotesHash)
	require.Equal(t, "raw-after", state.RawNotesHash)
	require.Equal(t, int64(2), state.LoadedAt)
}

func TestResolveStoredNoteLinks_UsesExactPathAndRejectsAmbiguousBasename(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", RawNotesHash: "raw-notes-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "target.md", Title: "Top", ContentHash: "a", Mtime: 1, Size: 1},
			{Path: "nested/target.md", Title: "Nested", ContentHash: "b", Mtime: 1, Size: 1},
			{Path: "deep/path/target.md", Title: "Deep", ContentHash: "c", Mtime: 1, Size: 1},
			{Path: "other/unique.md", Title: "Unique", ContentHash: "d", Mtime: 1, Size: 1},
		},
	}))

	resolved, err := store.ResolveStoredNoteLinks(ctx, []string{"target", "nested/target", "unique", "missing"})
	require.NoError(t, err)

	tests := []struct {
		name string
		link string
		want string
		ok   bool
	}{
		{name: "ambiguous basename", link: "target", ok: false},
		{name: "exact path", link: "nested/target", want: "nested/target.md", ok: true},
		{name: "unique basename", link: "unique", want: "other/unique.md", ok: true},
		{name: "missing", link: "missing", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolved[tt.link]
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGraphDocBacklinksForPath_ReturnsPersistedIncomingNoteEdges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "metadata.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", RawNotesHash: "raw-notes-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/target.md", Title: "Target", ContentHash: "a", Mtime: 1, Size: 1},
			{Path: "notes/basic.md", Title: "Basic", ContentHash: "b", Mtime: 1, Size: 1},
			{Path: "notes/heading.md", Title: "Heading", ContentHash: "c", Mtime: 1, Size: 1},
		},
		WikilinkEdges: []GraphDocEdgeRow{
			{SrcPath: "notes/basic.md", DstPath: "notes/target.md", Kind: GraphDocEdgeKindWikilink},
			{SrcPath: "notes/heading.md", DstPath: "notes/target.md", Kind: NoteLinkKind("wikilink", "heading")},
		},
	}))

	rows, err := store.GraphDocBacklinksForPath(ctx, "notes/target.md", 0)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "notes/basic.md", rows[0].SrcPath)
	require.Equal(t, GraphDocEdgeKindWikilink, rows[0].Kind)
	require.Equal(t, "notes/heading.md", rows[1].SrcPath)
	require.Equal(t, NoteLinkKind("wikilink", "heading"), rows[1].Kind)
}

func TestCurrentNoteAliases_ReturnsFrontmatterAliasesInOrder(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "aliases.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "docs/specs/product/001-indexed-search.md", Title: "Indexed Search", ContentHash: "a", Mtime: 1, Size: 1},
			{Path: "docs/specs/product/002-rename-backlinks.md", Title: "Rename Backlinks", ContentHash: "b", Mtime: 2, Size: 1},
			{Path: "notes/plain.md", Title: "Plain", ContentHash: "c", Mtime: 3, Size: 1},
		},
		PropertyValues: []NotePropertyValueRow{
			{NotePath: "docs/specs/product/001-indexed-search.md", PropertyName: "aliases", Source: NotePropertySourceFrontmatter, ValueText: "SPEC-001", ValueNorm: "spec-001", ValueKind: NotePropertyValueString, IsList: true, ListOrdinal: 0},
			{NotePath: "docs/specs/product/001-indexed-search.md", PropertyName: "aliases", Source: NotePropertySourceFrontmatter, ValueText: "spec-indexed-search", ValueNorm: "spec-indexed-search", ValueKind: NotePropertyValueString, IsList: true, ListOrdinal: 1},
			{NotePath: "docs/specs/product/002-rename-backlinks.md", PropertyName: "aliases", Source: NotePropertySourceFrontmatter, ValueText: "SPEC-002", ValueNorm: "spec-002", ValueKind: NotePropertyValueString, IsList: true, ListOrdinal: 0},
			// Non-aliases property must not leak into the result.
			{NotePath: "notes/plain.md", PropertyName: "type", Source: NotePropertySourceFrontmatter, ValueText: "Note", ValueNorm: "note", ValueKind: NotePropertyValueString},
		},
	}))

	out, err := store.CurrentNoteAliases(ctx)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, []string{"SPEC-001", "spec-indexed-search"}, out["docs/specs/product/001-indexed-search.md"])
	require.Equal(t, []string{"SPEC-002"}, out["docs/specs/product/002-rename-backlinks.md"])
	_, plainOK := out["notes/plain.md"]
	require.False(t, plainOK, "notes without aliases must not appear")
}

func TestCurrentNotePathsByPropertyValue_AliasLookup(t *testing.T) {
	// Pins that the existing CurrentNotePathsByPropertyValue works as our
	// single-alias resolver at retrieval time (no new method required for
	// the find: shortcut — we reuse this one in MCP/CLI).
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "aliases-lookup.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "docs/specs/product/001-indexed-search.md", Title: "Indexed Search", ContentHash: "a", Mtime: 1, Size: 1},
		},
		PropertyValues: []NotePropertyValueRow{
			{NotePath: "docs/specs/product/001-indexed-search.md", PropertyName: "aliases", Source: NotePropertySourceFrontmatter, ValueText: "SPEC-001", ValueNorm: "spec-001", ValueKind: NotePropertyValueString, IsList: true, ListOrdinal: 0},
		},
	}))

	// value_norm is what gets matched — lowercased during indexing.
	hits, err := store.CurrentNotePathsByPropertyValue(ctx, "aliases", "spec-001", NotePropertySourceFrontmatter)
	require.NoError(t, err)
	require.Equal(t, []string{"docs/specs/product/001-indexed-search.md"}, hits)
}

func TestCurrentNotePathsByFindPattern_UsesIndexedTerms(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "find-index.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", RawNotesHash: "raw-notes-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "Notes/Great Pyramid Plans.md", Title: "Great Pyramid Plans", ContentHash: "a", Mtime: 1, Size: 1},
			{Path: "My Notes/Sync with Joe.md", Title: "Weekly Sync", ContentHash: "b", Mtime: 2, Size: 1},
			{Path: "Archive/notes.v1.md", Title: "notes.v1", ContentHash: "c", Mtime: 3, Size: 1},
		},
	}))

	t.Run("content only", func(t *testing.T) {
		paths, indexed, err := store.CurrentNotePathsByFindPattern(ctx, "pyramid plans")
		require.NoError(t, err)
		require.True(t, indexed)
		require.Equal(t, []string{"Notes/Great Pyramid Plans.md"}, paths)
	})

	t.Run("slash directory filter", func(t *testing.T) {
		paths, indexed, err := store.CurrentNotePathsByFindPattern(ctx, "m/s joe")
		require.NoError(t, err)
		require.True(t, indexed)
		require.Equal(t, []string{"My Notes/Sync with Joe.md"}, paths)
	})

	t.Run("dotted segment filter", func(t *testing.T) {
		paths, indexed, err := store.CurrentNotePathsByFindPattern(ctx, "notes.v1")
		require.NoError(t, err)
		require.True(t, indexed)
		require.Equal(t, []string{"Archive/notes.v1.md"}, paths)
	})

	t.Run("wildcards fall back", func(t *testing.T) {
		_, indexed, err := store.CurrentNotePathsByFindPattern(ctx, "*pyramid*")
		require.NoError(t, err)
		require.False(t, indexed)
	})

	t.Run("unicode search terms stay intact", func(t *testing.T) {
		require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
			State: NoteMetadataState{NotesHash: "notes-hash-2", RawNotesHash: "raw-notes-hash-2", LoadedAt: 43, Ready: true},
			Notes: []NoteMetadataRow{
				{Path: "Notes/漢字 notes.md", Title: "漢字 notes", ContentHash: "d", Mtime: 4, Size: 1},
			},
		}))

		paths, indexed, err := store.CurrentNotePathsByFindPattern(ctx, "漢字")
		require.NoError(t, err)
		require.True(t, indexed)
		require.Equal(t, []string{"Notes/漢字 notes.md"}, paths)
	})
}

func TestReplaceNoteMetadataSnapshot_IgnoresDuplicateSearchTermRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "duplicate-search-terms.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", RawNotesHash: "raw-notes-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "Notes/Weekly Sync.md", Title: "Weekly Sync", ContentHash: "a", Mtime: 1, Size: 1},
			{Path: "Notes/Weekly Sync.md", Title: "Weekly Sync", ContentHash: "a", Mtime: 1, Size: 1},
		},
	}))

	paths, indexed, err := store.CurrentNotePathsByFindPattern(ctx, "weekly sync")
	require.NoError(t, err)
	require.True(t, indexed)
	require.Equal(t, []string{"Notes/Weekly Sync.md"}, paths)
}
