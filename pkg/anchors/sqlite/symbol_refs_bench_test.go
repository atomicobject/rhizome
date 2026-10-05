package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func BenchmarkPruneOrphanIntelSymbolRefTargets100K(b *testing.B) {
	for _, orphanPercent := range []int{0, 10, 50} {
		b.Run(fmt.Sprintf("orphans_%d_percent", orphanPercent), func(b *testing.B) {
			ctx := context.Background()
			store, err := Open(filepath.Join(b.TempDir(), "target-gc.db"))
			require.NoError(b, err)
			b.Cleanup(func() { _ = store.Close() })

			const targets = 100_000
			referenced := targets * (100 - orphanPercent) / 100
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				require.NoError(b, store.withWriteTx(ctx, func(tx *sql.Tx) error {
					for _, stmt := range []string{
						`DELETE FROM intel_symbol_refs`,
						`DELETE FROM intel_symbol_ref_files`,
						`DELETE FROM intel_symbol_ref_targets`,
						`INSERT INTO intel_symbol_ref_files(file_id, path) VALUES (1, 'bench.ts')`,
						fmt.Sprintf(`WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n + 1 FROM seq WHERE n < %d)
							INSERT INTO intel_symbol_ref_targets(target_id, dst_lang, dst_pkg, dst_name, dst_fqn)
							SELECT n, 'ts', 'pkg', printf('Target%%06d', n), printf('pkg.Target%%06d', n) FROM seq`, targets),
						fmt.Sprintf(`WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n + 1 FROM seq WHERE n < %d)
							INSERT INTO intel_symbol_refs(src_file_id, owner_fqn, ref_kind, dst_target_id)
							SELECT 1, printf('owner%%06d', n), 'calls', n FROM seq`, referenced),
					} {
						if _, err := tx.ExecContext(ctx, stmt); err != nil {
							return err
						}
					}
					return nil
				}))
				b.StartTimer()
				require.NoError(b, store.withWriteTx(ctx, func(tx *sql.Tx) error {
					return pruneOrphanIntelSymbolRefTargetsTx(ctx, tx)
				}))
			}
		})
	}
}

func BenchmarkReferrerPathsBySymbolRefs(b *testing.B) {
	ctx := context.Background()
	store, err := Open(filepath.Join(b.TempDir(), "symbol-refs-bench.db"))
	require.NoError(b, err)
	b.Cleanup(func() { _ = store.Close() })

	const files = 400
	queries := []codeanchor.SymbolRef{
		{Lang: codeanchor.LangPy, Pkg: "pkg", Name: "TargetA"},
		{Lang: codeanchor.LangPy, Pkg: "pkg", Name: "TargetB"},
		{Lang: codeanchor.LangPy, Name: "LooseTarget"},
	}

	for i := 0; i < files; i++ {
		path := fmt.Sprintf("bench_%03d.py", i)
		fqn := fmt.Sprintf("bench_%03d", i)
		require.NoError(b, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
			FilePath: path,
			Lang:     codeanchor.LangPy,
			Hash:     fmt.Sprintf("hash-%03d", i),
			Symbols: []codeanchor.Symbol{{
				Lang: codeanchor.LangPy,
				Kind: codeanchor.SymFunc,
				File: path,
				Name: fqn,
				FQN:  fqn,
			}},
		}))
	}

	batch := make(map[string][]codeanchor.SymbolRefRow, files)
	for i := 0; i < files; i++ {
		path := fmt.Sprintf("bench_%03d.py", i)
		fqn := fmt.Sprintf("bench_%03d", i)
		rows := []codeanchor.SymbolRefRow{
			{
				SrcPath:  path,
				OwnerFQN: fqn,
				RefKind:  codeanchor.RefKindCalls,
				DstLang:  codeanchor.LangPy,
				DstPkg:   "pkg",
				DstName:  "TargetA",
				DstFQN:   "pkg.TargetA",
			},
			{
				SrcPath:  path,
				OwnerFQN: fqn,
				RefKind:  codeanchor.RefKindCalls,
				DstLang:  codeanchor.LangPy,
				DstPkg:   "pkg",
				DstName:  "TargetB",
				DstFQN:   "pkg.TargetB",
			},
		}
		if i%3 == 0 {
			rows = append(rows, codeanchor.SymbolRefRow{
				SrcPath:  path,
				OwnerFQN: fqn,
				RefKind:  codeanchor.RefKindTypeRef,
				DstLang:  codeanchor.LangPy,
				DstPkg:   "",
				DstName:  "LooseTarget",
				DstFQN:   "LooseTarget",
			})
		}
		batch[path] = rows
	}
	require.NoError(b, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, batch))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := store.ReferrerPathsBySymbolRefs(ctx, queries)
		if err != nil {
			b.Fatal(err)
		}
		if len(result) != len(queries) {
			b.Fatalf("unexpected result size: got %d want %d", len(result), len(queries))
		}
	}
}
