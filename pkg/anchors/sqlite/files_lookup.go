package sqlite

import (
	"context"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// ListFilesWithLang returns up to limit indexed code file paths with their languages.
func (s *Store) ListFilesWithLang(ctx context.Context, limit int) ([]codeanchor.FileWithLang, error) {
	if limit <= 0 {
		limit = 100000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, lang
		FROM files
		ORDER BY path
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.FileWithLang
	for rows.Next() {
		var p, l string
		if err := rows.Scan(&p, &l); err != nil {
			return nil, err
		}
		out = append(out, codeanchor.FileWithLang{Path: p, Lang: l})
	}
	return out, rows.Err()
}

// ListFiles returns up to limit indexed code file paths (vault-relative).
func (s *Store) ListFiles(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM files
		ORDER BY path
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FilesByPathPrefix returns up to limit file paths that equal prefix or are nested under it.
func (s *Store) FilesByPathPrefix(ctx context.Context, prefix string, limit int) ([]string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	prefix = string(paths.NormalizeCode(prefix))
	prefix = strings.TrimSuffix(prefix, "/")
	sep := "/"
	pattern := prefix + sep + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM files
		WHERE path = ? OR path LIKE ?
		ORDER BY path
		LIMIT ?
	`, prefix, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
