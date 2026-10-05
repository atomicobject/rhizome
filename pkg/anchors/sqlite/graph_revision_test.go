package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

func TestGraphWebFingerprintRevisionBehavior(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "graph-revision.db")
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	other, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })

	fingerprint := func(s *Store) string {
		t.Helper()
		got, lookupErr := s.GraphWebFingerprint(ctx)
		require.NoError(t, lookupErr)
		return got
	}
	assertChanged := func(before string) string {
		t.Helper()
		after := fingerprint(other)
		require.NotEqual(t, before, after)
		return after
	}

	before := fingerprint(store)
	_, err = store.db.ExecContext(ctx, `INSERT INTO files(path, lang, mtime) VALUES ('src/a.go', 'go', 1)`)
	require.NoError(t, err)
	before = assertChanged(before)

	_, err = store.db.ExecContext(ctx, `INSERT INTO graph_doc_edges(src_path, dst_path, kind, confidence, confidence_score, source_location) VALUES ('notes/a.md', 'notes/x.md', 'wikilink', 'extracted', 1, '')`)
	require.NoError(t, err)
	before = assertChanged(before)
	_, err = store.db.ExecContext(ctx, `UPDATE graph_doc_edges SET dst_path = 'notes/y.md' WHERE src_path = 'notes/a.md'`)
	require.NoError(t, err)
	before = assertChanged(before)

	_, err = store.db.ExecContext(ctx, `INSERT INTO ontology_note_types(note_path, type_name, schema_hash, updated_at) VALUES ('notes/a.md', 'Spec', 'schema', 1)`)
	require.NoError(t, err)
	before = assertChanged(before)
	_, err = store.db.ExecContext(ctx, `UPDATE ontology_note_types SET type_name = 'Decision' WHERE note_path = 'notes/a.md'`)
	require.NoError(t, err)
	before = assertChanged(before)
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"notes/a.md"}, []codeanchor.IntelOntologyNode{{
		NodeID: "node-a", NotePath: "notes/a.md", NodeRefJSON: `{"notePath":"notes/a.md","kind":"NOTE"}`,
		NodeKind: "NOTE", TypeName: "Decision", SourceLocator: "notes/a.md", DisplayLabel: "Original label",
	}}))
	before = assertChanged(before)
	_, err = store.db.ExecContext(ctx, `UPDATE ontology_nodes SET display_label = 'Changed label' WHERE node_id = 'node-a'`)
	require.NoError(t, err)
	before = assertChanged(before)

	_, err = store.db.ExecContext(ctx, `INSERT INTO graph_doc_scores(doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at) VALUES ('notes/a.md', 'note', 1, 2, 'one', 3, 4, 1)`)
	require.NoError(t, err)
	before = assertChanged(before)
	_, err = store.db.ExecContext(ctx, `UPDATE graph_doc_scores SET authority = 9 WHERE doc_path = 'notes/a.md'`)
	require.NoError(t, err)
	_ = assertChanged(before)
}

