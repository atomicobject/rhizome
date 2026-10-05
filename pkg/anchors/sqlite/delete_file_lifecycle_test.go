package sqlite

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestDeleteFile_RemovesReverseIndexLifecycleRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "delete-file.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"deleted.py": {{SrcPath: "deleted.py", OwnerFQN: "deleted", RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangPy, DstPkg: "pkg", DstName: "Gone", DstFQN: "pkg.Gone"}},
	}))
	require.NoError(t, store.ReplaceIntelImportRefsForPathsBatch(ctx, map[string][]codeanchor.ImportRefRow{"deleted.py": {{SrcPath: "deleted.py", Module: "pkg.gone"}}}))
	require.NoError(t, store.ReplaceIntelModuleDefsForPathsBatch(ctx, map[string][]codeanchor.ModuleDefRow{"deleted.py": {{SrcPath: "deleted.py", Lang: codeanchor.LangPy, Module: "pkg.deleted"}}}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "deleted.py", []codeanchor.DocLink{{
		SrcType: "code",
		SrcPath: "deleted.py",
		SrcID:   "deleted",
		DstKind: "note",
		DstPath: "notes/target.md",
		Lang:    string(codeanchor.LangPy),
	}}))

	require.NoError(t, store.DeleteFile(ctx, "deleted.py"))
	for _, table := range []string{"intel_symbol_refs", "intel_symbol_ref_files", "intel_symbol_ref_targets", "intel_import_refs", "intel_module_defs"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Zero(t, count, table)
	}
	var docLinkCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM doc_links WHERE src_path = 'deleted.py'`).Scan(&docLinkCount))
	require.Zero(t, docLinkCount)
}
