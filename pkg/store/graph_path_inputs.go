package store

import (
	"context"
	"strings"
)

// GraphDocPaths returns distinct doc paths seen in intel docs and doc links.
func (s *Store) GraphDocPaths(ctx context.Context) (notes []string, code []string, err error) {
	noteRows, err := s.db.QueryContext(ctx, `SELECT DISTINCT path FROM intel_doc_sections ORDER BY path`)
	if err != nil {
		return nil, nil, err
	}
	defer noteRows.Close()
	for noteRows.Next() {
		var p string
		if err := noteRows.Scan(&p); err != nil {
			return nil, nil, err
		}
		notes = append(notes, p)
	}
	if err := noteRows.Err(); err != nil {
		return nil, nil, err
	}

	codeRows, err := s.db.QueryContext(ctx, `
		WITH linked_code AS (
			SELECT DISTINCT src_path AS path
			FROM doc_links
			WHERE src_type = 'code'
			UNION
			SELECT DISTINCT a.path AS path
			FROM intel_edges e
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions'
		)
		SELECT DISTINCT path FROM linked_code ORDER BY path
	`)
	if err != nil {
		return notes, nil, err
	}
	defer codeRows.Close()
	for codeRows.Next() {
		var p string
		if err := codeRows.Scan(&p); err != nil {
			return notes, nil, err
		}
		code = append(code, p)
	}
	if err := codeRows.Err(); err != nil {
		return notes, nil, err
	}

	return notes, code, nil
}

// GraphDocPathsForPrefix returns distinct note/code doc paths that match the prefix.
func (s *Store) GraphDocPathsForPrefix(ctx context.Context, prefix string) (notes []string, code []string, err error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return s.GraphDocPaths(ctx)
	}
	like := prefix + "/%"

	noteRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT path
		FROM intel_doc_sections
		WHERE path = ? OR path LIKE ?
		ORDER BY path
	`, prefix, like)
	if err != nil {
		return nil, nil, err
	}
	defer noteRows.Close()
	for noteRows.Next() {
		var p string
		if err := noteRows.Scan(&p); err != nil {
			return nil, nil, err
		}
		notes = append(notes, p)
	}
	if err := noteRows.Err(); err != nil {
		return nil, nil, err
	}

	codeRows, err := s.db.QueryContext(ctx, `
		WITH linked_code AS (
			SELECT DISTINCT src_path AS path
			FROM doc_links
			WHERE src_type = 'code' AND (src_path = ? OR src_path LIKE ?)
			UNION
			SELECT DISTINCT a.path AS path
			FROM intel_edges e
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions' AND (a.path = ? OR a.path LIKE ?)
		)
		SELECT DISTINCT path FROM linked_code ORDER BY path
	`, prefix, like, prefix, like)
	if err != nil {
		return notes, nil, err
	}
	defer codeRows.Close()
	for codeRows.Next() {
		var p string
		if err := codeRows.Scan(&p); err != nil {
			return notes, nil, err
		}
		code = append(code, p)
	}
	if err := codeRows.Err(); err != nil {
		return notes, nil, err
	}

	return notes, code, nil
}
