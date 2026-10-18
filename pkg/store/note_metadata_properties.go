package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

func propertyNames(rows []NotePropertyValueRow) []string {
	propertyNames := make([]string, 0)
	seenProperties := make(map[string]struct{})
	for _, row := range rows {
		name := strings.TrimSpace(row.PropertyName)
		if name == "" {
			continue
		}
		if _, ok := seenProperties[name]; ok {
			continue
		}
		seenProperties[name] = struct{}{}
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
	return propertyNames
}

func ensurePropertyKeys(ctx context.Context, tx *sql.Tx, propertyNames []string, updatedAt int64) (map[string]int64, error) {
	propertyIDs := make(map[string]int64, len(propertyNames))
	if len(propertyNames) == 0 {
		return propertyIDs, nil
	}
	values := make([][]any, 0, len(propertyNames))
	for _, name := range propertyNames {
		values = append(values, []any{name, updatedAt})
	}
	if err := execValuesBatch(ctx, tx, `
		INSERT INTO property_keys(property_name, updated_at)
		VALUES
	`, values, 2, `
		ON CONFLICT(property_name) DO UPDATE SET updated_at = excluded.updated_at
	`); err != nil {
		return nil, err
	}
	return selectIDsByValues(ctx, tx, "property_keys", "property_id", "property_name", propertyNames)
}

func insertNotePropertyValues(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, propertyIDs map[string]int64, rows []NotePropertyValueRow) error {
	if len(rows) == 0 {
		return nil
	}
	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		noteID, ok := noteIDs[row.NotePath]
		if !ok {
			continue
		}
		propertyID, ok := propertyIDs[strings.TrimSpace(row.PropertyName)]
		if !ok {
			continue
		}
		values = append(values, []any{
			noteID,
			propertyID,
			int(row.Source),
			row.ValueText,
			row.ValueNorm,
			int(row.ValueKind),
			boolToInt(row.IsList),
			row.ListOrdinal,
		})
	}
	return execValuesBatch(ctx, tx, `
		INSERT INTO note_property_values(
			note_id, property_id, source, value_text, value_norm, value_kind, is_list, list_ordinal
		) VALUES
	`, values, 8, "")
}
