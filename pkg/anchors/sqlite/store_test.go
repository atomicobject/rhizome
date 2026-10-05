package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func TestOpenIntegrityCheckPolicyIsDefaultSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "integrity.db")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	collector := indexingperf.New()
	store, err = OpenWithOptions(path, OpenOptions{Context: indexingperf.WithCollector(context.Background(), collector)})
	require.NoError(t, err)
	require.NoError(t, store.Close())
	require.Equal(t, int64(1), agentStartOperation(t, collector, indexingperf.AgentStartOpIntegrityChecks).Count)
	require.True(t, agentStartOperation(t, collector, indexingperf.AgentStartOpIntegrityChecks).Available)
}

func TestOpenCanExplicitlySkipIntegrityCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "integrity.db")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	collector := indexingperf.New()
	store, err = OpenWithOptions(path, OpenOptions{
		Context:            indexingperf.WithCollector(context.Background(), collector),
		SkipIntegrityCheck: true,
	})
	require.NoError(t, err)
	require.NoError(t, store.Close())
	diagnostic := agentStartOperation(t, collector, indexingperf.AgentStartOpIntegrityChecks)
	require.False(t, diagnostic.Available)
	require.Zero(t, diagnostic.Count)
}

func TestOpenReadOnlyExistingDoesNotCreateOrPermitWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "db.sqlite")
	_, err := OpenReadOnlyExisting(path, context.Background(), sqliteutil.Options{})
	require.Error(t, err)
	require.NoFileExists(t, path)
	require.NoDirExists(t, filepath.Dir(path))

	path = filepath.Join(t.TempDir(), "db.sqlite")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	readOnly, err := OpenReadOnlyExisting(path, context.Background(), sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })
	require.Error(t, readOnly.EnsureSession(context.Background(), "session"))
}

func TestOpenReadOnlyExistingSearchesAnExistingVectorTableWithoutDDL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"owner-1"}, []codeanchor.IntelChunk{{
		ChunkID:     "chunk-1",
		OwnerID:     "owner-1",
		OwnerType:   "doc_section",
		Granularity: "section",
		ContentHash: "hash-1",
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"chunk-1": {1, 0, 0, 0},
	}))
	require.NoError(t, store.Close())
	validated, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, validated.Close())

	readOnly, err := OpenReadOnlyExisting(path, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })

	collector := indexingperf.NewSemanticQueryCollector()
	queryCtx := indexingperf.WithCollector(ctx, collector)
	hits, skipped, err := readOnly.SearchEmbeddings(queryCtx, embeddings.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, hits, 1)
	require.Equal(t, "chunk-1", hits[0].ChunkID)
	for _, operation := range collector.SemanticQueryDiagnostics().Operations {
		if operation.Label == indexingperf.SemanticQueryOpSchemaStatements {
			require.True(t, operation.Available)
			require.Zero(t, operation.Count)
			return
		}
	}
	t.Fatal("schema statement diagnostic not found")
}

func TestOpenReadOnlyExistingDoesNotCreateAMissingVectorTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	readOnly, err := OpenReadOnlyExisting(path, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })
	collector := indexingperf.NewSemanticQueryCollector()
	_, _, err = readOnly.SearchEmbeddings(indexingperf.WithCollector(ctx, collector), embeddings.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{})
	require.ErrorContains(t, err, "run `rzm index`")
	exists, err := readOnly.tableExists(ctx, intelVecTableName(4))
	require.NoError(t, err)
	require.False(t, exists)
	for _, operation := range collector.SemanticQueryDiagnostics().Operations {
		if operation.Label == indexingperf.SemanticQueryOpSchemaStatements {
			require.True(t, operation.Available)
			require.Zero(t, operation.Count)
			return
		}
	}
	t.Fatal("schema statement diagnostic not found")
}

func TestOpenSessionStoreExistingDoesNotCreateAndPermitsSessionWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "db.sqlite")
	_, err := OpenSessionStoreExisting(path, context.Background(), sqliteutil.Options{})
	require.Error(t, err)
	require.NoFileExists(t, path)
	require.NoDirExists(t, filepath.Dir(path))

	path = filepath.Join(t.TempDir(), "db.sqlite")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	sessionStore, err := OpenSessionStoreExisting(path, context.Background(), sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sessionStore.Close() })
	require.NoError(t, sessionStore.EnsureSession(context.Background(), "session"))
}

func TestSessionStoreExistingFailsFastUnderWriterContention(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "intel.db")
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sessionStore, err := OpenSessionStoreExisting(path, ctx, sqliteutil.Options{MaxOpenConns: 1, MaxIdleConns: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sessionStore.Close() })

	conn, err := store.db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") })

	start := time.Now()
	err = sessionStore.EnsureSession(ctx, "contended")
	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)
}

func agentStartOperation(t *testing.T, collector *indexingperf.Collector, label string) indexingperf.AgentStartOperationDiagnostic {
	t.Helper()
	for _, operation := range collector.AgentStartDiagnostics().Operations {
		if operation.Label == label {
			return operation
		}
	}
	t.Fatalf("operation %q not found", label)
	return indexingperf.AgentStartOperationDiagnostic{}
}

