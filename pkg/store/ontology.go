package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
)

func (s *Store) ReplaceOntologySnapshot(ctx context.Context, snapshot OntologySnapshot) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_ontology")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			`DELETE FROM ontology_note_assessments`,
			`DELETE FROM ontology_note_state`,
			`DELETE FROM ontology_note_types`,
			`DELETE FROM ontology_edges`,
			`DELETE FROM ontology_type_policies`,
			`DELETE FROM ontology_schema_state`,
			`DELETE FROM ontology_node_embedding_state`,
			`DELETE FROM ontology_node_field_values`,
			`DELETE FROM ontology_node_field_value_dependencies`,
			`DELETE FROM intel_chunks WHERE owner_type = 'ontology_node'`,
			`DELETE FROM ontology_nodes`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if err := insertOntologyAssessments(ctx, tx, snapshot.Assessments); err != nil {
			return err
		}
		if err := insertOntologyNoteStates(ctx, tx, snapshot.NoteStates); err != nil {
			return err
		}
		if err := insertOntologyNoteTypes(ctx, tx, snapshot.NoteTypes); err != nil {
			return err
		}
		if err := insertOntologyEdges(ctx, tx, snapshot.Edges); err != nil {
			return err
		}
		if err := insertOntologyTypePolicies(ctx, tx, snapshot.TypePolicies); err != nil {
			return err
		}
		state := snapshot.SchemaState
		if strings.TrimSpace(state.SchemaHash) == "" && strings.TrimSpace(state.NotesHash) == "" && state.MaterializationVersion == 0 && state.LoadedAt == 0 && !state.Ready && strings.TrimSpace(state.ErrorJSON) == "" {
			return nil
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO ontology_schema_state (schema_hash, notes_hash, materialization_version, loaded_at, ready, error_json)
			VALUES (?, ?, ?, ?, ?, ?)
		`, state.SchemaHash, state.NotesHash, state.MaterializationVersion, state.LoadedAt, boolToInt(state.Ready), state.ErrorJSON)
		return err
	})
}

func (s *Store) InvalidateOntologyState(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.invalidate_ontology")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return invalidateOntologyStateTx(ctx, tx)
	})
}

func invalidateOntologyStateTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM ontology_schema_state`)
	return err
}

// OntologyNodeCount returns the number of materialized catalog rows for the
// boot-time readiness probe. Ontology sync publishes schema state and its node
// catalog atomically; a populated catalog can remain readable during startup.
func (s *Store) OntologyNodeCount(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_nodes`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) GetOntologySchemaState(ctx context.Context) (OntologySchemaState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT schema_hash, notes_hash, materialization_version, loaded_at, ready, COALESCE(error_json, '')
		FROM ontology_schema_state
		ORDER BY loaded_at DESC
		LIMIT 1
	`)
	var state OntologySchemaState
	var ready int
	err := row.Scan(&state.SchemaHash, &state.NotesHash, &state.MaterializationVersion, &state.LoadedAt, &ready, &state.ErrorJSON)
	if err == sql.ErrNoRows {
		return OntologySchemaState{}, nil
	}
	if err != nil {
		return OntologySchemaState{}, err
	}
	state.Ready = ready != 0
	return state, nil
}
