package sqlite

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// NotePathsByPathPrefix returns a bounded, stable set of indexed note paths
// that equal prefix or live below its directory boundary.
func (s *Store) NotePathsByPathPrefix(ctx context.Context, prefix string, limit int) ([]string, error) {
	prefix = string(paths.Normalize(strings.TrimSpace(prefix)))
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return []string{}, nil
	}
	if limit <= 0 {
		limit = 60
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM notes
		WHERE indexed_at > 0 AND (path = ? OR path LIKE ? ESCAPE '\')
		ORDER BY path
		LIMIT ?
	`, prefix, escapeLike(prefix)+"/%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0, limit)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}