func TestGraphWebFingerprintEligibilityTransitionsAndIgnoredWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "graph-eligibility.db")
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	other, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })

	fingerprint := func() string {
		t.Helper()
		got, lookupErr := other.GraphWebFingerprint(ctx)
		require.NoError(t, lookupErr)
		return got
	}

	before := fingerprint()
	_, err = store.db.ExecContext(ctx, `INSERT INTO doc_links(src_path, src_type, dst_path, dst_kind, updated_at) VALUES ('src/a.go', 'external', 'notes/a.md', 'external', 1)`)
	require.NoError(t, err)
	require.Equal(t, before, fingerprint())

	_, err = store.db.ExecContext(ctx, `UPDATE doc_links SET src_type = 'code', dst_kind = 'anchor' WHERE src_path = 'src/a.go'`)
	require.NoError(t, err)
	afterEligible := fingerprint()
	require.NotEqual(t, before, afterEligible)
	_, codePaths, err := other.GraphDocPaths(ctx)
	require.NoError(t, err)
	require.Contains(t, codePaths, "src/a.go", "code-to-anchor sources are graph nodes even without a files row")

	_, err = store.db.ExecContext(ctx, `UPDATE doc_links SET dst_kind = 'note' WHERE src_path = 'src/a.go'`)
	require.NoError(t, err)
	afterCodeUpdate := fingerprint()
	require.NotEqual(t, afterEligible, afterCodeUpdate)
	_, err = store.db.ExecContext(ctx, `UPDATE doc_links SET src_type = 'external' WHERE src_path = 'src/a.go'`)
	require.NoError(t, err)
	afterLeaving := fingerprint()
	require.NotEqual(t, afterCodeUpdate, afterLeaving)
	_, err = store.db.ExecContext(ctx, `UPDATE doc_links SET dst_kind = 'external' WHERE src_path = 'src/a.go'`)
	require.NoError(t, err)
	require.Equal(t, afterLeaving, fingerprint())
	_, err = store.db.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = 'src/a.go'`)
	require.NoError(t, err)
	require.Equal(t, afterLeaving, fingerprint())
	_, err = store.db.ExecContext(ctx, `INSERT INTO doc_links(src_path, src_type, dst_path, dst_kind, updated_at) VALUES ('src/delete.go', 'code', 'anchor-a', 'anchor', 1)`)
	require.NoError(t, err)
	afterCodeInsert := fingerprint()
	require.NotEqual(t, afterLeaving, afterCodeInsert)
	_, err = store.db.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = 'src/delete.go'`)
	require.NoError(t, err)
	require.NotEqual(t, afterCodeInsert, fingerprint())

	before = fingerprint()
	_, err = store.db.ExecContext(ctx, `INSERT INTO graph_doc_edges(src_path, dst_path, kind, confidence, confidence_score, source_location) VALUES ('a', 'b', 'unsupported', 'extracted', 1, '')`)
	require.NoError(t, err)
	require.Equal(t, before, fingerprint())
	_, err = store.db.ExecContext(ctx, `UPDATE graph_doc_edges SET kind = 'mdlink' WHERE src_path = 'a'`)
	require.NoError(t, err)
	afterMDLink := fingerprint()
	require.NotEqual(t, before, afterMDLink)
	_, err = store.db.ExecContext(ctx, `UPDATE graph_doc_edges SET kind = 'unsupported' WHERE src_path = 'a'`)
	require.NoError(t, err)
	afterLeavingMDLink := fingerprint()
	require.NotEqual(t, afterMDLink, afterLeavingMDLink)
	_, err = store.db.ExecContext(ctx, `UPDATE graph_doc_edges SET kind = 'note_link:wikilink:alias' WHERE src_path = 'a'`)
	require.NoError(t, err)
	afterNoteLink := fingerprint()
	require.NotEqual(t, afterLeavingMDLink, afterNoteLink)
	_, err = store.db.ExecContext(ctx, `DELETE FROM graph_doc_edges WHERE src_path = 'a'`)
	require.NoError(t, err)
	require.NotEqual(t, afterNoteLink, fingerprint())

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/one.go", []codeanchor.IntelAnchor{{AnchorID: "one", Lang: codeanchor.LangGo, Kind: "function", Path: "src/one.go", Symbol: "One", Fingerprint: "one"}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/two.go", []codeanchor.IntelAnchor{{AnchorID: "two", Lang: codeanchor.LangGo, Kind: "function", Path: "src/two.go", Symbol: "Two", Fingerprint: "two"}}, nil, nil))
	var oneID, twoID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM intel_code_anchors WHERE anchor_id = 'one'`).Scan(&oneID))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id FROM intel_code_anchors WHERE anchor_id = 'two'`).Scan(&twoID))
	before = fingerprint()
	_, err = store.db.ExecContext(ctx, `INSERT INTO intel_edges(src_type, src_row_id, dst_type, dst_row_id, kind) VALUES ('anchor', ?, 'anchor', ?, 'defines')`, oneID, twoID)
	require.NoError(t, err)
	require.Equal(t, before, fingerprint())
	_, err = store.db.ExecContext(ctx, `UPDATE intel_edges SET kind = 'calls' WHERE src_row_id = ? AND dst_row_id = ?`, oneID, twoID)
	require.NoError(t, err)
	afterCalls := fingerprint()
	require.NotEqual(t, before, afterCalls)
	_, err = store.db.ExecContext(ctx, `UPDATE intel_edges SET kind = 'defines' WHERE src_row_id = ? AND dst_row_id = ?`, oneID, twoID)
	require.NoError(t, err)
	afterLeavingCalls := fingerprint()
	require.NotEqual(t, afterCalls, afterLeavingCalls)
	_, err = store.db.ExecContext(ctx, `DELETE FROM intel_edges WHERE src_row_id = ? AND dst_row_id = ?`, oneID, twoID)
	require.NoError(t, err)
	require.Equal(t, afterLeavingCalls, fingerprint())
	_, err = store.db.ExecContext(ctx, `INSERT INTO intel_edges(src_type, src_row_id, dst_type, dst_row_id, kind) VALUES ('anchor', ?, 'anchor', ?, 'calls')`, oneID, twoID)
	require.NoError(t, err)
	afterCallsInsert := fingerprint()
	require.NotEqual(t, afterLeavingCalls, afterCallsInsert)
	_, err = store.db.ExecContext(ctx, `DELETE FROM intel_edges WHERE src_row_id = ? AND dst_row_id = ?`, oneID, twoID)
	require.NoError(t, err)
	require.NotEqual(t, afterCallsInsert, fingerprint())
}

