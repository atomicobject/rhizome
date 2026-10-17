package store

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) CurrentNotePathsByPropertyValue(ctx context.Context, propertyName, valueNorm string, sourceFilter NotePropertySource) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	propertyName = strings.TrimSpace(propertyName)
	valueNorm = strings.TrimSpace(valueNorm)
	if propertyName == "" || valueNorm == "" {
		return []string{}, nil
	}
	propertyName = strings.ToLower(propertyName)
	conditions := []string{"n.indexed_at > 0", "k.property_name = ?", "v.value_norm = ?"}
	args := []any{propertyName, valueNorm}
	if sourceFilter != 0 {
		conditions = append(conditions, "v.source = ?")
		args = append(args, int(sourceFilter))
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT n.path
		FROM note_property_values v
		JOIN notes n ON n.id = v.note_id
		JOIN property_keys k ON k.property_id = v.property_id
		WHERE %s
		ORDER BY n.path
	`, strings.Join(conditions, " AND ")), args...)
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

func (s *Store) CurrentNotePathsByTag(ctx context.Context, tagNorm string) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	tagNorm = strings.TrimSpace(tagNorm)
	if tagNorm == "" {
		return []string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT n.path
		FROM note_tags t
		JOIN notes n ON n.id = t.note_id
		WHERE n.indexed_at > 0 AND (t.tag_norm = ? OR t.tag_norm LIKE ?)
		ORDER BY n.path
	`, tagNorm, tagNorm+"/%")
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
