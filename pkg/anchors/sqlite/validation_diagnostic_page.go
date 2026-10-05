package sqlite

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Store) GetValidationDiagnosticsPage(ctx context.Context, request ValidationDiagnosticPageRequest) (ValidationDiagnosticPage, error) {
	request, filterIdentity, err := normalizeValidationDiagnosticPageRequest(request)
	if err != nil {
		return ValidationDiagnosticPage{}, err
	}
	after, afterFile := -1, ""
	if request.Cursor != "" {
		cursor, cursorErr := decodeValidationDiagnosticCursor(request.Cursor)
		if cursorErr != nil {
			return ValidationDiagnosticPage{}, cursorErr
		}
		if cursor.Generation != request.Generation || cursor.FilterIdentity != filterIdentity || cursor.Sort != request.Sort {
			return ValidationDiagnosticPage{}, fmt.Errorf("%w: cursor does not match generation, filter, and sort", ErrValidationPageCursor)
		}
		after, afterFile = cursor.After, cursor.AfterFile
	}
	fileSort := request.Sort == ValidationDiagnosticSortFile
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ValidationDiagnosticPage{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var total int
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM validation_generations WHERE generation = ?`, request.Generation).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ValidationDiagnosticPage{}, ErrValidationGenerationExpired
		}
		return ValidationDiagnosticPage{}, err
	}
	where, args := validationDiagnosticFilterSQL(request.Generation, request.Filter)
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM validation_diagnostics d WHERE `+where, args...).Scan(&total); err != nil {
		return ValidationDiagnosticPage{}, err
	}
	const columns = `diagnostic_order, issue_key, check_name, code, message, evidence_json, primary_path,
		type_name, field_name, source, target, location_unit, location_start, location_end,
		node_id, location_field, location_relation, variant_key, variant_label`
	pageArgs := append([]any(nil), args...)
	pageQuery := `SELECT '', ` + columns + ` FROM validation_diagnostics d
		WHERE ` + where + ` AND d.diagnostic_order > ? ORDER BY diagnostic_order LIMIT ?`
	pageArgs = append(pageArgs, after, request.Limit+1)
	if fileSort {
		// ponytail: the file key is computed per filtered row on every page;
		// store it on the diagnostic row if vaults outgrow tens of thousands of issues.
		pageQuery = `SELECT file_key, ` + columns + ` FROM (` + validationDiagnosticFileKeyedSQL(where) + `)
			WHERE file_key > ? OR (file_key = ? AND diagnostic_order > ?)
			ORDER BY file_key, diagnostic_order LIMIT ?`
		pageArgs = append(pageArgs[:len(args)], afterFile, afterFile, after, request.Limit+1)
	}
	rows, err := tx.QueryContext(ctx, pageQuery, pageArgs...)
	if err != nil {
		return ValidationDiagnosticPage{}, err
	}
	defer rows.Close()
	diagnostics := make([]ValidationDiagnostic, 0)
	var ordinals []int
	var fileKeys []string
	for rows.Next() {
		var diagnostic ValidationDiagnostic
		var ordinal int
		var fileKey, evidence, locationUnit string
		var start, end int
		var nodeID, locationField, relation, variantKey, variantLabel string
		if err := rows.Scan(&fileKey, &ordinal, &diagnostic.IssueKey, &diagnostic.Check, &diagnostic.Code,
			&diagnostic.Message, &evidence, &diagnostic.PrimaryPath, &diagnostic.Type, &diagnostic.Field,
			&diagnostic.Source, &diagnostic.Target, &locationUnit, &start, &end, &nodeID, &locationField, &relation,
			&variantKey, &variantLabel); err != nil {
			return ValidationDiagnosticPage{}, err
		}
		diagnostic.Variant = validationIssueVariant(variantKey, variantLabel)
		if evidence != "" {
			diagnostic.Evidence = json.RawMessage(evidence)
		}
		if locationUnit != "" {
			diagnostic.Location = &ValidationDiagnosticLocation{Unit: locationUnit, Start: start, End: end, NodeID: nodeID, Field: locationField, Relation: relation}
		}
		diagnostics = append(diagnostics, diagnostic)
		ordinals = append(ordinals, ordinal)
		fileKeys = append(fileKeys, fileKey)
	}
	if err := rows.Err(); err != nil {
		return ValidationDiagnosticPage{}, err
	}
	if err := rows.Close(); err != nil {
		return ValidationDiagnosticPage{}, err
	}
	next := ""
	if len(diagnostics) > request.Limit {
		diagnostics = diagnostics[:request.Limit]
		ordinals = ordinals[:request.Limit]
		fileKeys = fileKeys[:request.Limit]
		next, err = encodeValidationDiagnosticCursor(validationDiagnosticCursor{
			Version: 1, Generation: request.Generation, FilterIdentity: filterIdentity,
			Sort: request.Sort, After: ordinals[len(ordinals)-1], AfterFile: fileKeys[len(fileKeys)-1],
		})
		if err != nil {
			return ValidationDiagnosticPage{}, err
		}
	}
	var fileTotals map[string]int
	if len(diagnostics) > 0 {
		if err := attachValidationDiagnosticMemberships(ctx, tx, request.Generation, diagnostics); err != nil {
			return ValidationDiagnosticPage{}, err
		}
		if fileSort {
			if fileTotals, err = validationDiagnosticFileTotals(ctx, tx, where, args, fileKeys); err != nil {
				return ValidationDiagnosticPage{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return ValidationDiagnosticPage{}, err
	}
	return ValidationDiagnosticPage{
		Generation: request.Generation, FilterIdentity: filterIdentity, Sort: request.Sort,
		Returned: len(diagnostics), Total: total, Diagnostics: diagnostics, NextCursor: next,
		FileTotals: fileTotals,
	}, nil
}

// validationDiagnosticFileKeyedSQL selects filtered diagnostics with the file
// key the Problems view groups by: the primary path, else the first affected
// path, else "" for vault-wide issues.
func validationDiagnosticFileKeyedSQL(where string) string {
	return `SELECT COALESCE(NULLIF(d.primary_path, ''), (
			SELECT MIN(fp.path) FROM validation_diagnostic_paths fp
			WHERE fp.generation = d.generation AND fp.issue_key = d.issue_key AND fp.membership_kind = 'affected'
		), '') AS file_key, d.*
		FROM validation_diagnostics d WHERE ` + where
}

func validationDiagnosticFileTotals(ctx context.Context, tx *sql.Tx, where string, args []any, fileKeys []string) (map[string]int, error) {
	unique := make(map[string]struct{}, len(fileKeys))
	queryArgs := append([]any(nil), args...)
	for _, key := range fileKeys {
		if _, seen := unique[key]; !seen {
			unique[key] = struct{}{}
			queryArgs = append(queryArgs, key)
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT file_key, COUNT(*) FROM (`+validationDiagnosticFileKeyedSQL(where)+`)
		WHERE file_key IN (?`+strings.Repeat(", ?", len(unique)-1)+`) GROUP BY file_key`, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := make(map[string]int, len(unique))
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		totals[key] = count
	}
	return totals, rows.Err()
}

type validationDiagnosticCursor struct {
	Version        int    `json:"version"`
	Generation     int64  `json:"generation"`
	FilterIdentity string `json:"filterIdentity"`
	Sort           string `json:"sort"`
	After          int    `json:"after"`
	AfterFile      string `json:"afterFile,omitempty"`
}

func encodeValidationDiagnosticCursor(cursor validationDiagnosticCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeValidationDiagnosticCursor(raw string) (validationDiagnosticCursor, error) {
	encoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return validationDiagnosticCursor{}, fmt.Errorf("%w: malformed encoding", ErrValidationPageCursor)
	}
	var cursor validationDiagnosticCursor
	if err := json.Unmarshal(encoded, &cursor); err != nil || cursor.Version != 1 || cursor.Generation <= 0 || cursor.After < 0 || strings.TrimSpace(cursor.FilterIdentity) == "" ||
		(cursor.Sort != ValidationDiagnosticSortStable && cursor.Sort != ValidationDiagnosticSortFile) ||
		(cursor.Sort == ValidationDiagnosticSortStable && cursor.AfterFile != "") {
		return validationDiagnosticCursor{}, fmt.Errorf("%w: malformed payload", ErrValidationPageCursor)
	}
	return cursor, nil
}

// validationScopeTypeNames is the set of current resolved note types a type or
// interface scope covers: the type itself, or the interface's implementors.
func validationScopeTypeNames(kind, key string, implementors map[string][]string) []string {
	switch kind {
	case ValidationScopeType:
		return []string{key}
	case ValidationScopeInterface:
		return implementors[key]
	}
	return nil
}

// validationTypeNoteMembershipSQL matches a diagnostic inside a note whose
// current resolved type is one of count bound type names. A type or interface
// scope is the union of this and the snapshot's explicit scope rows.
func validationTypeNoteMembershipSQL(count int) string {
	return `EXISTS (
	SELECT 1 FROM validation_diagnostic_paths tp
	JOIN ontology_note_types nt ON nt.note_path = tp.path
	WHERE tp.generation = d.generation AND tp.issue_key = d.issue_key
		AND tp.membership_kind = 'note' AND nt.type_name IN (` + strings.TrimSuffix(strings.Repeat("?,", count), ",") + `)
)`
}

func validationDiagnosticFilterSQL(generation int64, filter ValidationDiagnosticFilter) (string, []any) {
	clauses := []string{"d.generation = ?"}
	args := []any{generation}
	if filter.Check != "" {
		clauses = append(clauses, "d.check_name = ?")
		args = append(args, filter.Check)
	}
	if filter.Code != "" {
		clauses = append(clauses, "d.code = ?")
		args = append(args, filter.Code)
	}
	if filter.Variant != "" {
		clauses = append(clauses, "d.variant_key = ?")
		args = append(args, filter.Variant)
	}
	if filter.Path != "" {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM validation_diagnostic_paths p
			WHERE p.generation = d.generation AND p.issue_key = d.issue_key AND p.path = ?
		)`)
		args = append(args, filter.Path)
	}
	if filter.Text != "" {
		clauses = append(clauses, `(instr(lower(d.message), lower(?)) > 0
			OR instr(lower(d.code), lower(?)) > 0
			OR instr(lower(d.primary_path), lower(?)) > 0
			OR instr(lower(d.type_name), lower(?)) > 0
			OR instr(lower(d.field_name), lower(?)) > 0
			OR instr(lower(d.source), lower(?)) > 0
			OR instr(lower(d.target), lower(?)) > 0)`)
		for range 7 {
			args = append(args, filter.Text)
		}
	}
	if filter.ScopeKind != "" {
		if filter.ScopeKind == ValidationScopeFile || filter.ScopeKind == ValidationScopeNote {
			membership := "affected"
			if filter.ScopeKind == ValidationScopeNote {
				membership = "note"
			}
			clauses = append(clauses, `EXISTS (
				SELECT 1 FROM validation_diagnostic_paths p
				WHERE p.generation = d.generation AND p.issue_key = d.issue_key
					AND p.membership_kind = ? AND p.path = ?
			)`)
			args = append(args, membership, filter.ScopeKey)
		} else {
			scoped := `EXISTS (
				SELECT 1 FROM validation_diagnostic_scopes s
				WHERE s.generation = d.generation AND s.issue_key = d.issue_key
					AND s.scope_kind = ? AND s.scope_key = ?
			)`
			args = append(args, filter.ScopeKind, filter.ScopeKey)
			if typeNames := validationScopeTypeNames(filter.ScopeKind, filter.ScopeKey, filter.InterfaceImplementors); len(typeNames) > 0 {
				scoped = "(" + scoped + " OR " + validationTypeNoteMembershipSQL(len(typeNames)) + ")"
				for _, typeName := range typeNames {
					args = append(args, typeName)
				}
			}
			clauses = append(clauses, scoped)
		}
	}
	switch filter.RepairAvailability {
	case ValidationRepairAvailabilityPresent:
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM validation_action_issues ai
			WHERE ai.generation = d.generation AND ai.issue_key = d.issue_key
		)`)
	case ValidationRepairAvailabilityAbsent:
		clauses = append(clauses, `NOT EXISTS (
			SELECT 1 FROM validation_action_issues ai
			WHERE ai.generation = d.generation AND ai.issue_key = d.issue_key
		)`)
	case ValidationRepairAvailabilityApplicable, ValidationRepairAvailabilityInapplicable:
		applicable := `EXISTS (
			SELECT 1 FROM validation_action_issues ai
			JOIN validation_actions a ON a.generation = ai.generation AND a.action_id = ai.action_id
			WHERE ai.generation = d.generation AND ai.issue_key = d.issue_key
				AND a.safety IN ('safe', 'needs_confirmation')
		)`
		if filter.RepairAvailability == ValidationRepairAvailabilityInapplicable {
			applicable = "NOT " + applicable
		}
		clauses = append(clauses, applicable)
	}
	return strings.Join(clauses, " AND "), args
}
