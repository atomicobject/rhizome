package store

import (
	"context"
	"database/sql"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

// AnchorIDByLabel returns anchor ID for a label.
func (s *Store) AnchorIDByLabel(ctx context.Context, label string) (int64, bool) {
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = ?`, label).Scan(&id); err != nil {
		return 0, false
	}
	return id, true
}

// AnchorsMatchingSymbols finds anchors whose base matches provided symbols (lang+pkg+name).
func (s *Store) AnchorsMatchingSymbols(ctx context.Context, names []string, pkgs []string, lang codeanchor.Lang) ([]int64, error) {
	if len(names) == 0 || len(pkgs) == 0 {
		return nil, nil
	}
	limit := len(names)
	if len(pkgs) < limit {
		limit = len(pkgs)
	}

	seenPairs := make(map[string]bool)
	values := make([]string, 0, limit)
	args := make([]any, 0, 1+limit*2)
	args = append(args, lang)

	for i := 0; i < limit; i++ {
		if names[i] == "" {
			continue
		}
		key := pkgs[i] + "\x00" + names[i]
		if seenPairs[key] {
			continue
		}
		seenPairs[key] = true
		values = append(values, "(?, ?)")
		args = append(args, pkgs[i], names[i])
	}

	if len(values) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT id FROM anchors
		WHERE base_lang = ? AND (base_pkg, base_name) IN (%s)
	`, strings.Join(values, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
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

// SymbolsBySuffix finds symbols whose FQN ends with the given suffix.
// Uses the fqn_reversed index for efficient prefix matching on the reversed string.
// Results are ordered by FQN length ascending (shortest = most specific match first).
func (s *Store) SymbolsBySuffix(ctx context.Context, suffix string, lang codeanchor.Lang, limit int) ([]codeanchor.Symbol, error) {
	if suffix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	reversed := codeanchor.ReverseString(suffix)

	query := `
		SELECT id, lang, kind, file, pkg, name, fqn
		FROM symbols
		WHERE fqn_reversed LIKE ? || '%'
	`
	args := []any{reversed}

	if lang != "" {
		query += ` AND lang = ?`
		args = append(args, lang)
	}

	query += ` ORDER BY LENGTH(fqn) ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var symbols []codeanchor.Symbol
	for rows.Next() {
		var sym codeanchor.Symbol
		var id int64
		var kind string
		if err := rows.Scan(&id, &sym.Lang, &kind, &sym.File, &sym.Pkg, &sym.Name, &sym.FQN); err != nil {
			return nil, err
		}
		sym.Kind = codeanchor.SymbolKind(kind)
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// SymbolExistsByFQN checks if a symbol with the exact FQN exists.
func (s *Store) SymbolExistsByFQN(ctx context.Context, fqn string, lang codeanchor.Lang) (bool, error) {
	if fqn == "" {
		return false, nil
	}
	query := `SELECT 1 FROM symbols WHERE fqn = ?`
	args := []any{fqn}
	if lang != "" {
		query += ` AND lang = ?`
		args = append(args, lang)
	}
	query += ` LIMIT 1`

	var exists int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// AnchorsMatchingAnnotations finds annotation anchors by annotation type (lang+pkg+name).
func (s *Store) AnchorsMatchingAnnotations(ctx context.Context, annTypes []codeanchor.SymbolRef) ([]int64, error) {
	if len(annTypes) == 0 {
		return nil, nil
	}
	seen := make(map[int64]bool)
	var ids []int64
	for _, ref := range annTypes {
		// Match by lang+pkg+name for precision; also match where anchor has empty pkg (wildcard)
		rows, err := s.db.QueryContext(ctx, `
			SELECT id FROM anchors
			WHERE ann_lang = ? AND ann_name = ? AND (ann_pkg = ? OR ann_pkg = '')
		`, ref.Lang, ref.Name, ref.Pkg)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		rows.Close()
	}
	return ids, nil
}
