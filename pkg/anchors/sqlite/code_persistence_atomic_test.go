package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestCodePersistenceBatchFailurePreservesCompleteState(t *testing.T) {
	for _, seeded := range []bool{false, true} {
		for _, failure := range []struct{ name, table, path string }{
			{"first_intel", "intel_code_anchors", "src/f000.py"},
			{"later_intel", "intel_code_anchors", "src/f128.py"},
			{"later_rationale", "intel_rationale", "src/f128.py"},
		} {
			t.Run(fmt.Sprintf("seeded=%t/%s", seeded, failure.name), func(t *testing.T) {
				ctx := context.Background()
				store, err := Open(currentSchemaTestDBPath(t, "index.db"))
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, store.Close()) })
				if seeded {
					require.NoError(t, store.ApplyCodePersistenceBatch(ctx, atomicCodeBatch(t, 129, "old")))
				}
				before := codePersistenceSnapshot(t, store)
				_, err = store.db.ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER fail_batch BEFORE INSERT ON %s WHEN NEW.path = '%s' BEGIN SELECT RAISE(ABORT, 'injected persistence failure'); END", failure.table, failure.path))
				require.NoError(t, err)
				err = store.ApplyCodePersistenceBatch(ctx, atomicCodeBatch(t, 129, "new"))
				require.ErrorContains(t, err, "injected persistence failure")
				require.Equal(t, before, codePersistenceSnapshot(t, store), "failed batch must retain all prior artifacts and freshness")
				_, err = store.db.ExecContext(ctx, "DROP TRIGGER fail_batch")
				require.NoError(t, err)
				require.NoError(t, store.ApplyCodePersistenceBatch(ctx, atomicCodeBatch(t, 129, "new")))
				hash, _, _, ok, err := store.FileHash(ctx, "src/f128.py")
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, "new", hash)
				require.NotEqual(t, before, codePersistenceSnapshot(t, store))
			})
		}
	}
}

func TestCodePersistenceBatchCancellationRollsBack(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, atomicCodeBatch(t, 129, "old")))
	before := codePersistenceSnapshot(t, store)
	store.db.SetMaxOpenConns(1)
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, err := store.db.Conn(ctx)
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(driverConn any) error {
		return driverConn.(*sqlite3.SQLiteConn).RegisterFunc("cancel_batch", func() int { cancel(); return 0 }, false)
	}))
	require.NoError(t, conn.Close())
	_, err = store.db.ExecContext(ctx, "CREATE TRIGGER cancel_insert BEFORE INSERT ON intel_code_anchors WHEN NEW.path = 'src/f128.py' BEGIN SELECT cancel_batch(); END")
	require.NoError(t, err)
	err = store.ApplyCodePersistenceBatch(canceled, atomicCodeBatch(t, 129, "new"))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, before, codePersistenceSnapshot(t, store))
}

func TestCodePersistenceFailedServiceBatchIsRetried(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(filepath.Join(root, "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots([]string{"src"})}, codeanchor.WithBasePath(root), codeanchor.WithWriteAccess(), codeanchor.WithoutWarmCache())
	path := filepath.Join(root, "src", "app.py")
	old, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangPy, path, []byte("def old():\n    pass\n"))
	require.NoError(t, err)
	require.NotNil(t, old)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*old}))
	content := []byte("def replacement():\n    pass\n")
	next, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangPy, path, content)
	require.NoError(t, err)
	require.NotNil(t, next)
	_, err = store.db.ExecContext(ctx, "CREATE TRIGGER fail_service BEFORE INSERT ON intel_code_anchors BEGIN SELECT RAISE(ABORT, 'injected persistence failure'); END")
	require.NoError(t, err)
	require.ErrorContains(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*next}), "injected persistence failure")
	_, err = store.db.ExecContext(ctx, "DROP TRIGGER fail_service")
	require.NoError(t, err)
	retry, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangPy, path, content)
	require.NoError(t, err)
	require.NotNil(t, retry, "failed projection must not be skipped as fresh")
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*retry}))
	names := selectStrings(t, store, ctx, "SELECT name FROM symbols WHERE file='src/app.py'")
	require.Equal(t, []string{"replacement"}, names)
	unchanged, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangPy, path, content)
	require.NoError(t, err)
	require.Nil(t, unchanged)
}

