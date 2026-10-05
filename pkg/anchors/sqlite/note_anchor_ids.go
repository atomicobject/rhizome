package sqlite

import (
	"context"
	"strings"
)

// AnchorIDsForNotePath returns anchor IDs attached to a note (defined + referenced anchors).
// notePath must match the stored notes.path value (vault-relative, normalized).
func (s *Store) AnchorIDsForNotePath(ctx context.Context, notePath string) ([]int64, error) {
	notePath = normalizeNoteLookupPath(notePath)
	rows, err := s.db.QueryContext(ctx, `
		SELECT na.anchor_id
		FROM note_anchors na
		JOIN notes n ON n.id = na.note_id
		WHERE n.path = ?
		ORDER BY na.anchor_id
	`, notePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// IntelAnchorIDsByFQNs returns intel anchor IDs for the provided FQNs.
// This is a bulk helper used for code↔docs bridging; callers should apply their own limits.
func (s *Store) IntelAnchorIDsByFQNs(ctx context.Context, fqns []string) ([]string, error) {
	if len(fqns) == 0 {
		return nil, nil
	}
	holders := make([]string, len(fqns))
	args := make([]any, len(fqns))
	for i, f := range fqns {
		holders[i] = "?"
		args[i] = f
	}
	stmt := `
		SELECT anchor_id
		FROM intel_code_anchors
		WHERE fqn IN (` + strings.Join(holders, ",") + `)
		ORDER BY path, fqn, anchor_id
	`
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
