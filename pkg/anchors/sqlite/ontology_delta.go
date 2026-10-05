package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type OntologyNoteStateRow struct {
	NotePath         string
	InputFingerprint string
	SchemaHash       string
	ResolvedType     string
	UpdatedAt        int64
}

type OntologyDelta struct {
	// ReadModels are published in order in the same transaction as the derived facts.
	ReadModels   []codeanchor.IntelOntologyNodeReadModel
	FullRebuild  bool
	DeletePaths  []string
	ReplacePaths []string
	EdgeSources  []string
	Assessments  []OntologyNoteAssessmentRow
	NoteStates   []OntologyNoteStateRow
	NoteTypes    []OntologyNoteTypeRow
	Edges        []OntologyEdgeRow
	TypePolicies []OntologyTypePolicyRow
	SchemaState  *OntologySchemaState
}

func (d OntologyDelta) RowCount() int {
	count := len(d.DeletePaths) + len(d.ReplacePaths) + len(d.EdgeSources) +
		len(d.Assessments) + len(d.NoteStates) + len(d.NoteTypes) + len(d.Edges) + len(d.TypePolicies)
	if d.SchemaState != nil {
		count++
	}
	for _, model := range d.ReadModels {
		count += len(model.Nodes) + len(model.FieldValues) + len(model.LinkDependencies)
	}
	return count
}

func MergeOntologyDelta(dst *OntologyDelta, src OntologyDelta) {
	if dst == nil {
		return
	}
	if src.FullRebuild {
		*dst = OntologyDelta{}
	}
	dst.ReadModels = append(dst.ReadModels, src.ReadModels...)
	dst.FullRebuild = dst.FullRebuild || src.FullRebuild
	dst.DeletePaths = append(dst.DeletePaths, src.DeletePaths...)
	dst.ReplacePaths = append(dst.ReplacePaths, src.ReplacePaths...)
	dst.EdgeSources = append(dst.EdgeSources, src.EdgeSources...)
	dst.Assessments = append(dst.Assessments, src.Assessments...)
	dst.NoteStates = append(dst.NoteStates, src.NoteStates...)
	dst.NoteTypes = append(dst.NoteTypes, src.NoteTypes...)
	dst.Edges = append(dst.Edges, src.Edges...)
	dst.TypePolicies = append(dst.TypePolicies, src.TypePolicies...)
	if src.SchemaState != nil {
		stateCopy := *src.SchemaState
		dst.SchemaState = &stateCopy
	}
}

func NormalizeOntologyDelta(delta OntologyDelta) OntologyDelta {
	delta.DeletePaths = normalizeNonEmptyStrings(delta.DeletePaths)
	delta.ReplacePaths = normalizeNonEmptyStrings(delta.ReplacePaths)
	delta.EdgeSources = normalizeNonEmptyStrings(delta.EdgeSources)
	sort.Slice(delta.Assessments, func(i, j int) bool { return delta.Assessments[i].NotePath < delta.Assessments[j].NotePath })
	sort.Slice(delta.NoteStates, func(i, j int) bool { return delta.NoteStates[i].NotePath < delta.NoteStates[j].NotePath })
	sort.Slice(delta.NoteTypes, func(i, j int) bool {
		if delta.NoteTypes[i].NotePath != delta.NoteTypes[j].NotePath {
			return delta.NoteTypes[i].NotePath < delta.NoteTypes[j].NotePath
		}
		return delta.NoteTypes[i].TypeName < delta.NoteTypes[j].TypeName
	})
	sort.Slice(delta.TypePolicies, func(i, j int) bool { return delta.TypePolicies[i].TypeName < delta.TypePolicies[j].TypeName })
	sort.Slice(delta.Edges, func(i, j int) bool {
		a := delta.Edges[i]
		b := delta.Edges[j]
		switch {
		case a.SrcPath != b.SrcPath:
			return a.SrcPath < b.SrcPath
		case a.SrcNodeID != b.SrcNodeID:
			return a.SrcNodeID < b.SrcNodeID
		case a.RelationName != b.RelationName:
			return a.RelationName < b.RelationName
		case a.DstPath != b.DstPath:
			return a.DstPath < b.DstPath
		case a.DstNodeID != b.DstNodeID:
			return a.DstNodeID < b.DstNodeID
		default:
			return a.Provenance < b.Provenance
		}
	})
	return delta
}

