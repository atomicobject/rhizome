package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Store) PublishValidationSnapshot(ctx context.Context, snapshot ValidationSnapshot) (bool, error) {
	if err := validateValidationSnapshot(snapshot); err != nil {
		return false, err
	}
	selectedChecksJSON, err := json.Marshal(snapshot.SelectedChecks)
	if err != nil {
		return false, err
	}
	published := false
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var currentGeneration int64
		if err := tx.QueryRowContext(ctx, `SELECT generation FROM validation_state WHERE id = 1`).Scan(&currentGeneration); err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return err
		}
		if currentGeneration != snapshot.Generation {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO validation_generations(
				generation, vault_identity, scope, selected_checks_json, schema_identity,
				config_identity, input_revision, started_at, finished_at, duration_ms, completion,
				stale_reason, issue_count, error_count, affected_file_count,
				affected_note_count, repair_action_count, repair_plan_fingerprint
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, snapshot.Generation, snapshot.VaultIdentity, snapshot.Scope, string(selectedChecksJSON),
			snapshot.SchemaIdentity, snapshot.ConfigIdentity, snapshot.InputRevision,
			snapshot.StartedAt, snapshot.FinishedAt, snapshot.DurationMs, snapshot.Completion, snapshot.StaleReason,
			snapshot.IssueCount, snapshot.ErrorCount, snapshot.AffectedFileCount,
			snapshot.AffectedNoteCount, snapshot.RepairActionCount, snapshot.RepairPlanFingerprint); err != nil {
			return err
		}
		if err := insertValidationSnapshotChecks(ctx, tx, snapshot); err != nil {
			return err
		}
		if err := insertValidationSnapshotDiagnostics(ctx, tx, snapshot); err != nil {
			return err
		}
		if err := insertValidationSnapshotActions(ctx, tx, snapshot); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE validation_state
			SET status = ?, result_json = '', error = '', published_generation = ?,
				finished_at = ?, duration_ms = ?
			WHERE id = 1 AND generation = ?
		`, ValidationStatusOK, snapshot.Generation, snapshot.FinishedAt, snapshot.DurationMs, snapshot.Generation)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("validation generation changed during publication")
		}
		published = true
		_, err = tx.ExecContext(ctx, `
			DELETE FROM validation_generations
			WHERE generation NOT IN (
				SELECT generation FROM validation_generations ORDER BY generation DESC LIMIT 2
			)
		`)
		return err
	})
	return published, err
}

// MarkPublishedValidationStale retains the last complete snapshot while making
// its freshness explicit to every reader and fences every earlier run, even
// without a retained snapshot. A later request can publish a fresh generation.
func (s *Store) MarkPublishedValidationStale(ctx context.Context, reason string) (bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "validation inputs changed"
	}
	var updated bool
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE validation_generations
			SET stale_reason = ?
			WHERE generation = (
				SELECT published_generation FROM validation_state WHERE id = 1
			) AND stale_reason <> ?
		`, reason, reason)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		updated = count > 0
		_, err = tx.ExecContext(ctx, `
			UPDATE validation_state
			SET generation = generation + 1,
				status = CASE WHEN status = ? THEN
					CASE WHEN published_generation > 0 THEN ? ELSE ? END
					ELSE status END
			WHERE id = 1
		`, ValidationStatusRunning, ValidationStatusOK, ValidationStatusNeverRan)
		return err
	})
	return updated, err
}