func TestOpenCorruptDatabaseDoesNotUseValidationProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	require.NoError(t, os.WriteFile(path, []byte("not a sqlite database"), 0o600))
	_, err := Open(path)
	require.Error(t, err)
}

func TestOpenWarmCurrentUsesValidatedSchemaProbe(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "warm.db")
	store, err := Open(path)
	require.NoError(t, err)

	// Make the next open take the full path and insert a proof whose observed
	// schema_version includes this trigger. A subsequent full-path refresh would
	// abort on UPDATE; the validated probe must therefore be what permits open.
	_, err = store.db.ExecContext(ctx, `DELETE FROM rzm_migration_validation WHERE domain = 'intel'`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		CREATE TRIGGER reject_intel_validation_refresh
		BEFORE UPDATE ON rzm_migration_validation
		WHEN OLD.domain = 'intel'
		BEGIN
			SELECT RAISE(ABORT, 'unexpected full schema validation');
		END
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err, "missing proof must fall back and create a fresh proof")
	require.NoError(t, store.Close())

	collector := indexingperf.New()
	store, err = OpenWithOptions(path, OpenOptions{
		Context: indexingperf.WithCollector(ctx, collector),
	})
	require.NoError(t, err, "matching current proof must skip full structural validation")
	require.NoError(t, store.Close())
	var schemaStatements int64
	for _, operation := range collector.AgentStartDiagnostics().Operations {
		if operation.Label == indexingperf.AgentStartOpSchemaStatements {
			schemaStatements = operation.Count
		}
	}
	require.Equal(t, int64(2), schemaStatements, "warm current open must use exactly two schema-probe statements")
}

func TestOpenExternalSchemaChangeInvalidatesValidatedSchemaProbe(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "external-change.db")
	store, err := Open(path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		CREATE TRIGGER reject_external_change_proof_refresh
		BEFORE UPDATE ON rzm_migration_validation
		WHEN OLD.domain = 'intel'
		BEGIN
			SELECT RAISE(ABORT, 'full validation fallback observed');
		END
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	_, err = Open(path)
	require.ErrorContains(t, err, "full validation fallback observed",
		"schema_version mismatch must take the authoritative full path")
}

func TestOpenConcurrentWarmCurrentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-warm.db")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	const workers = 8
	errCh := make(chan error, workers)
	for range workers {
		go func() {
			opened, openErr := Open(path)
			if openErr == nil {
				openErr = opened.Close()
			}
			errCh <- openErr
		}()
	}
	for range workers {
		require.NoError(t, <-errCh)
	}
}

func TestOpenRepairsLegacyAnnotationsBeforeCreatingDependentIndexes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-annotations.db")
	store, err := Open(path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `DROP INDEX idx_annotations_owner`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `DROP INDEX idx_annotations_type_owner`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `ALTER TABLE annotations DROP COLUMN owner_fqn`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	hasOwner, err := store.tableHasColumn(ctx, "annotations", "owner_fqn")
	require.NoError(t, err)
	require.True(t, hasOwner)
}

func TestAnchorsMatchingSymbols_BatchedQuery(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO anchors(id, label, kind, base_lang, base_pkg, base_name)
		VALUES
			(1, 'a', 'baseClass', 'py', 'svc', 'Alpha'),
			(2, 'b', 'baseClass', 'py', 'svc', 'Beta'),
			(3, 'c', 'baseClass', 'ts', 'svc', 'Alpha')
	`)
	require.NoError(t, err)

	names := []string{"Alpha", "Beta", "Alpha"} // duplicate pair should dedupe
	pkgs := []string{"svc", "svc", "svc"}

	ids, err := store.AnchorsMatchingSymbols(ctx, names, pkgs, codeanchor.LangPy)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{1, 2}, ids)
}

func TestOpen_UpgradesV56WithMarkdownTargetSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "markdown-target-v57.db")
	store, err := Open(path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO index_metadata(key, value) VALUES ('preserved', 'yes')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		DROP TABLE note_fragment_targets;
		UPDATE schema_version SET version = 56;
		UPDATE rzm_migration_state SET version = 56 WHERE domain = 'intel';
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for _, name := range []string{
		"note_fragment_targets",
		"idx_note_fragment_targets_note",
		"idx_note_fragment_targets_lookup",
	} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count))
		require.Equal(t, 1, count, name)
	}
	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)
	var preserved string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT value FROM index_metadata WHERE key = 'preserved'`).Scan(&preserved))
	require.Equal(t, "yes", preserved)
}

