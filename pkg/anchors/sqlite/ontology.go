package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type OntologyNoteTypeRow struct {
	NotePath   string
	TypeName   string
	SchemaHash string
	UpdatedAt  int64
}

type OntologyEdgeRow struct {
	SrcPath      string
	SrcNodeID    string
	RelationName string
	DstPath      string
	DstNodeID    string
	DstType      string
	Provenance   string
	Structural   bool
	SchemaHash   string
	UpdatedAt    int64
}

type OntologyEdgeKind string

const (
	OntologyEdgeKindField    OntologyEdgeKind = "field"
	OntologyEdgeKindBodyLink OntologyEdgeKind = "body_link"
	OntologyEdgeKindBacklink OntologyEdgeKind = "backlink"
	OntologyEdgeKindUnknown  OntologyEdgeKind = "unknown"
)

func (k OntologyEdgeKind) IsStructural() bool {
	return k == OntologyEdgeKindField
}

func (r OntologyEdgeRow) Kind() OntologyEdgeKind {
	if r.Structural {
		return OntologyEdgeKindField
	}
	switch strings.ToLower(strings.TrimSpace(r.Provenance)) {
	case "body_link":
		return OntologyEdgeKindBodyLink
	case "backlink":
		return OntologyEdgeKindBacklink
	default:
		return OntologyEdgeKindUnknown
	}
}

type OntologyTypePolicyRow struct {
	TypeName   string
	PolicyJSON string
	SchemaHash string
	UpdatedAt  int64
}

type OntologyNoteAssessmentRow struct {
	NotePath       string
	DeclaredType   string
	ResolvedType   string
	AssessmentJSON string
	HasIssues      bool
	TypeAmbiguous  bool
	SchemaHash     string
	UpdatedAt      int64
}

// OntologyAssessmentFlags are the inventory-level facts materialized next to
// each assessment row so whole-vault reads never decode assessment JSON.
type OntologyAssessmentFlags struct {
	HasIssues     bool
	TypeAmbiguous bool
}

type OntologySchemaState struct {
	SchemaHash             string
	NotesHash              string
	MaterializationVersion int
	LoadedAt               int64
	Ready                  bool
	ErrorJSON              string
}

type OntologySnapshot struct {
	Assessments  []OntologyNoteAssessmentRow
	NoteStates   []OntologyNoteStateRow
	NoteTypes    []OntologyNoteTypeRow
	Edges        []OntologyEdgeRow
	TypePolicies []OntologyTypePolicyRow
	SchemaState  OntologySchemaState
}

const (
	ValidationStatusNeverRan = "never_ran"
	ValidationStatusRunning  = "running"
	ValidationStatusOK       = "ok"
	ValidationStatusError    = "error"
)

type ValidationState struct {
	Status              string
	ResultJSON          string
	Error               string
	Generation          int64
	PublishedGeneration int64
	StartedAt           int64
	FinishedAt          int64
	DurationMs          int64
}

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

// GetBrokenLinksJSON returns the stored broken-links validation JSON.
func (s *Store) GetBrokenLinksJSON(ctx context.Context) (string, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(broken_links_json, '')
		FROM ontology_schema_state
		ORDER BY loaded_at DESC
		LIMIT 1
	`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return raw, nil
}

// SetBrokenLinksJSON stores the broken-links validation JSON on the latest
// ontology schema state row.
func (s *Store) SetBrokenLinksJSON(ctx context.Context, jsonStr string) error {
	ctx = indexingperf.WithOp(ctx, "intel.set_broken_links_json")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// ontology_schema_state may be empty in vaults with no ontology schema.
		// Seed a placeholder state row so cached validate data still has a home.
		var schemaHash, notesHash string
		row := tx.QueryRowContext(ctx, `
			SELECT schema_hash, notes_hash FROM ontology_schema_state ORDER BY loaded_at DESC LIMIT 1
		`)
		if err := row.Scan(&schemaHash, &notesHash); err != nil {
			if err != sql.ErrNoRows {
				return err
			}
			schemaHash = ""
			notesHash = ""
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ontology_schema_state (schema_hash, notes_hash, loaded_at, ready, error_json, broken_links_json)
				VALUES (?, ?, ?, 0, '', ?)
			`, schemaHash, notesHash, time.Now().Unix(), jsonStr); err != nil {
				return err
			}
			return nil
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE ontology_schema_state SET broken_links_json = ? WHERE schema_hash = ? AND notes_hash = ?
		`, jsonStr, schemaHash, notesHash)
		return err
	})
}

// GetValidationResultJSON returns the cached full validation result.
func (s *Store) GetValidationResultJSON(ctx context.Context) (string, error) {
	state, err := s.GetValidationState(ctx)
	if err != nil {
		return "", err
	}
	return state.ResultJSON, nil
}

// SetValidationResultJSON stores the full validation result JSON.
func (s *Store) SetValidationResultJSON(ctx context.Context, jsonStr string) error {
	generation, err := s.SetValidationRunning(ctx)
	if err != nil {
		return err
	}
	_, err = s.SetValidationResult(ctx, generation, jsonStr, 0)
	return err
}

func (s *Store) GetValidationState(ctx context.Context) (ValidationState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT status, result_json, error, generation, published_generation,
			COALESCE(started_at, 0), COALESCE(finished_at, 0), duration_ms
		FROM validation_state
		WHERE id = 1
	`)
	var state ValidationState
	if err := row.Scan(&state.Status, &state.ResultJSON, &state.Error, &state.Generation, &state.PublishedGeneration, &state.StartedAt, &state.FinishedAt, &state.DurationMs); err != nil {
		if err == sql.ErrNoRows {
			return ValidationState{Status: ValidationStatusNeverRan}, nil
		}
		return ValidationState{}, err
	}
	if strings.TrimSpace(state.Status) == "" {
		state.Status = ValidationStatusNeverRan
	}
	return state, nil
}