func (s *Store) DeleteOntologyForPaths(ctx context.Context, paths []string) error {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.delete_ontology_paths")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return deleteOntologyForPathsTx(ctx, tx, paths, true)
	})
}

func (s *Store) UpsertOntologyAssessments(ctx context.Context, rows []OntologyNoteAssessmentRow) error {
	ctx = indexingperf.WithOp(ctx, "intel.upsert_ontology_assessments")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return insertOntologyAssessments(ctx, tx, rows)
	})
}

func (s *Store) UpsertOntologyTypes(ctx context.Context, rows []OntologyNoteTypeRow) error {
	ctx = indexingperf.WithOp(ctx, "intel.upsert_ontology_types")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return insertOntologyNoteTypes(ctx, tx, rows)
	})
}

func (s *Store) UpsertOntologyNoteStates(ctx context.Context, rows []OntologyNoteStateRow) error {
	ctx = indexingperf.WithOp(ctx, "intel.upsert_ontology_note_state")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return insertOntologyNoteStates(ctx, tx, rows)
	})
}

func (s *Store) ReplaceOntologyEdgesForSources(ctx context.Context, paths []string, rows []OntologyEdgeRow) error {
	paths = normalizeNonEmptyStrings(paths)
	ctx = indexingperf.WithOp(ctx, "intel.replace_ontology_edges")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if err := deleteOntologyEdgesForSourcesTx(ctx, tx, paths); err != nil {
			return err
		}
		return insertOntologyEdges(ctx, tx, rows)
	})
}

func (s *Store) UpsertOntologySchemaState(ctx context.Context, state OntologySchemaState) error {
	ctx = indexingperf.WithOp(ctx, "intel.upsert_ontology_schema_state")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return replaceOntologySchemaStateTx(ctx, tx, state)
	})
}