func atomicCodeBatch(t testing.TB, count int, generation string) codeanchor.CodePersistenceBatch {
	t.Helper()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{Ecosystem: codeanchor.ExternalEcosystemNPM, Module: "library", SymbolPath: generation, Kind: codeanchor.ExternalTargetSymbol})
	require.NoError(t, err)
	batch := codeanchor.CodePersistenceBatch{SymbolRefBatches: map[string][]codeanchor.SymbolRefRow{}, ImportRefBatches: map[string][]codeanchor.ImportRefRow{}, ModuleDefBatches: map[string][]codeanchor.ModuleDefRow{}, ExternalEvidenceBatches: map[string]codeanchor.ExternalEvidenceBatch{}}
	for n := range count {
		path := fmt.Sprintf("src/f%03d.py", n)
		name := fmt.Sprintf("%s%d", generation, n)
		fqn := "pkg." + name
		anchor := "anchor:" + fqn
		batch.Summaries = append(batch.Summaries, codeanchor.FileSummary{FilePath: path, Lang: codeanchor.LangPy, Hash: generation, ParseStatus: codeanchor.ParseOK, Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: path, Pkg: "pkg", Name: name, FQN: fqn}}})
		batch.Metas = append(batch.Metas, codeanchor.FileMeta{Path: fmt.Sprintf("src/failed%03d.py", n), Lang: codeanchor.LangPy, Hash: generation, ParseStatus: codeanchor.ParseErrored})
		row := codeanchor.SymbolRefRow{SrcPath: path, OwnerFQN: fqn, RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangTS, DstPkg: "library", DstName: generation, DstFQN: "library." + generation}
		batch.SymbolRefBatches[path] = []codeanchor.SymbolRefRow{row}
		batch.ImportRefBatches[path] = []codeanchor.ImportRefRow{{Module: generation}}
		batch.ModuleDefBatches[path] = []codeanchor.ModuleDefRow{{Lang: codeanchor.LangPy, Module: generation}}
		batch.ExternalEvidenceBatches[path] = codeanchor.ExternalEvidenceBatch{Symbols: []codeanchor.ExternalSymbolEvidenceInput{{OwnerFQN: fqn, RefKind: row.RefKind, Raw: codeanchor.RawSymbolTargetKey{DstLang: row.DstLang, DstPkg: row.DstPkg, DstName: row.DstName, DstFQN: row.DstFQN}, Target: target, Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh}}}}
		batch.IntelReps = append(batch.IntelReps, codeanchor.IntelCodeFileReplace{Path: path, Anchors: []codeanchor.IntelAnchor{{AnchorID: anchor, Path: path, Lang: codeanchor.LangPy, Kind: "symbol", Symbol: name, FQN: fqn, Fingerprint: generation}}, FTSRows: []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: anchor, Path: path, Title: name, Body: generation}}})
		batch.RationaleBatches = append(batch.RationaleBatches, codeanchor.RationaleBatch{Path: path, Rationales: []codeanchor.Rationale{{ID: "rationale:" + fqn, Path: path, Kind: codeanchor.RationaleTodo, Content: generation, Fingerprint: generation}}})
	}
	return batch
}

func codePersistenceSnapshot(t *testing.T, store *Store) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range []string{"files", "symbols", "intel_symbol_refs", "intel_symbol_ref_targets", "intel_import_refs", "intel_module_defs", "intel_external_targets", "intel_external_target_map", "intel_external_symbol_evidence", "intel_external_import_evidence", "intel_code_anchors", "intel_edges", "intel_fts", "intel_fts_rowid", "intel_rationale", "intel_rationale_fts", "intel_rationale_fts_rowid"} {
		rows, err := store.db.Query("SELECT * FROM " + table)
		require.NoError(t, err)
		columns, err := rows.Columns()
		require.NoError(t, err)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for n := range values {
				pointers[n] = &values[n]
			}
			require.NoError(t, rows.Scan(pointers...))
			out[table] = append(out[table], fmt.Sprintf("%#v", values))
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		sort.Strings(out[table])
	}
	return out
}