func (s *Store) SetValidationRunning(ctx context.Context) (int64, error) {
	ctx = indexingperf.WithOp(ctx, "intel.validation_running")
	now := time.Now().Unix()
	var generation int64
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO validation_state (id, status, generation, started_at, duration_ms)
			VALUES (1, ?, 0, ?, 0)
			ON CONFLICT(id) DO NOTHING
		`, ValidationStatusNeverRan, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE validation_state
			SET status = ?, result_json = '', error = '', generation = generation + 1,
				started_at = ?, finished_at = NULL, duration_ms = 0
			WHERE id = 1
		`, ValidationStatusRunning, now); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT generation FROM validation_state WHERE id = 1`).Scan(&generation)
	})
	return generation, err
}

func (s *Store) SetValidationResult(ctx context.Context, generation int64, jsonStr string, durationMs int64) (bool, error) {
	ctx = indexingperf.WithOp(ctx, "intel.validation_result")
	return s.setValidationFinished(ctx, generation, ValidationStatusOK, jsonStr, "", durationMs)
}

func (s *Store) SetValidationError(ctx context.Context, generation int64, message string, durationMs int64) (bool, error) {
	ctx = indexingperf.WithOp(ctx, "intel.validation_error")
	return s.setValidationFinished(ctx, generation, ValidationStatusError, "", message, durationMs)
}

func (s *Store) setValidationFinished(ctx context.Context, generation int64, status, resultJSON, errorMessage string, durationMs int64) (bool, error) {
	if durationMs < 0 {
		durationMs = 0
	}
	now := time.Now().Unix()
	var updated bool
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE validation_state
			SET status = ?, result_json = ?, error = ?, finished_at = ?, duration_ms = ?
			WHERE id = 1 AND generation = ?
		`, status, resultJSON, errorMessage, now, durationMs, generation)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		updated = affected > 0
		return nil
	})
	return updated, err
}

func (s *Store) GetOntologyTypeByPath(ctx context.Context, notePath string) (OntologyNoteTypeRow, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT note_path, type_name, schema_hash, updated_at
		FROM ontology_note_types
		WHERE note_path = ?
		LIMIT 1
	`, notePath)
	var out OntologyNoteTypeRow
	if err := row.Scan(&out.NotePath, &out.TypeName, &out.SchemaHash, &out.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return OntologyNoteTypeRow{}, false, nil
		}
		return OntologyNoteTypeRow{}, false, err
	}
	return out, true, nil
}

func (s *Store) GetOntologyAssessmentByPath(ctx context.Context, notePath string) (OntologyNoteAssessmentRow, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT note_path, declared_type, resolved_type, assessment_json, has_issues, type_ambiguous, schema_hash, updated_at
		FROM ontology_note_assessments
		WHERE note_path = ?
		LIMIT 1
	`, notePath)
	var out OntologyNoteAssessmentRow
	if err := row.Scan(&out.NotePath, &out.DeclaredType, &out.ResolvedType, &out.AssessmentJSON, &out.HasIssues, &out.TypeAmbiguous, &out.SchemaHash, &out.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return OntologyNoteAssessmentRow{}, false, nil
		}
		return OntologyNoteAssessmentRow{}, false, err
	}
	return out, true, nil
}