func TestOpen_UpgradesV60WithNoteProjectionProvenance(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "note-projection-v61.db")
	store, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 60)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO index_metadata(key, value) VALUES ('preserved', 'yes');
		INSERT INTO notes(path, title, content_hash, mtime, size, indexed_at)
		VALUES ('notes/existing.md', 'Existing', 'existing-hash', 7, 11, 13);
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var formatID, status, providerVersion, projectionVersion, sourceHash string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT n.format_id, p.status, p.provider_version, p.projection_version, p.source_content_hash
		FROM notes n
		JOIN note_projection_state p ON p.note_id = n.id
		WHERE n.path = 'notes/existing.md'
	`).Scan(&formatID, &status, &providerVersion, &projectionVersion, &sourceHash))
	require.Equal(t, "markdown", formatID)
	require.Equal(t, "stale", status)
	require.Empty(t, providerVersion)
	require.Empty(t, projectionVersion)
	require.Empty(t, sourceHash, "the migration must not claim legacy parser output is current")

	var preserved string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT value FROM index_metadata WHERE key = 'preserved'`).Scan(&preserved))
	require.Equal(t, "yes", preserved)
	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)
}

func TestOpen_UpgradesV64ToGenericProjectionFacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "note-projection-facts-v65.db")
	store, err := Open(path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO notes(path, title, content_hash, mtime, size, indexed_at, format_id)
		VALUES ('notes/existing.md', 'Existing', 'hash', 1, 1, 1, 'markdown');
		DROP TABLE note_fragment_targets;
		DROP TABLE note_projection_diagnostics;
		DROP TABLE note_search_regions;
		CREATE TABLE note_markdown_targets (
			note_id INTEGER NOT NULL,
			target_kind TEXT NOT NULL CHECK (target_kind IN ('heading', 'block')),
			target_text TEXT NOT NULL CHECK (target_text != ''),
			target_norm TEXT NOT NULL CHECK (target_norm != ''),
			ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) STRICT;
		INSERT INTO note_markdown_targets(note_id, target_kind, target_text, target_norm, ordinal)
		SELECT id, 'heading', 'Preserved', 'preserved', 1 FROM notes WHERE path = 'notes/existing.md';
		UPDATE schema_version SET version = 64;
		UPDATE rzm_migration_state SET version = 64 WHERE domain = 'intel';
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for _, name := range []string{"note_fragment_targets", "note_projection_diagnostics", "note_search_regions"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count))
		require.Equal(t, 1, count, name)
	}
	var oldCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'note_markdown_targets'`).Scan(&oldCount))
	require.Zero(t, oldCount)
	targets, err := store.CurrentNoteFragmentTargets(ctx, []string{"notes/existing.md"}, NoteFragmentTargetHeading, "preserved")
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, "Preserved", targets[0].Target)
}

func TestOpen_RecoversNoteProjectionStateSchemaDrift(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "note-projection-drift.db")
	store, err := Open(path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		DROP TABLE note_projection_state;
		CREATE TABLE note_projection_state (
			note_id INTEGER PRIMARY KEY,
			provider_version TEXT NOT NULL DEFAULT '',
			projection_version TEXT NOT NULL DEFAULT '',
			source_content_hash TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'stale',
			diagnostic_code TEXT NOT NULL DEFAULT '',
			diagnostic_detail TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT 0
		);
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var ddl string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'note_projection_state'
	`).Scan(&ddl))
	require.Contains(t, strings.ToUpper(ddl), "STRICT")
	require.Contains(t, strings.ToUpper(ddl), "FOREIGN KEY")
	require.Contains(t, strings.ToUpper(ddl), "CHECK")
}

func TestOpen_UpgradesV57WithOntologyMaterializationVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ontology-materialization-v58.db")
	store, err := Open(path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO index_metadata(key, value) VALUES ('preserved', 'yes');
		INSERT INTO ontology_schema_state (
			schema_hash, notes_hash, materialization_version, loaded_at, ready, error_json
		) VALUES ('schema', 'notes', 1, 1, 1, '[]');
		ALTER TABLE ontology_schema_state DROP COLUMN materialization_version;
		UPDATE schema_version SET version = 57;
		UPDATE rzm_migration_state SET version = 57 WHERE domain = 'intel';
		DELETE FROM rzm_migration_log WHERE domain = 'intel' AND from_version >= 57;
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)
	var materializationVersion int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT materialization_version FROM ontology_schema_state`).Scan(&materializationVersion))
	require.Zero(t, materializationVersion, "migrated rows must force one ontology rebuild")
	var preserved string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT value FROM index_metadata WHERE key = 'preserved'`).Scan(&preserved))
	require.Equal(t, "yes", preserved)
}

