package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

func (s *Store) GetValidationScopeSummaries(ctx context.Context, request ValidationScopeSummaryRequest) (ValidationScopeSummaryResponse, error) {
	request, err := normalizeValidationScopeSummaryRequest(request)
	if err != nil {
		return ValidationScopeSummaryResponse{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ValidationScopeSummaryResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM validation_generations WHERE generation = ?`, request.Generation).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ValidationScopeSummaryResponse{}, ErrValidationGenerationExpired
		}
		return ValidationScopeSummaryResponse{}, err
	}
	if len(request.Scopes) == 0 {
		if err := tx.Commit(); err != nil {
			return ValidationScopeSummaryResponse{}, err
		}
		return ValidationScopeSummaryResponse{Generation: request.Generation, Summaries: []ValidationScopeSummary{}}, nil
	}

	requested, args := validationScopeRequestedCTEs(request.Scopes, request.Filter.InterfaceImplementors)
	filterWhere, filterArgs := validationDiagnosticFilterSQL(request.Generation, request.Filter)
	args = append(args, request.Generation, request.Generation, request.Generation, request.Generation, request.Generation)
	args = append(args, filterArgs...)
	rows, err := tx.QueryContext(ctx, `
		WITH `+requested+`,
		`+validationScopeCandidatesCTE+`,
        matched AS (
            SELECT c.ordinal, d.issue_key FROM candidates c
            JOIN validation_diagnostics d ON d.generation = ? AND d.issue_key = c.issue_key
            WHERE `+filterWhere+`
        )
		SELECT r.ordinal, r.scope_kind, r.scope_key,
			COUNT(DISTINCT m.issue_key),
			COUNT(DISTINCT fp.path),
			COUNT(DISTINCT np.path),
			COUNT(DISTINCT ai.action_id)
		FROM requested r
		LEFT JOIN matched m ON m.ordinal = r.ordinal
		LEFT JOIN validation_diagnostic_paths fp
			ON fp.generation = ? AND fp.issue_key = m.issue_key AND fp.membership_kind = 'affected'
		LEFT JOIN validation_diagnostic_paths np
			ON np.generation = ? AND np.issue_key = m.issue_key AND np.membership_kind = 'note'
		LEFT JOIN validation_action_issues ai
			ON ai.generation = ? AND ai.issue_key = m.issue_key
		GROUP BY r.ordinal, r.scope_kind, r.scope_key
		ORDER BY r.ordinal
	`, append(args, request.Generation, request.Generation, request.Generation)...)
	if err != nil {
		return ValidationScopeSummaryResponse{}, err
	}
	defer rows.Close()
	summaries := make([]ValidationScopeSummary, 0, len(request.Scopes))
	for rows.Next() {
		var summary ValidationScopeSummary
		if err := rows.Scan(&exists, &summary.Scope.Kind, &summary.Scope.Key, &summary.IssueCount,
			&summary.AffectedFileCount, &summary.AffectedNoteCount, &summary.RepairActionCount); err != nil {
			return ValidationScopeSummaryResponse{}, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return ValidationScopeSummaryResponse{}, err
	}
	if len(summaries) != len(request.Scopes) {
		return ValidationScopeSummaryResponse{}, fmt.Errorf("validation scope summary returned %d rows for %d scopes", len(summaries), len(request.Scopes))
	}
	if err := tx.Commit(); err != nil {
		return ValidationScopeSummaryResponse{}, err
	}
	return ValidationScopeSummaryResponse{Generation: request.Generation, Summaries: summaries}, nil
}

// validationScopeRequestedCTEs binds scopes as the requested(ordinal,
// scope_kind, scope_key) CTE, and the resolved note types each type or
// interface scope covers as requested_types(ordinal, type_name).
func validationScopeRequestedCTEs(scopes []ValidationScope, implementors map[string][]string) (string, []any) {
	requested := make([]string, 0, len(scopes))
	args := make([]any, 0, len(scopes)*3)
	for ordinal, scope := range scopes {
		requested = append(requested, "(?, ?, ?)")
		args = append(args, ordinal, scope.Kind, scope.Key)
	}
	var types []string
	for ordinal, scope := range scopes {
		for _, typeName := range validationScopeTypeNames(scope.Kind, scope.Key, implementors) {
			types = append(types, "(?, ?)")
			args = append(args, ordinal, typeName)
		}
	}
	typeRows := "SELECT NULL, NULL WHERE 0"
	if len(types) > 0 {
		typeRows = "VALUES " + strings.Join(types, ",")
	}
	return "requested(ordinal, scope_kind, scope_key) AS (VALUES " + strings.Join(requested, ",") + "), " +
		"requested_types(ordinal, type_name) AS (" + typeRows + ")", args
}

// validationScopeCandidatesCTE selects (ordinal, issue_key) for every row of
// the requested and requested_types CTEs from validationScopeRequestedCTEs. It
// binds the generation four times.
const validationScopeCandidatesCTE = `candidates AS (
            SELECT r.ordinal, d.issue_key FROM requested r
            JOIN validation_diagnostics d ON d.generation = ?
            WHERE r.scope_kind = 'global'
            UNION
            SELECT r.ordinal, p.issue_key FROM requested r
            JOIN validation_diagnostic_paths p ON p.generation = ? AND p.path = r.scope_key
            WHERE r.scope_kind IN ('file', 'note')
                AND (r.scope_kind = 'file' OR p.membership_kind = 'note')
            UNION
            SELECT r.ordinal, s.issue_key FROM requested r
            JOIN validation_diagnostic_scopes s ON s.generation = ?
                AND s.scope_kind = r.scope_kind AND s.scope_key = r.scope_key
            WHERE r.scope_kind IN ('node', 'type', 'interface')
            UNION
            SELECT rt.ordinal, p.issue_key FROM requested_types rt
            JOIN ontology_note_types nt ON nt.type_name = rt.type_name
            JOIN validation_diagnostic_paths p ON p.generation = ? AND p.path = nt.note_path
                AND p.membership_kind = 'note'
        )`

// GetValidationIssueGroups counts one generation's diagnostics in one scope by
// check, code, and variant with a single GROUP BY. Counts use the same scope
// candidates and filter as GetValidationScopeSummaries, so a group equals the
// summary for its filter narrowed to that check, code, and variant. Codes are
// ordered by their first diagnostic; within a code, by issue count descending
// then variant key, with variant-free issues as the row with an empty key.
func (s *Store) GetValidationIssueGroups(ctx context.Context, request ValidationIssueGroupRequest) (ValidationIssueGroupResponse, error) {
	scope := request.Scope
	if strings.TrimSpace(scope.Kind) == "" {
		scope.Kind = ValidationScopeGlobal
	}
	normalized, err := normalizeValidationScopeSummaryRequest(ValidationScopeSummaryRequest{
		Generation: request.Generation, Scopes: []ValidationScope{scope}, Filter: request.Filter,
	})
	if err != nil {
		return ValidationIssueGroupResponse{}, err
	}
	generation, scope := normalized.Generation, normalized.Scopes[0]
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ValidationIssueGroupResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM validation_generations WHERE generation = ?`, generation).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ValidationIssueGroupResponse{}, ErrValidationGenerationExpired
		}
		return ValidationIssueGroupResponse{}, err
	}
	filterWhere, filterArgs := validationDiagnosticFilterSQL(generation, normalized.Filter)
	requested, args := validationScopeRequestedCTEs([]ValidationScope{scope}, normalized.Filter.InterfaceImplementors)
	args = append(args, generation, generation, generation, generation, generation)
	args = append(args, filterArgs...)
	args = append(args, generation, generation)
	rows, err := tx.QueryContext(ctx, `
		WITH `+requested+`,
		`+validationScopeCandidatesCTE+`,
		matched AS (
			SELECT d.issue_key, d.check_name, d.code, d.variant_key, d.variant_label, d.diagnostic_order
			FROM candidates c
			JOIN validation_diagnostics d ON d.generation = ? AND d.issue_key = c.issue_key
			WHERE `+filterWhere+`
		),
		ranked AS (
			-- Variants beyond the per-code limit share one bucket (NULL key), so the
			-- response stays bounded while distinct counts stay exact.
			SELECT check_name, code, variant_key,
				CASE WHEN variant_key <> '' AND ROW_NUMBER() OVER (
					PARTITION BY check_name, code, variant_key = ''
					ORDER BY COUNT(*) DESC, variant_key
				) > `+strconv.Itoa(ValidationIssueGroupVariantLimit)+` THEN NULL ELSE variant_key END AS bucket_key
			FROM matched
			GROUP BY check_name, code, variant_key
		),
		grouped AS (
			SELECT m.check_name, m.code, r.bucket_key,
				CASE WHEN r.bucket_key IS NULL THEN '' ELSE MIN(m.variant_label) END AS variant_label,
				MIN(m.diagnostic_order) AS first_order,
				COUNT(DISTINCT m.issue_key) AS issue_count,
				COUNT(DISTINCT fp.path) AS file_count,
				COUNT(DISTINCT CASE WHEN a.safety IN ('safe', 'needs_confirmation') THEN m.issue_key END) AS applicable_count,
				CASE WHEN r.bucket_key IS NULL THEN COUNT(DISTINCT m.variant_key) ELSE 0 END AS other_variants
			FROM matched m
			JOIN ranked r ON r.check_name = m.check_name AND r.code = m.code AND r.variant_key = m.variant_key
			LEFT JOIN validation_diagnostic_paths fp
				ON fp.generation = ? AND fp.issue_key = m.issue_key AND fp.membership_kind = 'affected'
			LEFT JOIN validation_action_issues ai
				ON ai.generation = ? AND ai.issue_key = m.issue_key
			LEFT JOIN validation_actions a
				ON a.generation = ai.generation AND a.action_id = ai.action_id
			GROUP BY m.check_name, m.code, r.bucket_key
		)
		SELECT check_name, code, COALESCE(bucket_key, ''), variant_label, issue_count, file_count, applicable_count,
			other_variants, MIN(first_order) OVER (PARTITION BY check_name, code) AS code_order
		FROM grouped
		ORDER BY code_order, other_variants > 0, issue_count DESC, bucket_key
	`, args...)
	if err != nil {
		return ValidationIssueGroupResponse{}, err
	}
	defer rows.Close()
	groups := make([]ValidationIssueGroup, 0)
	for rows.Next() {
		var group ValidationIssueGroup
		var variantKey, variantLabel string
		var codeOrder int
		if err := rows.Scan(&group.Check, &group.Code, &variantKey, &variantLabel, &group.IssueCount,
			&group.AffectedFileCount, &group.ApplicableRepairCount, &group.OtherVariants, &codeOrder); err != nil {
			return ValidationIssueGroupResponse{}, err
		}
		group.Variant = validationIssueVariant(variantKey, variantLabel)
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return ValidationIssueGroupResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return ValidationIssueGroupResponse{}, err
	}
	return ValidationIssueGroupResponse{Generation: generation, Groups: groups}, nil
}