func (s *Store) OntologyAssessmentsByPaths(ctx context.Context, paths []string) (map[string]OntologyNoteAssessmentRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]OntologyNoteAssessmentRow{}, nil
	}
	out := make(map[string]OntologyNoteAssessmentRow, len(paths))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT note_path, declared_type, resolved_type, assessment_json, has_issues, type_ambiguous, schema_hash, updated_at
			FROM ontology_note_assessments
			WHERE note_path IN (%s)
		`, placeholders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyNoteAssessmentRow
			if err := rows.Scan(&row.NotePath, &row.DeclaredType, &row.ResolvedType, &row.AssessmentJSON, &row.HasIssues, &row.TypeAmbiguous, &row.SchemaHash, &row.UpdatedAt); err != nil {
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

// OntologyAssessmentFlags returns the materialized inventory flags for every
// assessment row in one scan. It never reads assessment_json.
func (s *Store) OntologyAssessmentFlags(ctx context.Context) (map[string]OntologyAssessmentFlags, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT note_path, has_issues, type_ambiguous FROM ontology_note_assessments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]OntologyAssessmentFlags{}
	for rows.Next() {
		var notePath string
		var flags OntologyAssessmentFlags
		if err := rows.Scan(&notePath, &flags.HasIssues, &flags.TypeAmbiguous); err != nil {
			return nil, err
		}
		out[notePath] = flags
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) OntologyTypesByPaths(ctx context.Context, paths []string) (map[string]OntologyNoteTypeRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]OntologyNoteTypeRow{}, nil
	}
	out := make(map[string]OntologyNoteTypeRow, len(paths))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT note_path, type_name, schema_hash, updated_at
			FROM ontology_note_types
			WHERE note_path IN (%s)
		`, placeholders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyNoteTypeRow
			if err := rows.Scan(&row.NotePath, &row.TypeName, &row.SchemaHash, &row.UpdatedAt); err != nil {
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

func (s *Store) OntologyPathsByType(ctx context.Context, typeName string, limit int) ([]string, error) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, nil
	}
	query := `
		SELECT note_path
		FROM ontology_note_types
		WHERE type_name = ?
		ORDER BY note_path
	`
	args := []any{typeName}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) OntologyTypePoliciesForPaths(ctx context.Context, paths []string) (map[string]OntologyTypePolicyRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]OntologyTypePolicyRow{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
	query := fmt.Sprintf(`
		SELECT nt.note_path, tp.type_name, tp.policy_json, tp.schema_hash, tp.updated_at
		FROM ontology_note_types nt
		JOIN ontology_type_policies tp ON tp.type_name = nt.type_name
		WHERE nt.note_path IN (%s)
	`, placeholders)
	rows, err := s.db.QueryContext(ctx, query, sliceAny(paths)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]OntologyTypePolicyRow, len(paths))
	for rows.Next() {
		var notePath string
		var row OntologyTypePolicyRow
		if err := rows.Scan(&notePath, &row.TypeName, &row.PolicyJSON, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out[notePath] = row
	}
	return out, rows.Err()
}

func (s *Store) OntologyEdgesForPath(ctx context.Context, path string, includeAmbient bool, relationFilter string, limit int) ([]OntologyEdgeRow, error) {
	return s.OntologyEdgesForPaths(ctx, []string{path}, includeAmbient, relationFilter, limit)
}

func (s *Store) AllOntologyEdges(ctx context.Context, includeAmbient bool, limit int) ([]OntologyEdgeRow, error) {
	conditions := []string{"1 = 1"}
	if !includeAmbient {
		conditions = append(conditions, "structural = 1")
	}
	query := fmt.Sprintf(`
		SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
		FROM ontology_edges
		WHERE %s
		ORDER BY structural DESC, src_path, relation_name, dst_path, src_node_id, dst_node_id
	`, strings.Join(conditions, " AND "))
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OntologyEdgeRow, 0)
	for rows.Next() {
		var row OntologyEdgeRow
		var structural int
		if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &structural, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.Structural = structural != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) OntologyEdgesForPaths(ctx context.Context, paths []string, includeAmbient bool, relationFilter string, limit int) ([]OntologyEdgeRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	outCap := len(paths) * 8
	if limit > 0 && limit < outCap {
		outCap = limit
	}
	out := make([]OntologyEdgeRow, 0, outCap)
	batchSize := sqliteValuesBatchMaxParams / 2
	if batchSize < 1 {
		batchSize = 1
	}
	for start := 0; start < len(paths); start += batchSize {
		end := start + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		conditions := []string{fmt.Sprintf("(src_path IN (%s) OR dst_path IN (%s))", placeholders, placeholders)}
		args := make([]any, 0, len(batch)*2+2)
		args = append(args, sliceAny(batch)...)
		args = append(args, sliceAny(batch)...)
		if !includeAmbient {
			conditions = append(conditions, "structural = 1")
		}
		if strings.TrimSpace(relationFilter) != "" {
			conditions = append(conditions, "relation_name = ?")
			args = append(args, relationFilter)
		}
		query := fmt.Sprintf(`
			SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
			FROM ontology_edges
			WHERE %s
			ORDER BY structural DESC, src_path, relation_name, dst_path
		`, strings.Join(conditions, " AND "))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyEdgeRow
			var structural int
			if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &structural, &row.SchemaHash, &row.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			row.Structural = structural != 0
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return ontologyEdgePathOrderLess(out[i], out[j])
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) OntologyStructuralEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]OntologyEdgeRow, error) {
	return s.ontologyEdgesByEndpoint(ctx, "src_path", paths, true, relation, limit)
}

