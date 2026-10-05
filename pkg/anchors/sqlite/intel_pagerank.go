package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// TopCodePathsByPageRankPrefix returns up to limit code file paths under (or equal to) prefix,
// ordered by the maximum stored anchor PageRank within each file (descending).
//
// This is designed for "directory seed" sampling: pick a few high-centrality files (entrypoints)
// within a directory without scanning the full tree.
func (s *Store) TopCodePathsByPageRankPrefix(ctx context.Context, prefix string, limit int) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 25
	}

	// Normalize to forward slashes since intel paths are stored as repo/vault-relative slashes.
	prefix = filepath.ToSlash(filepath.Clean(prefix))
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "." || prefix == "/" {
		return nil, nil
	}
	pattern := prefix + "/%"

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.path, MAX(COALESCE(s.pagerank, 0)) AS pr
		FROM intel_code_anchors a
		LEFT JOIN graph_anchor_scores s ON s.anchor_id = a.anchor_id
		WHERE a.path = ? OR a.path LIKE ?
		GROUP BY a.path
		ORDER BY pr DESC, a.path ASC
		LIMIT ?
	`, prefix, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0, min(limit, 64))
	for rows.Next() {
		var path string
		var _pr float64
		if err := rows.Scan(&path, &_pr); err != nil {
			return nil, err
		}
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" {
			continue
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

// TopModuleAnchorIDsByPaths returns up to limit anchor IDs for anchors of kind "module" whose path
// is one of the provided code paths, ordered by stored PageRank descending.
//
// This is used to pick a small, high-signal set of anchor seeds for directory-derived file seeds.
func (s *Store) TopModuleAnchorIDsByPaths(ctx context.Context, paths []string, limit int) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 25
	}

	holders := strings.Repeat("?,", len(paths))
	holders = strings.TrimSuffix(holders, ",")
	args := make([]any, 0, len(paths)+1)
	for _, p := range paths {
		args = append(args, p)
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
		SELECT a.anchor_id, MAX(COALESCE(s.pagerank, 0)) AS pr
		FROM intel_code_anchors a
		LEFT JOIN graph_anchor_scores s ON s.anchor_id = a.anchor_id
		WHERE a.kind = 'module'
			AND a.path IN (%s)
		GROUP BY a.anchor_id
		ORDER BY pr DESC, a.anchor_id ASC
		LIMIT ?
	`, holders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0, min(limit, 64))
	for rows.Next() {
		var id string
		var _pr float64
		if err := rows.Scan(&id, &_pr); err != nil {
			return nil, err
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