func validateValidationSnapshot(snapshot ValidationSnapshot) error {
	if snapshot.Generation <= 0 {
		return fmt.Errorf("validation snapshot generation must be positive")
	}
	if strings.TrimSpace(snapshot.VaultIdentity) == "" {
		return fmt.Errorf("validation snapshot vault identity is required")
	}
	if snapshot.Completion != ValidationCompletionComplete && snapshot.Completion != ValidationCompletionIncomplete {
		return fmt.Errorf("invalid validation snapshot completion %q", snapshot.Completion)
	}
	if snapshot.DurationMs < 0 {
		return fmt.Errorf("validation snapshot duration must not be negative")
	}
	if snapshot.IssueCount != len(snapshot.Diagnostics) {
		return fmt.Errorf("validation snapshot issue count %d does not match %d diagnostics", snapshot.IssueCount, len(snapshot.Diagnostics))
	}
	if snapshot.RepairActionCount != len(snapshot.Actions) {
		return fmt.Errorf("validation snapshot action count %d does not match %d actions", snapshot.RepairActionCount, len(snapshot.Actions))
	}
	if len(snapshot.SelectedChecks) != len(snapshot.Checks) {
		return fmt.Errorf("validation snapshot has %d selected checks and %d check outcomes", len(snapshot.SelectedChecks), len(snapshot.Checks))
	}
	selected := make(map[string]struct{}, len(snapshot.SelectedChecks))
	for _, check := range snapshot.SelectedChecks {
		if strings.TrimSpace(check) == "" {
			return fmt.Errorf("validation snapshot selected check is empty")
		}
		if _, duplicate := selected[check]; duplicate {
			return fmt.Errorf("validation snapshot selected check %q is duplicated", check)
		}
		selected[check] = struct{}{}
	}
	failedChecks := 0
	for _, check := range snapshot.Checks {
		if _, ok := selected[check.Check]; !ok {
			return fmt.Errorf("validation snapshot check outcome %q is not selected", check.Check)
		}
		delete(selected, check.Check)
		switch check.Outcome {
		case ValidationCheckOutcomeCompleted, ValidationCheckOutcomeBlocked, ValidationCheckOutcomeNotApplicable, ValidationCheckOutcomeSkipped:
		case ValidationCheckOutcomeFailed:
			failedChecks++
		default:
			return fmt.Errorf("validation snapshot check %q has invalid outcome %q", check.Check, check.Outcome)
		}
	}
	if len(selected) != 0 {
		return fmt.Errorf("validation snapshot is missing selected check outcomes")
	}
	if snapshot.ErrorCount != failedChecks {
		return fmt.Errorf("validation snapshot error count %d does not match %d failed checks", snapshot.ErrorCount, failedChecks)
	}
	issues := make(map[string]struct{}, len(snapshot.Diagnostics))
	affectedFiles := make(map[string]struct{})
	affectedNotes := make(map[string]struct{})
	for _, diagnostic := range snapshot.Diagnostics {
		if strings.TrimSpace(diagnostic.IssueKey) == "" || strings.TrimSpace(diagnostic.Check) == "" {
			return fmt.Errorf("validation diagnostic requires issue key and check")
		}
		if _, duplicate := issues[diagnostic.IssueKey]; duplicate {
			return fmt.Errorf("duplicate validation diagnostic issue key %q", diagnostic.IssueKey)
		}
		issues[diagnostic.IssueKey] = struct{}{}
		if len(diagnostic.Evidence) > 0 && !json.Valid(diagnostic.Evidence) {
			return fmt.Errorf("validation diagnostic %q has invalid evidence JSON", diagnostic.IssueKey)
		}
		if diagnostic.Location != nil && diagnostic.Location.Unit != ValidationLocationUnitUTF8Bytes && diagnostic.Location.Unit != ValidationLocationUnitLine {
			return fmt.Errorf("validation diagnostic %q has invalid location unit %q", diagnostic.IssueKey, diagnostic.Location.Unit)
		}
		for _, path := range diagnostic.AffectedPaths {
			if strings.TrimSpace(path) != "" {
				affectedFiles[path] = struct{}{}
			}
		}
		for _, path := range diagnostic.AffectedNotePaths {
			if strings.TrimSpace(path) != "" {
				affectedNotes[path] = struct{}{}
			}
		}
	}
	if snapshot.AffectedFileCount != len(affectedFiles) {
		return fmt.Errorf("validation snapshot affected file count %d does not match %d diagnostic paths", snapshot.AffectedFileCount, len(affectedFiles))
	}
	if snapshot.AffectedNoteCount != len(affectedNotes) {
		return fmt.Errorf("validation snapshot affected note count %d does not match %d diagnostic note paths", snapshot.AffectedNoteCount, len(affectedNotes))
	}
	for _, action := range snapshot.Actions {
		if strings.TrimSpace(action.ID) == "" || strings.TrimSpace(action.Check) == "" || strings.TrimSpace(action.Kind) == "" {
			return fmt.Errorf("validation action requires id, check, and kind")
		}
		for _, issueKey := range action.IssueKeys {
			if _, ok := issues[issueKey]; !ok {
				return fmt.Errorf("validation action %q references unknown issue %q", action.ID, issueKey)
			}
		}
	}
	return nil
}

