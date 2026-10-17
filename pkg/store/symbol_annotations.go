package store

import (
	"context"
	"database/sql"
	"encoding/json"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

// AnnotationUsesByType returns annotation uses filtered by type.
// If ref.Pkg is empty, it matches any package (wildcard).
func (s *Store) AnnotationUsesByType(ctx context.Context, ref codeanchor.SymbolRef) ([]codeanchor.AnnotationUse, error) {
	var rows *sql.Rows
	var err error
	if ref.Pkg == "" {
		// Empty pkg acts as wildcard - match any package
		rows, err = s.db.QueryContext(ctx, `
			SELECT owner_fqn, ann_pkg, args_json
			FROM annotations
			WHERE ann_lang = ? AND ann_name = ?
		`, ref.Lang, ref.Name)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT owner_fqn, ann_pkg, args_json
			FROM annotations
			WHERE ann_lang = ? AND ann_pkg = ? AND ann_name = ?
		`, ref.Lang, ref.Pkg, ref.Name)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []codeanchor.AnnotationUse
	for rows.Next() {
		var owner, pkg, argsJSON string
		if err := rows.Scan(&owner, &pkg, &argsJSON); err != nil {
			return nil, err
		}
		ann := codeanchor.AnnotationUse{
			OwnerFQN: owner,
			AnnSymbol: codeanchor.SymbolRef{
				Lang: ref.Lang,
				Pkg:  pkg,
				Name: ref.Name,
			},
		}
		if argsJSON != "" {
			_ = json.Unmarshal([]byte(argsJSON), &ann.Args)
		}
		res = append(res, ann)
	}
	return res, rows.Err()
}

// AnnotationUsesByTypes returns annotation uses for multiple types in one query.
// Wildcard pkg refs (empty pkg) are supported and keyed by lang|""|name.
func (s *Store) AnnotationUsesByTypes(ctx context.Context, refs []codeanchor.SymbolRef) (map[string][]codeanchor.AnnotationUse, error) {
	if len(refs) == 0 {
		return map[string][]codeanchor.AnnotationUse{}, nil
	}
	result := make(map[string][]codeanchor.AnnotationUse, len(refs))

	return result, s.withWriteTx(ctx, func(tx *sql.Tx) error {
		exact := make(map[string]codeanchor.SymbolRef)
		wild := make(map[string]codeanchor.SymbolRef)
		for _, ref := range refs {
			key := symbolRefKey(ref)
			if ref.Pkg == "" {
				wild[key] = ref
			} else {
				exact[key] = ref
			}
		}

		if len(exact) > 0 {
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_ann_exact`)
			if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_ann_exact (lang TEXT, pkg TEXT, name TEXT, key TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_ann_exact(lang, pkg, name, key) VALUES (?, ?, ?, ?)`)
			if err != nil {
				return err
			}
			for key, ref := range exact {
				if _, err := stmt.ExecContext(ctx, ref.Lang, ref.Pkg, ref.Name, key); err != nil {
					_ = stmt.Close()
					return err
				}
			}
			_ = stmt.Close()

			rows, err := tx.QueryContext(ctx, `
				SELECT t.key, a.owner_fqn, a.ann_pkg, a.args_json
				FROM annotations a
				JOIN temp_ann_exact t
				  ON a.ann_lang = t.lang AND a.ann_pkg = t.pkg AND a.ann_name = t.name
			`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var key, owner, pkg, argsJSON string
				if err := rows.Scan(&key, &owner, &pkg, &argsJSON); err != nil {
					_ = rows.Close()
					return err
				}
				ref := exact[key]
				ann := codeanchor.AnnotationUse{
					OwnerFQN: owner,
					AnnSymbol: codeanchor.SymbolRef{
						Lang: ref.Lang,
						Pkg:  pkg,
						Name: ref.Name,
					},
				}
				if argsJSON != "" {
					_ = json.Unmarshal([]byte(argsJSON), &ann.Args)
				}
				result[key] = append(result[key], ann)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			_ = rows.Close()
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_ann_exact`)
		}

		if len(wild) > 0 {
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_ann_wild`)
			if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_ann_wild (lang TEXT, name TEXT, key TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_ann_wild(lang, name, key) VALUES (?, ?, ?)`)
			if err != nil {
				return err
			}
			for key, ref := range wild {
				if _, err := stmt.ExecContext(ctx, ref.Lang, ref.Name, key); err != nil {
					_ = stmt.Close()
					return err
				}
			}
			_ = stmt.Close()

			rows, err := tx.QueryContext(ctx, `
				SELECT t.key, a.owner_fqn, a.ann_pkg, a.args_json
				FROM annotations a
				JOIN temp_ann_wild t
				  ON a.ann_lang = t.lang AND a.ann_name = t.name
			`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var key, owner, pkg, argsJSON string
				if err := rows.Scan(&key, &owner, &pkg, &argsJSON); err != nil {
					_ = rows.Close()
					return err
				}
				ref := wild[key]
				ann := codeanchor.AnnotationUse{
					OwnerFQN: owner,
					AnnSymbol: codeanchor.SymbolRef{
						Lang: ref.Lang,
						Pkg:  pkg,
						Name: ref.Name,
					},
				}
				if argsJSON != "" {
					_ = json.Unmarshal([]byte(argsJSON), &ann.Args)
				}
				result[key] = append(result[key], ann)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			_ = rows.Close()
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_ann_wild`)
		}
		return nil
	})
}
