package store

import (
	"context"
	"sort"
	"strings"
)

// CurrentNoteFragmentTargets returns persisted targets without deduplicating
// them. Optional path, kind, and normalized-target filters are conjunctive.
// TargetNorm uses explicit BINARY collation to preserve block-id case.
func (s *Store) CurrentNoteFragmentTargets(ctx context.Context, notePaths []string, kind NoteFragmentTargetKind, targetNorm string) ([]NoteFragmentTargetRow, error) {
	notePaths = normalizeNonEmptyStrings(notePaths)
	queryBatch := func(paths []string) ([]NoteFragmentTargetRow, error) {
		conditions := make([]string, 0, 3)
		args := make([]any, 0, len(paths)+2)
		if len(paths) > 0 {
			holders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
			conditions = append(conditions, "n.path IN ("+holders+")")
			args = append(args, sliceAny(paths)...)
		}
		if kind != "" {
			conditions = append(conditions, "t.target_kind = ?")
			args = append(args, string(kind))
		}
		if targetNorm != "" {
			conditions = append(conditions, "t.target_norm = ? COLLATE BINARY")
			args = append(args, targetNorm)
		}
		where := ""
		if len(conditions) > 0 {
			where = " WHERE " + strings.Join(conditions, " AND ")
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT t.note_id, n.path, t.target_kind, t.target_text, t.target_norm, t.ordinal
			FROM note_fragment_targets t
			JOIN notes n ON n.id = t.note_id
		`+where+`
			ORDER BY n.path, t.target_kind, t.target_norm COLLATE BINARY, t.ordinal, t.rowid
		`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []NoteFragmentTargetRow
		for rows.Next() {
			var row NoteFragmentTargetRow
			if err := rows.Scan(&row.NoteID, &row.NotePath, &row.Kind, &row.Target, &row.TargetNorm, &row.Ordinal); err != nil {
				return nil, err
			}
			out = append(out, row)
		}
		return out, rows.Err()
	}

	if len(notePaths) == 0 {
		return queryBatch(nil)
	}
	maxPaths := sqliteValuesBatchMaxParams - 2
	out := make([]NoteFragmentTargetRow, 0)
	for start := 0; start < len(notePaths); start += maxPaths {
		end := min(start+maxPaths, len(notePaths))
		rows, err := queryBatch(notePaths[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NotePath != out[j].NotePath {
			return out[i].NotePath < out[j].NotePath
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].TargetNorm != out[j].TargetNorm {
			return out[i].TargetNorm < out[j].TargetNorm
		}
		return out[i].Ordinal < out[j].Ordinal
	})
	return out, nil
}
