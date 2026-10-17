package store

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

// SymbolRefsByPaths returns stored symbol-ref rows keyed by source path.
func (s *Store) SymbolRefsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.SymbolRefRow, error) {
	normalized := normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(normalized) == 0 {
		return map[string][]codeanchor.SymbolRefRow{}, nil
	}
	holders, args := placeholders(normalized)
	query := fmt.Sprintf(`
		SELECT sf.path, r.owner_fqn, r.ref_kind, t.dst_lang, t.dst_pkg, t.dst_name, t.dst_fqn, t.dst_member
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_files sf ON sf.file_id = r.src_file_id
		JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
		WHERE sf.path IN (%s)
		ORDER BY sf.path, r.owner_fqn, r.ref_kind, t.dst_lang, t.dst_pkg, t.dst_name
	`, holders)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.SymbolRefRow, len(normalized))
	for rows.Next() {
		var row codeanchor.SymbolRefRow
		var refKind string
		var dstLang string
		var dstMember int
		if err := rows.Scan(&row.SrcPath, &row.OwnerFQN, &refKind, &dstLang, &row.DstPkg, &row.DstName, &row.DstFQN, &dstMember); err != nil {
			return nil, err
		}
		row.RefKind = codeanchor.RefKind(refKind)
		row.DstLang = codeanchor.Lang(dstLang)
		row.DstMember = dstMember != 0
		out[row.SrcPath] = append(out[row.SrcPath], row)
	}
	return out, rows.Err()
}

// ImportRefsByPaths returns stored import-ref rows keyed by source path.
func (s *Store) ImportRefsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.ImportRefRow, error) {
	normalized := normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(normalized) == 0 {
		return map[string][]codeanchor.ImportRefRow{}, nil
	}
	holders, args := placeholders(normalized)
	query := fmt.Sprintf(`
		SELECT src_path, module
		FROM intel_import_refs
		WHERE src_path IN (%s)
		ORDER BY src_path, module
	`, holders)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.ImportRefRow, len(normalized))
	for rows.Next() {
		var row codeanchor.ImportRefRow
		if err := rows.Scan(&row.SrcPath, &row.Module); err != nil {
			return nil, err
		}
		out[row.SrcPath] = append(out[row.SrcPath], row)
	}
	return out, rows.Err()
}

// ModuleDefsByPaths returns stored module-def rows keyed by source path.
func (s *Store) ModuleDefsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.ModuleDefRow, error) {
	normalized := normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(normalized) == 0 {
		return map[string][]codeanchor.ModuleDefRow{}, nil
	}
	holders, args := placeholders(normalized)
	query := fmt.Sprintf(`
		SELECT src_path, lang, module
		FROM intel_module_defs
		WHERE src_path IN (%s)
		ORDER BY src_path, lang, module
	`, holders)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.ModuleDefRow, len(normalized))
	for rows.Next() {
		var row codeanchor.ModuleDefRow
		var lang string
		if err := rows.Scan(&row.SrcPath, &lang, &row.Module); err != nil {
			return nil, err
		}
		row.Lang = codeanchor.Lang(lang)
		out[row.SrcPath] = append(out[row.SrcPath], row)
	}
	return out, rows.Err()
}
