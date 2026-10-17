package store

import (
	"context"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"slices"
	"strings"
)

// CallFilesByCallee returns files containing calls to the callee symbol.
func (s *Store) CallFilesByCallee(ctx context.Context, ref *codeanchor.SymbolRef) ([]string, error) {
	if ref == nil {
		return nil, nil
	}
	refs := []codeanchor.SymbolRef{*ref}
	out, err := s.CallFilesByCallees(ctx, refs)
	if err != nil {
		return nil, err
	}
	return out[symbolRefKey(*ref)], nil
}

// CallFilesByCallees returns files containing calls for multiple callee symbols.
func (s *Store) CallFilesByCallees(ctx context.Context, refs []codeanchor.SymbolRef) (map[string][]string, error) {
	if len(refs) == 0 {
		return map[string][]string{}, nil
	}
	result := make(map[string][]string, len(refs))
	const refChunkSize = 200
	fqnToKeys := make(map[string][]string, len(refs))
	symbolToKeys := make(map[string][]string, len(refs))
	for _, ref := range refs {
		key := symbolRefKey(ref)
		if key == "" || strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(string(ref.Lang)) == "" {
			continue
		}
		if ref.Pkg != "" {
			fqn := callLookupFQN(ref)
			fqnToKeys[string(ref.Lang)+"\x00"+fqn] = append(fqnToKeys[string(ref.Lang)+"\x00"+fqn], key)
			continue
		}
		symbolToKeys[string(ref.Lang)+"\x00"+ref.Name] = append(symbolToKeys[string(ref.Lang)+"\x00"+ref.Name], key)
	}
	refs = dedupeSymbolRefs(refs)
	for batch := range slices.Chunk(refs, refChunkSize) {
		var clauses []string
		args := make([]any, 0, len(batch)*2)
		for _, ref := range batch {
			if strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(string(ref.Lang)) == "" {
				continue
			}
			if ref.Pkg != "" {
				clauses = append(clauses, `(callee.lang = ? AND callee.fqn = ?)`)
				args = append(args, string(ref.Lang), callLookupFQN(ref))
				continue
			}
			clauses = append(clauses, `(callee.lang = ? AND callee.symbol = ?)`)
			args = append(args, string(ref.Lang), ref.Name)
		}
		if len(clauses) == 0 {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT caller.path, callee.lang, callee.symbol, callee.fqn
			FROM intel_edges e
			JOIN intel_code_anchors caller ON caller.id = e.src_row_id
			JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
			WHERE e.kind = 'calls'
			  AND e.src_type = 'anchor'
			  AND e.dst_type = 'anchor'
			  AND (`+strings.Join(clauses, ` OR `)+`)
			GROUP BY caller.path, callee.lang, callee.symbol, callee.fqn
		`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var file, lang, symbol, fqn string
			if err := rows.Scan(&file, &lang, &symbol, &fqn); err != nil {
				_ = rows.Close()
				return nil, err
			}
			for _, key := range fqnToKeys[lang+"\x00"+fqn] {
				result[key] = append(result[key], file)
			}
			for _, key := range symbolToKeys[lang+"\x00"+symbol] {
				result[key] = append(result[key], file)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	for key, paths := range result {
		result[key] = dedupeNonEmptyStrings(paths)
	}
	return result, nil
}

func callLookupFQN(ref codeanchor.SymbolRef) string {
	if ref.Lang == codeanchor.LangPhp {
		return ref.Pkg + "::" + ref.Name
	}
	return ref.Pkg + "." + ref.Name
}

// HasAnyCallEdges returns true if any call edges exist in the store.
// Used to avoid unnecessary RebuildAllCallEdges on boot when edges already exist.
func (s *Store) HasAnyCallEdges(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE kind = 'calls' LIMIT 1`).Scan(&count)
	return count > 0, err
}
