package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpen_UpgradesV63WithOntologyAssessmentFlags(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ontology-assessment-flags-v64.db")
	store, err := Open(path)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO index_metadata(key, value) VALUES ('preserved', 'yes');
		INSERT INTO ontology_schema_state (
			schema_hash, notes_hash, materialization_version, loaded_at, ready, error_json
		) VALUES ('schema', 'notes', 3, 1, 1, '[]');
		INSERT INTO ontology_note_assessments (
			note_path, declared_type, resolved_type, assessment_json, schema_hash, updated_at
		) VALUES ('notes/legacy.md', 'Decision', 'Decision', '{"notePath":"notes/legacy.md"}', 'schema', 5);
		ALTER TABLE ontology_note_assessments DROP COLUMN has_issues;
		ALTER TABLE ontology_note_assessments DROP COLUMN type_ambiguous;
		UPDATE schema_version SET version = 63;
		UPDATE rzm_migration_state SET version = 63 WHERE domain = 'intel';
		DELETE FROM rzm_migration_log WHERE domain = 'intel' AND from_version >= 63;
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)

	rows, err := store.OntologyAssessmentsByPaths(ctx, []string{"notes/legacy.md"})
	require.NoError(t, err)
	row, ok := rows["notes/legacy.md"]
	require.True(t, ok)
	require.False(t, row.HasIssues, "migrated rows default to 0 until the materialization bump rebuilds them")
	require.False(t, row.TypeAmbiguous)

	var materializationVersion int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT materialization_version FROM ontology_schema_state`).Scan(&materializationVersion))
	require.Equal(t, 3, materializationVersion, "the migration must not touch ontology_schema_state")

	var preserved string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT value FROM index_metadata WHERE key = 'preserved'`).Scan(&preserved))
	require.Equal(t, "yes", preserved)
}

func TestOntologyAssessmentFlagsScansEveryRow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "flags.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Assessments: []OntologyNoteAssessmentRow{
			{NotePath: "notes/a.md", DeclaredType: "Decision", ResolvedType: "Decision", AssessmentJSON: "{}", SchemaHash: "h", UpdatedAt: 1, HasIssues: true},
			{NotePath: "notes/b.md", DeclaredType: "", ResolvedType: "", AssessmentJSON: "{}", SchemaHash: "h", UpdatedAt: 1, TypeAmbiguous: true},
			{NotePath: "notes/c.md", DeclaredType: "", ResolvedType: "", AssessmentJSON: "{}", SchemaHash: "h", UpdatedAt: 1},
		},
		SchemaState: OntologySchemaState{SchemaHash: "h", NotesHash: "n", LoadedAt: 1, Ready: true},
	}))

	flags, err := store.OntologyAssessmentFlags(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]OntologyAssessmentFlags{
		"notes/a.md": {HasIssues: true},
		"notes/b.md": {TypeAmbiguous: true},
		"notes/c.md": {},
	}, flags)
}