func TestGraphWebFingerprintRollbackAndStableReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "graph-rollback.db")
	store, err := Open(path)
	require.NoError(t, err)

	before, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	tx, err := store.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO files(path, lang, mtime) VALUES ('rolled-back.go', 'go', 1)`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	afterRollback, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, before, afterRollback)
	require.NoError(t, store.Close())

	reopened, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	afterReopen, err := reopened.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, before, afterReopen)
}

func TestGraphWebFingerprintUpgradeV61RetainsRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "graph-v61.db")
	store, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 61)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO files(path, lang, mtime) VALUES ('retained.go', 'go', 1)`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	var rows, version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE path = 'retained.go'`).Scan(&rows))
	require.Equal(t, 1, rows)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)
	_, err = store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
}

func TestValidateGraphRevisionSchemaRejectsAlteredTrigger(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "graph-validation.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.db.ExecContext(ctx, `
		DROP TRIGGER graph_web_revision_files_insert;
		CREATE TRIGGER graph_web_revision_files_insert AFTER INSERT ON files
		BEGIN
			UPDATE graph_web_revision SET revision = revision + 2 WHERE id = 1;
		END
	`)
	require.NoError(t, err)
	tx, err := store.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.ErrorContains(t, validateGraphRevisionSchema(ctx, tx), "invalid graph revision trigger")
}

func TestValidateGraphRevisionSchemaRejectsCaseOnlyLiteralDrift(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "graph-validation-case.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.db.ExecContext(ctx, `
		DROP TRIGGER graph_web_revision_doc_links_insert;
		CREATE TRIGGER graph_web_revision_doc_links_insert
		AFTER insert ON doc_links
		WHEN NEW.src_type = 'CODE'
		BEGIN
			UPDATE graph_web_revision SET revision = revision + 1 WHERE id = 1;
		END
	`)
	require.NoError(t, err)
	before, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO doc_links(src_path, src_type, dst_path, dst_kind, updated_at) VALUES ('src/case.go', 'code', 'anchor', 'anchor', 1)`)
	require.NoError(t, err)
	after, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "the malformed trigger must miss a normal code row")

	tx, err := store.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.ErrorContains(t, validateGraphRevisionSchema(ctx, tx), "invalid graph revision trigger")
}