func (s *Store) OntologyNoteStatesByPaths(ctx context.Context, paths []string) (map[string]OntologyNoteStateRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]OntologyNoteStateRow{}, nil
	}
	out := make(map[string]OntologyNoteStateRow, len(paths))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(paths))
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT note_path, input_fingerprint, schema_hash, resolved_type, updated_at
			FROM ontology_note_state
			WHERE note_path IN (%s)
		`, placeholders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyNoteStateRow
			if err := rows.Scan(&row.NotePath, &row.InputFingerprint, &row.SchemaHash, &row.ResolvedType, &row.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			out[row.NotePath] = row
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func (s *Store) ApplyOntologyDelta(ctx context.Context, delta OntologyDelta) error {
	return s.applyOntologyDelta(ctx, delta, false)
}

// ApplyOntologyDeltaPreservingSemantic publishes validation facts and the node
// catalog without pruning independently maintained semantic data.
func (s *Store) ApplyOntologyDeltaPreservingSemantic(ctx context.Context, delta OntologyDelta) error {
	return s.applyOntologyDelta(ctx, delta, true)
}

func (s *Store) applyOntologyDelta(ctx context.Context, delta OntologyDelta, preserveSemantic bool) error {
	delta = NormalizeOntologyDelta(delta)
	ctx = indexingperf.WithOp(ctx, "intel.apply_ontology_delta")
	var fieldRowsWritten int
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		fieldRowsWritten = 0
		if delta.FullRebuild {
			// Full rebuild = clean slate (see pkg/ontology/sync.go FullRebuild path).
			// ontology_node_field_values has FK ON DELETE CASCADE on ontology_nodes,
			// so order matters; we explicitly truncate both to keep behavior obvious
			// regardless of FK enforcement state.
			for _, stmt := range []string{
				`DELETE FROM ontology_note_assessments`,
				`DELETE FROM ontology_note_state`,
				`DELETE FROM ontology_note_types`,
				`DELETE FROM ontology_edges`,
				`DELETE FROM ontology_type_policies`,
				`DELETE FROM ontology_schema_state`,
				`DELETE FROM ontology_node_field_value_dependencies`,
				`DELETE FROM ontology_node_field_values`,
				`DELETE FROM ontology_nodes`,
			} {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
		} else {
			if err := deleteOntologyForPathsTx(ctx, tx, delta.DeletePaths, true); err != nil {
				return err
			}
			if err := deleteOntologyForPathsTx(ctx, tx, delta.ReplacePaths, false); err != nil {
				return err
			}
			if err := deleteOntologyEdgesForSourcesTx(ctx, tx, delta.EdgeSources); err != nil {
				return err
			}
		}
		if err := insertOntologyAssessments(ctx, tx, delta.Assessments); err != nil {
			return err
		}
		if err := insertOntologyNoteStates(ctx, tx, delta.NoteStates); err != nil {
			return err
		}
		if err := insertOntologyNoteTypes(ctx, tx, delta.NoteTypes); err != nil {
			return err
		}
		if err := insertOntologyEdges(ctx, tx, delta.Edges); err != nil {
			return err
		}
		if err := insertOntologyTypePolicies(ctx, tx, delta.TypePolicies); err != nil {
			return err
		}
		if delta.SchemaState != nil {
			if err := replaceOntologySchemaStateTx(ctx, tx, *delta.SchemaState); err != nil {
				return err
			}
		}
		for _, model := range delta.ReadModels {
			written, err := replaceOntologyNodeReadModelTx(ctx, tx, model, preserveSemantic)
			if err != nil {
				return err
			}
			fieldRowsWritten += written
		}
		return nil
	})
	if err == nil {
		indexingperf.AddCount(ctx, "ontology.field_rows_written", int64(fieldRowsWritten))
	}
	return err
}

func deleteOntologyForPathsTx(ctx context.Context, tx *sql.Tx, paths []string, deleteIncidentEdges bool) error {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil
	}
	holders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
	args := sliceAny(paths)
	for _, stmt := range []string{
		fmt.Sprintf(`DELETE FROM ontology_note_assessments WHERE note_path IN (%s)`, holders),
		fmt.Sprintf(`DELETE FROM ontology_note_state WHERE note_path IN (%s)`, holders),
		fmt.Sprintf(`DELETE FROM ontology_note_types WHERE note_path IN (%s)`, holders),
	} {
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	if deleteIncidentEdges {
		edgeArgs := make([]any, 0, len(paths)*2)
		edgeArgs = append(edgeArgs, sliceAny(paths)...)
		edgeArgs = append(edgeArgs, sliceAny(paths)...)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM ontology_edges WHERE src_path IN (%s) OR dst_path IN (%s)`, holders, holders), edgeArgs...); err != nil {
			return err
		}
	}
	return nil
}

func deleteOntologyEdgesForSourcesTx(ctx context.Context, tx *sql.Tx, paths []string) error {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil
	}
	holders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM ontology_edges WHERE src_path IN (%s)`, holders), sliceAny(paths)...)
	return err
}

func replaceOntologySchemaStateTx(ctx context.Context, tx *sql.Tx, state OntologySchemaState) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM ontology_schema_state`); err != nil {
		return err
	}
	if strings.TrimSpace(state.SchemaHash) == "" && strings.TrimSpace(state.NotesHash) == "" && state.MaterializationVersion == 0 && state.LoadedAt == 0 && !state.Ready && strings.TrimSpace(state.ErrorJSON) == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ontology_schema_state (schema_hash, notes_hash, materialization_version, loaded_at, ready, error_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`, state.SchemaHash, state.NotesHash, state.MaterializationVersion, state.LoadedAt, boolToInt(state.Ready), state.ErrorJSON)
	return err
}

func insertOntologyNoteStates(ctx context.Context, tx *sql.Tx, rows []OntologyNoteStateRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_note_state (note_path, input_fingerprint, schema_hash, resolved_type, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if strings.TrimSpace(row.NotePath) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.NotePath, row.InputFingerprint, row.SchemaHash, row.ResolvedType, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}