func TestOpen_ReconcilesIncompatibleV59SchemasWithoutReset(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		dropStmt string
	}{
		{
			name: "main-v59-without-release-session-tables",
			dropStmt: `
				DROP TABLE mcp_session_item_reservations;
				DROP TABLE mcp_session_maintenance;
			`,
		},
		{
			name: "release-v59-without-external-target-tables",
			dropStmt: `
				DROP TABLE intel_external_symbol_evidence;
				DROP TABLE intel_external_import_evidence;
				DROP TABLE intel_external_target_map;
				DROP TABLE intel_external_targets;
			`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incompatible-v59.db")
			store, err := Open(path)
			require.NoError(t, err)
			_, err = store.db.ExecContext(ctx, `INSERT INTO index_metadata(key, value) VALUES ('preserved', 'yes')`)
			require.NoError(t, err)
			_, err = store.db.ExecContext(ctx, tc.dropStmt+`
				UPDATE schema_version SET version = 59;
				UPDATE rzm_migration_state SET version = 59 WHERE domain = 'intel';
				DELETE FROM rzm_migration_log WHERE domain = 'intel' AND from_version >= 59;
			`)
			require.NoError(t, err)
			require.NoError(t, store.Close())

			store, err = Open(path)
			require.NoError(t, err)
			defer func() { _ = store.Close() }()

			var version, preserved int
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
			require.Equal(t, currentSchemaVersion, version)
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM index_metadata WHERE key = 'preserved' AND value = 'yes'`).Scan(&preserved))
			require.Equal(t, 1, preserved, "the v59 reconciliation must not reset indexed state")
			for _, table := range []string{
				"intel_external_targets",
				"intel_external_target_map",
				"intel_external_symbol_evidence",
				"intel_external_import_evidence",
				"mcp_session_item_reservations",
				"mcp_session_maintenance",
			} {
				exists, tableErr := store.tableExists(ctx, table)
				require.NoError(t, tableErr)
				require.True(t, exists, table)
			}
		})
	}
}

func TestOpen_ResetsObsoleteIntelBaselineAndPreservesEmbeddingDomains(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "obsolete-v55.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE schema_version(version INTEGER NOT NULL);
		INSERT INTO schema_version(version) VALUES (55);
		CREATE TABLE rzm_migration_state(
			domain TEXT PRIMARY KEY,
			version INTEGER NOT NULL,
			dirty INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		);
		INSERT INTO rzm_migration_state(domain, version, dirty, updated_at) VALUES
			('intel', 55, 0, 1),
			('note_embeddings', 7, 0, 1),
			('code_embeddings', 9, 0, 1);
		CREATE TABLE files(path TEXT PRIMARY KEY, lang TEXT NOT NULL);
		INSERT INTO files(path, lang) VALUES ('stale.go', 'go');
		CREATE TABLE intel_chunks(chunk_id TEXT PRIMARY KEY);
		INSERT INTO intel_chunks VALUES ('old-card-chunk');
		CREATE TABLE ontology_node_embedding_state(node_id TEXT PRIMARY KEY);
		INSERT INTO ontology_node_embedding_state VALUES ('old-node');
		CREATE TABLE indexed_card_specs(id TEXT);
		CREATE TABLE indexed_cards(id TEXT);
		CREATE TABLE indexed_card_facts(id TEXT);
		CREATE TABLE indexed_card_context_targets(id TEXT);
		CREATE TABLE indexed_card_context_diagnostics(id TEXT);
		CREATE TABLE file_symbols(id TEXT);
		INSERT INTO indexed_card_specs VALUES ('old');
		INSERT INTO indexed_cards VALUES ('old');
		INSERT INTO indexed_card_facts VALUES ('old');
		INSERT INTO indexed_card_context_targets VALUES ('old');
		INSERT INTO indexed_card_context_diagnostics VALUES ('old');
		INSERT INTO file_symbols VALUES ('old');
		CREATE TABLE emb_sentinel(id INTEGER PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO emb_sentinel(id, value) VALUES (1, 'note');
		CREATE TABLE code_sentinel(id INTEGER PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO code_sentinel(id, value) VALUES (1, 'code');
	`)
	require.NoError(t, err)
	for _, table := range []string{"intel_chunks", "ontology_node_embedding_state", "indexed_card_specs", "indexed_cards", "indexed_card_facts", "indexed_card_context_targets", "indexed_card_context_diagnostics", "file_symbols"} {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Equal(t, 1, count, "obsolete fixture: "+table)
	}
	require.NoError(t, db.Close())

	store, err := Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainIntel)).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)
	var migrationSteps int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rzm_migration_log WHERE domain = ?`, string(migration.DomainIntel)).Scan(&migrationSteps))
	require.Equal(t, currentSchemaVersion-intelBaselineVersion, migrationSteps, "the consolidated baseline should be followed by every forward migration")
	for table, want := range map[string]string{"emb_sentinel": "note", "code_sentinel": "code"} {
		var got string
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT value FROM `+table+` WHERE id = 1`).Scan(&got))
		require.Equal(t, want, got)
	}
	for domain, want := range map[string]int{"note_embeddings": 7, "code_embeddings": 9} {
		var got int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = ?`, domain).Scan(&got))
		require.Equal(t, want, got)
	}
	var stale int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE path = 'stale.go'`).Scan(&stale))
	require.Zero(t, stale)
	for _, table := range []string{"intel_chunks", "ontology_node_embedding_state"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count, table)
	}
	for _, table := range []string{"indexed_card_specs", "indexed_cards", "indexed_card_facts", "indexed_card_context_targets", "indexed_card_context_diagnostics", "file_symbols"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count))
		require.Zero(t, count, table)
	}
	for _, column := range []string{"content_hash", "indexer_version", "mtime"} {
		has, err := store.tableHasColumn(ctx, "notes", column)
		require.NoError(t, err)
		require.True(t, has, column)
	}
	var rationaleDDL string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'intel_rationale'`).Scan(&rationaleDDL))
	require.Contains(t, strings.ToUpper(rationaleDDL), "STRICT")
	require.Contains(t, strings.ToUpper(rationaleDDL), "WITHOUT ROWID")
	for _, name := range []string{"idx_intel_rationale_path", "idx_intel_rationale_symbol", "idx_intel_rationale_kind"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'index' AND tbl_name = 'intel_rationale' AND name = ?`, name).Scan(&count))
		require.Equal(t, 1, count, name)
	}
}

