package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func upsertMetadataNotes(ctx context.Context, tx *sql.Tx, notes []NoteMetadataRow, loadedAt int64) (map[string]int64, error) {
	noteIDs := make(map[string]int64, len(notes))
	if len(notes) == 0 {
		return noteIDs, nil
	}
	paths := make([]string, 0, len(notes))
	seen := make(map[string]struct{}, len(notes))
	values := make([][]any, 0, len(notes))
	for _, note := range notes {
		path := strings.TrimSpace(note.Path)
		if path == "" {
			continue
		}
		fullKey, baseKey, pathLen := noteLookupKeys(path)
		values = append(values, []any{path, note.Title, note.ContentHash, note.Mtime, note.Size, loadedAt, fullKey, baseKey, pathLen, firstSegmentNorm(path), normalizeNoteFormatID(note.FormatID)})
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if err := execValuesBatch(ctx, tx, `
		INSERT INTO notes(path, title, content_hash, mtime, size, indexed_at, note_key_full, note_key_base, path_len, first_segment_norm, format_id)
		VALUES
	`, values, 11, `
		ON CONFLICT(path) DO UPDATE SET
			title = excluded.title,
			content_hash = excluded.content_hash,
			mtime = excluded.mtime,
			size = excluded.size,
			indexed_at = excluded.indexed_at,
			note_key_full = excluded.note_key_full,
			note_key_base = excluded.note_key_base,
			path_len = excluded.path_len,
			first_segment_norm = excluded.first_segment_norm,
			format_id = excluded.format_id
	`); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return noteIDs, nil
	}
	return selectIDsByValues(ctx, tx, "notes", "id", "path", paths)
}

func upsertNoteProjectionStates(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, notes []NoteMetadataRow, loadedAt int64) error {
	values := make([][]any, 0, len(notes))
	for _, note := range notes {
		noteID, ok := noteIDs[strings.TrimSpace(note.Path)]
		if !ok {
			continue
		}
		state, err := normalizeNoteProjectionState(note.Projection, note.ContentHash, loadedAt)
		if err != nil {
			return fmt.Errorf("note projection state for %s: %w", note.Path, err)
		}
		values = append(values, []any{
			noteID,
			state.ProviderVersion,
			state.ProjectionVersion,
			state.SourceContentHash,
			string(state.Status),
			state.DiagnosticCode,
			state.DiagnosticDetail,
			state.UpdatedAt,
		})
	}
	return execValuesBatch(ctx, tx, `
		INSERT INTO note_projection_state(
			note_id, provider_version, projection_version, source_content_hash,
			status, diagnostic_code, diagnostic_detail, updated_at
		) VALUES
	`, values, 8, `
		ON CONFLICT(note_id) DO UPDATE SET
			provider_version = excluded.provider_version,
			projection_version = excluded.projection_version,
			source_content_hash = excluded.source_content_hash,
			status = excluded.status,
			diagnostic_code = excluded.diagnostic_code,
			diagnostic_detail = excluded.diagnostic_detail,
			updated_at = excluded.updated_at
	`)
}

func normalizeNoteFormatID(formatID string) string {
	if formatID = strings.TrimSpace(formatID); formatID != "" {
		return strings.ToLower(formatID)
	}
	return "markdown"
}

func normalizeNoteProjectionState(state NoteProjectionState, contentHash string, loadedAt int64) (NoteProjectionState, error) {
	if state.Status == "" {
		state.Status = NoteProjectionStatusStale
	}
	state.ProviderVersion = strings.TrimSpace(state.ProviderVersion)
	state.ProjectionVersion = strings.TrimSpace(state.ProjectionVersion)
	state.SourceContentHash = strings.TrimSpace(state.SourceContentHash)
	contentHash = strings.TrimSpace(contentHash)
	state.DiagnosticCode = strings.TrimSpace(state.DiagnosticCode)
	state.DiagnosticDetail = strings.TrimSpace(state.DiagnosticDetail)
	switch state.Status {
	case NoteProjectionStatusFatal:
		if state.DiagnosticCode == "" || state.DiagnosticDetail == "" {
			return NoteProjectionState{}, fmt.Errorf("fatal projections require non-empty diagnostic code and detail")
		}
		if state.ProviderVersion == "" || state.ProjectionVersion == "" {
			return NoteProjectionState{}, fmt.Errorf("fatal projections require non-empty provider and projection versions")
		}
		if state.SourceContentHash != contentHash {
			return NoteProjectionState{}, fmt.Errorf("fatal projection source hash must match the note content hash when source is available")
		}
	case NoteProjectionStatusCurrent:
		if state.DiagnosticCode != "" || state.DiagnosticDetail != "" {
			return NoteProjectionState{}, fmt.Errorf("%s projections must not retain a blocking diagnostic", state.Status)
		}
		if state.ProviderVersion == "" || state.ProjectionVersion == "" {
			return NoteProjectionState{}, fmt.Errorf("current projections require non-empty provider and projection versions")
		}
		if contentHash == "" || state.SourceContentHash != contentHash {
			return NoteProjectionState{}, fmt.Errorf("current projection source hash must match the note content hash")
		}
	case NoteProjectionStatusStale:
		if state.DiagnosticCode != "" || state.DiagnosticDetail != "" {
			return NoteProjectionState{}, fmt.Errorf("%s projections must not retain a blocking diagnostic", state.Status)
		}
	default:
		return NoteProjectionState{}, fmt.Errorf("unsupported status %q", state.Status)
	}
	if state.UpdatedAt == 0 {
		state.UpdatedAt = loadedAt
	}
	if state.UpdatedAt < 0 {
		return NoteProjectionState{}, fmt.Errorf("updated_at must not be negative")
	}
	return state, nil
}
