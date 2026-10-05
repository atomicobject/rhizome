package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// IntelAnchorIDsByFQN returns anchor IDs whose FQN matches exactly (or suffix-matches) the provided value.
func (s *Store) IntelAnchorIDsByFQN(ctx context.Context, fqn string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	fqn = strings.TrimSpace(fqn)
	if fqn == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id
		FROM intel_code_anchors
		WHERE fqn = ? OR fqn LIKE ?
		ORDER BY path, fqn, anchor_id
		LIMIT ?
	`, fqn, "%."+fqn, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// IntelAnchorsByFQNsLimited returns anchors keyed by FQN (exact or suffix match),
// capped to a maximum number of anchors per FQN.
func (s *Store) IntelAnchorsByFQNsLimited(ctx context.Context, fqns []string, limitPerFQN int) (map[string][]codeanchor.IntelAnchor, error) {
	if limitPerFQN <= 0 {
		limitPerFQN = 5
	}
	unique := normalizeNonEmptyStrings(fqns)
	if len(unique) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}

	where := make([]string, 0, len(unique))
	args := make([]any, 0, len(unique)*2)
	for _, f := range unique {
		where = append(where, "(fqn = ? OR fqn LIKE ?)")
		args = append(args, f, "%."+f)
	}

	query := fmt.Sprintf(`
		SELECT
			anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
			start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		FROM intel_code_anchors
		WHERE %s
		ORDER BY fqn, path, anchor_id
	`, strings.Join(where, " OR "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.IntelAnchor, len(unique))
	for rows.Next() {
		var anchor codeanchor.IntelAnchor
		if err := rows.Scan(
			&anchor.AnchorID,
			&anchor.Lang,
			&anchor.Kind,
			&anchor.Path,
			&anchor.Symbol,
			&anchor.FQN,
			&anchor.Signature,
			&anchor.DocComment,
			&anchor.StartByte,
			&anchor.EndByte,
			&anchor.StartLine,
			&anchor.EndLine,
			&anchor.Fingerprint,
			&anchor.UpdatedAt,
		); err != nil {
			return nil, err
		}
		for _, input := range unique {
			if anchor.FQN == input || strings.HasSuffix(anchor.FQN, "."+input) {
				if len(out[input]) >= limitPerFQN {
					continue
				}
				out[input] = append(out[input], anchor)
			}
		}
	}
	return out, rows.Err()
}

// IntelAnchorIDsByFQNsAndLang returns anchor IDs keyed by FQN (exact or suffix match) for a given language.
// When lang is empty, it returns matches across all languages.
//
// Suffix matching allows imports like `charm.services.X` to resolve to anchors with FQNs like
// `backend.src.charm.services.X`, making edge creation robust to source-root configuration.
func (s *Store) IntelAnchorIDsByExactFQNsAndLang(ctx context.Context, lang string, fqns []string) (map[string][]string, error) {
	if len(fqns) == 0 {
		return map[string][]string{}, nil
	}
	lang = strings.TrimSpace(lang)
	indexingperf.AddCount(ctx, "calledge.cache_build.fqn_lookup.input.count", int64(len(fqns)))

	seen := make(map[string]struct{}, len(fqns))
	unique := make([]string, 0, len(fqns))
	for _, f := range fqns {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		unique = append(unique, f)
	}
	if len(unique) == 0 {
		return map[string][]string{}, nil
	}
	indexingperf.AddCount(ctx, "calledge.cache_build.fqn_lookup.unique.count", int64(len(unique)))

	out := make(map[string][]string, len(unique))
	const exactBatchSize = 400
	exactStarted := time.Now()
	exactChunks := 0
	for start := 0; start < len(unique); start += exactBatchSize {
		end := start + exactBatchSize
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		exactChunks++
		query, args := intelAnchorIDsByFQNsAndLangExactQuery(lang, batch)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var inputFQN, id string
			if err := rows.Scan(&inputFQN, &id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[inputFQN] = append(out[inputFQN], id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	indexingperf.ObserveLatency(ctx, "calledge.cache_build.fqn_lookup.exact", time.Since(exactStarted))

	exactResolved := 0
	for _, fqn := range unique {
		if len(out[fqn]) > 0 {
			exactResolved++
		}
	}
	indexingperf.AddCount(ctx, "calledge.cache_build.fqn_lookup.exact_resolved.count", int64(exactResolved))
	indexingperf.AddCount(ctx, "calledge.cache_build.fqn_lookup.chunk.count", int64(exactChunks))
	return out, nil
}

func (s *Store) IntelAnchorIDsByFQNsAndLang(ctx context.Context, lang string, fqns []string) (map[string][]string, error) {
	out, err := s.IntelAnchorIDsByExactFQNsAndLang(ctx, lang, fqns)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(fqns))
	unique := make([]string, 0, len(fqns))
	for _, f := range fqns {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		unique = append(unique, f)
	}

	unresolved := make([]string, 0, len(unique))
	for _, fqn := range unique {
		if len(out[fqn]) > 0 {
			continue
		}
		unresolved = append(unresolved, fqn)
	}
	indexingperf.AddCount(ctx, "calledge.cache_build.fqn_lookup.suffix_requested.count", int64(len(unresolved)))

	if len(unresolved) == 0 {
		return out, nil
	}

	const suffixBatchSize = 200
	suffixStarted := time.Now()
	suffixChunks := 0
	suffixResolved := 0
	for start := 0; start < len(unresolved); start += suffixBatchSize {
		end := start + suffixBatchSize
		if end > len(unresolved) {
			end = len(unresolved)
		}
		batch := unresolved[start:end]
		suffixChunks++
		query, args := intelAnchorIDsByFQNsAndLangSuffixQuery(lang, batch)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		batchResolved := make(map[string]struct{}, len(batch))
		for rows.Next() {
			var inputFQN, id string
			if err := rows.Scan(&inputFQN, &id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[inputFQN] = append(out[inputFQN], id)
			batchResolved[inputFQN] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		suffixResolved += len(batchResolved)
	}
	indexingperf.ObserveLatency(ctx, "calledge.cache_build.fqn_lookup.suffix", time.Since(suffixStarted))
	indexingperf.AddCount(ctx, "calledge.cache_build.fqn_lookup.suffix_resolved.count", int64(suffixResolved))
	indexingperf.AddCount(ctx, "calledge.cache_build.fqn_lookup.chunk.count", int64(suffixChunks))
	return out, nil
}

func (s *Store) CallEdgeSuffixSeedsByLangs(ctx context.Context, langs []codeanchor.Lang) ([]codeanchor.CallEdgeSuffixSeed, error) {
	uniqueLangs := make([]string, 0, len(langs))
	seen := make(map[string]struct{}, len(langs))
	for _, lang := range langs {
		key := strings.TrimSpace(string(lang))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniqueLangs = append(uniqueLangs, key)
	}
	if len(uniqueLangs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, 0, len(uniqueLangs))
	args := make([]any, 0, len(uniqueLangs))
	for _, lang := range uniqueLangs {
		placeholders = append(placeholders, "?")
		args = append(args, lang)
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT s.lang, s.fqn, a.anchor_id
		FROM symbols s
		JOIN intel_code_anchors a
		  ON a.lang = s.lang
		 AND a.fqn = s.fqn
		 AND a.path = s.file
		WHERE s.lang IN (%s)
		ORDER BY s.lang, s.fqn, a.anchor_id
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seeds := make([]codeanchor.CallEdgeSuffixSeed, 0)
	for rows.Next() {
		var seed codeanchor.CallEdgeSuffixSeed
		if err := rows.Scan(&seed.Lang, &seed.FQN, &seed.AnchorID); err != nil {
			return nil, err
		}
		seeds = append(seeds, seed)
	}
	return seeds, rows.Err()
}

func intelAnchorIDsByFQNsAndLangExactQuery(lang string, batch []string) (string, []any) {
	values := make([]string, 0, len(batch))
	args := make([]any, 0, len(batch)+1)
	for _, fqn := range batch {
		values = append(values, "(?)")
		args = append(args, fqn)
	}
	query := fmt.Sprintf(`
		WITH input(fqn) AS (VALUES %s)
		SELECT i.fqn, a.anchor_id
		FROM input i
		JOIN intel_code_anchors a ON a.fqn = i.fqn
	`, strings.Join(values, ","))
	if lang != "" {
		query += ` WHERE a.lang = ?`
		args = append(args, lang)
	}
	query += ` ORDER BY i.fqn, a.path, a.anchor_id`
	return query, args
}

func intelAnchorIDsByFQNsAndLangSuffixQuery(lang string, batch []string) (string, []any) {
	values := make([]string, 0, len(batch))
	args := make([]any, 0, len(batch)*2+1)
	for _, fqn := range batch {
		values = append(values, "(?, ?)")
		args = append(args, fqn, codeanchor.ReverseString(fqn))
	}
	query := fmt.Sprintf(`
		WITH input(fqn, rev) AS (VALUES %s)
		SELECT i.fqn, a.anchor_id
		FROM input i
		JOIN symbols s ON s.fqn_reversed LIKE i.rev || '.%%'
		JOIN intel_code_anchors a
		  ON a.lang = s.lang
		 AND a.fqn = s.fqn
		 AND a.path = s.file
	`, strings.Join(values, ","))
	if lang != "" {
		query += ` WHERE s.lang = ?`
		args = append(args, lang)
	}
	query += ` ORDER BY i.fqn, LENGTH(s.fqn), a.path, a.anchor_id`
	return query, args
}

// IntelAnchorIDsBySymbol returns anchor IDs whose symbol matches exactly (or contains) the provided value.
func (s *Store) IntelAnchorIDsBySymbol(ctx context.Context, symbol string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id
		FROM intel_code_anchors
		WHERE symbol = ? OR symbol LIKE ?
		ORDER BY path, fqn, anchor_id
		LIMIT ?
	`, symbol, "%"+symbol+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// IntelAnchorIDsByGoMethodNameInPkg resolves a Go symbol name to candidate anchors within a package import path.
//
// This handles two cases for same-package calls where the Go indexer cannot infer the full FQN:
//   - Methods: FQN matches "<pkgImportPath>.<ReceiverType>.<name>" (e.g., pkg.Service.Run)
//   - Functions: FQN equals "<pkgImportPath>.<name>" (e.g., pkg.DetectLanguage)
func (s *Store) IntelAnchorIDsByGoMethodNameInPkg(ctx context.Context, pkgImportPath string, methodName string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}
	pkgImportPath = strings.TrimSpace(pkgImportPath)
	methodName = strings.TrimSpace(methodName)
	if pkgImportPath == "" || methodName == "" {
		return nil, nil
	}
	methodLike := pkgImportPath + ".%." + methodName // pkg.Type.Method
	funcFQN := pkgImportPath + "." + methodName      // pkg.Function
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id
		FROM intel_code_anchors
		WHERE lang = 'go'
		  AND symbol = ?
		  AND (fqn LIKE ? OR fqn = ?)
		ORDER BY path, fqn, anchor_id
		LIMIT ?
	`, methodName, methodLike, funcFQN, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

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
			result[path] = id
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
