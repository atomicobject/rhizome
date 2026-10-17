package store

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/bmatcuk/doublestar/v4"
	"path/filepath"
	"slices"
	"strings"
)

func (s *Store) globsByAnchorIDs(ctx context.Context, ids []int64) (map[int64][]string, error) {
	if len(ids) == 0 {
		return map[int64][]string{}, nil
	}
	holders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		holders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT anchor_id, pattern
		FROM anchor_globs
		WHERE anchor_id IN (%s)
	`, strings.Join(holders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]string)
	for rows.Next() {
		var anchorID int64
		var pat string
		if err := rows.Scan(&anchorID, &pat); err != nil {
			return nil, err
		}
		out[anchorID] = append(out[anchorID], pat)
	}
	return out, rows.Err()
}

// AnchorsBySymbols returns anchors whose scopes reference any of the provided symbols.
func (s *Store) AnchorsBySymbols(ctx context.Context, symbols []string) (map[int64][]string, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	out := make(map[int64][]string)
	const maxBatch = 500 // stay under SQLite variable limit (default 999)
	for batch := range slices.Chunk(symbols, maxBatch) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT anchor_id, symbol_fqn
			FROM anchor_scopes
			WHERE symbol_fqn IN (%s)
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID int64
			var sym string
			if err := rows.Scan(&anchorID, &sym); err != nil {
				rows.Close()
				return nil, err
			}
			out[anchorID] = append(out[anchorID], sym)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AnchorsByCallFiles returns anchors whose call_file scope matches provided files.
func (s *Store) AnchorsByCallFiles(ctx context.Context, files []string) ([]int64, error) {
	if len(files) == 0 {
		return nil, nil
	}
	holders, args := placeholders(files)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT anchor_id
		FROM anchor_scopes
		WHERE call_file IN (%s)
	`, holders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AnchorsByPathPrefix returns anchors of kind "path" whose path_prefix matches the target.
// A match occurs when target equals path_prefix or is nested under it (directory-boundary aware).
func (s *Store) AnchorsByPathPrefix(ctx context.Context, target string) ([]int64, error) {
	target = string(paths.NormalizeCode(strings.TrimSpace(target)))
	if target == "" {
		return nil, nil
	}
	sep := "/"
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT id
		FROM anchors
		WHERE kind = ?
			AND path_prefix IS NOT NULL
			AND path_prefix != ''
			AND (? = path_prefix OR substr(?, 1, length(path_prefix) + 1) = path_prefix || ?)
	`, codeanchor.AnchorPath, target, target, sep)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AnchorsByGlobMatch returns anchors of kind "glob" whose patterns match the target.
// Patterns use doublestar semantics and should be stored in normalized (typically absolute) form.
func (s *Store) AnchorsByGlobMatch(ctx context.Context, target string) ([]int64, error) {
	target = filepath.Clean(strings.TrimSpace(target))
	if target == "" {
		return nil, nil
	}
	t := filepath.ToSlash(target)

	rows, err := s.db.QueryContext(ctx, `
		SELECT ag.anchor_id, ag.pattern
		FROM anchor_globs ag
		JOIN anchors a ON a.id = ag.anchor_id
		WHERE a.kind = ?
	`, codeanchor.AnchorGlob)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[int64]bool)
	var ids []int64
	for rows.Next() {
		var id int64
		var pat string
		if err := rows.Scan(&id, &pat); err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		ok, _ := doublestar.Match(pat, t)
		if ok {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}
