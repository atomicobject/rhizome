package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
)

// FileHash returns stored hash, per-file indexer version, and last parse status (if present).
func (s *Store) FileHash(ctx context.Context, path string) (string, string, codeanchor.ParseStatus, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT hash, indexer_version, parse_status FROM files WHERE path = ?`, path)
	var hash sql.NullString
	var version sql.NullString
	var status sql.NullString
	if err := row.Scan(&hash, &version, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", codeanchor.ParseOK, false, nil
		}
		return "", "", codeanchor.ParseOK, false, err
	}
	if !hash.Valid || hash.String == "" {
		return "", "", codeanchor.ParseOK, false, nil
	}
	parseStatus := codeanchor.ParseOK
	if status.Valid && strings.TrimSpace(status.String) != "" {
		parseStatus = codeanchor.ParseStatus(status.String)
	}
	return hash.String, version.String, parseStatus, true, nil
}

// MarkCallEdgesStale marks code files as requiring a second-pass call-edge rebuild.
func (s *Store) MarkCallEdgesStale(ctx context.Context, paths []string) error {
	paths = normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(paths) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.mark_call_edges_stale")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		holders, args := placeholders(paths)
		_, err := tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE files
			SET call_edges_stale = 1
			WHERE path IN (%s)
		`, holders), args...)
		return err
	})
}

// ClearCallEdgesStale clears the call-edge-stale flag for the provided paths.
func (s *Store) ClearCallEdgesStale(ctx context.Context, paths []string) error {
	paths = normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(paths) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.clear_call_edges_stale")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		holders, args := placeholders(paths)
		_, err := tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE files
			SET call_edges_stale = 0
			WHERE path IN (%s)
		`, holders), args...)
		return err
	})
}

// StaleCallEdgePaths returns code paths marked as needing call-edge rebuild.
func (s *Store) StaleCallEdgePaths(ctx context.Context, limit int) ([]string, error) {
	query := `SELECT path FROM files WHERE call_edges_stale = 1 ORDER BY path`
	var args []any
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

// IndexedFilePaths returns all indexed file paths in this code index.
func (s *Store) IndexedFilePaths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM files ORDER BY path`)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// IndexedFilePathsByPrefix returns indexed file paths that match the prefix or live under it.
func (s *Store) IndexedFilePathsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	prefix = normalizeCodeLookupPath(prefix)
	if prefix == "" {
		return nil, nil
	}

	escapeGlob := func(in string) string {
		out := strings.ReplaceAll(in, "[", "[[]")
		out = strings.ReplaceAll(out, "*", "[*]")
		out = strings.ReplaceAll(out, "?", "[?]")
		return out
	}

	escaped := escapeGlob(prefix)
	glob := escaped + "/*"
	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM files
		WHERE path = ? OR path GLOB ?
		ORDER BY path
	`, prefix, glob)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// FilesDefiningSymbolFQN returns code file paths that define the given symbol FQN.
func (s *Store) FilesDefiningSymbolFQN(ctx context.Context, fqn string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT file FROM symbols WHERE fqn = ? ORDER BY file`, fqn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var file string
		if err := rows.Scan(&file); err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