func TestResetDomainRecreatesGraphRevision(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "graph-reset.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ResetDomain(ctx))
	require.NoError(t, store.ensureSchemaWithRecovery(ctx, currentSchemaVersion))
	before, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO files(path, lang, mtime) VALUES ('after-reset.go', 'go', 1)`)
	require.NoError(t, err)
	after, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
}

func TestGraphWebFingerprintFreshDatabaseGetsNewIncarnation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	store, err := Open(path)
	require.NoError(t, err)
	first, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	require.NoError(t, os.Rename(path, filepath.Join(dir, "old.db")))

	replacement, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = replacement.Close() })
	second, err := replacement.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestGraphWebFingerprintReadOnlyAndCanceled(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "graph-readonly.db")
	store, err := Open(path)
	require.NoError(t, err)
	want, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	readOnly, err := OpenReadOnlyExisting(path, ctx, sqliteutil.Options{MaxOpenConns: 1, MaxIdleConns: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })
	got, err := readOnly.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, want, got)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = readOnly.GraphWebFingerprint(canceled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestGraphWebFingerprintIgnoresNoteMetadataOnlyUpdates(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "graph-notes-metadata.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	fingerprint := func() string {
		t.Helper()
		got, lookupErr := store.GraphWebFingerprint(ctx)
		require.NoError(t, lookupErr)
		return got
	}

	require.NoError(t, store.UpsertNote(ctx, codeanchor.Note{Path: "notes/a.md", Title: "A"}))
	before := fingerprint()

	require.NoError(t, store.TouchNoteMtimes(ctx, map[string]int64{"notes/a.md": 12345}))
	require.Equal(t, before, fingerprint(), "TouchNoteMtimes must not change the graph fingerprint")

	require.NoError(t, store.TouchIntelNotePaths(ctx, map[string]int64{"notes/a.md": 12346}))
	require.Equal(t, before, fingerprint(), "TouchIntelNotePaths must not change the graph fingerprint")

	require.NoError(t, store.UpsertNoteMeta(ctx, "notes/a.md", "hash", "v1", 12347))
	require.Equal(t, before, fingerprint(), "UpsertNoteMeta must not change the graph fingerprint")

	require.NoError(t, store.UpsertNoteMetadataBatch(ctx, map[string]codeanchor.NoteIndexMeta{
		"notes/a.md": {ContentHash: "hash2", IndexerVersion: "v2", Mtime: 12348},
	}))
	require.Equal(t, before, fingerprint(), "UpsertNoteMetadataBatch must not change the graph fingerprint")

	_, err = store.db.ExecContext(ctx, `UPDATE notes SET title = 'Renamed' WHERE path = 'notes/a.md'`)
	require.NoError(t, err)
	afterTitle := fingerprint()
	require.NotEqual(t, before, afterTitle, "a title update must change the graph fingerprint")

	_, err = store.db.ExecContext(ctx, `UPDATE notes SET path = 'notes/b.md' WHERE path = 'notes/a.md'`)
	require.NoError(t, err)
	afterPath := fingerprint()
	require.NotEqual(t, afterTitle, afterPath, "a path update must change the graph fingerprint")

	_, err = store.db.ExecContext(ctx, `INSERT INTO notes(path, title) VALUES ('notes/c.md', 'C')`)
	require.NoError(t, err)
	afterInsert := fingerprint()
	require.NotEqual(t, afterPath, afterInsert, "a note insert must change the graph fingerprint")

	_, err = store.db.ExecContext(ctx, `DELETE FROM notes WHERE path = 'notes/c.md'`)
	require.NoError(t, err)
	require.NotEqual(t, afterInsert, fingerprint(), "a note delete must change the graph fingerprint")
}

func TestGraphWebFingerprintUpgradeV62ScopesNotesUpdateTrigger(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "graph-v62.db")
	store, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 62)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO notes(path, title) VALUES ('retained.md', 'Retained');
		DROP TRIGGER graph_web_revision_notes_update;
		CREATE TRIGGER graph_web_revision_notes_update AFTER update ON notes
		BEGIN
			UPDATE graph_web_revision SET revision = revision + 1 WHERE id = 1;
		END
	`)
	require.NoError(t, err)
	legacyBefore, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NoError(t, store.TouchNoteMtimes(ctx, map[string]int64{"retained.md": 7}))
	legacyAfter, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NotEqual(t, legacyBefore, legacyAfter, "the v62 trigger bumped on metadata-only updates")
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	var rows, version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes WHERE path = 'retained.md'`).Scan(&rows))
	require.Equal(t, 1, rows)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)

	var upgradedSQL string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = 'graph_web_revision_notes_update'`).Scan(&upgradedSQL))
	require.Contains(t, upgradedSQL, "OF path, title")

	tx, err := store.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, validateGraphRevisionSchema(ctx, tx))
	require.NoError(t, tx.Rollback())

	before, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.NoError(t, store.TouchNoteMtimes(ctx, map[string]int64{"retained.md": 99}))
	after, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)

	require.NoError(t, store.Close())
	reopened, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	afterReopen, err := reopened.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, before, afterReopen)
}

func TestValidateGraphRevisionSchemaRejectsUnscopedNotesUpdateTrigger(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "graph-notes-validation.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.db.ExecContext(ctx, `
		DROP TRIGGER graph_web_revision_notes_update;
		CREATE TRIGGER graph_web_revision_notes_update AFTER update ON notes
		BEGIN
			UPDATE graph_web_revision SET revision = revision + 1 WHERE id = 1;
		END
	`)
	require.NoError(t, err)
	tx, err := store.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.ErrorContains(t, validateGraphRevisionSchema(ctx, tx), "invalid graph revision trigger")
}
