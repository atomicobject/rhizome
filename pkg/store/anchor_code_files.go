package store

import (
	"context"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/bmatcuk/doublestar/v4"
	"path/filepath"
	"sort"
	"strings"
)

// CodeFilesForNote returns the distinct code files linked to the given note
// via its code anchors. It resolves all four anchor flavors:
//
//   - symbol-based (anchor_scopes.symbol_fqn → symbols.file)
//   - call-based (anchor_scopes.call_file — direct path)
//   - path-prefix (anchors.path_prefix → files.path starting with the prefix)
//   - glob (anchor_globs.pattern → files.path matched via doublestar)
//
// Paths are returned in stable sorted order.
func (s *Store) CodeFilesForNote(ctx context.Context, notePath string) ([]string, error) {
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		p = filepath.ToSlash(p)
		seen[p] = struct{}{}
	}

	// Symbol-based scopes: resolve symbol_fqn to the file that owns the symbol.
	symRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT sym.file
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchor_scopes sc ON sc.anchor_id = na.anchor_id
		JOIN symbols sym ON sym.fqn = sc.symbol_fqn
		WHERE n.path = ?
		  AND sc.symbol_fqn IS NOT NULL
		  AND sc.symbol_fqn != ''
	`, notePath)
	if err != nil {
		return nil, err
	}
	for symRows.Next() {
		var file string
		if err := symRows.Scan(&file); err != nil {
			symRows.Close()
			return nil, err
		}
		add(file)
	}
	if err := symRows.Close(); err != nil {
		return nil, err
	}

	// Call-based scopes: the file is already stored directly.
	callRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT sc.call_file
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchor_scopes sc ON sc.anchor_id = na.anchor_id
		WHERE n.path = ?
		  AND sc.call_file IS NOT NULL
		  AND sc.call_file != ''
	`, notePath)
	if err != nil {
		return nil, err
	}
	for callRows.Next() {
		var file string
		if err := callRows.Scan(&file); err != nil {
			callRows.Close()
			return nil, err
		}
		add(file)
	}
	if err := callRows.Close(); err != nil {
		return nil, err
	}

	// Path-prefix anchors: match any indexed file under the prefix.
	prefixRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT a.path_prefix
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchors a ON a.id = na.anchor_id
		WHERE n.path = ?
		  AND a.kind = ?
		  AND a.path_prefix IS NOT NULL
		  AND a.path_prefix != ''
	`, notePath, codeanchor.AnchorPath)
	if err != nil {
		return nil, err
	}
	var prefixes []string
	for prefixRows.Next() {
		var pfx string
		if err := prefixRows.Scan(&pfx); err != nil {
			prefixRows.Close()
			return nil, err
		}
		prefixes = append(prefixes, pfx)
	}
	if err := prefixRows.Close(); err != nil {
		return nil, err
	}
	for _, pfx := range prefixes {
		pfx = filepath.ToSlash(strings.TrimSpace(pfx))
		if pfx == "" {
			continue
		}
		fileRows, err := s.db.QueryContext(ctx, `
			SELECT path FROM files
			WHERE path = ? OR path LIKE ? || '/%'
		`, pfx, pfx)
		if err != nil {
			return nil, err
		}
		for fileRows.Next() {
			var p string
			if err := fileRows.Scan(&p); err != nil {
				fileRows.Close()
				return nil, err
			}
			add(p)
		}
		if err := fileRows.Close(); err != nil {
			return nil, err
		}
	}

	// Glob anchors: load patterns, then walk all files once and doublestar-match.
	globRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ag.pattern
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchors a ON a.id = na.anchor_id
		JOIN anchor_globs ag ON ag.anchor_id = a.id
		WHERE n.path = ?
		  AND a.kind = ?
	`, notePath, codeanchor.AnchorGlob)
	if err != nil {
		return nil, err
	}
	var patterns []string
	for globRows.Next() {
		var pat string
		if err := globRows.Scan(&pat); err != nil {
			globRows.Close()
			return nil, err
		}
		patterns = append(patterns, pat)
	}
	if err := globRows.Close(); err != nil {
		return nil, err
	}
	if len(patterns) > 0 {
		allFiles, err := s.db.QueryContext(ctx, `SELECT path FROM files`)
		if err != nil {
			return nil, err
		}
		for allFiles.Next() {
			var p string
			if err := allFiles.Scan(&p); err != nil {
				allFiles.Close()
				return nil, err
			}
			norm := filepath.ToSlash(p)
			for _, pat := range patterns {
				if ok, _ := doublestar.Match(pat, norm); ok {
					add(p)
					break
				}
			}
		}
		if err := allFiles.Close(); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