func normalizeValidationScopeSummaryRequest(request ValidationScopeSummaryRequest) (ValidationScopeSummaryRequest, error) {
	if request.Generation <= 0 {
		return ValidationScopeSummaryRequest{}, fmt.Errorf("%w: generation must be positive", ErrValidationScopeRequest)
	}
	if len(request.Scopes) > ValidationScopeBatchMax {
		return ValidationScopeSummaryRequest{}, fmt.Errorf("%w: at most %d scopes are allowed", ErrValidationScopeRequest, ValidationScopeBatchMax)
	}
	normalizedPage, _, err := normalizeValidationDiagnosticPageRequest(ValidationDiagnosticPageRequest{
		Generation: request.Generation, Limit: 1, Filter: request.Filter,
	})
	if err != nil {
		return ValidationScopeSummaryRequest{}, fmt.Errorf("%w: %v", ErrValidationScopeRequest, err)
	}
	request.Filter = normalizedPage.Filter
	request.Scopes = append([]ValidationScope(nil), request.Scopes...)
	for index := range request.Scopes {
		scope := &request.Scopes[index]
		scope.Kind = strings.TrimSpace(scope.Kind)
		scope.Key = strings.TrimSpace(scope.Key)
		switch scope.Kind {
		case ValidationScopeGlobal:
			if scope.Key != "" {
				return ValidationScopeSummaryRequest{}, fmt.Errorf("%w: global scope must not have a key", ErrValidationScopeRequest)
			}
		case ValidationScopeFile, ValidationScopeNote, ValidationScopeNode, ValidationScopeType, ValidationScopeInterface:
			if scope.Key == "" {
				return ValidationScopeSummaryRequest{}, fmt.Errorf("%w: %s scope requires a key", ErrValidationScopeRequest, scope.Kind)
			}
		default:
			return ValidationScopeSummaryRequest{}, fmt.Errorf("%w: unknown scope kind %q", ErrValidationScopeRequest, scope.Kind)
		}
	}
	return request, nil
}
