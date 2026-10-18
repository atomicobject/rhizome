package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

// SymbolsByFile returns FQNs of symbols defined in the file.
func (s *Store) SymbolsByFile(ctx context.Context, file string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT fqn FROM symbols WHERE file = ? ORDER BY fqn`, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []string
	for rows.Next() {
		var fqn string
		if err := rows.Scan(&fqn); err != nil {
			return nil, err
		}
		res = append(res, fqn)
	}
	return res, rows.Err()
}

// Ancestors returns all ancestors (direct and transitive) for the symbol.
func (s *Store) Ancestors(ctx context.Context, fqn string) ([]string, error) {
	seen := map[string]bool{}
	var out []string

	// NOTE: This uses an explicit stack rather than recursion so we never attempt
	// nested queries while a rows cursor is still open. This matters when the store
	// limits MaxOpenConns to 1 (to reduce SQLite lock contention).
	stack := []string{fqn}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		rows, err := s.db.QueryContext(ctx, `SELECT parent_fqn FROM super_edges WHERE child_fqn = ?`, cur)
		if err != nil {
			return nil, err
		}

		var parents []string
		for rows.Next() {
			var parent string
			if err := rows.Scan(&parent); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if parent == "" || seen[parent] {
				continue
			}
			seen[parent] = true
			out = append(out, parent)
			parents = append(parents, parent)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()

		// Preserve the prior depth-first-ish behavior: process the first-seen parent next.
		for i := len(parents) - 1; i >= 0; i-- {
			stack = append(stack, parents[i])
		}
	}
	return out, nil
}

// AnnotationsOnSymbols returns annotations for the provided owners.
func (s *Store) AnnotationsOnSymbols(ctx context.Context, owners []string) ([]codeanchor.AnnotationUse, error) {
	if len(owners) == 0 {
		return nil, nil
	}
	holders, args := placeholders(owners)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT owner_fqn, ann_lang, ann_pkg, ann_name, args_json
		FROM annotations
		WHERE owner_fqn IN (%s)
	`, holders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []codeanchor.AnnotationUse
	for rows.Next() {
		var owner, lang, pkg, name, argsJSON string
		if err := rows.Scan(&owner, &lang, &pkg, &name, &argsJSON); err != nil {
			return nil, err
		}
		ann := codeanchor.AnnotationUse{
			OwnerFQN: owner,
			AnnSymbol: codeanchor.SymbolRef{
				Lang: codeanchor.Lang(lang),
				Pkg:  pkg,
				Name: name,
			},
		}
		if argsJSON != "" {
			_ = json.Unmarshal([]byte(argsJSON), &ann.Args)
		}
		res = append(res, ann)
	}
	return res, rows.Err()
}

// CallsFromFile returns all call sites for a file.
func (s *Store) CallsFromFile(ctx context.Context, file string) ([]codeanchor.CallSite, error) {
	// Callsites are indexed using canonical file paths; normalize for cross-platform callers.
	file = normalizeCodeLookupPath(file)
	rows, err := s.db.QueryContext(ctx, `
		SELECT caller.fqn, callee.lang, callee.fqn, callee.symbol
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls' AND e.src_type = 'anchor' AND e.dst_type = 'anchor' AND caller.path = ?
		ORDER BY callee.fqn, callee.symbol, callee.anchor_id
	`, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []codeanchor.CallSite
	for rows.Next() {
		var ownerFQN, lang, fqn, symbol string
		if err := rows.Scan(&ownerFQN, &lang, &fqn, &symbol); err != nil {
			return nil, err
		}
		pkg, name := splitFQN(fqn)
		if name == "" {
			name = strings.TrimSpace(symbol)
			pkg = ""
		}
		if name == "" {
			continue
		}
		res = append(res, codeanchor.CallSite{
			File:     file,
			OwnerFQN: strings.TrimSpace(ownerFQN),
			CalleeSymbol: codeanchor.SymbolRef{
				Lang: codeanchor.Lang(lang),
				Pkg:  pkg,
				Name: name,
			},
		})
	}
	return res, rows.Err()
}

// Children returns symbols that extend or implement the parent.
func (s *Store) Children(ctx context.Context, parent string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT child_fqn FROM super_edges WHERE parent_fqn = ?`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []string
	for rows.Next() {
		var child string
		if err := rows.Scan(&child); err != nil {
			return nil, err
		}
		res = append(res, child)
	}
	return res, rows.Err()
}

// ChildrenBatch returns children for multiple parent FQNs in one query.
func (s *Store) ChildrenBatch(ctx context.Context, parents []string) (map[string][]string, error) {
	if len(parents) == 0 {
		return map[string][]string{}, nil
	}
	returns := make(map[string][]string, len(parents))
	return returns, s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_super_parents`)
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_super_parents (parent_fqn TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_super_parents(parent_fqn) VALUES (?)`)
		if err != nil {
			return err
		}
		for _, p := range parents {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, p); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()

		rows, err := tx.QueryContext(ctx, `
			SELECT p.parent_fqn, s.child_fqn
			FROM super_edges s
			JOIN temp_super_parents p ON p.parent_fqn = s.parent_fqn
		`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var parent, child string
			if err := rows.Scan(&parent, &child); err != nil {
				_ = rows.Close()
				return err
			}
			returns[parent] = append(returns[parent], child)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_super_parents`)
		return nil
	})
}
