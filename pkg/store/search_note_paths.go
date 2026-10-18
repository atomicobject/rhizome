package store

import (
	"context"
	"strings"
)

// SearchNotePath separates section membership from current projected ownership.
// A retained section can participate in corpus shape without claiming ownership
// over a current code file at the same path.
type SearchNotePath struct {
	Path              string
	HasSections       bool
	CurrentProjection bool
}

// SearchNotePaths returns both search path sets in one row stream. This avoids
// crossing the SQLite driver once for sections and again for the same notes.
func (s *Store) SearchNotePaths(ctx context.Context) ([]SearchNotePath, error) {
	rows, err := s.db.QueryContext(ctx, `
 WITH path_roles AS (
  SELECT DISTINCT path, 1 AS has_sections, 0 AS current_projection
  FROM intel_doc_sections
  UNION ALL
  SELECT n.path, 0, 1
  FROM notes n
  JOIN note_projection_state p ON p.note_id=n.id
  WHERE n.indexed_at>0 AND p.status='current'
   AND COALESCE((SELECT ready<>0 AND loaded_at<>0 FROM note_metadata_state ORDER BY loaded_at DESC LIMIT 1),0)
 )
 SELECT path, max(has_sections), max(current_projection)
 FROM path_roles GROUP BY path ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []SearchNotePath
	for rows.Next() {
		var path SearchNotePath
		if err := rows.Scan(&path.Path, &path.HasSections, &path.CurrentProjection); err != nil {
			return nil, err
		}
		if strings.TrimSpace(path.Path) != "" {
			paths = append(paths, path)
		}
	}
	return paths, rows.Err()
}
