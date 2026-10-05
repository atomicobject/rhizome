package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ownershipEffectiveTransitionsTx drops an idempotent note source publication.
// The observation time is diagnostic bookkeeping rather than authored-source
// provenance: re-observing identical bytes, filesystem identity, provider
// versions, projection status, and diagnostics must not retire durable facts or
// create a fresh reconciliation generation.
func ownershipEffectiveTransitionsTx(ctx context.Context, tx *sql.Tx, transitions []OwnershipTransition) ([]OwnershipTransition, error) {
	states, err := ownershipCurrentStatesTx(ctx, tx, transitions)
	if err != nil {
		return nil, err
	}
	effective := make([]OwnershipTransition, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Target == OwnershipTargetNote {
			current := states[transition.Path]
			if current.noteFound && !current.codeOwned && (ownershipSourceStateMatchesRow(*transition.Note, current.note) || ownershipSourceObservationMatchesRow(*transition.Note, current.note) || ownershipPreparedCurrentMatchesRow(*transition.Note, current.note)) {
				continue
			}
		}
		effective = append(effective, transition)
	}
	return effective, nil
}

type ownershipCurrentState struct {
	note      NoteMetadataRow
	noteFound bool
	codeOwned bool
}

func ownershipCurrentStatesTx(ctx context.Context, tx *sql.Tx, transitions []OwnershipTransition) (map[string]ownershipCurrentState, error) {
	notePaths := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Target == OwnershipTargetNote {
			notePaths = append(notePaths, transition.Path)
		}
	}
	states := make(map[string]ownershipCurrentState, len(notePaths))
	const batchSize = 400 // Two bound parameters per path; stay below SQLite's conservative limit.
	for start := 0; start < len(notePaths); start += batchSize {
		end := start + batchSize
		if end > len(notePaths) {
			end = len(notePaths)
		}
		batch := notePaths[start:end]
		values := strings.TrimSuffix(strings.Repeat("(?, ?),", len(batch)), ",")
		args := make([]any, 0, len(batch)*2)
		for _, path := range batch {
			args = append(args, path, normalizeCodeLookupPath(path))
		}
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
			WITH requested(path, code_path) AS (VALUES %s)
			SELECT requested.path,
				EXISTS(SELECT 1 FROM files WHERE files.path = requested.code_path),
				n.id IS NOT NULL,
				COALESCE(n.id, 0), requested.path, COALESCE(n.title, ''), COALESCE(n.content_hash, ''), COALESCE(n.indexer_version, ''),
				COALESCE(n.mtime, 0), COALESCE(n.size, 0), COALESCE(n.indexed_at, 0), COALESCE(n.format_id, 'markdown'),
				COALESCE(p.provider_version, ''), COALESCE(p.projection_version, ''), COALESCE(p.source_content_hash, ''),
				COALESCE(p.status, 'stale'), COALESCE(p.diagnostic_code, ''), COALESCE(p.diagnostic_detail, ''), COALESCE(p.updated_at, 0)
			FROM requested
			LEFT JOIN notes n ON n.path = requested.path AND n.indexed_at > 0
			LEFT JOIN note_projection_state p ON p.note_id = n.id
		`, values), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var path string
			var codeOwned, noteFound bool
			var note NoteMetadataRow
			if err := rows.Scan(
				&path, &codeOwned, &noteFound,
				&note.NoteID, &note.Path, &note.Title, &note.ContentHash, &note.IndexerVersion,
				&note.Mtime, &note.Size, &note.IndexedAt, &note.FormatID,
				&note.Projection.ProviderVersion, &note.Projection.ProjectionVersion, &note.Projection.SourceContentHash,
				&note.Projection.Status, &note.Projection.DiagnosticCode, &note.Projection.DiagnosticDetail, &note.Projection.UpdatedAt,
			); err != nil {
				_ = rows.Close()
				return nil, err
			}
			states[path] = ownershipCurrentState{note: note, noteFound: noteFound, codeOwned: codeOwned}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return states, nil
}
