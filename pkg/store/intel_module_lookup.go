package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// IntelModuleAnchorIDByPath returns the module anchor ID for a given path/lang if present.
func (s *Store) IntelModuleAnchorIDByPath(ctx context.Context, lang, path string) (string, bool, error) {
	lang = strings.TrimSpace(lang)
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT anchor_id
		FROM intel_code_anchors
		WHERE lower(kind) = 'module' AND path = ? AND (? = '' OR lang = ?)
		LIMIT 1
	`, path, lang, lang)
	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

// ModuleAnchorIDsByPaths returns module anchor IDs for a batch of paths.
func (s *Store) ModuleAnchorIDsByPaths(ctx context.Context, paths []string) (map[string]string, error) {
	result := make(map[string]string, len(paths))
	seen := make(map[string]struct{}, len(paths))
	var deduped []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		deduped = append(deduped, p)
	}
	if len(deduped) == 0 {
		return result, nil
	}
	const batchSize = 400
	for start := 0; start < len(deduped); start += batchSize {
		end := start + batchSize
		if end > len(deduped) {
			end = len(deduped)
		}
		batch := deduped[start:end]
		holders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, p := range batch {
			holders[i] = "?"
			args[i] = p
		}
		query := fmt.Sprintf(`
			SELECT path, anchor_id
			FROM intel_code_anchors
			WHERE lower(kind) = 'module' AND path IN (%s)
 ORDER BY path, lang, anchor_id
		`, strings.Join(holders, ","))
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var path, id string
			if err := rows.Scan(&path, &id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if _, exists := result[path]; !exists {
				result[path] = id
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return result, nil
}

// ModuleAnchorIDByPath returns the module anchor ID for a given path (any language) if present.
func (s *Store) ModuleAnchorIDByPath(ctx context.Context, path string) (string, bool, error) {
	return s.IntelModuleAnchorIDByPath(ctx, "", path)
}

// ModuleAnchorIDsByModuleSuffix resolves Python module names to module anchor IDs using path suffix matching.
// Given module names like "charm.misc.colors", it finds anchors whose paths end with "charm/misc/colors.py"
// (or "__init__.py" for package modules).
//
// This enables import edge creation without needing to know the full path prefix (e.g., "backend/src/").
func (s *Store) ModuleAnchorIDsByModuleSuffix(ctx context.Context, modules []string) (map[string]string, error) {
	unique := normalizeNonEmptyStrings(modules)
	if len(unique) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(unique))
	const batchSize = 200

	for start := 0; start < len(unique); start += batchSize {
		end := start + batchSize
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]

		where := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*2)
		for _, mod := range batch {
			where = append(where, "(d.module = ? OR d.module LIKE ?)")
			args = append(args, mod, "%."+mod)
		}

		query := fmt.Sprintf(`
			SELECT d.module, a.anchor_id
			FROM intel_module_defs d
			JOIN intel_code_anchors a
			  ON a.path = d.src_path
			 AND lower(a.kind) = 'module'
			WHERE %s
			ORDER BY d.module, a.path, a.anchor_id
		`, strings.Join(where, " OR "))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var module, id string
			if err := rows.Scan(&module, &id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			for _, input := range batch {
				if module == input || strings.HasSuffix(module, "."+input) {
					if _, ok := result[input]; !ok {
						result[input] = id
					}
					break
				}
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}

	unresolved := make([]string, 0, len(unique))
	for _, mod := range unique {
		if _, ok := result[mod]; !ok {
			unresolved = append(unresolved, mod)
		}
	}
	if len(unresolved) == 0 {
		return result, nil
	}

	for start := 0; start < len(unresolved); start += batchSize {
		end := start + batchSize
		if end > len(unresolved) {
			end = len(unresolved)
		}
		batch := unresolved[start:end]

		// Build WHERE clause for path suffix matching.
		// For module "a.b.c", match paths ending in "/a/b/c.py" or "/a/b/c/__init__.py".
		where := make([]string, 0, len(batch)*2)
		args := make([]any, 0, len(batch)*2)
		for _, mod := range batch {
			// Convert module to path suffix: "a.b.c" -> "a/b/c"
			pathSuffix := strings.ReplaceAll(mod, ".", "/")
			// Match either "a/b/c.py" or "a/b/c/__init__.py"
			where = append(where, "path LIKE ?", "path LIKE ?")
			args = append(args, "%/"+pathSuffix+".py", "%/"+pathSuffix+"/__init__.py")
		}

		query := fmt.Sprintf(`
			SELECT path, anchor_id
			FROM intel_code_anchors
			WHERE lower(kind) = 'module' AND (%s)
		`, strings.Join(where, " OR "))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var path, id string
			if err := rows.Scan(&path, &id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			// Map back to the input module that matched.
			for _, mod := range batch {
				pathSuffix := strings.ReplaceAll(mod, ".", "/")
				if strings.HasSuffix(path, "/"+pathSuffix+".py") ||
					strings.HasSuffix(path, "/"+pathSuffix+"/__init__.py") ||
					path == pathSuffix+".py" ||
					path == pathSuffix+"/__init__.py" {
					result[mod] = id
					break
				}
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return result, nil
}
