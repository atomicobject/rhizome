package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// DurableNoteFactsByPaths reads metadata, property values, and tags for the
// requested canonical paths from one short SQLite read transaction. No
// partial facts are returned when any read or the transaction fails.
func (s *Store) DurableNoteFactsByPaths(ctx context.Context, paths []string) (DurableNoteFacts, error) {
	canonical, err := canonicalNoteMetadataPaths(dedupeStringsStable(paths))
	if err != nil {
		return DurableNoteFacts{}, err
	}
	if len(canonical) == 0 {
		return DurableNoteFacts{MetadataRows: map[string]NoteMetadataRow{}}, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DurableNoteFacts{}, err
	}
	defer func() { _ = tx.Rollback() }()

	facts := DurableNoteFacts{}
	facts.MetadataRows, err = noteMetadataRowsByPaths(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.PropertyValues, err = notePropertyValues(ctx, tx, canonical, nil, 0)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.Tags, err = noteTags(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.FragmentTargets, err = noteFragmentTargets(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.ProjectionDiagnostics, err = noteProjectionDiagnostics(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.ProjectionPayloads, err = noteProjectionPayloads(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.SearchRegions, err = noteSearchRegions(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	if err := tx.Commit(); err != nil {
		return DurableNoteFacts{}, err
	}
	return facts, nil
}

func noteFragmentTargets(ctx context.Context, queryer noteMetadataQueryer, paths []string) ([]NoteFragmentTargetRow, error) {
	var out []NoteFragmentTargetRow
	err := queryNoteFactsByPaths(ctx, queryer, paths, `
		SELECT t.note_id, n.path, t.target_kind, t.target_text, t.target_norm, t.ordinal
		FROM note_fragment_targets t JOIN notes n ON n.id = t.note_id
		WHERE n.path IN (%s) ORDER BY n.path, t.ordinal, t.target_kind, t.target_norm COLLATE BINARY
	`, func(rows *sql.Rows) error {
		var row NoteFragmentTargetRow
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.Kind, &row.Target, &row.TargetNorm, &row.Ordinal); err != nil {
			return err
		}
		out = append(out, row)
		return nil
	})
	return out, err
}

func noteProjectionDiagnostics(ctx context.Context, queryer noteMetadataQueryer, paths []string) ([]NoteProjectionDiagnosticRow, error) {
	var out []NoteProjectionDiagnosticRow
	err := queryNoteFactsByPaths(ctx, queryer, paths, `
		SELECT d.note_id, n.path, d.ordinal, d.code, d.category, d.message,
			d.range_present, d.start_byte, d.end_byte, d.blocking, d.affected_operation
		FROM note_projection_diagnostics d JOIN notes n ON n.id = d.note_id
		WHERE n.path IN (%s) ORDER BY n.path, d.ordinal
	`, func(rows *sql.Rows) error {
		var row NoteProjectionDiagnosticRow
		var rangePresent, blocking int
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.Ordinal, &row.Code, &row.Category, &row.Message,
			&rangePresent, &row.StartByte, &row.EndByte, &blocking, &row.AffectedOperation); err != nil {
			return err
		}
		row.RangePresent = rangePresent != 0
		row.Blocking = blocking != 0
		out = append(out, row)
		return nil
	})
	return out, err
}

func noteSearchRegions(ctx context.Context, queryer noteMetadataQueryer, paths []string) ([]NoteSearchRegionRow, error) {
	var out []NoteSearchRegionRow
	err := queryNoteFactsByPaths(ctx, queryer, paths, `
		SELECT r.note_id, n.path, r.ordinal, r.origin, r.region_kind, r.region_text,
			r.media_type, r.range_present, r.start_byte, r.end_byte
		FROM note_search_regions r JOIN notes n ON n.id = r.note_id
		WHERE n.path IN (%s) ORDER BY n.path, r.ordinal
	`, func(rows *sql.Rows) error {
		var row NoteSearchRegionRow
		var rangePresent int
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.Ordinal, &row.Origin, &row.Kind, &row.Text,
			&row.MediaType, &rangePresent, &row.StartByte, &row.EndByte); err != nil {
			return err
		}
		row.RangePresent = rangePresent != 0
		out = append(out, row)
		return nil
	})
	return out, err
}

func queryNoteFactsByPaths(ctx context.Context, queryer noteMetadataQueryer, paths []string, query string, scan func(*sql.Rows) error) error {
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(paths))
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := queryer.QueryContext(ctx, fmt.Sprintf(query, placeholders), sliceAny(batch)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			if err := scan(rows); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
