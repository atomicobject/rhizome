package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestPurgeIntelCodeNotInPaths_RemovesReverseIndexRows(t *testing.T) {
	store, err := Open(currentSchemaTestDBPath(t, "purge-intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "keep.py", []codeanchor.IntelAnchor{{
		AnchorID:    "keep-anchor",
		Lang:        codeanchor.LangPy,
		Kind:        "function",
		Path:        "keep.py",
		Symbol:      "keep",
		FQN:         "keep",
		Fingerprint: "keep-anchor-fp",
	}}, nil, []codeanchor.IntelFTSRow{{
		ItemType: "anchor",
		ItemID:   "keep-anchor",
		Path:     "keep.py",
		Title:    "keep",
		Body:     "keep body",
	}}))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "stale.py", []codeanchor.IntelAnchor{{
		AnchorID:    "stale-anchor",
		Lang:        codeanchor.LangPy,
		Kind:        "function",
		Path:        "stale.py",
		Symbol:      "stale",
		FQN:         "stale",
		Fingerprint: "stale-anchor-fp",
	}}, nil, []codeanchor.IntelFTSRow{{
		ItemType: "anchor",
		ItemID:   "stale-anchor",
		Path:     "stale.py",
		Title:    "stale",
		Body:     "stale body",
	}}))

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO files(path, lang, hash, mtime)
		VALUES
			('keep.py', 'py', 'keep-hash', 1),
			('stale.py', 'py', 'stale-hash', 1)
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn, fqn_reversed)
		VALUES
			('py', 'func', 'keep.py', '', 'keep', 'keep', 'peek'),
			('py', 'func', 'stale.py', '', 'stale', 'stale', 'elats')
	`)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"keep.py": {{
			SrcPath:  "keep.py",
			OwnerFQN: "keep",
			RefKind:  codeanchor.RefKindCalls,
			DstLang:  codeanchor.LangPy,
			DstPkg:   "pkg",
			DstName:  "Name",
			DstFQN:   "pkg.Name",
		}},
		"stale.py": {{
			SrcPath:  "stale.py",
			OwnerFQN: "stale",
			RefKind:  codeanchor.RefKindCalls,
			DstLang:  codeanchor.LangPy,
			DstPkg:   "pkg",
			DstName:  "Name",
			DstFQN:   "pkg.Name",
		}},
	}))

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_import_refs (src_path, module)
		VALUES
			('keep.py', 'pkg.keep'),
			('stale.py', 'pkg.stale')
	`)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_module_defs (src_path, lang, module)
		VALUES
			('keep.py', 'py', 'pkg.keep'),
			('stale.py', 'py', 'pkg.stale')
	`)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "keep.py", []codeanchor.Rationale{{
		ID:          "keep-rationale",
		Path:        "keep.py",
		Kind:        codeanchor.RationaleNote,
		Content:     "keep",
		StartLine:   1,
		EndLine:     1,
		Fingerprint: "keep-fp",
	}}))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "stale.py", []codeanchor.Rationale{{
		ID:          "stale-rationale",
		Path:        "stale.py",
		Kind:        codeanchor.RationaleNote,
		Content:     "stale",
		StartLine:   1,
		EndLine:     1,
		Fingerprint: "stale-fp",
	}}))

	require.NoError(t, store.PurgeIntelCodeNotInPaths(ctx, []string{"keep.py"}, false))

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_files sf ON sf.file_id = r.src_file_id
		WHERE sf.path = 'stale.py'
	`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_import_refs WHERE src_path = 'stale.py'`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_module_defs WHERE src_path = 'stale.py'`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_rationale WHERE path = 'stale.py'`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_code_anchors WHERE path = 'stale.py'`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts_rowid WHERE item_type = 'anchor' AND item_id = 'stale-anchor'`).Scan(&count))
	require.Equal(t, 0, count)

	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_files sf ON sf.file_id = r.src_file_id
		WHERE sf.path = 'keep.py'
	`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_import_refs WHERE src_path = 'keep.py'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_module_defs WHERE src_path = 'keep.py'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_rationale WHERE path = 'keep.py'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_code_anchors WHERE path = 'keep.py'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts_rowid WHERE item_type = 'anchor' AND item_id = 'keep-anchor'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_ref_targets`).Scan(&count))
	require.Equal(t, 1, count, "shared target remains referenced by kept file")

	results, err := store.SearchIntelFTS(ctx, "stale", 10)
	require.NoError(t, err)
	require.Empty(t, results)

	results, err = store.SearchIntelFTS(ctx, "keep", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "keep-anchor", results[0].ID)
}

func TestPurgeIntelCodeNotInPaths_FullPurgeConvergesAfterReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full-purge.db")
	ctx := context.Background()
	store, err := Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"stale.py": {{SrcPath: "stale.py", OwnerFQN: "stale", RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangPy, DstPkg: "pkg", DstName: "Gone", DstFQN: "pkg.Gone"}},
	}))
	require.NoError(t, store.ReplaceIntelImportRefsForPathsBatch(ctx, map[string][]codeanchor.ImportRefRow{"stale.py": {{SrcPath: "stale.py", Module: "pkg.gone"}}}))
	require.NoError(t, store.ReplaceIntelModuleDefsForPathsBatch(ctx, map[string][]codeanchor.ModuleDefRow{"stale.py": {{SrcPath: "stale.py", Lang: codeanchor.LangPy, Module: "pkg.stale"}}}))
	require.NoError(t, store.PurgeIntelCodeNotInPaths(ctx, nil, true))
	require.NoError(t, store.Close())

	store, err = Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	for _, table := range []string{"intel_symbol_refs", "intel_symbol_ref_files", "intel_symbol_ref_targets", "intel_import_refs", "intel_module_defs"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestPurgeIntelCodeNotInPaths_FullPurgePrunesPreexistingOrphanTargets(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "orphan-only-purge.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.db.ExecContext(ctx, `INSERT INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn) VALUES ('ts', 'pkg', 'Orphan', 'pkg.Orphan')`)
	require.NoError(t, err)

	require.NoError(t, store.PurgeIntelCodeNotInPaths(ctx, nil, true))
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_ref_targets`).Scan(&count))
	require.Zero(t, count)
}