func TestOpen_ResetsDirtyCurrentIntelBaseline(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "dirty-v56.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO files(path, lang) VALUES ('stale.go', 'go')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `UPDATE rzm_migration_state SET dirty = 1 WHERE domain = ?`, string(migration.DomainIntel))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	var dirty, stale int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT dirty FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainIntel)).Scan(&dirty))
	require.Zero(t, dirty)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE path = 'stale.go'`).Scan(&stale))
	require.Zero(t, stale)
}

func TestOpen_PreservesValidCurrentIntelBaseline(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "valid-v56.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO files(path, lang) VALUES ('kept.go', 'go')`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	var kept int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE path = 'kept.go'`).Scan(&kept))
	require.Equal(t, 1, kept)
}

func TestOpen_FutureIntelBaselineFailsWithoutMutation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "future-intel.db")
	futureVersion := currentSchemaVersion + 1
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE schema_version(version INTEGER NOT NULL);
		INSERT INTO schema_version(version) VALUES (?);
		CREATE TABLE future_sentinel(id INTEGER PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO future_sentinel(id, value) VALUES (1, 'untouched');
	`, futureVersion)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Open(dbPath)
	var future *migration.ErrFutureSchema
	require.ErrorAs(t, err, &future)
	require.Equal(t, futureVersion, future.Current)

	db, err = sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()
	var got string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT value FROM future_sentinel WHERE id = 1`).Scan(&got))
	require.Equal(t, "untouched", got)
	var metadataTables int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'rzm_migration_state'`).Scan(&metadataTables))
	require.Zero(t, metadataTables)
}

func TestOpen_RepairsLegacySchemaWithMissingPathPrefix(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL);`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (?);`, currentSchemaVersion)
	require.NoError(t, err)

	// Simulate a legacy anchors table that predates the path_prefix column.
	_, err = db.ExecContext(ctx, `
		CREATE TABLE anchors (
			id INTEGER PRIMARY KEY,
			label TEXT NOT NULL UNIQUE,
			kind TEXT NOT NULL,
			lang TEXT,
			base_lang TEXT,
			base_pkg TEXT,
			base_name TEXT,
			ann_lang TEXT,
			ann_pkg TEXT,
			ann_name TEXT,
			ann_args_json TEXT
		);
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	has, err := store.tableHasColumn(ctx, "anchors", "path_prefix")
	require.NoError(t, err)
	require.True(t, has)
}

func TestOpen_ResetsDomainOnSchemaDriftAtCurrentVersion(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy-note-mtime.db")

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL);`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (?);`, currentSchemaVersion)
	require.NoError(t, err)

	// Simulate a legacy notes table that predates v25 metadata columns.
	_, err = db.ExecContext(ctx, `
		CREATE TABLE notes (
			id INTEGER PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			title TEXT
		);
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO notes(path, title) VALUES ('note-a.md', 'A'), ('note-b.md', 'B');`)
	require.NoError(t, err)

	// Simulate existing intel sections + notes rows that are incompatible with current schema.
	_, err = db.ExecContext(ctx, `
		CREATE TABLE intel_doc_sections (
			section_id TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			title TEXT,
			level INTEGER NOT NULL DEFAULT 0,
			start_byte INTEGER NOT NULL DEFAULT 0,
			end_byte INTEGER NOT NULL DEFAULT 0,
			content TEXT NOT NULL DEFAULT '',
			fingerprint TEXT,
			updated_at INTEGER
		);
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO intel_doc_sections(section_id, path, content, updated_at) VALUES
			('s1', 'note-a.md', 'x', 100),
			('s2', 'note-a.md', 'y', 350),
			('s3', 'note-b.md', 'z', 210);
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	hasHash, err := store.tableHasColumn(ctx, "notes", "content_hash")
	require.NoError(t, err)
	require.True(t, hasHash)
	hasVersion, err := store.tableHasColumn(ctx, "notes", "indexer_version")
	require.NoError(t, err)
	require.True(t, hasVersion)
	hasMtime, err := store.tableHasColumn(ctx, "notes", "mtime")
	require.NoError(t, err)
	require.True(t, hasMtime)

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes`).Scan(&count))
	require.Equal(t, 0, count, "expected drifted intel domain to be reset")
}

func TestOpen_ResetsDomainOnSchemaErrorPreservingEmbeddings(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "broken-reset.db")

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `CREATE TABLE schema_version (version INTEGER NOT NULL);`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (?);`, currentSchemaVersion)
	require.NoError(t, err)

	// Create a view that shadows a table the store expects; subsequent index creation will fail.
	_, err = db.ExecContext(ctx, `CREATE VIEW symbols AS SELECT 1 AS id, 'py' AS lang, 'func' AS kind, 'x.go' AS file, 'pkg' AS pkg, 'fn' AS name, 'pkg.fn' AS fqn;`)
	require.NoError(t, err)

	// Sentinel table representing code embeddings (should NOT be dropped by a code-index domain reset).
	_, err = db.ExecContext(ctx, `CREATE TABLE code_items (id INTEGER PRIMARY KEY, anchor_id TEXT);`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO code_items(id, anchor_id) VALUES (1, 'anchor-1');`)
	require.NoError(t, err)

	require.NoError(t, db.Close())

	store, err := Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var typ string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT type FROM sqlite_master WHERE name = 'symbols'`).Scan(&typ))
	require.Equal(t, "table", typ)

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_items`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestOpen_SchemaDriftResetRebuildsSessionReservationTables(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "repair-session-reservations.db")

	store, err := Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, store.MarkSessionItem(ctx, "sess", "committed", "fingerprint"))
	_, err = store.db.ExecContext(ctx, `
		DROP TABLE mcp_session_item_reservations;
		DROP TABLE mcp_session_maintenance;
		CREATE TABLE mcp_session_item_reservations (session_id TEXT PRIMARY KEY);
		CREATE TABLE mcp_session_maintenance (maintenance_key TEXT PRIMARY KEY);
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	for table, columns := range map[string][]string{
		"mcp_session_item_reservations": {"session_id", "item_key", "fingerprint", "reservation_id", "reserved_at"},
		"mcp_session_maintenance":       {"maintenance_key", "next_due_at"},
	} {
		for _, column := range columns {
			hasColumn, columnErr := store.tableHasColumn(ctx, table, column)
			require.NoError(t, columnErr)
			require.True(t, hasColumn, "%s.%s", table, column)
		}
	}

	fingerprint, ok, err := store.SessionItemFingerprint(ctx, "sess", "committed")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "fingerprint", fingerprint, "committed session history must survive Intel recovery")
}

