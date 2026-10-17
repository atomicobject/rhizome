package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func validateNoteProjectionStateSchemaTx(ctx context.Context, tx *sql.Tx) error {
	var ddl string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(sql, '')
		FROM sqlite_master
		WHERE type = 'table' AND name = 'note_projection_state'
		LIMIT 1
	`).Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return intelSchemaDrift("missing required table note_projection_state")
		}
		return fmt.Errorf("load note projection state schema: %w", err)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(ddl)), "")
	for _, required := range []string{
		"strict",
		"foreignkey(note_id)referencesnotes(id)ondeletecascade",
		"check(statusin('stale','current','fatal'))",
		"status='current'andlength(trim(provider_version))>0andlength(trim(projection_version))>0andlength(trim(source_content_hash))>0anddiagnostic_code=''anddiagnostic_detail=''",
		"status='fatal'andlength(trim(provider_version))>0andlength(trim(projection_version))>0andlength(trim(diagnostic_code))>0andlength(trim(diagnostic_detail))>0",
		"status='stale'anddiagnostic_code=''anddiagnostic_detail=''",
	} {
		if !strings.Contains(normalized, required) {
			return intelSchemaDrift("invalid note_projection_state schema: missing %q", required)
		}
	}
	return nil
}

func validateNoteFormatIDSchemaTx(ctx context.Context, tx *sql.Tx) error {
	var ddl string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(sql, '')
		FROM sqlite_master
		WHERE type = 'table' AND name = 'notes'
		LIMIT 1
	`).Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return intelSchemaDrift("missing required table notes")
		}
		return fmt.Errorf("load notes schema: %w", err)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(ddl)), "")
	for _, required := range []string{
		"format_idtextnotnulldefault'markdown'",
		"check(format_id!=''andformat_id=lower(trim(format_id)))",
	} {
		if !strings.Contains(normalized, required) {
			return intelSchemaDrift("invalid notes schema: missing %q", required)
		}
	}
	return nil
}
