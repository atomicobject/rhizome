package store

import (
	"context"
	"fmt"
	"strings"
)

// CurrentNoteAliases returns a map from note path to the ordered list of
// `aliases:` frontmatter values for that note. Only notes with a populated
// frontmatter `aliases:` list appear in the result. Used to build
// alias-aware NotePathCache instances for wikilink resolution.
//
// This is a thin wrapper over CurrentNotePropertyValues scoped to
// property_name = "aliases" and source = frontmatter.
func (s *Store) CurrentNoteAliases(ctx context.Context) (map[string][]string, error) {
	rows, err := s.CurrentNotePropertyValues(ctx, nil, []string{"aliases"}, NotePropertySourceFrontmatter)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(rows))
	for _, row := range rows {
		value := strings.TrimSpace(row.ValueText)
		if value == "" {
			continue
		}
		out[row.NotePath] = append(out[row.NotePath], value)
	}
	return out, nil
}

func (s *Store) CurrentNoteTags(ctx context.Context, notePaths []string) ([]NoteTagRow, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	return s.noteTags(ctx, notePaths)
}

// DurableNoteTagsByPaths returns derived tags for exactly the requested
// canonical paths without treating global metadata readiness as a barrier.
// Callers must first prove each requested note projection current.
func (s *Store) DurableNoteTagsByPaths(ctx context.Context, notePaths []string) ([]NoteTagRow, error) {
	canonical, err := canonicalNoteMetadataPaths(notePaths)
	if err != nil {
		return nil, err
	}
	if len(canonical) == 0 {
		return []NoteTagRow{}, nil
	}
	return s.noteTags(ctx, canonical)
}

func (s *Store) noteTags(ctx context.Context, notePaths []string) ([]NoteTagRow, error) {
	return noteTags(ctx, s.db, notePaths)
}

func noteTags(ctx context.Context, queryer noteMetadataQueryer, notePaths []string) ([]NoteTagRow, error) {
	hasNotePathFilter := len(notePaths) > 0
	notePaths = normalizeNonEmptyStrings(notePaths)
	if hasNotePathFilter && len(notePaths) == 0 {
		return []NoteTagRow{}, nil
	}
	if len(notePaths) > sqliteValuesBatchMaxParams {
		out := make([]NoteTagRow, 0, len(notePaths))
		for start := 0; start < len(notePaths); start += sqliteValuesBatchMaxParams {
			end := start + sqliteValuesBatchMaxParams
			if end > len(notePaths) {
				end = len(notePaths)
			}
			rows, err := noteTags(ctx, queryer, notePaths[start:end])
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
		}
		return out, nil
	}
	conditions := []string{"n.indexed_at > 0"}
	args := []any{}
	if len(notePaths) > 0 {
		holders := strings.TrimSuffix(strings.Repeat("?,", len(notePaths)), ",")
		conditions = append(conditions, fmt.Sprintf("n.path IN (%s)", holders))
		args = append(args, sliceAny(notePaths)...)
	}
	rows, err := queryer.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.note_id, n.path, t.tag_norm
		FROM note_tags t
		JOIN notes n ON n.id = t.note_id
		WHERE %s
		ORDER BY n.path, t.tag_norm
	`, strings.Join(conditions, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteTagRow
	for rows.Next() {
		var row NoteTagRow
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.TagNorm); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