func TestIndexedFilePaths_ReturnsPaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "files.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = store.db.ExecContext(ctx, `INSERT INTO files(path, lang, hash, mtime) VALUES (?, ?, ?, ?)`, "a/b.py", "py", "h", 1)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO files(path, lang, hash, mtime) VALUES (?, ?, ?, ?)`, "a/a.py", "py", "h", 1)
	require.NoError(t, err)

	paths, err := store.IndexedFilePaths(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"a/a.py", "a/b.py"}, paths)
}

func TestFilesDefiningSymbolFQN_ReturnsFiles(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "symfiles.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = store.db.ExecContext(ctx, `INSERT INTO symbols(lang, kind, file, pkg, name, fqn) VALUES (?, ?, ?, ?, ?, ?)`,
		"py", "func", "a/c.py", "pkg", "add_task", "pkg.add_task")
	require.NoError(t, err)

	files, err := store.FilesDefiningSymbolFQN(ctx, "pkg.add_task")
	require.NoError(t, err)
	require.Equal(t, []string{"a/c.py"}, files)
}

func TestCodeFilesForNote_ResolvesAllScopes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "notefiles.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Indexed code files.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO files(path, lang, hash, mtime) VALUES
			('pkg/alpha/service.go', 'go', 'h1', 1),
			('pkg/alpha/helper.go', 'go', 'h2', 1),
			('pkg/beta/other.go', 'go', 'h3', 1),
			('scripts/tool.py', 'py', 'h4', 1)
	`)
	require.NoError(t, err)

	// Symbols table — resolves symbol_fqn → file.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn) VALUES
			('go', 'func', 'pkg/alpha/service.go', 'pkg/alpha', 'Run', 'pkg/alpha.Run'),
			('py', 'func', 'scripts/tool.py', 'scripts', 'do_it', 'scripts.tool.do_it')
	`)
	require.NoError(t, err)

	// One note with four anchors: symbol, call, dir, glob.
	_, err = store.db.ExecContext(ctx, `INSERT INTO notes(id, path, title) VALUES (1, 'docs/hub.md', 'Hub')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO anchors(id, label, kind, base_lang, base_pkg, base_name, path_prefix) VALUES
			(10, 'sym',   'function', 'go', '', '', ''),
			(11, 'call',  'calls',    'go', '', '', ''),
			(12, 'dir',   'path',     '',   '', '', 'pkg/beta'),
			(13, 'glob',  'glob',     '',   '', '', '')
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO note_anchors(note_id, anchor_id) VALUES (1, 10), (1, 11), (1, 12), (1, 13)
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO anchor_scopes(anchor_id, symbol_fqn, call_file) VALUES
			(10, 'pkg/alpha.Run', NULL),
			(11, NULL, 'pkg/alpha/helper.go')
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO anchor_globs(anchor_id, pattern) VALUES (13, 'scripts/**/*.py')
	`)
	require.NoError(t, err)

	files, err := store.CodeFilesForNote(ctx, "docs/hub.md")
	require.NoError(t, err)
	require.Equal(t, []string{
		"pkg/alpha/helper.go",  // call
		"pkg/alpha/service.go", // symbol
		"pkg/beta/other.go",    // dir prefix
		"scripts/tool.py",      // glob
	}, files)

	// An unrelated note returns nothing.
	other, err := store.CodeFilesForNote(ctx, "docs/unrelated.md")
	require.NoError(t, err)
	require.Empty(t, other)
}

