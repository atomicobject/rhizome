package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

type validationReadQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validationSnapshotChecks(ctx context.Context, query validationReadQuerier, generation int64) ([]ValidationCheckSnapshot, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT check_name, outcome, issue_count, summary, notes_json, error, duration_ms
		FROM validation_checks
		WHERE generation = ?
		ORDER BY check_order
	`, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var checks []ValidationCheckSnapshot
	for rows.Next() {
		var check ValidationCheckSnapshot
		var notesJSON string
		if err := rows.Scan(&check.Check, &check.Outcome, &check.IssueCount, &check.Summary, &notesJSON, &check.Error, &check.DurationMs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(notesJSON), &check.Notes); err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, rows.Err()
}

func validationSnapshotActions(ctx context.Context, query validationReadQuerier, generation int64) ([]ValidationActionSnapshot, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT action_id, check_name, issue_code, kind, safety, title, summary, question,
			instance_count, candidate_paths_json
		FROM validation_actions
		WHERE generation = ?
		ORDER BY action_id
	`, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var actions []ValidationActionSnapshot
	positions := make(map[string]int)
	for rows.Next() {
		var action ValidationActionSnapshot
		var candidatePathsJSON string
		if err := rows.Scan(&action.ID, &action.Check, &action.IssueCode, &action.Kind, &action.Safety,
			&action.Title, &action.Summary, &action.Question, &action.InstanceCount, &candidatePathsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(candidatePathsJSON), &action.CandidatePaths); err != nil {
			return nil, err
		}
		positions[action.ID] = len(actions)
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	issueRows, err := query.QueryContext(ctx, `
		SELECT action_id, issue_key FROM validation_action_issues
		WHERE generation = ? ORDER BY action_id, issue_key
	`, generation)
	if err != nil {
		return nil, err
	}
	for issueRows.Next() {
		var actionID, issueKey string
		if err := issueRows.Scan(&actionID, &issueKey); err != nil {
			_ = issueRows.Close()
			return nil, err
		}
		if position, ok := positions[actionID]; ok {
			actions[position].IssueKeys = append(actions[position].IssueKeys, issueKey)
		}
	}
	if err := issueRows.Close(); err != nil {
		return nil, err
	}
	pathRows, err := query.QueryContext(ctx, `
		SELECT action_id, path FROM validation_action_paths
		WHERE generation = ? ORDER BY action_id, path
	`, generation)
	if err != nil {
		return nil, err
	}
	defer pathRows.Close()
	for pathRows.Next() {
		var actionID, path string
		if err := pathRows.Scan(&actionID, &path); err != nil {
			return nil, err
		}
		if position, ok := positions[actionID]; ok {
			actions[position].AffectedPaths = append(actions[position].AffectedPaths, path)
		}
	}
	return actions, pathRows.Err()
}

func attachValidationDiagnosticMemberships(ctx context.Context, query validationReadQuerier, generation int64, diagnostics []ValidationDiagnostic) error {
	positions := make(map[string]int, len(diagnostics))
	args := make([]any, 0, len(diagnostics)+1)
	args = append(args, generation)
	placeholders := make([]string, 0, len(diagnostics))
	for i := range diagnostics {
		positions[diagnostics[i].IssueKey] = i
		args = append(args, diagnostics[i].IssueKey)
		placeholders = append(placeholders, "?")
	}
	if len(diagnostics) == 0 {
		return nil
	}
	pathRows, err := query.QueryContext(ctx, `
		SELECT p.issue_key, p.path, p.membership_kind
		FROM validation_diagnostic_paths p
		WHERE p.generation = ? AND p.issue_key IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY p.issue_key, p.membership_kind, p.path
	`, args...)
	if err != nil {
		return err
	}
	for pathRows.Next() {
		var issueKey, path, kind string
		if err := pathRows.Scan(&issueKey, &path, &kind); err != nil {
			_ = pathRows.Close()
			return err
		}
		position, ok := positions[issueKey]
		if !ok {
			continue
		}
		if kind == "note" {
			diagnostics[position].AffectedNotePaths = append(diagnostics[position].AffectedNotePaths, path)
		} else {
			diagnostics[position].AffectedPaths = append(diagnostics[position].AffectedPaths, path)
		}
	}
	if err := pathRows.Close(); err != nil {
		return err
	}
	scopeRows, err := query.QueryContext(ctx, `
		SELECT s.issue_key, s.scope_kind, s.scope_key
		FROM validation_diagnostic_scopes s
		WHERE s.generation = ? AND s.issue_key IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY s.issue_key, s.scope_kind, s.scope_key
	`, args...)
	if err != nil {
		return err
	}
	for scopeRows.Next() {
		var issueKey, kind, key string
		if err := scopeRows.Scan(&issueKey, &kind, &key); err != nil {
			_ = scopeRows.Close()
			return err
		}
		position, ok := positions[issueKey]
		if !ok {
			continue
		}
		switch kind {
		case ValidationScopeNode:
			diagnostics[position].AffectedNodeIDs = append(diagnostics[position].AffectedNodeIDs, key)
		case ValidationScopeType:
			diagnostics[position].AffectedTypes = append(diagnostics[position].AffectedTypes, key)
		case ValidationScopeInterface:
			diagnostics[position].AffectedInterfaces = append(diagnostics[position].AffectedInterfaces, key)
		}
	}
	if err := scopeRows.Close(); err != nil {
		return err
	}
	actionRows, err := query.QueryContext(ctx, `
		SELECT ai.issue_key, ai.action_id
		FROM validation_action_issues ai
		WHERE ai.generation = ? AND ai.issue_key IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY ai.issue_key, ai.action_id
	`, args...)
	if err != nil {
		return err
	}
	defer actionRows.Close()
	for actionRows.Next() {
		var issueKey, actionID string
		if err := actionRows.Scan(&issueKey, &actionID); err != nil {
			return err
		}
		if position, ok := positions[issueKey]; ok {
			diagnostics[position].ActionIDs = append(diagnostics[position].ActionIDs, actionID)
		}
	}
	return actionRows.Err()
}

func validationSnapshotIssueCodes(ctx context.Context, query validationReadQuerier, generation int64) ([]string, error) {
	rows, err := query.QueryContext(ctx, `SELECT DISTINCT code FROM validation_diagnostics WHERE generation = ? AND code != '' ORDER BY code`, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
}
