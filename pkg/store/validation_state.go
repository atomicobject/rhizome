package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
	"time"
)

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
