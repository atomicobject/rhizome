package store

import (
	"context"
	"database/sql"
	"fmt"
)

func addOntologyMaterializationVersionSchema(ctx context.Context, tx *sql.Tx) error {
	hasColumn, err := tableHasColumnQuery(ctx, tx, "ontology_schema_state", "materialization_version")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE ontology_schema_state
		ADD COLUMN materialization_version INTEGER NOT NULL DEFAULT 0 CHECK (materialization_version >= 0)
	`)
	return err
}

// addOntologyAssessmentFlagsSchema adds the materialized inventory flags to
// ontology_note_assessments. The columns default to 0; the ontology
// materialization version bump, not this migration, populates them.
func addOntologyAssessmentFlagsSchema(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{"has_issues", "type_ambiguous"} {
		hasColumn, err := tableHasColumnQuery(ctx, tx, "ontology_note_assessments", column)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		stmt := fmt.Sprintf(
			`ALTER TABLE ontology_note_assessments ADD COLUMN %s INTEGER NOT NULL DEFAULT 0 CHECK (%s IN (0, 1))`,
			column, column,
		)
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
