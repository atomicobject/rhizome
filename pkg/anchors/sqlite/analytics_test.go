package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestDocCoverage_ResolvedWithAndWithoutMentions(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Matching intel anchors + one mention edge.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('caller1', 'go', 'function', 'repo/a.go', 'Caller', 'example.com/mod/pkg.Caller', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee1', 'go', 'function', 'pkg/foo.go', 'Foo', 'example.com/mod/pkg.Foo', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee2', 'go', 'function', 'pkg/bar.go', 'Bar', 'example.com/mod/other.Bar', '', '', 0, 1, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_doc_sections(section_id, path, title, level, start_byte, end_byte, content, fingerprint, updated_at)
		VALUES ('s1', 'notes/foo.md', 'Foo', 1, 0, 10, 'foo', 'sfp', 1)
	`)
	require.NoError(t, err)
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "callee1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "callee2", "calls")
	mustInsertIntelEdge(t, ctx, store, "doc_section", "s1", "anchor", "callee1", "mentions")

	rows, err := store.DocCoverage(ctx, DocCoverageOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byFQN := make(map[string]DocCoverageRow, len(rows))
	for _, row := range rows {
		byFQN[row.FQN] = row
	}
	foo, ok := byFQN["example.com/mod/pkg.Foo"]
	require.True(t, ok)
	require.True(t, foo.Resolved)
	require.Equal(t, "go", foo.Lang)
	require.Equal(t, 1, foo.Mentions)
	bar, ok := byFQN["example.com/mod/other.Bar"]
	require.True(t, ok)
	require.True(t, bar.Resolved)
	require.Zero(t, bar.Mentions)
}

func TestHotspots_PackagesAndFiles(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Calls: two files call the same pkg symbol.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('caller1', 'go', 'function', 'repo/a.go', 'CallerA', 'example.com/mod/pkg.CallerA', '', '', 0, 1, 1, 1, 'fp', 1),
			('caller2', 'go', 'function', 'repo/b.go', 'CallerB', 'example.com/mod/pkg.CallerB', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee1', 'go', 'function', 'pkg/foo.go', 'Foo', 'example.com/mod/pkg.Foo', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee2', 'go', 'function', 'pkg/bar.go', 'Bar', 'example.com/mod/pkg.Bar', '', '', 0, 1, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn)
		VALUES
			('go', 'function', 'pkg/foo.go', 'example.com/mod/pkg', 'Foo', 'example.com/mod/pkg.Foo'),
			('go', 'function', 'pkg/bar.go', 'example.com/mod/pkg', 'Bar', 'example.com/mod/pkg.Bar')
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_doc_sections(section_id, path, title, level, start_byte, end_byte, content, fingerprint, updated_at)
		VALUES ('s1', 'notes/foo.md', 'Foo', 1, 0, 10, 'foo', 'sfp', 1)
	`)
	require.NoError(t, err)
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "callee1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller2", "anchor", "callee1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller2", "anchor", "callee2", "calls")
	mustInsertIntelEdge(t, ctx, store, "doc_section", "s1", "anchor", "callee1", "mentions")

	pkgs, err := store.HotspotPackages(ctx, HotspotPackagesOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.Equal(t, "go", pkgs[0].Lang)
	require.Equal(t, "example.com/mod/pkg", pkgs[0].Pkg)
	require.Equal(t, 3, pkgs[0].Calls)
	require.Equal(t, 2, pkgs[0].Callers)
	require.Equal(t, 2, pkgs[0].UsedSymbols)
	require.Equal(t, 1, pkgs[0].DocumentedSymbols)

	files, err := store.HotspotFiles(ctx, HotspotFilesOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "repo/b.go", files[0].File)
	require.Equal(t, 2, files[0].UsedSymbols)
}

