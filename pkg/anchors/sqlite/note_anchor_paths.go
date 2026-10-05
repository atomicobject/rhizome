package sqlite

import (
	"context"
	"sort"
	"strings"
)

// NotePathsByAnchorIDs returns defining note paths for anchor IDs in one store
// read. Validation uses this to attach vault-relative source paths without a
// per-anchor query loop.
func (s *Store) NotePathsByAnchorIDs(ctx context.Context, anchorIDs []int64) (map[int64][]string, error) {
	result := make(map[int64][]string)
	if len(anchorIDs) == 0 {
		return result, nil
	}
	unique := make(map[int64]struct{}, len(anchorIDs))
	for _, anchorID := range anchorIDs {
		unique[anchorID] = struct{}{}
	}
	ids := make([]int64, 0, len(unique))
	for anchorID := range unique {
		ids = append(ids, anchorID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for start := 0; start < len(ids); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(ids) {
			end = len(ids)
		}
		if err := s.appendNotePathsByAnchorIDs(ctx, result, ids[start:end]); err != nil {
			return nil, err
		}
	}
	for anchorID, paths := range result {
		result[anchorID] = sortedUniqueStrings(paths)
	}
	return result, nil
}

func (s *Store) appendNotePathsByAnchorIDs(ctx context.Context, result map[int64][]string, ids []int64) error {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for index, anchorID := range ids {
		placeholders[index] = "?"
		args[index] = anchorID
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT na.anchor_id, n.path
		FROM note_anchors na
		JOIN notes n ON n.id = na.note_id
		WHERE na.anchor_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY na.anchor_id, n.path
	`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var anchorID int64
		var path string
		if err := rows.Scan(&anchorID, &path); err != nil {
			return err
		}
		result[anchorID] = append(result[anchorID], path)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