func (s *Store) GetPublishedValidationSnapshot(ctx context.Context) (ValidationSnapshot, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ValidationSnapshot{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	snapshot, ok, err := getPublishedValidationSnapshot(ctx, tx)
	if err != nil {
		return ValidationSnapshot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ValidationSnapshot{}, false, err
	}
	return snapshot, ok, nil
}

// GetValidationStateSnapshot reads lifecycle state and the retained published
// snapshot from one SQLite read transaction. A response therefore cannot pair
// a state row from one publication with checks/actions from another.
func (s *Store) GetValidationStateSnapshot(ctx context.Context) (ValidationStateSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ValidationStateSnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := validationStateFromQuery(ctx, tx)
	if err != nil {
		return ValidationStateSnapshot{}, err
	}
	snapshot, ok, err := getPublishedValidationSnapshot(ctx, tx)
	if err != nil {
		return ValidationStateSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return ValidationStateSnapshot{}, err
	}
	return ValidationStateSnapshot{State: state, Snapshot: snapshot, HasSnapshot: ok}, nil
}

func validationStateFromQuery(ctx context.Context, query validationReadQuerier) (ValidationState, error) {
	row := query.QueryRowContext(ctx, `
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

func getPublishedValidationSnapshot(ctx context.Context, query validationReadQuerier) (ValidationSnapshot, bool, error) {
	var snapshot ValidationSnapshot
	var selectedChecksJSON string
	err := query.QueryRowContext(ctx, `
		SELECT g.vault_identity, g.generation, g.scope, g.selected_checks_json, g.schema_identity,
			g.config_identity, g.input_revision, g.started_at, g.finished_at, g.duration_ms, g.completion,
			g.stale_reason, g.issue_count, g.error_count, g.affected_file_count,
			g.affected_note_count, g.repair_action_count, g.repair_plan_fingerprint
		FROM validation_state s
		JOIN validation_generations g ON g.generation = s.published_generation
		WHERE s.id = 1 AND s.published_generation > 0
	`).Scan(&snapshot.VaultIdentity, &snapshot.Generation, &snapshot.Scope, &selectedChecksJSON,
		&snapshot.SchemaIdentity, &snapshot.ConfigIdentity, &snapshot.InputRevision, &snapshot.StartedAt,
		&snapshot.FinishedAt, &snapshot.DurationMs, &snapshot.Completion, &snapshot.StaleReason, &snapshot.IssueCount,
		&snapshot.ErrorCount, &snapshot.AffectedFileCount, &snapshot.AffectedNoteCount,
		&snapshot.RepairActionCount, &snapshot.RepairPlanFingerprint)
	if err == sql.ErrNoRows {
		return ValidationSnapshot{}, false, nil
	}
	if err != nil {
		return ValidationSnapshot{}, false, err
	}
	if err := json.Unmarshal([]byte(selectedChecksJSON), &snapshot.SelectedChecks); err != nil {
		return ValidationSnapshot{}, false, err
	}
	checks, err := validationSnapshotChecks(ctx, query, snapshot.Generation)
	if err != nil {
		return ValidationSnapshot{}, false, err
	}
	snapshot.Checks = checks
	actions, err := validationSnapshotActions(ctx, query, snapshot.Generation)
	if err != nil {
		return ValidationSnapshot{}, false, err
	}
	snapshot.Actions = actions
	snapshot.IssueCodes, err = validationSnapshotIssueCodes(ctx, query, snapshot.Generation)
	if err != nil {
		return ValidationSnapshot{}, false, err
	}
	return snapshot, true, nil
}