// TestCodeAnchorUpsert_DoesNotClobberMetadataTitle pins the invariant that the
// code-anchor indexer's note upserts never overwrite a title previously written
// by the note metadata indexer. The metadata indexer extracts rich titles
// (frontmatter title or first H1), whereas the code-anchor path only knows the
// file basename. A prior regression caused e.g.
// `docs/efforts/call-edge-reverse-index.md` to show up as "plan" in the UI
// after a code-anchor reindex clobbered the H1-derived title.
func TestCodeAnchorUpsert_DoesNotClobberMetadataTitle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "title-preservation.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	const (
		notePath = "docs/efforts/call-edge-reverse-index.md"
		// Rich title the metadata indexer would have derived from the H1.
		richTitle = "Implementation Plan: Call Edge Reverse Index"
		// Weak title the code-anchor indexer derives (basename-ish).
		weakTitle = "plan"
	)

	// Simulate the metadata indexer writing the rich title first.
	_, err = store.db.ExecContext(ctx,
		`INSERT INTO notes(path, title) VALUES (?, ?)`,
		notePath, richTitle,
	)
	require.NoError(t, err)

	// The single-note path (UpsertNote) must not clobber the existing title.
	err = store.UpsertNote(ctx, codeanchor.Note{
		Path:  notePath,
		Title: weakTitle,
	})
	require.NoError(t, err)

	var got string
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT title FROM notes WHERE path = ?`, notePath,
	).Scan(&got))
	require.Equal(t, richTitle, got, "UpsertNote must not overwrite title")

	// The batch path (UpsertNotesWithCleanupBatch) is the hot path used during
	// full code-anchor reindexes and must also preserve the metadata title.
	_, err = store.UpsertNotesWithCleanupBatch(ctx, []codeanchor.NoteWithKeepLabels{
		{
			Note: codeanchor.Note{
				Path:  notePath,
				Title: weakTitle,
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT title FROM notes WHERE path = ?`, notePath,
	).Scan(&got))
	require.Equal(t, richTitle, got, "UpsertNotesWithCleanupBatch must not overwrite title")

	// A brand-new note (no prior row) should still get its code-anchor title
	// so new notes aren't left titleless until metadata catches up.
	const newPath = "docs/code-anchors/new-note.md"
	err = store.UpsertNote(ctx, codeanchor.Note{
		Path:  newPath,
		Title: "new-note",
	})
	require.NoError(t, err)

	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT title FROM notes WHERE path = ?`, newPath,
	).Scan(&got))
	require.Equal(t, "new-note", got, "UpsertNote must seed title for brand-new notes")
}

func TestSymbolsBySuffix(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "suffix.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Insert test symbols with reversed FQNs.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn, fqn_reversed)
		VALUES
			('py', 'class', 'backend/src/myapp/models.py', 'backend.src.myapp.models', 'User', 'backend.src.myapp.models.User', ?),
			('py', 'class', 'myapp/models.py', 'myapp.models', 'User', 'myapp.models.User', ?),
			('py', 'class', 'tests/models.py', 'tests.models', 'MockUser', 'tests.models.MockUser', ?),
			('go', 'type', 'pkg/models/user.go', 'pkg/models', 'User', 'pkg/models.User', ?)
	`,
		codeanchor.ReverseString("backend.src.myapp.models.User"),
		codeanchor.ReverseString("myapp.models.User"),
		codeanchor.ReverseString("tests.models.MockUser"),
		codeanchor.ReverseString("pkg/models.User"),
	)
	require.NoError(t, err)

	// Test suffix match: should find both User symbols but not MockUser.
	syms, err := store.SymbolsBySuffix(ctx, "models.User", codeanchor.LangPy, 10)
	require.NoError(t, err)
	require.Len(t, syms, 2)
	// Shortest FQN should be first (most specific match).
	require.Equal(t, "myapp.models.User", syms[0].FQN)
	require.Equal(t, "backend.src.myapp.models.User", syms[1].FQN)

	// Test with no matches.
	syms, err = store.SymbolsBySuffix(ctx, "nonexistent.Class", codeanchor.LangPy, 10)
	require.NoError(t, err)
	require.Empty(t, syms)

	// Test empty suffix returns nil.
	syms, err = store.SymbolsBySuffix(ctx, "", codeanchor.LangPy, 10)
	require.NoError(t, err)
	require.Nil(t, syms)
}

