package store

import (
	"context"
	"database/sql"
)

func currentNoteMetadataStateTx(ctx context.Context, tx *sql.Tx) (NoteMetadataState, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT notes_hash, COALESCE(raw_notes_hash, ''), loaded_at, ready
		FROM note_metadata_state
		ORDER BY loaded_at DESC
		LIMIT 1
	`)
	var state NoteMetadataState
	var ready int
	err := row.Scan(&state.NotesHash, &state.RawNotesHash, &state.LoadedAt, &ready)
	if err == sql.ErrNoRows {
		return NoteMetadataState{}, nil
	}
	if err != nil {
		return NoteMetadataState{}, err
	}
	state.Ready = ready != 0
	return state, nil
}

func (s *Store) GetNoteMetadataState(ctx context.Context) (NoteMetadataState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT notes_hash, COALESCE(raw_notes_hash, ''), loaded_at, ready
		FROM note_metadata_state
		ORDER BY loaded_at DESC
		LIMIT 1
	`)
	var state NoteMetadataState
	var ready int
	err := row.Scan(&state.NotesHash, &state.RawNotesHash, &state.LoadedAt, &ready)
	if err == sql.ErrNoRows {
		return NoteMetadataState{}, nil
	}
	if err != nil {
		return NoteMetadataState{}, err
	}
	state.Ready = ready != 0
	return state, nil
}

func (s *Store) CurrentNoteMetadataRows(ctx context.Context) ([]NoteMetadataRow, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.path, COALESCE(n.title, ''), COALESCE(n.content_hash, ''), COALESCE(n.indexer_version, ''),
			n.mtime, n.size, n.indexed_at, COALESCE(n.format_id, 'markdown'),
			COALESCE(p.provider_version, ''), COALESCE(p.projection_version, ''), COALESCE(p.source_content_hash, ''),
			COALESCE(p.status, 'stale'), COALESCE(p.diagnostic_code, ''), COALESCE(p.diagnostic_detail, ''), COALESCE(p.updated_at, 0)
		FROM notes n
		LEFT JOIN note_projection_state p ON p.note_id = n.id
		WHERE n.indexed_at > 0
		ORDER BY n.path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteMetadataRow
	for rows.Next() {
		var row NoteMetadataRow
		if err := scanNoteMetadataRow(rows, &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type noteMetadataRowScanner interface {
	Scan(...any) error
}

func scanNoteMetadataRow(scanner noteMetadataRowScanner, row *NoteMetadataRow) error {
	return scanner.Scan(
		&row.NoteID, &row.Path, &row.Title, &row.ContentHash, &row.IndexerVersion,
		&row.Mtime, &row.Size, &row.IndexedAt, &row.FormatID,
		&row.Projection.ProviderVersion, &row.Projection.ProjectionVersion, &row.Projection.SourceContentHash,
		&row.Projection.Status, &row.Projection.DiagnosticCode, &row.Projection.DiagnosticDetail, &row.Projection.UpdatedAt,
	)
}