func TestDocCoverage_PathAndFQNFilter(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('caller1', 'go', 'function', 'repo/a.go', 'CallerA', 'example.com/mod/pkg.CallerA', '', '', 0, 1, 1, 1, 'fp', 1),
			('caller2', 'go', 'function', 'repo/other/b.go', 'CallerB', 'example.com/mod/pkg.CallerB', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee1', 'go', 'function', 'pkg/foo.go', 'Foo', 'example.com/mod/pkg.Foo', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee2', 'go', 'function', 'pkg/bar.go', 'Bar', 'example.com/mod/pkg.Bar', '', '', 0, 1, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "callee1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller2", "anchor", "callee2", "calls")

	rows, err := store.DocCoverage(ctx, DocCoverageOptions{
		Limit:        10,
		PathPrefixes: []string{"repo/a.go"},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "example.com/mod/pkg.Foo", rows[0].FQN)

	rows, err = store.DocCoverage(ctx, DocCoverageOptions{
		Limit: 5,
		FQNs:  []string{"example.com/mod/pkg.Bar"},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "example.com/mod/pkg.Bar", rows[0].FQN)
}

// seedSparseRefs registers a (file, target) pair in the intel_symbol_refs tables
// for tests exercising the fallback path. Returns (fileID, targetID). Uses
// get-or-insert semantics because both tables have UNIQUE constraints.
func seedSparseRefs(t *testing.T, ctx context.Context, store *Store, path, lang, pkg, name, fqn string) (int64, int64) {
	t.Helper()
	_, err := store.db.ExecContext(ctx, `INSERT OR IGNORE INTO intel_symbol_ref_files(path) VALUES (?)`, path)
	require.NoError(t, err)
	var fileID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT file_id FROM intel_symbol_ref_files WHERE path = ?`, path).Scan(&fileID))
	_, err = store.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn)
		VALUES (?, ?, ?, ?)
	`, lang, pkg, name, fqn)
	require.NoError(t, err)
	var targetID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT target_id FROM intel_symbol_ref_targets
		WHERE dst_lang = ? AND dst_pkg = ? AND dst_name = ? AND dst_fqn = ?
	`, lang, pkg, name, fqn).Scan(&targetID))
	return fileID, targetID
}

func insertSymbolRef(t *testing.T, ctx context.Context, store *Store, fileID, targetID int64, ownerFQN, refKind string) {
	t.Helper()
	_, err := store.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO intel_symbol_refs(src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id)
		VALUES (?, NULL, ?, ?, ?)
	`, fileID, ownerFQN, refKind, targetID)
	require.NoError(t, err)
}

// TestDocCoverage_FallbackBelowThreshold exercises the sparse-graph fallback path.
// Per SPEC-0073.US1: when a language's resolution rate (callsResolved / callsExtracted)
// is below SparseCallGraphThreshold (0.60), the DocCoverage query supplements its
// scoring with rows derived from intel_symbol_refs, surfacing files and symbols
// that would otherwise be filtered by the anchor-join.
func TestDocCoverage_FallbackBelowThreshold(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Sparse PHP: 5 refs to a callee that has NO symbol row (resolution rate = 0).
	// Below threshold → fallback must fire.
	for i := 0; i < 5; i++ {
		fileID, targetID := seedSparseRefs(t, ctx, store,
			fmt.Sprintf("plugin/file_%d.php", i),
			"php", "App", "register_hook", `App\register_hook`)
		insertSymbolRef(t, ctx, store, fileID, targetID, "App", "calls")
	}

	rows, err := store.DocCoverage(ctx, DocCoverageOptions{Limit: 10})
	require.NoError(t, err)

	var fallbackRows []DocCoverageRow
	for _, r := range rows {
		if r.Fallback {
			fallbackRows = append(fallbackRows, r)
		}
	}
	require.NotEmpty(t, fallbackRows, "expected fallback rows for sparse PHP: %+v", rows)
	require.Equal(t, `App\register_hook`, fallbackRows[0].FQN, "fallback row should surface the unresolved callee")
	require.Equal(t, "php", fallbackRows[0].Lang)
	require.Equal(t, 5, fallbackRows[0].Calls, "five refs from five distinct caller files")
	require.Equal(t, 5, fallbackRows[0].Callers, "five distinct caller files")
}

