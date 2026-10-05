package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestIntelSymbolRefs_RoundTripAndReferrers(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "symbol-refs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
		FilePath: "keep.py",
		Lang:     codeanchor.LangPy,
		Hash:     "keep-hash",
		Symbols: []codeanchor.Symbol{{
			Lang: codeanchor.LangPy,
			Kind: codeanchor.SymFunc,
			File: "keep.py",
			Name: "keep",
			FQN:  "keep",
		}},
	}))
	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
		FilePath: "other.py",
		Lang:     codeanchor.LangPy,
		Hash:     "other-hash",
		Symbols: []codeanchor.Symbol{{
			Lang: codeanchor.LangPy,
			Kind: codeanchor.SymFunc,
			File: "other.py",
			Name: "other",
			FQN:  "other",
		}},
	}))

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"keep.py": {
			{
				SrcPath:  "keep.py",
				OwnerFQN: "keep",
				RefKind:  codeanchor.RefKindCalls,
				DstLang:  codeanchor.LangPy,
				DstPkg:   "pkg",
				DstName:  "Target",
				DstFQN:   "pkg.Target",
			},
			{
				SrcPath:  "keep.py",
				OwnerFQN: "keep",
				RefKind:  codeanchor.RefKindTypeRef,
				DstLang:  codeanchor.LangPy,
				DstPkg:   "",
				DstName:  "LooseTarget",
				DstFQN:   "LooseTarget",
			},
		},
		"other.py": {
			{
				SrcPath:  "other.py",
				OwnerFQN: "other",
				RefKind:  codeanchor.RefKindCalls,
				DstLang:  codeanchor.LangPy,
				DstPkg:   "pkg",
				DstName:  "Target",
				DstFQN:   "pkg.Target",
			},
		},
	}))

	rowsByPath, err := store.SymbolRefsByPaths(ctx, []string{"keep.py", "other.py"})
	require.NoError(t, err)
	require.Len(t, rowsByPath["keep.py"], 2)
	require.Len(t, rowsByPath["other.py"], 1)

	referrers, err := store.ReferrerPathsBySymbolRefs(ctx, []codeanchor.SymbolRef{
		{Lang: codeanchor.LangPy, Pkg: "pkg", Name: "Target"},
		{Lang: codeanchor.LangPy, Name: "LooseTarget"},
	})
	require.NoError(t, err)

	withPkg := referrers[symbolRefKey(codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "pkg", Name: "Target"})]
	sort.Strings(withPkg)
	require.Equal(t, []string{"keep.py", "other.py"}, withPkg)

	withoutPkg := referrers[symbolRefKey(codeanchor.SymbolRef{Lang: codeanchor.LangPy, Name: "LooseTarget"})]
	require.Equal(t, []string{"keep.py"}, withoutPkg)

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"keep.py": {{
			SrcPath:  "keep.py",
			OwnerFQN: "keep",
			RefKind:  codeanchor.RefKindCalls,
			DstLang:  codeanchor.LangPy,
			DstPkg:   "pkg",
			DstName:  "Retargeted",
			DstFQN:   "pkg.Retargeted",
		}},
	}))

	rowsByPath, err = store.SymbolRefsByPaths(ctx, []string{"keep.py"})
	require.NoError(t, err)
	require.Equal(t, []codeanchor.SymbolRefRow{{
		SrcPath:  "keep.py",
		OwnerFQN: "keep",
		RefKind:  codeanchor.RefKindCalls,
		DstLang:  codeanchor.LangPy,
		DstPkg:   "pkg",
		DstName:  "Retargeted",
		DstFQN:   "pkg.Retargeted",
	}}, rowsByPath["keep.py"])
}

func TestReplaceIntelSymbolRefsForPathsBatch_PrunesOnlyOrphanTargets(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "replace-target-gc.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	shared := codeanchor.SymbolRefRow{OwnerFQN: "owner", RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangPy, DstPkg: "pkg", DstName: "Shared", DstFQN: "pkg.Shared"}
	orphan := codeanchor.SymbolRefRow{OwnerFQN: "owner", RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangPy, DstPkg: "pkg", DstName: "Orphan", DstFQN: "pkg.Orphan"}
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"a.py": {shared, orphan},
		"b.py": {shared},
	}))
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{"a.py": nil}))

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_ref_targets`).Scan(&count))
	require.Equal(t, 1, count)
	var name string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT dst_name FROM intel_symbol_ref_targets`).Scan(&name))
	require.Equal(t, "Shared", name)
}

func TestAddSymbolRefTargetMemberSchemaRecoversPersistedPHPMembers(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "php-member-migration.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			`DROP INDEX idx_intel_symbol_ref_targets_name`,
			`DROP INDEX idx_intel_symbol_ref_targets_fqn`,
			`DROP INDEX idx_intel_symbol_refs_dst_target`,
			`DROP INDEX idx_intel_symbol_refs_owner`,
			`ALTER TABLE intel_symbol_refs RENAME TO intel_symbol_refs_current_test`,
			`ALTER TABLE intel_symbol_ref_targets RENAME TO intel_symbol_ref_targets_current_test`,
			`CREATE TABLE intel_symbol_ref_targets (
				target_id INTEGER PRIMARY KEY,
				dst_lang TEXT NOT NULL,
				dst_pkg TEXT NOT NULL,
				dst_name TEXT NOT NULL,
				dst_fqn TEXT NOT NULL,
				UNIQUE (dst_lang, dst_pkg, dst_name, dst_fqn)
			) STRICT`,
			`CREATE TABLE intel_symbol_refs (
				src_file_id INTEGER NOT NULL,
				owner_symbol_id INTEGER,
				owner_fqn TEXT NOT NULL,
				ref_kind TEXT NOT NULL,
				dst_target_id INTEGER NOT NULL,
				PRIMARY KEY (src_file_id, owner_fqn, ref_kind, dst_target_id),
				FOREIGN KEY(dst_target_id) REFERENCES intel_symbol_ref_targets(target_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn) VALUES
			('php', 'Polyglot\Todo\SyncClient', 'pushUpdates', 'Polyglot\Todo\SyncClient::pushUpdates'),
			('php', 'Polyglot\Todo', 'pushUpdates', 'Polyglot\Todo\pushUpdates')`); err != nil {
			return err
		}
		return addSymbolRefTargetMemberSchema(ctx, tx)
	}))
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
		VALUES ('php', 'Polyglot\Todo\SyncClient', 'pushUpdates', 'Polyglot\Todo\SyncClient::pushUpdates', 0)
	`)
	require.NoError(t, err, "typed member identity must participate in storage uniqueness")

	rows, err := store.db.QueryContext(ctx, `SELECT dst_fqn, dst_member FROM intel_symbol_ref_targets ORDER BY dst_fqn`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string][]int{}
	for rows.Next() {
		var fqn string
		var member int
		require.NoError(t, rows.Scan(&fqn, &member))
		got[fqn] = append(got[fqn], member)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int{0}, got[`Polyglot\Todo\pushUpdates`])
	require.ElementsMatch(t, []int{0, 1}, got[`Polyglot\Todo\SyncClient::pushUpdates`])
}