func TestSymbolsBySuffix_SymbolNameOnly(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "suffix_name.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Insert symbols where we want to match by name alone.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn, fqn_reversed)
		VALUES
			('py', 'class', 'myapp/models.py', 'myapp.models', 'User', 'myapp.models.User', ?),
			('py', 'class', 'myapp/models.py', 'myapp.models', 'UserProfile', 'myapp.models.UserProfile', ?),
			('py', 'class', 'myapp/admin.py', 'myapp.admin', 'AdminUser', 'myapp.admin.AdminUser', ?)
	`,
		codeanchor.ReverseString("myapp.models.User"),
		codeanchor.ReverseString("myapp.models.UserProfile"),
		codeanchor.ReverseString("myapp.admin.AdminUser"),
	)
	require.NoError(t, err)

	// Searching for "User" should match User and AdminUser (ends with User).
	// But NOT UserProfile (User is prefix, not suffix).
	syms, err := store.SymbolsBySuffix(ctx, "User", codeanchor.LangPy, 10)
	require.NoError(t, err)
	require.Len(t, syms, 2, "expected User and AdminUser")

	fqns := make([]string, len(syms))
	for i, s := range syms {
		fqns[i] = s.FQN
	}
	require.Contains(t, fqns, "myapp.models.User")
	require.Contains(t, fqns, "myapp.admin.AdminUser")
	require.NotContains(t, fqns, "myapp.models.UserProfile")
}

func TestSymbolsBySuffix_LanguageFiltering(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "suffix_lang.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Insert symbols in different languages with same name.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn, fqn_reversed)
		VALUES
			('py', 'class', 'myapp/models.py', 'myapp.models', 'User', 'myapp.models.User', ?),
			('go', 'type', 'pkg/models/user.go', 'pkg/models', 'User', 'pkg/models.User', ?),
			('ts', 'class', 'src/models/user.ts', 'src/models', 'User', 'src/models.User', ?)
	`,
		codeanchor.ReverseString("myapp.models.User"),
		codeanchor.ReverseString("pkg/models.User"),
		codeanchor.ReverseString("src/models.User"),
	)
	require.NoError(t, err)

	// Filter by Python.
	syms, err := store.SymbolsBySuffix(ctx, "User", codeanchor.LangPy, 10)
	require.NoError(t, err)
	require.Len(t, syms, 1)
	require.Equal(t, "myapp.models.User", syms[0].FQN)

	// Filter by Go.
	syms, err = store.SymbolsBySuffix(ctx, "User", codeanchor.LangGo, 10)
	require.NoError(t, err)
	require.Len(t, syms, 1)
	require.Equal(t, "pkg/models.User", syms[0].FQN)

	// No language filter returns all.
	syms, err = store.SymbolsBySuffix(ctx, "User", "", 10)
	require.NoError(t, err)
	require.Len(t, syms, 3)
}

func TestSymbolsBySuffix_LimitBehavior(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "suffix_limit.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Insert many symbols.
	for i := 0; i < 20; i++ {
		fqn := fmt.Sprintf("pkg%d.models.User", i)
		_, err = store.db.ExecContext(ctx, `
			INSERT INTO symbols(lang, kind, file, pkg, name, fqn, fqn_reversed)
			VALUES ('py', 'class', ?, ?, 'User', ?, ?)
		`, fmt.Sprintf("pkg%d/models.py", i), fmt.Sprintf("pkg%d.models", i), fqn, codeanchor.ReverseString(fqn))
		require.NoError(t, err)
	}

	// Limit should cap results.
	syms, err := store.SymbolsBySuffix(ctx, "User", codeanchor.LangPy, 5)
	require.NoError(t, err)
	require.Len(t, syms, 5)

	// Default limit (0 treated as 10).
	syms, err = store.SymbolsBySuffix(ctx, "User", codeanchor.LangPy, 0)
	require.NoError(t, err)
	require.Len(t, syms, 10)
}

func TestSymbolsBySuffix_PartialPackageSuffix(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "suffix_partial.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Test that partial package paths work correctly.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn, fqn_reversed)
		VALUES
			('py', 'class', 'a/b/c/service.py', 'a.b.c.service', 'MyService', 'a.b.c.service.MyService', ?),
			('py', 'class', 'x/c/service.py', 'x.c.service', 'MyService', 'x.c.service.MyService', ?),
			('py', 'class', 'service.py', 'service', 'MyService', 'service.MyService', ?)
	`,
		codeanchor.ReverseString("a.b.c.service.MyService"),
		codeanchor.ReverseString("x.c.service.MyService"),
		codeanchor.ReverseString("service.MyService"),
	)
	require.NoError(t, err)

	// Full suffix: "service.MyService" should match all three.
	syms, err := store.SymbolsBySuffix(ctx, "service.MyService", codeanchor.LangPy, 10)
	require.NoError(t, err)
	require.Len(t, syms, 3)

	// Partial suffix: "c.service.MyService" should match two (a.b.c... and x.c...).
	syms, err = store.SymbolsBySuffix(ctx, "c.service.MyService", codeanchor.LangPy, 10)
	require.NoError(t, err)
	require.Len(t, syms, 2)

	// Symbol only: "MyService" matches all.
	syms, err = store.SymbolsBySuffix(ctx, "MyService", codeanchor.LangPy, 10)
	require.NoError(t, err)
	require.Len(t, syms, 3)
}