func TestDocCoverage_FallbackKeepsStrongerSparseDuplicate(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn)
		VALUES ('php', 'function', 'app/hooks.php', 'App', 'register_hook', 'App\register_hook')
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('caller1', 'php', 'function', 'plugin/resolved.php', 'Caller', 'App\Caller', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee1', 'php', 'function', 'app/hooks.php', 'register_hook', 'App\register_hook', '', '', 0, 1, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "callee1", "calls")

	// One resolved target plus four unresolved targets keeps PHP below the sparse
	// threshold. The duplicated resolved callee has five raw refs, so the fallback
	// count is the stronger signal and should be the row users see.
	for i := 0; i < 5; i++ {
		fileID, targetID := seedSparseRefs(t, ctx, store,
			fmt.Sprintf("plugin/raw_%d.php", i),
			"php", "App", "register_hook", `App\register_hook`)
		insertSymbolRef(t, ctx, store, fileID, targetID, "App", "calls")
	}
	for i := 0; i < 4; i++ {
		fileID, targetID := seedSparseRefs(t, ctx, store,
			fmt.Sprintf("plugin/noise_%d.php", i),
			"php", "App", fmt.Sprintf("missing_%d", i), fmt.Sprintf(`App\missing_%d`, i))
		insertSymbolRef(t, ctx, store, fileID, targetID, "App", "calls")
	}

	rows, err := store.DocCoverage(ctx, DocCoverageOptions{Limit: 10, FQNs: []string{`App\register_hook`}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, `App\register_hook`, rows[0].FQN)
	require.True(t, rows[0].Fallback, "sparse duplicate should keep the stronger fallback row")
	require.True(t, rows[0].Resolved, "fallback row should still join the anchor when one exists")
	require.Equal(t, 5, rows[0].Calls)
	require.Equal(t, 5, rows[0].Callers)
}

// TestHotspotPackages_FallbackBelowThreshold exercises the fallback for HotspotPackages.
func TestHotspotPackages_FallbackBelowThreshold(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Sparse PHP refs across 3 caller files, all pointing at the App\ package.
	for i := 0; i < 3; i++ {
		fileA, targetA := seedSparseRefs(t, ctx, store,
			fmt.Sprintf("caller_%d.php", i),
			"php", "App", "hook_a", `App\hook_a`)
		insertSymbolRef(t, ctx, store, fileA, targetA, "App", "calls")
		fileB, targetB := seedSparseRefs(t, ctx, store,
			fmt.Sprintf("caller_%d.php.b", i),
			"php", "App", "hook_b", `App\hook_b`)
		insertSymbolRef(t, ctx, store, fileB, targetB, "App", "calls")
	}

	pkgs, err := store.HotspotPackages(ctx, HotspotPackagesOptions{Limit: 10})
	require.NoError(t, err)

	var phpRows []HotspotPackageRow
	for _, p := range pkgs {
		if p.Lang == "php" {
			phpRows = append(phpRows, p)
		}
	}
	require.NotEmpty(t, phpRows, "expected PHP fallback rows in HotspotPackages: %+v", pkgs)
	require.True(t, phpRows[0].Fallback, "PHP rows on sparse graph should carry Fallback=true")
	require.Equal(t, "App", phpRows[0].Pkg)
	require.Equal(t, 2, phpRows[0].UsedSymbols, "two distinct callees (hook_a, hook_b)")
}

// TestHotspotFiles_FallbackBelowThreshold exercises the fallback for HotspotFiles.
func TestHotspotFiles_FallbackBelowThreshold(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// One PHP caller file calls into 3 distinct packages: fan-out=3.
	for i, pkg := range []string{"Auth", "Cart", "Billing"} {
		fileID, targetID := seedSparseRefs(t, ctx, store,
			"hub.php",
			"php", pkg, fmt.Sprintf("handler_%d", i), fmt.Sprintf(`App\%s\handler_%d`, pkg, i))
		insertSymbolRef(t, ctx, store, fileID, targetID, "App", "calls")
	}

	files, err := store.HotspotFiles(ctx, HotspotFilesOptions{Limit: 10})
	require.NoError(t, err)

	var phpFiles []HotspotFileRow
	for _, f := range files {
		if f.Fallback {
			phpFiles = append(phpFiles, f)
		}
	}
	require.NotEmpty(t, phpFiles, "expected PHP fallback files: %+v", files)
	require.Equal(t, "hub.php", phpFiles[0].File)
	require.Equal(t, 3, phpFiles[0].Deps, "hub.php calls into 3 distinct packages")
}

func TestHotspotFiles_FallbackKeepsPartiallyResolvedFile(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn)
		VALUES ('php', 'function', 'app/resolved.php', 'Resolved', 'resolved', 'App\Resolved')
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('caller1', 'php', 'function', 'hub.php', 'Hub', 'App\Hub', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee1', 'php', 'function', 'app/resolved.php', 'Resolved', 'App\Resolved', '', '', 0, 1, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "callee1", "calls")

	for i, pkg := range []string{"Auth", "Cart", "Billing"} {
		fileID, targetID := seedSparseRefs(t, ctx, store,
			"hub.php",
			"php", pkg, fmt.Sprintf("handler_%d", i), fmt.Sprintf(`App\%s\handler_%d`, pkg, i))
		insertSymbolRef(t, ctx, store, fileID, targetID, "App", "calls")
	}
	for i := 0; i < 3; i++ {
		fileID, targetID := seedSparseRefs(t, ctx, store,
			fmt.Sprintf("noise_%d.php", i),
			"php", "Noise", fmt.Sprintf("missing_%d", i), fmt.Sprintf(`App\Noise\missing_%d`, i))
		insertSymbolRef(t, ctx, store, fileID, targetID, "App", "calls")
	}

	files, err := store.HotspotFiles(ctx, HotspotFilesOptions{Limit: 10, Langs: []string{"php"}})
	require.NoError(t, err)
	require.NotEmpty(t, files)
	require.Equal(t, "hub.php", files[0].File)
	require.True(t, files[0].Fallback, "sparse file should keep the stronger fallback row")
	require.Equal(t, 3, files[0].Deps)
	require.Equal(t, 3, files[0].Calls)
	require.Equal(t, 3, files[0].UsedSymbols)
}

// TestDocCoverage_FallbackAboveThresholdNoChange asserts that on a high-resolution-rate
// language, the fallback path does NOT activate and existing rankings are preserved.
func TestDocCoverage_FallbackAboveThresholdNoChange(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Seed both intel_edges-style resolved data AND a small amount of intel_symbol_refs.
	// Crucially: ALL the symbol_refs targets have matching `symbols` rows → resolution
	// rate = 1.0 → above threshold → fallback must NOT fire.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn)
		VALUES
			('go', 'function', 'pkg/foo.go', 'example.com/mod/pkg', 'Foo', 'example.com/mod/pkg.Foo')
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('caller1', 'go', 'function', 'repo/a.go', 'Caller', 'example.com/mod/pkg.Caller', '', '', 0, 1, 1, 1, 'fp', 1),
			('callee1', 'go', 'function', 'pkg/foo.go', 'Foo', 'example.com/mod/pkg.Foo', '', '', 0, 1, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "callee1", "calls")

	// One resolved symbol_ref (target FQN matches symbols row).
	fileID, targetID := seedSparseRefs(t, ctx, store,
		"pkg/another.go",
		"go", "example.com/mod/pkg", "Foo", "example.com/mod/pkg.Foo")
	insertSymbolRef(t, ctx, store, fileID, targetID, "another", "calls")

	rows, err := store.DocCoverage(ctx, DocCoverageOptions{Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, r := range rows {
		require.False(t, r.Fallback, "resolution rate is 1.0 → no fallback rows expected: %+v", r)
	}
}

func TestComplexityHotspots_RanksBySizeAndFanInOut(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO symbols(lang, kind, file, pkg, name, fqn)
		VALUES
			('go', 'function', 'pkg/foo.go', 'example.com/mod/pkg', 'Foo', 'example.com/mod/pkg.Foo'),
			('go', 'function', 'pkg/bar.go', 'example.com/mod/pkg', 'Bar', 'example.com/mod/pkg.Bar')
	`)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('a1', 'go', 'function', 'pkg/foo.go', 'Foo', 'example.com/mod/pkg.Foo', '', '', 0, 1, 1, 30, 'fp1', 1),
			('a2', 'go', 'function', 'pkg/bar.go', 'Bar', 'example.com/mod/pkg.Bar', '', '', 0, 1, 1, 5, 'fp2', 1),
			('caller1', 'go', 'module', '/repo/a.go', 'a.go', '', '', '', 0, 1, 1, 1, 'fp3', 1),
			('caller2', 'go', 'module', '/repo/b.go', 'b.go', '', '', '', 0, 1, 1, 1, 'fp4', 1),
			('caller3', 'go', 'module', '/repo/c.go', 'c.go', '', '', '', 0, 1, 1, 1, 'fp5', 1),
			('dep1', 'go', 'function', '/repo/dep/zap.go', 'Zap', 'example.com/mod/dep.Zap', '', '', 0, 1, 1, 1, 'fp6', 1),
			('dep2', 'go', 'function', '/repo/dep/zed.go', 'Zed', 'example.com/mod/dep.Zed', '', '', 0, 1, 1, 1, 'fp7', 1)
	`)
	require.NoError(t, err)

	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "a1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller2", "anchor", "a1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller3", "anchor", "a2", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "a1", "anchor", "dep1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "a1", "anchor", "dep2", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "a2", "anchor", "dep1", "calls")

	rows, err := store.ComplexityHotspots(ctx, ComplexityOptions{Limit: 10, PathPrefixes: []string{"pkg/"}})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.Equal(t, "example.com/mod/pkg.Foo", rows[0].FQN)
	require.Equal(t, 30, rows[0].SpanLines)
	require.Equal(t, 2, rows[0].Callers)
	require.Equal(t, 2, rows[0].Callees)

	require.Equal(t, "example.com/mod/pkg.Bar", rows[1].FQN)
	require.Equal(t, 5, rows[1].SpanLines)
	require.Equal(t, 1, rows[1].Callers)
	require.Equal(t, 1, rows[1].Callees)
}

func TestRationaleAttention_RanksDeterministicFindings(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('caller1', 'go', 'function', 'repo/a.go', 'CallerA', 'example.com/mod/pkg.CallerA', '', '', 0, 1, 1, 1, 'fp', 1),
			('caller2', 'go', 'function', 'repo/b.go', 'CallerB', 'example.com/mod/pkg.CallerB', '', '', 0, 1, 1, 1, 'fp', 1),
			('target1', 'go', 'function', 'pkg/attention.go', 'Target', 'example.com/mod/pkg.Target', '', '', 0, 1, 20, 30, 'fp', 1)
	`)
	require.NoError(t, err)
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "target1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller2", "anchor", "target1", "calls")
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "pkg/attention.go", []codeanchor.Rationale{{
		ID:          "attention001",
		Path:        "pkg/attention.go",
		SymbolFQN:   "example.com/mod/pkg.Target",
		Kind:        codeanchor.RationaleImportant,
		Content:     "IMPORTANT: " + strings.Repeat("😀", 100),
		StartLine:   21,
		EndLine:     21,
		Fingerprint: "fp-attention",
	}}))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "pkg/other.go", []codeanchor.Rationale{{
		ID:          "other001",
		Path:        "pkg/other.go",
		Kind:        codeanchor.RationaleWhy,
		Content:     "WHY: ordinary local note",
		StartLine:   1,
		EndLine:     1,
		Fingerprint: "fp-other",
	}}))

	rows, err := store.RationaleAttention(ctx, RationaleAttentionOptions{Limit: 10, PathPrefixes: []string{"pkg/attention.go"}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "pkg/attention.go", rows[0].Path)
	require.Equal(t, "example.com/mod/pkg.Target", rows[0].SymbolFQN)
	require.Equal(t, 2, rows[0].Callers)
	require.Contains(t, rows[0].Reasons, "important_marker")
	require.Contains(t, rows[0].Reasons, "high_fanin_symbol")
	require.Contains(t, rows[0].Reasons, "undocumented_symbol")
	require.Greater(t, rows[0].Attention, 0)
	require.True(t, utf8.ValidString(rows[0].Content))
	require.LessOrEqual(t, len(rows[0].Content), 243)
	require.True(t, strings.HasSuffix(rows[0].Content, "..."))
}

func TestRationaleAttention_DeduplicatesSuffixMatchedAnchors(t *testing.T) {
	ctx := context.Background()
	dbPath := currentSchemaTestDBPath(t, "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES
			('caller1', 'go', 'function', 'repo/a.go', 'CallerA', 'example.com.mod.pkg.CallerA', '', '', 0, 1, 1, 1, 'fp', 1),
			('caller2', 'go', 'function', 'repo/b.go', 'CallerB', 'example.com.mod.pkg.CallerB', '', '', 0, 1, 1, 1, 'fp', 1),
			('target1', 'go', 'function', 'pkg/attention.go', 'Target', 'example.com.mod.pkg.Target', '', '', 0, 1, 20, 30, 'fp', 1),
			('target2', 'go', 'function', 'other/attention.go', 'Target', 'example.com.mod.other.pkg.Target', '', '', 0, 1, 20, 30, 'fp', 1)
	`)
	require.NoError(t, err)
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "target1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller2", "anchor", "target1", "calls")
	mustInsertIntelEdge(t, ctx, store, "anchor", "caller1", "anchor", "target2", "calls")
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "pkg/attention.go", []codeanchor.Rationale{{
		ID:          "attention001",
		Path:        "pkg/attention.go",
		SymbolFQN:   "pkg.Target",
		Kind:        codeanchor.RationaleImportant,
		Content:     "IMPORTANT: short-form rationale should not duplicate when suffixes collide",
		StartLine:   21,
		EndLine:     21,
		Fingerprint: "fp-attention",
	}}))

	rows, err := store.RationaleAttention(ctx, RationaleAttentionOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "pkg.Target", rows[0].SymbolFQN)
	require.Equal(t, 2, rows[0].Callers)
}