func (s *Store) OntologyAmbientEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]OntologyEdgeRow, error) {
	return s.ontologyEdgesByEndpoint(ctx, "src_path", paths, false, relation, limit)
}

func (s *Store) OntologyAmbientEdgesByTargets(ctx context.Context, paths []string, relation string, limit int) ([]OntologyEdgeRow, error) {
	return s.ontologyEdgesByEndpoint(ctx, "dst_path", paths, false, relation, limit)
}

func (s *Store) ontologyEdgesByEndpoint(ctx context.Context, column string, paths []string, structural bool, relation string, limit int) ([]OntologyEdgeRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	var out []OntologyEdgeRow
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		conditions := []string{fmt.Sprintf("%s IN (%s)", column, placeholders), "structural = ?"}
		args := make([]any, 0, len(batch)+3)
		args = append(args, sliceAny(batch)...)
		args = append(args, boolToInt(structural))
		if strings.TrimSpace(relation) != "" {
			conditions = append(conditions, "relation_name = ?")
			args = append(args, relation)
		}
		query := fmt.Sprintf(`
			SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
			FROM ontology_edges
			WHERE %s
			ORDER BY src_path, src_node_id, provenance, relation_name, dst_path, dst_node_id
		`, strings.Join(conditions, " AND "))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyEdgeRow
			var structuralInt int
			if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &structuralInt, &row.SchemaHash, &row.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			row.Structural = structuralInt != 0
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return ontologyEdgeEndpointOrderLess(out[i], out[j])
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func ontologyEdgeEndpointOrderLess(a, b OntologyEdgeRow) bool {
	if a.SrcPath != b.SrcPath {
		return a.SrcPath < b.SrcPath
	}
	if a.SrcNodeID != b.SrcNodeID {
		return a.SrcNodeID < b.SrcNodeID
	}
	if a.Provenance != b.Provenance {
		return a.Provenance < b.Provenance
	}
	if a.RelationName != b.RelationName {
		return a.RelationName < b.RelationName
	}
	if a.DstPath != b.DstPath {
		return a.DstPath < b.DstPath
	}
	return a.DstNodeID < b.DstNodeID
}

func ontologyEdgePathOrderLess(a, b OntologyEdgeRow) bool {
	if a.Structural != b.Structural {
		return a.Structural
	}
	if a.SrcPath != b.SrcPath {
		return a.SrcPath < b.SrcPath
	}
	if a.RelationName != b.RelationName {
		return a.RelationName < b.RelationName
	}
	if a.DstPath != b.DstPath {
		return a.DstPath < b.DstPath
	}
	if a.SrcNodeID != b.SrcNodeID {
		return a.SrcNodeID < b.SrcNodeID
	}
	return a.DstNodeID < b.DstNodeID
}

func insertOntologyNoteTypes(ctx context.Context, tx *sql.Tx, rows []OntologyNoteTypeRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_note_types (note_path, type_name, schema_hash, updated_at)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if strings.TrimSpace(row.NotePath) == "" || strings.TrimSpace(row.TypeName) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.NotePath, row.TypeName, row.SchemaHash, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func insertOntologyAssessments(ctx context.Context, tx *sql.Tx, rows []OntologyNoteAssessmentRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_note_assessments (note_path, declared_type, resolved_type, assessment_json, has_issues, type_ambiguous, schema_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, row.NotePath, row.DeclaredType, row.ResolvedType, row.AssessmentJSON, boolToInt(row.HasIssues), boolToInt(row.TypeAmbiguous), row.SchemaHash, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func insertOntologyEdges(ctx context.Context, tx *sql.Tx, rows []OntologyEdgeRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_edges (src_path, src_node_id, relation_name, dst_path, dst_node_id, dst_type, provenance, structural, schema_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if strings.TrimSpace(row.SrcPath) == "" || strings.TrimSpace(row.RelationName) == "" || strings.TrimSpace(row.DstPath) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.SrcPath, row.SrcNodeID, row.RelationName, row.DstPath, row.DstNodeID, row.DstType, row.Provenance, boolToInt(row.Structural), row.SchemaHash, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func insertOntologyTypePolicies(ctx context.Context, tx *sql.Tx, rows []OntologyTypePolicyRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_type_policies (type_name, policy_json, schema_hash, updated_at)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if strings.TrimSpace(row.TypeName) == "" || strings.TrimSpace(row.PolicyJSON) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.TypeName, row.PolicyJSON, row.SchemaHash, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
