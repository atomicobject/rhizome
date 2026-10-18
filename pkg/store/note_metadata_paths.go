package store

import (
	"context"
)

func (s *Store) CurrentNoteMetadataPaths(ctx context.Context) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	return s.AllNoteMetadataPaths(ctx)
}

// AllNoteMetadataPaths returns materialized note rows without treating metadata
// readiness as a read barrier. Bootstrap replacement uses it to tombstone stale
// rows left behind by an interrupted or outdated derivation.
func (s *Store) AllNoteMetadataPaths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM notes
		WHERE indexed_at > 0
		ORDER BY path
	`)
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

func (s *Store) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]NoteMetadataRow, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return map[string]NoteMetadataRow{}, err
	}
	paths = normalizeNonEmptyStrings(paths)
	return s.noteMetadataRowsByPaths(ctx, paths)
}

// DurableNoteMetadataRowsByPaths returns exactly the requested materialized
// source rows without treating global metadata readiness as a read barrier.
// It is for reconciliation that must rebuild state after ownership transitions
// invalidate readiness; callers must not use it as a derived-fact read API.
func (s *Store) DurableNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]NoteMetadataRow, error) {
	canonical, err := canonicalNoteMetadataPaths(paths)
	if err != nil {
		return nil, err
	}
	return s.noteMetadataRowsByPaths(ctx, canonical)
}
