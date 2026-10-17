package store

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) CurrentNotePropertyValues(ctx context.Context, notePaths []string, onlyProperties []string, sourceFilter NotePropertySource) ([]NotePropertyValueRow, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	return s.notePropertyValues(ctx, notePaths, onlyProperties, sourceFilter)
}

// DurableNotePropertyValuesByPaths returns derived property rows for exactly
// the requested canonical paths without treating global metadata readiness as
// a barrier. Callers must first prove each requested note projection current.
func (s *Store) DurableNotePropertyValuesByPaths(ctx context.Context, notePaths []string, onlyProperties []string, sourceFilter NotePropertySource) ([]NotePropertyValueRow, error) {
	canonical, err := canonicalNoteMetadataPaths(notePaths)
	if err != nil {
		return nil, err
	}
	if len(canonical) == 0 {
		return []NotePropertyValueRow{}, nil
	}
	return s.notePropertyValues(ctx, canonical, onlyProperties, sourceFilter)
}

func (s *Store) notePropertyValues(ctx context.Context, notePaths []string, onlyProperties []string, sourceFilter NotePropertySource) ([]NotePropertyValueRow, error) {
	return notePropertyValues(ctx, s.db, notePaths, onlyProperties, sourceFilter)
}

func notePropertyValues(ctx context.Context, queryer noteMetadataQueryer, notePaths []string, onlyProperties []string, sourceFilter NotePropertySource) ([]NotePropertyValueRow, error) {
	hasNotePathFilter := len(notePaths) > 0
	notePaths = normalizeNonEmptyStrings(notePaths)
	if hasNotePathFilter && len(notePaths) == 0 {
		return []NotePropertyValueRow{}, nil
	}
	if len(notePaths) > sqliteValuesBatchMaxParams {
		out := make([]NotePropertyValueRow, 0, len(notePaths))
		for start := 0; start < len(notePaths); start += sqliteValuesBatchMaxParams {
			end := start + sqliteValuesBatchMaxParams
			if end > len(notePaths) {
				end = len(notePaths)
			}
			rows, err := notePropertyValues(ctx, queryer, notePaths[start:end], onlyProperties, sourceFilter)
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
	if len(onlyProperties) > 0 {
		onlyProperties = normalizeNonEmptyStrings(onlyProperties)
		if len(onlyProperties) == 0 {
			return []NotePropertyValueRow{}, nil
		}
		for i := range onlyProperties {
			onlyProperties[i] = strings.ToLower(strings.TrimSpace(onlyProperties[i]))
		}
		holders := strings.TrimSuffix(strings.Repeat("?,", len(onlyProperties)), ",")
		conditions = append(conditions, fmt.Sprintf("k.property_name IN (%s)", holders))
		args = append(args, sliceAny(onlyProperties)...)
	}
	if sourceFilter != 0 {
		conditions = append(conditions, "v.source = ?")
		args = append(args, int(sourceFilter))
	}
	rows, err := queryer.QueryContext(ctx, fmt.Sprintf(`
		SELECT v.note_id, n.path, k.property_name, v.source, v.value_text, v.value_norm, v.value_kind, v.is_list, v.list_ordinal
		FROM note_property_values v
		JOIN notes n ON n.id = v.note_id
		JOIN property_keys k ON k.property_id = v.property_id
		WHERE %s
		ORDER BY n.path, k.property_name, v.source, v.list_ordinal, v.value_norm
	`, strings.Join(conditions, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotePropertyValueRow
	for rows.Next() {
		var row NotePropertyValueRow
		var source int
		var kind int
		var isList int
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.PropertyName, &source, &row.ValueText, &row.ValueNorm, &kind, &isList, &row.ListOrdinal); err != nil {
			return nil, err
		}
		row.Source = NotePropertySource(source)
		row.ValueKind = NotePropertyValueKind(kind)
		row.IsList = isList != 0
		out = append(out, row)
	}
	return out, rows.Err()
}
