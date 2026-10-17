package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

// OntologyReadinessPublication belongs to one complete sync. Validate must use
// only the supplied transaction snapshot, without file I/O or provider calls.
type OntologyReadinessPublication struct {
	SchemaState   OntologySchemaState
	MetadataState NoteMetadataState
	ProviderHash  string
	Validate      func(OntologyReadinessSnapshot) (bool, error)
}

type OntologyReadinessSnapshot struct {
	SchemaState      OntologySchemaState
	MetadataState    NoteMetadataState
	Metadata         []NoteMetadataRow
	NoteStates       map[string]OntologyNoteStateRow
	Assessments      map[string]OntologyNoteAssessmentRow
	Nodes            []readmodel.NodeRow
	FieldValues      []readmodel.FieldValueRow
	NoteTypes        []OntologyNoteTypeRow
	Edges            []OntologyEdgeRow
	LinkDependencies []readmodel.LinkDependencyRow
	TypePolicies     []OntologyTypePolicyRow
}

type OntologyReadinessState struct {
	SchemaState   OntologySchemaState
	MetadataState NoteMetadataState
	Ready         bool
}

// GetOntologyReadiness checks only singleton publication witnesses. Mutations
// invalidate the proof with triggers, so reads never rehydrate the catalog.
func (s *Store) GetOntologyReadiness(ctx context.Context, providerHash string) (OntologyReadinessState, error) {
	return ontologyReadinessState(ctx, s.db, providerHash)
}

func ontologyReadinessState(ctx context.Context, db queryRower, providerHash string) (OntologyReadinessState, error) {
	var state OntologyReadinessState
	err := db.QueryRowContext(ctx, `
		WITH ontology AS (
			SELECT * FROM ontology_schema_state ORDER BY loaded_at DESC LIMIT 1
		), metadata AS (
			SELECT * FROM note_metadata_state ORDER BY loaded_at DESC LIMIT 1
		)
		SELECT COALESCE(o.schema_hash, ''), COALESCE(o.notes_hash, ''),
			COALESCE(o.materialization_version, 0), COALESCE(o.loaded_at, 0),
			COALESCE(o.ready, 0), COALESCE(o.error_json, ''),
			COALESCE(m.notes_hash, ''), COALESCE(m.raw_notes_hash, ''),
			COALESCE(m.loaded_at, 0), COALESCE(m.ready, 0),
			EXISTS (SELECT 1 FROM ontology_readiness p WHERE p.id = 1
				AND p.provider_hash = ? AND o.ready = 1 AND m.ready = 1
				AND p.schema_hash = o.schema_hash AND p.notes_hash = o.notes_hash
				AND p.notes_hash = m.notes_hash AND p.raw_notes_hash = m.raw_notes_hash
				AND p.materialization_version = o.materialization_version
				AND p.ontology_loaded_at = o.loaded_at AND p.metadata_loaded_at = m.loaded_at
				AND p.error_json = COALESCE(o.error_json, ''))
		FROM (SELECT 1) LEFT JOIN ontology o ON 1 LEFT JOIN metadata m ON 1
	`, providerHash).Scan(
		&state.SchemaState.SchemaHash, &state.SchemaState.NotesHash,
		&state.SchemaState.MaterializationVersion, &state.SchemaState.LoadedAt,
		&state.SchemaState.Ready, &state.SchemaState.ErrorJSON,
		&state.MetadataState.NotesHash, &state.MetadataState.RawNotesHash,
		&state.MetadataState.LoadedAt, &state.MetadataState.Ready, &state.Ready,
	)
	return state, err
}

func publishOntologyReadinessTx(ctx context.Context, tx *sql.Tx, publication *OntologyReadinessPublication) error {
	if publication == nil {
		return nil
	}
	if publication.Validate == nil || strings.TrimSpace(publication.ProviderHash) == "" {
		return errors.New("ontology readiness requires a provider witness and completeness validator")
	}
	state, err := ontologyReadinessState(ctx, tx, publication.ProviderHash)
	if err != nil {
		return err
	}
	if state.SchemaState != publication.SchemaState || state.MetadataState != publication.MetadataState {
		return errors.New("ontology readiness publication does not match its schema and metadata witnesses")
	}
	if !state.SchemaState.Ready || !state.MetadataState.Ready || state.MetadataState.LoadedAt <= 0 ||
		state.SchemaState.LoadedAt <= 0 || state.SchemaState.MaterializationVersion <= 0 ||
		strings.TrimSpace(state.SchemaState.SchemaHash) == "" || strings.TrimSpace(state.MetadataState.NotesHash) == "" ||
		state.SchemaState.NotesHash != state.MetadataState.NotesHash {
		return errors.New("ontology readiness publication requires current schema and metadata states")
	}
	snapshot, err := ontologyReadinessSnapshotTx(ctx, tx, state)
	if err != nil {
		return err
	}
	valid, err := publication.Validate(snapshot)
	if err != nil {
		return fmt.Errorf("validate ontology readiness: %w", err)
	}
	if !valid {
		return errors.New("ontology readiness completeness validation failed")
	}
	_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO ontology_readiness (
		id, schema_hash, notes_hash, raw_notes_hash, metadata_loaded_at,
		materialization_version, ontology_loaded_at, provider_hash, error_json
	) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)`, state.SchemaState.SchemaHash,
		state.MetadataState.NotesHash, state.MetadataState.RawNotesHash, state.MetadataState.LoadedAt,
		state.SchemaState.MaterializationVersion, state.SchemaState.LoadedAt,
		publication.ProviderHash, state.SchemaState.ErrorJSON)
	return err
}
